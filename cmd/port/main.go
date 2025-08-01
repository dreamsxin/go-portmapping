package main

import (
	"flag"
	"log"
	"os"
	"sync"

	"github.com/dreamsxin/go-portmapping/internal/config"
	"github.com/dreamsxin/go-portmapping/internal/protocols"
	"github.com/dreamsxin/go-portmapping/internal/stats"
)

var (
	configFile = flag.String("c", "configs/rules.json", "配置文件路径")
	mu         sync.Mutex
)

func main() {
	flag.Parse()
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// 加载初始配置
	if err := config.LoadConfig(*configFile); err != nil {
		log.Fatalf("加载配置文件失败: %v", err)
	}

	// 启动配置文件监控（热加载）
	go config.WatchConfig(*configFile, func() {
		log.Println("配置文件变化，重新加载规则...")
		startAllForwarders()
	})

	// 启动流量统计打印
	go stats.PrintTrafficStats()

	// 启动所有转发规则
	startAllForwarders()

	// 保持主进程运行
	select {}
}

// startAllForwarders 启动所有转发规则
func startAllForwarders() {
	mu.Lock()
	defer mu.Unlock()

	// 获取当前规则
	rules := config.GetRules()

	// 生成当前活跃规则的键集合
	activeKeys := make(map[string]bool)
	for _, rule := range rules {
		if rule.Enabled {
			key := config.GetRuleKey(rule)
			activeKeys[key] = true
		}
	}

	// 停止各协议已禁用的转发器
	protocols.StopTCPForwarders(activeKeys)
	protocols.StopUDPForwarders(activeKeys)
	protocols.StopWebSocketForwarders(activeKeys)

	// 启动或重启启用的规则
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		key := config.GetRuleKey(rule)
		switch rule.Protocol {
		case "tcp":
			protocols.StartTCPForwarder(rule, key)
		case "udp":
			protocols.StartUDPForwarder(rule, key)
		case "websocket":
			protocols.StartWebSocketForwarder(rule, key)
		default:
			log.Printf("不支持的协议类型: %s", rule.Protocol)
		}
	}
}
