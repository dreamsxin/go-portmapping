package protocols

import (
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/dreamsxin/go-portmapping/internal/config"
	"github.com/dreamsxin/go-portmapping/internal/stats"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有源，生产环境需根据需求修改
	},
}

// 存储活跃的HTTP服务器以便关闭
var (
	httpServers = make(map[string]*http.Server)
	httpMu      sync.Mutex
)

// StartWebSocketForwarder 使用net/http实现WebSocket监听
func StartWebSocketForwarder(rule config.Rule, key string) {
	httpMu.Lock()
	defer httpMu.Unlock()

	// 如果HTTP服务器已存在，则不需要重新启动
	if _, exists := httpServers[key]; exists {
		return
	}

	addr := fmt.Sprintf(":%d", rule.ListenPort)

	// 创建HTTP处理器
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("WebSocket请求: ", r.RemoteAddr, r.URL.Path)
		// 升级HTTP连接为WebSocket
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket升级失败: %v", err)
			return
		}
		// 处理WebSocket连接
		go handleWebSocketConnection(wsConn, r.URL.Path, rule, key)
	})

	// 创建HTTP服务器
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	// 存储服务器引用
	httpServers[key] = server

	log.Printf("WebSocket转发已启动: %s -> %s:%d", addr, rule.TargetHost, rule.TargetPort)

	// 在goroutine中启动服务器
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP服务器错误: %v", err)
		}
	}()
}

// handleWebSocketConnection 处理WebSocket连接
func handleWebSocketConnection(wsConn *websocket.Conn, path string, rule config.Rule, key string) {
	defer wsConn.Close()

	// 更新连接统计
	stats.IncrementConnections(key)
	defer stats.DecrementConnections(key)

	// 连接目标WebSocket服务器
	targetAddr := fmt.Sprintf("ws://%s:%d%s", rule.TargetHost, rule.TargetPort, path)
	targetWsConn, _, err := websocket.DefaultDialer.Dial(targetAddr, nil)
	if err != nil {
		log.Printf("连接目标WebSocket服务器失败 %s: %v", targetAddr, err)
		return
	}
	defer targetWsConn.Close()

	log.Printf("WebSocket连接已建立: %s <-> %s", wsConn.RemoteAddr(), targetAddr)

	// 双向转发WebSocket消息
	go forwardWebSocketMessages(wsConn, targetWsConn, key, true)
	forwardWebSocketMessages(targetWsConn, wsConn, key, false)
}

// forwardWebSocketMessages 转发WebSocket消息并统计流量
func forwardWebSocketMessages(src, dst *websocket.Conn, key string, isSent bool) {
	for {
		msgType, message, err := src.ReadMessage()
		if err != nil {
			log.Printf("读取WebSocket消息失败: %v", err)
			return
		}

		// 发送消息到目标
		if err := dst.WriteMessage(msgType, message); err != nil {
			log.Printf("发送WebSocket消息失败: %v", err)
			return
		}

		// 更新流量统计
		if isSent {
			stats.RecordBytesSent(key, uint64(len(message)))
		} else {
			stats.RecordBytesReceived(key, uint64(len(message)))
		}
	}
}

// StopWebSocketForwarders 停止不再需要的WebSocket转发器
func StopWebSocketForwarders(activeKeys map[string]bool) {
	httpMu.Lock()
	defer httpMu.Unlock()

	for key, server := range httpServers {
		if !activeKeys[key] {
			// 关闭服务器
			if err := server.Close(); err != nil {
				log.Printf("关闭WebSocket服务器失败: %v", err)
			} else {
				log.Printf("WebSocket转发已停止: %s", key)
			}
			// 从映射中删除
			delete(httpServers, key)
		}
	}
}
