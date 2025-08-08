package stats

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"
)

type mockConn struct {
	readBuffer  *bytes.Buffer
	writeBuffer *bytes.Buffer
}

func newMockConn(readData string) *mockConn {
	return &mockConn{
		readBuffer:  bytes.NewBufferString(readData),
		writeBuffer: &bytes.Buffer{},
	}
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	return m.readBuffer.Read(b)
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	return m.writeBuffer.Write(b)
}

func (m *mockConn) Close() error {
	return nil
}

func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

// TestUpdateTrafficStats 测试流量统计更新功能
func TestUpdateTrafficStats(t *testing.T) {
	key := "test-key"
	UpdateTrafficStats(key, 1024, true)
	UpdateTrafficStats(key, 2048, false)

	stats, _ := GetStats(key)
	if stats.BytesSent != 1024 || stats.BytesReceived != 2048 {
		t.Errorf("更新流量统计失败，期望BytesSent=1024, BytesReceived=2048，实际得到BytesSent=%d, BytesReceived=%d", stats.BytesSent, stats.BytesReceived)
	}
}

// TestConnectionCount 测试连接数增减功能
func TestConnectionCount(t *testing.T) {
	key := "test-key"
	IncrementConnections(key)
	stats, exists := GetStats(key)
	if !exists || stats.Connections != 1 {
		t.Errorf("增加连接失败，期望Connections=1，实际得到%d", stats.Connections)
	}

	DecrementConnections(key)
	stats, exists = GetStats(key)
	if !exists || stats.Connections != 0 {
		t.Errorf("减少连接失败，期望Connections=0，实际得到%d", stats.Connections)
	}
}

// TestConcurrentConnectionUpdates 测试并发环境下的连接数更新
func TestConcurrentConnectionUpdates(t *testing.T) {
	key := "test-concurrent"
	var wg sync.WaitGroup
	concurrency := 100
	wg.Add(concurrency * 2)

	// 并发增加连接
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			IncrementConnections(key)
		}()
	}

	// 并发减少连接
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			DecrementConnections(key)
		}()
	}

	wg.Wait()
	stats, exists := GetStats(key)
	if !exists || stats.Connections != 0 {
		t.Errorf("并发更新连接数失败，期望Connections=0，实际得到%d", stats.Connections)
	}
}

// TestCopyStreamWithStats 测试带统计功能的数据流复制
func TestCopyStreamWithStats(t *testing.T) {
	key := "test-copy"
	src := newMockConn("test data")
	dst := newMockConn("")

	size, err := CopyStreamWithStats(dst, src, key, true)
	if err != nil {
		t.Fatalf("数据复制失败: %v", err)
	}

	expectedSize := int64(len("test data"))
	if size != expectedSize {
		t.Errorf("复制数据大小不匹配，期望%d，实际得到%d", expectedSize, size)
	}

	stats, _ := GetStats(key)
	if stats.BytesSent != uint64(expectedSize) || stats.BytesReceived != 0 {
		t.Errorf("流量统计不正确，期望BytesSent=%d, BytesReceived=0，实际得到BytesSent=%d, BytesReceived=%d", expectedSize, stats.BytesSent, stats.BytesReceived)
	}
}

// TestCleanupInactiveStats 测试非活动连接清理
func TestCleanupInactiveStats(t *testing.T) {
	key := "test-cleanup"
	IncrementConnections(key)
	DecrementConnections(key)

	stats, exists := GetStats(key)
	if !exists {
		t.Fatal("获取统计信息失败")
	}
	stats.LastUpdated = time.Now().Add(-inactiveTimeout - time.Minute)

	cleanupInactiveStats()

	if _, exists := GetStats(key); exists {
		t.Error("应该清理非活跃连接的统计数据")
	}
}

// TestGetAllStats 测试获取所有统计信息
func TestGetAllStats(t *testing.T) {
	key := "test-all"
	IncrementConnections(key)

	allStats := GetAllStats()
	if _, exists := allStats[key]; !exists {
		t.Error("获取所有统计信息失败，未找到测试key")
	}
}
