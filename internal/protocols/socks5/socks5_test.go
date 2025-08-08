package socks5

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/dreamsxin/go-portmapping/internal/config"
)

// TestSOCKS5Proxy 测试SOCKS5代理服务器的基本功能
func TestSOCKS5Proxy(t *testing.T) {
	// 创建测试规则配置（匹配rules.json中的配置）
	testRule := config.Rule{
		SOCKS5Auth: "none", // 无认证模式
		// 可以添加其他必要的规则字段
	}
	testKey := "test-key"

	// 启动测试用SOCKS5服务器
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("无法启动测试服务器: %v", err)
	}
	defer listener.Close()

	// 异步启动服务器接受连接
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("无法接受连接: %v\n", err)
			return
		}
		fmt.Printf("接受连接\n")
		// 传递必要的参数给handleConnection
		handleConnection(conn, testRule, testKey)
	}()

	// 客户端连接测试
	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("无法连接到测试服务器: %v", err)
	}
	defer clientConn.Close()
	fmt.Printf("连接到测试服务器\n")

	clientConn.SetDeadline(time.Now().Add(5 * time.Second))

	// 1. 发送SOCKS5版本和认证方法请求
	_, err = clientConn.Write([]byte{0x05, 0x01, 0x00}) // 无认证
	if err != nil {
		t.Fatalf("发送认证请求失败: %v", err)
	}
	fmt.Printf("发送认证请求成功\n")

	// 读取服务器的认证响应
	authResp := make([]byte, 2)
	_, err = clientConn.Read(authResp)
	if err != nil {
		t.Fatalf("读取认证响应失败: %v", err)
	}
	fmt.Printf("读取认证响应成功\n")

	// 验证响应版本和选择的认证方法
	if authResp[0] != 0x05 || authResp[1] != 0x00 {
		t.Errorf("期望认证响应 [0x05, 0x00], 实际收到 %v", authResp)
	}

	// 2. 发送SOCKS5连接请求 (连接到8.8.8.8:80)
	request := []byte{
		0x05,         // SOCKS5版本
		0x01,         // CONNECT命令
		0x00,         // 保留字段
		0x01,         // IPv4地址类型
		127, 0, 0, 1, // 目标IP
		0x1F, 0x90, // 目标端口 (80)
	}
	_, err = clientConn.Write(request)
	if err != nil {
		t.Fatalf("发送连接请求失败: %v", err)
	}
	fmt.Printf("发送连接请求成功\n")

	// 读取服务器的连接响应
	resp := make([]byte, 10)
	n, err := clientConn.Read(resp)
	if err != nil {
		t.Fatalf("读取连接响应失败: %v", err)
	}
	fmt.Printf("读取连接响应成功\n")

	// 验证响应版本和状态码
	if n < 7 || resp[0] != 0x05 || resp[1] != 0x00 {
		t.Errorf("期望连接成功响应, 实际收到 %v", resp[:n])
	}
}
