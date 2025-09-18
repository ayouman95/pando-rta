package main

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"io"
	"net/http"
	"os"
	"pando-rta/config"
	"strings"
)

const (
	TargetAPINetwork = "https://growth-rta.tiktokv-us.com/api/v1/rta/network"
	TargetAPIReport  = "https://growth-rta.tiktokv-us.com/api/v1/rta/report"
	LogFile          = "./logs/api.log" // 所有日志写入这个文件，lumberjack 负责滚动
	MaxSize          = 1000             // 每个日志文件最大 100MB
	MaxBackups       = 4000             // 最多保留 10 个备份文件
)

// 初始化日志（每天一个文件，使用 lumberjack 滚动）
var logger *zap.Logger

func initLogger() {
	// 创建日志目录
	if err := os.MkdirAll("./logs", 0755); err != nil {
		panic(err)
	}

	// 使用 lumberjack 按大小滚动
	w := &lumberjack.Logger{
		Filename:   LogFile,    // 基础日志文件
		MaxSize:    MaxSize,    // MB
		MaxBackups: MaxBackups, // 保留 10 个旧文件
		MaxAge:     28,         // 旧文件最多保留 28 天（防无限堆积）
		Compress:   true,       // 压缩旧文件为 .gz
	}

	// JSON 编码器
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "time"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(w),
		zapcore.InfoLevel,
	)

	logger = zap.New(core)
}

func main() {
	initLogger()
	defer logger.Sync() // 确保日志刷写

	r := gin.New()

	// 健康检查接口 —— 极简，无依赖，无日志
	r.GET("/hc", func(c *gin.Context) {
		// 方式1：最简版本（推荐）
		c.String(http.StatusOK, "OK")
		c.Abort() // 不再执行后续中间件
	})

	// 添加日志中间件
	r.Use(func(c *gin.Context) {
		if c.Request.URL.Path == "/hc" {
			c.Next()
			return
		}

		pubID := c.Query("pub_id")
		if pubID == "" {
			pubID = "unknown"
		}

		body, _ := io.ReadAll(c.Request.Body)
		c.Request.Body = io.NopCloser(bytes.NewBuffer(body)) // 重置 body

		// 记录请求
		logger.Info("request received",
			zap.String("client_ip", c.ClientIP()),
			zap.String("method", c.Request.Method),
			zap.String("url", c.Request.URL.String()),
			zap.String("pub_id", pubID),
			zap.ByteString("body", body),
		)

		c.Next()
	})
	// 接口 B：接收请求，转发到接口 A
	r.POST("/api/v1/rta/network", proxyHandler)
	r.POST("/api/v1/rta/report", proxyHandler)

	// 启动服务
	r.Run(":8080") // 可以修改端口
}

func proxyHandler(c *gin.Context) {
	// 1. 解析请求，获取 pub_id（作为查询参数或 body？根据你的需求）
	pubID := c.Query("pub_id") // 假设 pub_id 是 query 参数
	if pubID == "" || pubID == "unknown" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing pub_id"})
		return
	}
	if !config.GetConfig().IsValidPubID(pubID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pub_id"})
		return
	}
	// 2. 读取原始请求 Body（包含接口 A 所需的所有参数）
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}
	_ = c.Request.Body.Close()

	// 3. 构造转发到接口 A 的请求
	var targetURL string
	if c.Request.URL.Path == "/api/v1/rta/network" {
		targetURL = TargetAPINetwork
	} else if c.Request.URL.Path == "/api/v1/rta/report" {
		targetURL = TargetAPIReport
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported endpoint"})
		return
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), "POST", targetURL, bytes.NewBuffer(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "rta request failed"})
		return
	}

	// 4. 复制原始请求的 Headers（除了 Host）
	for key, values := range c.Request.Header {
		if strings.ToLower(key) != "host" {
			req.Header.Del(key) // 先清空
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}

	// 5. 使用 http.Client 发起请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "rta request failed"})
		return
	}
	defer resp.Body.Close()

	// 6. 读取响应体以便记录日志
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read response body"})
		return
	}

	// 7. 记录响应日志
	logger.Info("response sent",
		zap.String("pub_id", pubID),
		zap.String("target_url", targetURL),
		zap.Int("status_code", resp.StatusCode),
		zap.ByteString("response_body", respBody),
	)

	// 8. 复制响应 Header
	for key, values := range resp.Header {
		for _, value := range values {
			c.Header(key, value)
		}
	}

	// 9. 设置相同的 Status Code
	c.Status(resp.StatusCode)

	// 10. 将接口 A 的响应体原样返回
	c.Writer.Write(respBody)
}
