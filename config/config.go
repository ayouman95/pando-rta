package config

import (
	"encoding/json"
	"log"
	"os"
	"sync/atomic"
	"time"
	"unsafe"
)

type Config struct {
	ValidPubIDs []string        `json:"valid_pub_ids"`
	validSet    map[string]bool // 用于快速查找的 set
}

var (
	config     unsafe.Pointer
	configPath = "config.json"
)

func init() {
	// 初始化配置
	loadConfig()
	// 启动定时刷新
	go refreshConfig()
}

// GetConfig 获取当前配置
func GetConfig() *Config {
	return (*Config)(atomic.LoadPointer(&config))
}

func (c *Config) IsValidPubID(pubID string) bool {
	return c.validSet[pubID]
}

// loadConfig 从文件加载配置
func loadConfig() {
	file, err := os.Open(configPath)
	if err != nil {
		log.Printf("Failed to open config file: %v, using default config", err)
		// 使用默认配置
		defaultConfig := &Config{
			ValidPubIDs: []string{"NovaBeyond", "ByteMedia", "FlyFunAds", "PinkTomato"},
		}
		// 构建 set
		defaultConfig.validSet = make(map[string]bool)
		for _, id := range defaultConfig.ValidPubIDs {
			defaultConfig.validSet[id] = true
		}
		atomic.StorePointer(&config, unsafe.Pointer(defaultConfig))
		return
	}
	defer file.Close()

	var newConfig Config
	if err := json.NewDecoder(file).Decode(&newConfig); err != nil {
		log.Printf("Failed to decode config file: %v, using default config", err)
		// 使用默认配置
		defaultConfig := &Config{
			ValidPubIDs: []string{"NovaBeyond", "ByteMedia", "FlyFunAds", "PinkTomato"},
		}
		// 构建 set
		defaultConfig.validSet = make(map[string]bool)
		for _, id := range defaultConfig.ValidPubIDs {
			defaultConfig.validSet[id] = true
		}
		atomic.StorePointer(&config, unsafe.Pointer(defaultConfig))
		return
	}

	// 构建 set
	newConfig.validSet = make(map[string]bool)
	for _, id := range newConfig.ValidPubIDs {
		newConfig.validSet[id] = true
	}

	atomic.StorePointer(&config, unsafe.Pointer(&newConfig))
	log.Println("Config loaded successfully")
}

// refreshConfig 每分钟刷新一次配置
func refreshConfig() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("Refreshing config...")
		loadConfig()
	}
}
