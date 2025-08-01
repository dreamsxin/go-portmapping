package config

import (
	"encoding/json"
	"log"
	"os"
	"strconv"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Rule 定义端口转发规则
type Rule struct {
	Protocol   string `json:"protocol"`   //协议类型: tcp/udp/websocket
	ListenPort int    `json:"listenPort"` //监听端口
	TargetHost string `json:"targetHost"` //目标主机
	TargetPort int    `json:"targetPort"` //目标端口
	Enabled    bool   `json:"enabled"`    //是否启用该规则
}

var (
	rules []Rule
	mu    sync.RWMutex
)

// LoadConfig 加载配置文件并返回错误
func LoadConfig(filePath string) error {
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	var newRules []Rule
	if err := json.Unmarshal(fileContent, &newRules); err != nil {
		return err
	}

	mu.Lock()
	rules = newRules
	mu.Unlock()

	log.Printf("已加载 %d 条转发规则", len(newRules))
	return nil
}

// WatchConfig 监控配置文件变化并触发回调
func WatchConfig(filePath string, onChange func()) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// 配置文件被写入或删除后重新加载
				if (event.Op&fsnotify.Write == fsnotify.Write ||
					event.Op&fsnotify.Remove == fsnotify.Remove) &&
					event.Name == filePath {
					log.Println("检测到配置文件变化...")
					if err := LoadConfig(filePath); err != nil {
						log.Printf("重新加载配置失败: %v", err)
						continue
					}
					if onChange != nil {
						onChange()
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("配置文件监控错误: %v", err)
			}
		}
	}()

	return watcher.Add(filePath)
}

// GetRules 获取当前规则列表（线程安全）
func GetRules() []Rule {
	mu.RLock()
	defer mu.RUnlock()
	// 返回规则副本避免外部修改
	result := make([]Rule, len(rules))
	copy(result, rules)
	return result
}

// GetRuleKey生成规则唯一标识
func GetRuleKey(rule Rule) string {
	return rule.Protocol + ":" + strconv.Itoa(rule.ListenPort)
}
