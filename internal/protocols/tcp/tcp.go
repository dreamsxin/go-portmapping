package tcp

import (
	"context"
	"errors"
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
	// 新增：获取连接限制，默认为1000
	maxConns := rule.MaxConnections
	if maxConns <= 0 {
		maxConns = 1000
	}
	connSemaphore := make(chan struct{}, maxConns)

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

			// 限制最大连接数
			select {
			case connSemaphore <- struct{}{}:
				// 处理新连接
				go func() {
					defer func() {
						<-connSemaphore
					}()
					HandleTCPConnection(clientConn, rule, key)
				}()
			default:
				clientConn.Close()
				log.Printf("TCP连接数已达上限: %d", maxConns)
			}
		}
	}()
}

// HandleTCPConnection 处理TCP连接（导出函数）
func HandleTCPConnection(clientConn net.Conn, rule config.Rule, key string) {
	// 设置连接超时
	clientConn.SetDeadline(time.Time{})
	defer clientConn.Close()

	// 更新连接统计
	stats.IncrementConnections(key)
	defer stats.DecrementConnections(key)

	// 创建上下文用于取消操作
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 连接目标服务器
	targetAddr := net.JoinHostPort(rule.TargetHost, strconv.Itoa(rule.TargetPort))
	// 设置连接超时
	conn, err := net.DialTimeout("tcp", targetAddr, 5*time.Second)
	if err != nil {
		log.Printf("连接目标服务器失败 %s: %v", targetAddr, err)
		// 向客户端发送错误信息
		clientConn.Write([]byte("Connection failed: " + err.Error() + "\n"))
		return
	}
	defer conn.Close()
	// 设置读写超时
	conn.SetDeadline(time.Time{})

	log.Printf("TCP连接已建立: %s <-> %s", clientConn.RemoteAddr(), targetAddr)

	// 双向转发数据
	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	wg.Add(2)

	// 客户端到目标服务器
	go func() {
		defer wg.Done()
		_, err := stats.CopyStreamWithStats(clientConn, conn, key, true)
		if err != nil && !errors.Is(err, net.ErrClosed) {
			errChan <- err
		}
	}()

	// 目标服务器到客户端
	go func() {
		defer wg.Done()
		_, err := stats.CopyStreamWithStats(conn, clientConn, key, false)
		if err != nil && !errors.Is(err, net.ErrClosed) {
			errChan <- err
		}
	}()

	// 等待任一方向出错或完成
	select {
	case err := <-errChan:
		if err != nil {
			log.Printf("连接传输错误: %v", err)
		}
		// 取消上下文，关闭所有连接
		cancel()
		clientConn.Close()
		conn.Close()
	case <-ctx.Done():
		// 上下文已取消
		return
	}

	// 等待所有goroutine完成
	wg.Wait()
	log.Printf("TCP连接已关闭: %s <-> %s", clientConn.RemoteAddr(), targetAddr)
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
