package udp

import (
	"context"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/dreamsxin/go-portmapping/internal/config"
	"github.com/dreamsxin/go-portmapping/internal/stats"
)

var (
	udpConns    = make(map[string]*net.UDPConn)
	udpMu       sync.Mutex
	clientConns = make(map[string]*clientConnState)
	clientMu    sync.Mutex
)

// 客户端连接状态
type clientConnState struct {
	lastActive time.Time
	ctx        context.Context
	cancel     context.CancelFunc
}

// StartUDPForwarder 启动UDP转发器
func StartUDPForwarder(rule config.Rule, key string) {
	udpMu.Lock()
	defer udpMu.Unlock()

	// 如果UDP连接已存在，则不需要重新启动
	if _, exists := udpConns[key]; exists {
		return
	}

	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort("0.0.0.0", strconv.Itoa(rule.ListenPort)))
	if err != nil {
		log.Printf("UDP地址解析失败: %v", err)
		return
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("UDP监听失败 %s: %v", addr, err)
		return
	}

	udpConns[key] = conn
	log.Printf("UDP转发已启动: %s -> %s:%d", addr, rule.TargetHost, rule.TargetPort)

	// 增加连接计数
	stats.IncrementConnections(key)

	// 开始处理UDP数据包
	go func() {
		defer func() {
			udpMu.Lock()
			delete(udpConns, key)
			udpMu.Unlock()
			conn.Close()
			stats.DecrementConnections(key)
			log.Printf("UDP转发已停止: %s", key)
		}()

		buffer := make([]byte, 65536)
		for {
			n, clientAddr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				// 检查连接是否已关闭
				udpMu.Lock()
				_, exists := udpConns[key]
				udpMu.Unlock()
				if !exists {
					return
				}
				log.Printf("UDP读取错误: %v", err)
				continue
			}

			// 处理UDP数据包
			go func() {
				clientKey := clientAddr.String()
				clientMu.Lock()
				// 检查是否已有活跃连接
				if state, exists := clientConns[clientKey]; exists {
					// 更新最后活动时间
					state.lastActive = time.Now()
					clientMu.Unlock()
					return
				}
				// 创建新的上下文
				ctx, cancel := context.WithCancel(context.Background())
				clientConns[clientKey] = &clientConnState{
					lastActive: time.Now(),
					ctx:        ctx,
					cancel:     cancel,
				}
				clientMu.Unlock()

				// 启动超时清理goroutine
				go func() {
					ticker := time.NewTicker(30 * time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							clientMu.Lock()
							state, exists := clientConns[clientKey]
							if exists && time.Since(state.lastActive) > 60*time.Second {
								state.cancel()
								delete(clientConns, clientKey)
							}
							clientMu.Unlock()
						case <-ctx.Done():
							return
						}
					}
				}()

				handleUDPConnection(conn, clientAddr, buffer[:n], rule, key, ctx)
			}()
		}
	}()
}

// handleUDPConnection 处理UDP连接和数据转发
func handleUDPConnection(serverConn *net.UDPConn, clientAddr *net.UDPAddr, data []byte, rule config.Rule, key string, ctx context.Context) {
	// 更新接收流量统计
	stats.UpdateTrafficStats(key, uint64(len(data)), false)

	// 解析目标服务器地址
	targetAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(rule.TargetHost, strconv.Itoa(rule.TargetPort)))
	if err != nil {
		log.Printf("解析UDP目标地址失败: %v", err)
		return
	}

	// 转发数据到目标服务器
	n, err := serverConn.WriteToUDP(data, targetAddr)
	if err != nil {
		log.Printf("UDP数据转发失败: %v", err)
		return
	}

	// 更新发送流量统计
	stats.UpdateTrafficStats(key, uint64(n), true)

	// 从目标服务器接收响应并返回给客户端
	buffer := make([]byte, 65536)
	for {
		select {
		case <-ctx.Done():
			// 上下文取消，退出goroutine
			return
		default:
			// 设置读取超时
			serverConn.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, _, err := serverConn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					return
				}
				log.Printf("UDP读取响应失败: %v", err)
				return
			}

			// 将响应返回给客户端
			serverConn.WriteToUDP(buffer[:n], clientAddr)

			// 更新流量统计
			stats.UpdateTrafficStats(key, uint64(n), false)
			stats.UpdateTrafficStats(key, uint64(n), true)

			// 更新最后活动时间
			clientMu.Lock()
			if state, exists := clientConns[clientAddr.String()]; exists {
				state.lastActive = time.Now()
			}
			clientMu.Unlock()
		}
	}
}

// StopUDPForwarders 停止所有UDP转发器
func StopUDPForwarders(activeKeys map[string]bool) {
	udpMu.Lock()
	defer udpMu.Unlock()

	for key, conn := range udpConns {
		if !activeKeys[key] {
			conn.Close()
			delete(udpConns, key)
			stats.DecrementConnections(key)
			log.Printf("UDP转发已停止: %s", key)
		}
	}

	// 清理所有客户端连接状态
	clientMu.Lock()
	for _, state := range clientConns {
		state.cancel()
	}
	clientConns = make(map[string]*clientConnState)
	clientMu.Unlock()
}
