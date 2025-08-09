package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/net/proxy"
)

func main() {
	// 创建SOCKS5代理拨号器
	dialer, err := proxy.SOCKS5("tcp", "localhost:1081", nil, proxy.Direct)
	if err != nil {
		fmt.Printf("创建SOCKS5拨号器失败: %v\n", err)
		return
	}

	// 创建带有超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 配置HTTP客户端
	transport := &http.Transport{
		Dial: dialer.Dial,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.baidu.com", nil)
	if err != nil {
		fmt.Printf("创建请求失败: %v\n", err)
		return
	}

	// 添加User-Agent头
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()

	// 读取响应内容
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("读取响应失败: %v\n", err)
		return
	}

	// 打印结果
	fmt.Printf("响应状态码: %d\n", resp.StatusCode)
	fmt.Printf("响应内容长度: %d bytes\n", len(body))
	fmt.Printf("响应内容: %s\n", body)
}
