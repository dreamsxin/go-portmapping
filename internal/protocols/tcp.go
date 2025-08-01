package protocols

import (
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/dreamsxin/go-portmapping/internal/config"
	"github.com/dreamsxin/go-portmapping/internal/stats"
)

var (
	listeners = make(map[string]net.Listener)
	mu        sync.Mutex
)

// StartTCPForwarder 启动TCP转发（导出函数）
func StartTCPForwarder(rule config.Rule, key string) {
	mu.Lock()
	defer mu.Unlock()

	// 如果监听器已存在，则不需要重新启动
	if _, exists := listeners[key]; exists {
		return
	}

	addr := net.JoinHostPort("0.0.0.0", strconv.Itoa(rule.ListenPort))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("TCP监听失败 %s: %v", addr, err)
		return
	}

	listeners[key] = listener
	log.Printf("TCP转发已启动: %s -> %s:%d", addr, rule.TargetHost, rule.TargetPort)

	// 开始接受连接
	go func() {
		for {
			clientConn, err := listener.Accept()
			if err != nil {
				// 检查监听器是否已关闭
				mu.Lock()
				_, exists := listeners[key]
				mu.Unlock()
				if !exists {
					log.Printf("TCP转发已停止: %s", key)
					return
				}
				log.Printf("接受TCP连接失败: %v", err)
				continue
			}

			// 处理新连接
			go HandleTCPConnection(clientConn, rule, key)
		}
	}()
}

// HandleTCPConnection 处理TCP连接（导出函数）
func HandleTCPConnection(clientConn net.Conn, rule config.Rule, key string) {
	defer clientConn.Close()

	// 更新连接统计
	stats.IncrementConnections(key)
	defer stats.DecrementConnections(key)

	// 连接目标服务器
	targetAddr := net.JoinHostPort(rule.TargetHost, strconv.Itoa(rule.TargetPort))
	targetConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		log.Printf("连接目标服务器失败 %s: %v", targetAddr, err)
		return
	}
	defer targetConn.Close()

	log.Printf("TCP连接已建立: %s <-> %s", clientConn.RemoteAddr(), targetAddr)

	// 双向转发数据
	go stats.CopyStreamWithStats(clientConn, targetConn, key, true)
	go stats.CopyStreamWithStats(targetConn, clientConn, key, false)

	// 等待连接关闭
	select {
	case <-time.After(30 * time.Minute):
		log.Printf("TCP连接超时关闭: %s <-> %s", clientConn.RemoteAddr(), targetAddr)
	}
}

// StopTCPForwarders 停止所有TCP转发器
func StopTCPForwarders(activeKeys map[string]bool) {
	mu.Lock()
	defer mu.Unlock()

	// 停止已移除或禁用的规则
	for key, listener := range listeners {
		if !activeKeys[key] {
			listener.Close()
			delete(listeners, key)
			log.Printf("TCP转发已停止: %s", key)
		}
	}
}
