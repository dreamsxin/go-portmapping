package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/dreamsxin/go-portmapping/internal/config"
	"github.com/dreamsxin/go-portmapping/internal/protocols/http"
	"github.com/dreamsxin/go-portmapping/internal/protocols/socks5"
	"github.com/dreamsxin/go-portmapping/internal/protocols/tcp"
	"github.com/dreamsxin/go-portmapping/internal/protocols/udp"
	"github.com/dreamsxin/go-portmapping/internal/protocols/websocket"
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
	// 新增：设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("收到退出信号，正在关闭服务...")
		// 实现优雅关闭逻辑
		os.Exit(0)
	}()

	// 确定配置文件路径，环境变量优先于命令行参数
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = *configFile
	}
	os.Setenv("CONFIG_PATH", configPath)

	// 加载初始配置（新接口无需传入文件路径参数）
	if err := config.LoadConfig(); err != nil {
		log.Fatalf("加载配置文件失败: %v", err)
	}

	// 启动配置文件监控（热加载）
	go func() {
		if err := config.WatchConfig(func() {
			log.Println("配置文件变化，重新加载规则...")
			startAllForwarders()
		}); err != nil {
			log.Printf("配置文件监控失败: %v", err)
		}
	}()

	// 启动流量统计打印
	go stats.PrintTrafficStats(30 * time.Second)

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
	tcp.StopTCPForwarders(activeKeys)
	udp.StopUDPForwarders(activeKeys)
	websocket.StopWebSocketForwarders(activeKeys)
	http.StopHTTPForwarders(activeKeys)
	socks5.StopSOCKS5Forwarders(activeKeys)

	// 启动或重启启用的规则
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		key := config.GetRuleKey(rule)
		switch rule.Protocol {
		case "tcp":
			tcp.StartTCPForwarder(rule, key)
		case "udp":
			udp.StartUDPForwarder(rule, key)
		case "websocket":
			websocket.StartWebSocketForwarder(rule, key)
		case "http":
			http.StartHTTPForwarder(rule, key)
		case "socks5":
			socks5.StartSOCKS5Forwarder(rule, key)
		default:
			log.Printf("不支持的协议类型: %s", rule.Protocol)
		}
	}
}
