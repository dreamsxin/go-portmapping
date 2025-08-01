package stats

import (
	"log"
	"net"
	"sync"
	"time"
)

// TrafficStats 流量统计信息
type TrafficStats struct {
	BytesSent     uint64
	BytesReceived uint64
	Connections   int
	LastUpdated   time.Time
}

var (
	statsMap = make(map[string]*TrafficStats)
	statsMu  sync.Mutex
)

// copyStreamWithStats 复制数据流并统计流量
func CopyStreamWithStats(dst, src net.Conn, key string, isSent bool) {
	buffer := make([]byte, 4096)
	for {
		n, err := src.Read(buffer)
		if n > 0 {
			dst.Write(buffer[:n])

			// 更新流量统计
			statsMu.Lock()
			stats := getOrCreateStats(key)
			if isSent {
				stats.BytesSent += uint64(n)
			} else {
				stats.BytesReceived += uint64(n)
			}
			stats.LastUpdated = time.Now()
			statsMu.Unlock()
		}

		if err != nil {
			break
		}
	}
}

// getOrCreateStats 获取或创建流量统计实例
func getOrCreateStats(key string) *TrafficStats {
	stats, exists := statsMap[key]
	if !exists {
		stats = &TrafficStats{LastUpdated: time.Now()}
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
}

// DecrementConnections 减少连接计数
func DecrementConnections(key string) {
	statsMu.Lock()
	defer statsMu.Unlock()
	stats := getOrCreateStats(key)
	stats.Connections--
}

// RecordBytesSent 记录发送字节数
func RecordBytesSent(key string, bytes uint64) {
	statsMu.Lock()
	defer statsMu.Unlock()
	stats := getOrCreateStats(key)
	stats.BytesSent += bytes
	stats.LastUpdated = time.Now()
}

// RecordBytesReceived 记录接收字节数
func RecordBytesReceived(key string, bytes uint64) {
	statsMu.Lock()
	defer statsMu.Unlock()
	stats := getOrCreateStats(key)
	stats.BytesReceived += bytes
	stats.LastUpdated = time.Now()
}

// printTrafficStats 定期打印流量统计
func PrintTrafficStats() {
	for {
		<-time.After(30 * time.Second)
		statsMu.Lock()
		if len(statsMap) == 0 {
			statsMu.Unlock()
			continue
		}

		log.Println("\n===== 流量统计信息 ====")
		for key, stats := range statsMap {
			log.Printf("%s: 发送 %d bytes, 接收 %d bytes, 当前连接 %d",
				key, stats.BytesSent, stats.BytesReceived, stats.Connections)
		}
		log.Println("======================")
		statsMu.Unlock()
	}
}
