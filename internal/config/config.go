package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Rule 定义端口转发规则
// 包含协议类型、监听端口、目标地址等必要信息
type Rule struct {
	Protocol         string `json:"protocol"`                   // 协议类型: tcp/udp/websocket
	ListenPort       int    `json:"listenPort"`                 // 监听端口(1-65535)
	TargetScheme     string `json:"targetScheme,omitempty"`     // SOCKS5可选，目标协议: http/https
	TargetHost       string `json:"targetHost,omitempty"`       // SOCKS5可选，目标主机
	TargetPort       int    `json:"targetPort,omitempty"`       // SOCKS5可选，目标端口(1-65535)
	MaxConnections   int    `json:"maxConnections,omitempty"`   // 最大连接数，默认1000
	Enabled          bool   `json:"enabled"`                    // 是否启用该规则
	DynamicPortParam string `json:"dynamicPortParam,omitempty"` // 动态端口参数名(仅websocket协议有效)
	TLSCertFile      string `json:"tlsCertFile,omitempty"`      // HTTPS证书路径
	TLSKeyFile       string `json:"tlsKeyFile,omitempty"`       // HTTPS密钥路径
	HTTPSEnabled     bool   `json:"httpsEnabled,omitempty"`     // 是否启用HTTPS
	SOCKS5Auth       string `json:"socks5Auth,omitempty"`       // "none"或"password"
	SOCKS5Username   string `json:"socks5Username,omitempty"`
	SOCKS5Password   string `json:"socks5Password,omitempty"`
}

var (
	rules      []Rule
	mu         sync.RWMutex
	configPath string // 配置文件路径
)

// Validate 验证规则是否有效
func (r *Rule) Validate() error {
	// 验证协议类型
	// 验证协议类型
	if r.Protocol != "tcp" && r.Protocol != "udp" && r.Protocol != "websocket" && r.Protocol != "http" && r.Protocol != "socks5" {
		return errors.New("不支持的协议类型，必须是tcp/udp/websocket/http/socks5")
	}

	// HTTPS协议需要验证证书文件
	if r.HTTPSEnabled && r.Protocol == "http" {
		if r.TLSCertFile == "" || r.TLSKeyFile == "" {
			return errors.New("HTTPS协议需要配置tlsCertFile和tlsKeyFile")
		}
		// 验证证书文件是否存在
		if _, err := os.Stat(r.TLSCertFile); os.IsNotExist(err) {
			return fmt.Errorf("证书文件不存在: %s", r.TLSCertFile)
		}
		if _, err := os.Stat(r.TLSKeyFile); os.IsNotExist(err) {
			return fmt.Errorf("密钥文件不存在: %s", r.TLSKeyFile)
		}
	} else if r.Protocol == "socks5" { // SOCKS5协议验证
		// 验证认证方式
		if r.SOCKS5Auth == "" {
			r.SOCKS5Auth = "none" // 默认无认证
		} else if r.SOCKS5Auth != "none" && r.SOCKS5Auth != "password" {
			return errors.New("SOCKS5认证方式必须是none或password")
		}
		// 密码认证需要检查用户名密码
		if r.SOCKS5Auth == "password" && (r.SOCKS5Username == "" || r.SOCKS5Password == "") {
			return errors.New("SOCKS5密码认证需要配置socks5Username和socks5Password")
		}
	}

	// 验证监听端口
	if r.ListenPort < 1 || r.ListenPort > 65535 {
		return errors.New("监听端口必须在1-65535范围内")
	}

	// 验证目标主机
	if r.Protocol != "socks5" {
		if r.TargetHost == "" {
			return errors.New("目标主机不能为空")
		}

		// 验证目标端口
		if r.TargetPort < 1 || r.TargetPort > 65535 {
			return errors.New("目标端口必须在1-65535范围内")
		}
	}

	return nil
}

// LoadConfig 加载配置文件并返回错误
// 支持通过环境变量 CONFIG_PATH 指定配置文件路径，默认使用当前目录下的rules.json
func LoadConfig() error {
	// 获取配置文件路径
	path := os.Getenv("CONFIG_PATH")
	if path == "" {
		path = "rules.json"
	}
	configPath = path

	fileContent, err := os.ReadFile(path)
	if err != nil {
		return errors.Join(errors.New("读取配置文件失败"), err)
	}

	var newRules []Rule
	if err := json.Unmarshal(fileContent, &newRules); err != nil {
		return errors.Join(errors.New("解析配置文件失败"), err)
	}

	// 验证所有规则
	validRules := []Rule{}
	for i, rule := range newRules {
		if err := rule.Validate(); err != nil {
			log.Printf("规则 %d 无效: %v (将被跳过)", i+1, err)
			continue
		}
		validRules = append(validRules, rule)
	}

	mu.Lock()
	rules = validRules
	mu.Unlock()

	log.Printf("已加载 %d 条有效转发规则 (共 %d 条，%d 条无效)", len(validRules), len(newRules), len(newRules)-len(validRules))
	return nil
}

// WatchConfig 监控配置文件变化并触发回调
// 实现了500ms防抖机制，避免文件修改时频繁触发加载
func WatchConfig(onChange func()) error {
	if configPath == "" {
		return errors.New("请先调用LoadConfig加载配置")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return errors.Join(errors.New("创建文件监控器失败"), err)
	}

	// 添加文件监控
	if err := watcher.Add(configPath); err != nil {
		watcher.Close()
		return errors.Join(errors.New("添加文件监控失败"), err)
	}

	// 防抖定时器
	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				// 只处理配置文件的写入和删除事件
				if (event.Op&fsnotify.Write == fsnotify.Write ||
					event.Op&fsnotify.Remove == fsnotify.Remove) &&
					event.Name == configPath {

					// 重置防抖定时器
					debounceTimer.Reset(500 * time.Millisecond)
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("配置文件监控错误: %v", err)

			case <-debounceTimer.C:
				log.Println("检测到配置文件变化，重新加载...")
				if err := LoadConfig(); err != nil {
					log.Printf("重新加载配置失败: %v", err)
					continue
				}
				if onChange != nil {
					onChange()
				}
			}
		}
	}()

	return nil
}

// GetRules 获取当前规则列表（线程安全）
// 返回规则副本以避免外部修改内部状态
func GetRules() []Rule {
	mu.RLock()
	defer mu.RUnlock()

	// 返回规则副本
	result := make([]Rule, len(rules))
	copy(result, rules)
	return result
}

// GetRuleKey 生成规则唯一标识
// 格式为 "protocol:listenPort"，确保不同规则有唯一标识
func GetRuleKey(rule Rule) string {
	return fmt.Sprintf("%s:%d", rule.Protocol, rule.ListenPort)
}
