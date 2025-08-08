package stats

import (
	"errors"
	"log"
	"net"
	"sync"
	"time"
)

// TrafficStats 流量统计信息
type TrafficStats struct {
	BytesSent        uint64
	BytesReceived    uint64
	Connections      int
	MaxConnections   int
	LastUpdated      time.Time
	CreatedAt        time.Time
	TotalConnections int
}

var (
	statsMap = make(map[string]*TrafficStats)
	statsMu  sync.RWMutex
	// 统计数据清理间隔
	cleanupInterval = 5 * time.Minute
	// 非活动超时时间
	inactiveTimeout = 10 * time.Minute
)

func init() {
	// 启动统计数据清理 goroutine
	go cleanupInactiveStats()
}

// CopyStreamWithStats 复制数据流并统计流量，返回错误信息
func CopyStreamWithStats(dst, src net.Conn, key string, isSent bool) (int64, error) {
	if dst == nil || src == nil {
		return 0, errors.New("源或目标连接为nil")
	}

	buffer := make([]byte, 4096)
	totalBytes := int64(0)

	for {
		n, err := src.Read(buffer)
		if n > 0 {
			w, writeErr := dst.Write(buffer[:n])
			if writeErr != nil {
				return totalBytes, writeErr
			}
			if w != n {
				return totalBytes, errors.New("写入字节数不匹配")
			}

			// 更新流量统计
			UpdateTrafficStats(key, uint64(n), isSent)
			totalBytes += int64(n)
		}

		if err != nil {
			// 正常关闭错误不需要返回
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return totalBytes, nil
			}
			if err.Error() == "EOF" || errors.Is(err, net.ErrClosed) {
				return totalBytes, nil
			}
			return totalBytes, err
		}
	}
}

// UpdateTrafficStats 更新流量统计
func UpdateTrafficStats(key string, bytes uint64, isSent bool) {
	statsMu.Lock()
	defer statsMu.Unlock()

	stats := getOrCreateStats(key)
	if isSent {
		stats.BytesSent += bytes
	} else {
		stats.BytesReceived += bytes
	}
	stats.LastUpdated = time.Now()
}

// getOrCreateStats 获取或创建流量统计实例
func getOrCreateStats(key string) *TrafficStats {
	stats, exists := statsMap[key]
	if !exists {
		now := time.Now()
		stats = &TrafficStats{
			LastUpdated: now,
			CreatedAt:   now,
		}
		statsMap[key] = stats
	}
	return stats
}

// IncrementConnections 增加连接计数
func IncrementConnections(key string) {
	statsMu.Lock()
	defer statsMu.Unlock()

	stats := getOrCreateStats(key)
	stats.Connections++
	stats.TotalConnections++
	// 更新最大连接数
	if stats.Connections > stats.MaxConnections {
		stats.MaxConnections = stats.Connections
	}
}

// DecrementConnections 减少连接计数
func DecrementConnections(key string) {
	statsMu.Lock()
	defer statsMu.Unlock()

	stats, exists := statsMap[key]
	if !exists {
		return
	}

	stats.Connections--
	stats.LastUpdated = time.Now()
}

// PrintTrafficStats 定期打印流量统计
func PrintTrafficStats(interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		PrintCurrentStats()
	}
}

// PrintCurrentStats 打印当前统计信息
func PrintCurrentStats() {
	statsMu.RLock()
	defer statsMu.RUnlock()

	if len(statsMap) == 0 {
		return
	}

	log.Println("\n===== 流量统计信息 ====")
	for key, stats := range statsMap {
		log.Printf("%s:\n", key)
		log.Printf("  发送: %d bytes, 接收: %d bytes\n", stats.BytesSent, stats.BytesReceived)
		log.Printf("  当前连接: %d, 峰值连接: %d, 总连接数: %d\n", stats.Connections, stats.MaxConnections, stats.TotalConnections)
		log.Printf("  已运行: %v, 最后更新: %v\n", time.Since(stats.CreatedAt).Round(time.Second), stats.LastUpdated.Format("15:04:05"))
	}
	log.Println("======================")
}

// cleanupInactiveStats 清理非活动的统计数据
func cleanupInactiveStats() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		statsMu.Lock()
		now := time.Now()
		for key, stats := range statsMap {
			if now.Sub(stats.LastUpdated) > inactiveTimeout && stats.Connections == 0 {
				delete(statsMap, key)
			}
		}
		statsMu.Unlock()
	}
}

// GetStats 获取指定key的统计信息
func GetStats(key string) (*TrafficStats, bool) {
	statsMu.RLock()
	defer statsMu.RUnlock()

	stats, exists := statsMap[key]
	return stats, exists
}

// GetAllStats 获取所有统计信息
func GetAllStats() map[string]*TrafficStats {
	statsMu.RLock()
	defer statsMu.RUnlock()

	// 返回副本避免外部修改
	result := make(map[string]*TrafficStats, len(statsMap))
	for k, v := range statsMap {
		// 创建浅拷贝
		statsCopy := *v
		result[k] = &statsCopy
	}
	return result
}
