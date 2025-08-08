package websocket

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

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
	clientConns = make(map[string]map[*websocket.Conn]context.CancelFunc) // 跟踪客户端连接
	httpMu      sync.RWMutex
	connLimit   = make(chan struct{}, 1000) // 限制最大并发连接数为1000
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
		// 检查连接限制
		select {
		case connLimit <- struct{}{}:
			// 获得连接许可
		default:
			http.Error(w, "连接数已达上限", http.StatusTooManyRequests)
			return
		}
		defer func() { <-connLimit }() // 释放连接许可

		log.Printf("WebSocket请求: %s %s", r.RemoteAddr, r.URL.Path)
		// 升级HTTP连接为WebSocket
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket升级失败: %v", err)
			return
		}

		// 创建连接上下文，设置超时
		ctx, cancel := context.WithCancel(context.Background())

		// 跟踪客户端连接
		httpMu.Lock()
		if _, ok := clientConns[key]; !ok {
			clientConns[key] = make(map[*websocket.Conn]context.CancelFunc)
		}
		clientConns[key][wsConn] = cancel
		httpMu.Unlock()
		// 处理WebSocket连接
		go handleWebSocketConnection(ctx, wsConn, r, rule, key, cancel)
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
func handleWebSocketConnection(ctx context.Context, wsConn *websocket.Conn, r *http.Request, rule config.Rule, key string, cancel context.CancelFunc) {
	defer func() {
		wsConn.Close()
		// 从连接跟踪中移除
		httpMu.Lock()
		if conns, ok := clientConns[key]; ok {
			delete(conns, wsConn)
			if len(conns) == 0 {
				delete(clientConns, key)
			}
		}
		httpMu.Unlock()
		cancel()
	}()

	// 更新连接统计
	stats.IncrementConnections(key)
	defer stats.DecrementConnections(key)

	// 解析动态端口参数
	path := r.URL.Path
	targetPort := rule.TargetPort
	if rule.DynamicPortParam != "" {
		// 从URL查询参数获取动态端口
		values := r.URL.Query()
		// 从请求URL查询参数中提取动态端口
		if paramValue := values.Get(rule.DynamicPortParam); paramValue != "" {
			if port, err := strconv.Atoi(paramValue); err == nil && port > 0 && port <= 65535 {
				targetPort = port
				log.Printf("使用动态端口: %d (来自参数 %s)", targetPort, rule.DynamicPortParam)
			} else {
				log.Printf("无效的动态端口值: %v", paramValue)
				return
			}
		} else {
			// 从URL路径中提取动态端口
			segments := strings.Split(strings.Trim(path, "/"), "/")
			for i, seg := range segments {
				if seg == rule.DynamicPortParam && i+1 < len(segments) {
					portStr := segments[i+1]
					if port, err := strconv.Atoi(portStr); err == nil && port > 0 && port <= 65535 {
						targetPort = port
						log.Printf("使用动态端口: %d (来自路径参数 %s)", targetPort, rule.DynamicPortParam)
						// 从路径中移除动态端口参数及其值
						segments = append(segments[:i], segments[i+2:]...)
						// 重建路径
						path = "/" + strings.Join(segments, "/")
						if path == "//" {
							path = "/"
						}
					} else {
						log.Printf("无效的动态端口值: %v", portStr)
						return
					}
					break
				}
			}
		}
	}

	// 连接目标WebSocket服务器
	targetAddr := fmt.Sprintf("ws://%s:%d%s", rule.TargetHost, targetPort, path)
	targetWsConn, _, err := websocket.DefaultDialer.Dial(targetAddr, nil)
	if err != nil {
		log.Printf("连接目标WebSocket服务器失败 %s: %v", targetAddr, err)
		// 向客户端发送错误消息
		if err := wsConn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("连接目标服务器失败: %v", err))); err != nil {
			log.Printf("发送错误消息失败: %v", err)
		}
		return
	}
	defer targetWsConn.Close()

	log.Printf("WebSocket连接已建立: %s <-> %s", wsConn.RemoteAddr(), targetAddr)

	// 双向转发WebSocket消息
	errChan := make(chan error, 2)
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := forwardWebSocketMessages(ctx, wsConn, targetWsConn, key, true); err != nil {
			errChan <- fmt.Errorf("客户端到目标转发错误: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := forwardWebSocketMessages(ctx, targetWsConn, wsConn, key, false); err != nil {
			errChan <- fmt.Errorf("目标到客户端转发错误: %v", err)
		}
	}()

	// 等待任一方向出错或上下文取消
	select {
	case err := <-errChan:
		log.Printf("转发错误: %v", err)
	case <-ctx.Done():
		log.Printf("连接已取消: %v", ctx.Err())
	}

	// 等待所有转发goroutine结束
	wg.Wait()
	log.Printf("WebSocket连接已关闭: %s", wsConn.RemoteAddr())
}

// forwardWebSocketMessages 转发WebSocket消息并统计流量
func forwardWebSocketMessages(ctx context.Context, src, dst *websocket.Conn, key string, isSent bool) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// 重置超时
			if err := src.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
				return fmt.Errorf("设置读取超时失败: %v", err)
			}

			msgType, message, err := src.ReadMessage()
			if err != nil {
				return fmt.Errorf("读取消息失败: %v", err)
			}

			// 重置写入超时
			if err := dst.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return fmt.Errorf("设置写入超时失败: %v", err)
			}

			// 发送消息到目标
			if err := dst.WriteMessage(msgType, message); err != nil {
				return fmt.Errorf("写入消息失败: %v", err)
			}

			// 更新流量统计
			stats.UpdateTrafficStats(key, uint64(len(message)), isSent)
		}
	}
}

// StopWebSocketForwarders 停止不再需要的WebSocket转发器
func StopWebSocketForwarders(activeKeys map[string]bool) {
	httpMu.Lock()
	defer httpMu.Unlock()

	for key, server := range httpServers {
		if !activeKeys[key] {
			// 关闭所有客户端连接
			if conns, ok := clientConns[key]; ok {
				for _, cancel := range conns {
					cancel()
				}
				delete(clientConns, key)
			}

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
