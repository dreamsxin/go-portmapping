package http

import (
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"

	"github.com/dreamsxin/go-portmapping/internal/config"
	"github.com/dreamsxin/go-portmapping/internal/stats"
)

var (
	servers = make(map[string]*http.Server)
	mu      sync.Mutex
)

// StartHTTPForwarder 启动HTTP转发器
func StartHTTPForwarder(rule config.Rule, key string) {
	mu.Lock()
	defer mu.Unlock()

	// 如果服务器已存在，则不需要重新启动
	if _, exists := servers[key]; exists {
		return
	}

	if rule.TargetScheme == "" {
		rule.TargetScheme = "http"
	}

	if rule.TargetPort == 0 {
		rule.TargetPort = 80
	}

	targetURL := &url.URL{
		Scheme: rule.TargetScheme,
		Host:   net.JoinHostPort(rule.TargetHost, strconv.Itoa(rule.TargetPort)),
	}

	// 创建反向代理
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// 自定义请求处理
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = targetURL.Scheme
		req.URL.Host = targetURL.Host
		req.Host = targetURL.Host
		req.Header.Set("X-Forwarded-Proto", req.URL.Scheme)
		req.Header.Set("X-Forwarded-For", req.RemoteAddr)
	}

	// 创建HTTP服务器
	server := &http.Server{
		Addr:    net.JoinHostPort("0.0.0.0", strconv.Itoa(rule.ListenPort)),
		Handler: statsMiddleware(proxy, key),
	}

	if rule.HTTPSEnabled {
		// 加载TLS证书
		cert, err := tls.LoadX509KeyPair(rule.TLSCertFile, rule.TLSKeyFile)
		if err != nil {
			log.Printf("加载TLS证书失败: %v", err)
			return
		}
		server.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
	}

	servers[key] = server
	log.Printf("HTTP转发已启动: %s -> %s", server.Addr, targetURL)

	// 启动服务器
	go func() {
		if rule.HTTPSEnabled {
			if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Printf("HTTPS服务器错误: %v", err)
			}
		} else {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("HTTP服务器错误: %v", err)
			}
		}
	}()
}

// StopHTTPForwarders 停止HTTP转发器
func StopHTTPForwarders(activeKeys map[string]bool) {
	mu.Lock()
	defer mu.Unlock()

	for key, server := range servers {
		if !activeKeys[key] {
			if err := server.Close(); err != nil {
				log.Printf("关闭HTTP服务器失败: %v", err)
			} else {
				log.Printf("HTTP转发已停止: %s", key)
			}
			delete(servers, key)
		}
	}
}

// statsMiddleware 用于统计HTTP流量的中间件
func statsMiddleware(next http.Handler, key string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stats.IncrementConnections(key)
		defer stats.DecrementConnections(key)

		// 记录接收字节数
		reqSize := r.ContentLength
		if reqSize > 0 {
			stats.RecordBytesReceived(key, uint64(reqSize))
		}

		// 包装ResponseWriter以记录发送字节数
		wrapped := &responseRecorder{w, 0}
		next.ServeHTTP(wrapped, r)

		// 记录发送字节数
		if wrapped.bytesWritten > 0 {
			stats.RecordBytesSent(key, uint64(wrapped.bytesWritten))
		}
	})
}

// responseRecorder 用于记录HTTP响应大小的包装器
type responseRecorder struct {
	http.ResponseWriter
	bytesWritten int
}

func (rec *responseRecorder) Write(b []byte) (int, error) {
	n, err := rec.ResponseWriter.Write(b)
	rec.bytesWritten += n
	return n, err
}
