package socks5

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/dreamsxin/go-portmapping/internal/config"
	"github.com/dreamsxin/go-portmapping/internal/stats"
)

// SOCKS5协议常量定义
const (
	socks5Version = 0x05 // SOCKS版本
	noAuth        = 0x00 // 无认证
	passwordAuth  = 0x02 // 密码认证
	noAcceptable  = 0xff // 无可用认证方法

	// 支持的命令
	cmdConnect      = 0x01 // CONNECT命令
	cmdBind         = 0x02 // BIND命令
	cmdUdpAssociate = 0x03 // UDP ASSOCIATE命令

	// 地址类型
	addrTypeIPv4   = 0x01 // IPv4地址
	addrTypeDomain = 0x03 // 域名地址
	addrTypeIPv6   = 0x04 // IPv6地址

	// 响应状态码
	replySucceeded            = 0x00 // 成功
	replyHostUnreachable      = 0x04 // 主机不可达
	replyCommandNotSupported  = 0x07 // 命令不支持
	replyAddrTypeNotSupported = 0x08 // 地址类型不支持
)

var (
	listeners = make(map[string]net.Listener)
	mu        sync.Mutex
)

// StartSOCKS5Forwarder 启动SOCKS5代理服务
func StartSOCKS5Forwarder(rule config.Rule, key string) {
	mu.Lock()
	defer mu.Unlock()

	// 如果监听器已存在，则不需要重新启动
	if _, exists := listeners[key]; exists {
		return
	}

	// 创建TCP监听器
	addr := net.JoinHostPort("0.0.0.0", strconv.Itoa(rule.ListenPort))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("SOCKS5监听失败: %v", err)
		return
	}

	listeners[key] = listener
	log.Printf("SOCKS5代理已启动: %s", addr)

	// 异步接受连接
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				// 检查监听器是否已关闭
				mu.Lock()
				_, exists := listeners[key]
				mu.Unlock()
				if !exists {
					return
				}
				log.Printf("SOCKS5接受连接失败: %v", err)
				continue
			}

			// 处理新连接
			go handleConnection(conn, rule, key)
		}
	}()
}

// StopSOCKS5Forwarders 停止SOCKS5代理服务
func StopSOCKS5Forwarders(activeKeys map[string]bool) {
	mu.Lock()
	defer mu.Unlock()

	for key, listener := range listeners {
		if !activeKeys[key] {
			listener.Close()
			log.Printf("SOCKS5代理已停止: %s", key)
			delete(listeners, key)
		}
	}
}

// 处理SOCKS5客户端连接
func handleConnection(conn net.Conn, rule config.Rule, key string) {
	defer conn.Close()
	stats.IncrementConnections(key)
	defer stats.DecrementConnections(key)

	// 1. 握手阶段 - 版本和认证方法协商
	if err := handshake(conn, rule); err != nil {
		log.Printf("SOCKS5握手失败: %v", err)
		return
	}
	log.Printf("SOCKS5握手成功")

	// 2. 认证阶段
	if rule.SOCKS5Auth == "password" {
		if err := authenticate(conn, rule); err != nil {
			log.Printf("SOCKS5认证失败: %v", err)
			return
		}
	}

	// 3. 处理客户端请求
	if err := processRequest(conn, &rule, key); err != nil {
		log.Printf("SOCKS5处理请求失败: %v", err)
		return
	}
}

// handshake 处理SOCKS5握手阶段 - 协商版本和认证方法
func handshake(conn net.Conn, rule config.Rule) error {
	// 读取客户端握手请求
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil || n < 2 {
		return fmt.Errorf("握手读取失败: %v", err)
	}

	// 验证版本号 (必须是SOCKS5)
	if buf[0] != socks5Version {
		return errors.New("不支持的SOCKS版本")
	}

	// 获取客户端支持的认证方法数量
	methodCount := int(buf[1])
	if n < 2+methodCount {
		return errors.New("握手数据包不完整")
	}

	// 提取客户端支持的认证方法
	supportedMethods := buf[2 : 2+methodCount]

	// 选择合适的认证方法
	var selectedMethod byte = noAcceptable
	if rule.SOCKS5Auth == "password" {
		// 检查客户端是否支持密码认证
		for _, m := range supportedMethods {
			if m == passwordAuth {
				selectedMethod = passwordAuth
				break
			}
		}
	} else {
		// 默认使用无认证
		for _, m := range supportedMethods {
			if m == noAuth {
				selectedMethod = noAuth
				break
			}
		}
	}

	// 发送选择结果
	_, err = conn.Write([]byte{socks5Version, selectedMethod})
	if err != nil {
		return fmt.Errorf("发送握手响应失败: %v", err)
	}
	log.Printf("发送握手成功")

	// 检查是否选择了可接受的方法
	if selectedMethod == noAcceptable {
		return errors.New("无可用的认证方法")
	}

	return nil
}

// authenticate 处理SOCKS5密码认证
func authenticate(conn net.Conn, rule config.Rule) error {
	// 读取认证请求
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil || n < 2 {
		return fmt.Errorf("认证读取失败: %v", err)
	}

	// 验证认证版本 (必须是0x01)
	if buf[0] != 0x01 {
		return errors.New("不支持的认证版本")
	}

	// 提取用户名和密码
	usernameLen := int(buf[1])
	if n < 2+usernameLen+1 {
		return errors.New("认证数据包不完整")
	}

	username := string(buf[2 : 2+usernameLen])
	passwordLen := int(buf[2+usernameLen])
	if n < 2+usernameLen+1+passwordLen {
		return errors.New("密码字段不完整")
	}

	password := string(buf[2+usernameLen+1 : 2+usernameLen+1+passwordLen])

	// 验证用户名和密码
	status := byte(0x01) // 默认为认证失败
	if username == rule.SOCKS5Username && password == rule.SOCKS5Password {
		status = 0x00 // 认证成功
	}

	// 发送认证响应
	_, err = conn.Write([]byte{0x01, status})
	if err != nil {
		return fmt.Errorf("发送认证响应失败: %v", err)
	}

	if status != 0x00 {
		return errors.New("用户名或密码错误")
	}

	return nil
}

// processRequest 处理SOCKS5请求命令并建立数据转发
func processRequest(conn net.Conn, rule *config.Rule, key string) error {
	// 读取请求头 (版本、命令、保留位、地址类型)
	reqHeader := make([]byte, 4)
	if _, err := io.ReadFull(conn, reqHeader); err != nil {
		return fmt.Errorf("读取请求头失败: %v", err)
	}

	// 验证版本和保留位
	if reqHeader[0] != socks5Version {
		return fmt.Errorf("不支持的SOCKS版本: %d", reqHeader[0])
	}
	if reqHeader[2] != 0x00 {
		return errors.New("保留位必须为0")
	}

	// 解析目标地址
	targetAddr, err := parseTargetAddress(conn, reqHeader[3])
	if err != nil {
		sendReply(conn, replyHostUnreachable, nil)
		return fmt.Errorf("解析目标地址失败: %v", err)
	}

	// 根据命令类型处理
	switch reqHeader[1] {
	case cmdConnect:
		return handleConnectCommand(conn, targetAddr, rule, key)
	case cmdBind:
		sendReply(conn, replyCommandNotSupported, nil)
		return errors.New("BIND命令暂不支持")
	case cmdUdpAssociate:
		sendReply(conn, replyCommandNotSupported, nil)
		return errors.New("UDP ASSOCIATE命令暂不支持")
	default:
		sendReply(conn, replyCommandNotSupported, nil)
		return fmt.Errorf("不支持的命令: %d", reqHeader[1])
	}
}

// parseTargetAddress 解析SOCKS5请求中的目标地址
func parseTargetAddress(conn net.Conn, addrType byte) (net.Addr, error) {
	switch addrType {
	case addrTypeIPv4:
		// IPv4地址 (4字节) + 端口 (2字节)
		addrData := make([]byte, 6)
		if _, err := io.ReadFull(conn, addrData); err != nil {
			return nil, err
		}
		ip := net.IPv4(addrData[0], addrData[1], addrData[2], addrData[3])
		port := binary.BigEndian.Uint16(addrData[4:6])
		return &net.TCPAddr{IP: ip, Port: int(port)}, nil

	case addrTypeDomain:
		// 域名 (1字节长度 + n字节域名) + 端口 (2字节)
		// 读取一个字节的域名长度
		// 域名 (1字节长度 + n字节域名) + 端口 (2字节)
		// 读取一个字节的域名长度
		var domainLen byte
		buf := make([]byte, 1)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return nil, err
		}
		domainLen = buf[0]
		domain := make([]byte, domainLen)
		if _, err := io.ReadFull(conn, domain); err != nil {
			return nil, err
		}
		// 解析域名获取IP地址
		ipAddr, err := net.ResolveIPAddr("ip", string(domain))
		if err != nil {
			return nil, fmt.Errorf("域名解析失败: %v", err)
		}
		portData := make([]byte, 2)
		if _, err := io.ReadFull(conn, portData); err != nil {
			return nil, err
		}
		port := binary.BigEndian.Uint16(portData)
		return &net.TCPAddr{IP: ipAddr.IP, Port: int(port)}, nil

	case addrTypeIPv6:
		// IPv6地址 (16字节) + 端口 (2字节)
		addrData := make([]byte, 18)
		if _, err := io.ReadFull(conn, addrData); err != nil {
			return nil, err
		}
		ip := net.IP(addrData[:16])
		port := binary.BigEndian.Uint16(addrData[16:18])
		return &net.TCPAddr{IP: ip, Port: int(port)}, nil

	default:
		return nil, fmt.Errorf("不支持的地址类型: %d", addrType)
	}
}

// handleConnectCommand 处理CONNECT命令，建立TCP连接并转发数据
func handleConnectCommand(clientConn net.Conn, targetAddr net.Addr, rule *config.Rule, key string) error {
	// 连接目标服务器
	targetConn, err := net.DialTCP("tcp", nil, targetAddr.(*net.TCPAddr))
	if err != nil {
		sendReply(clientConn, replyHostUnreachable, nil)
		return fmt.Errorf("连接目标服务器失败: %v", err)
	}
	defer targetConn.Close()

	// 发送成功响应
	if err := sendReply(clientConn, replySucceeded, targetConn.LocalAddr()); err != nil {
		return fmt.Errorf("发送响应失败: %v", err)
	}

	// 开始双向数据转发并统计流量
	var wg sync.WaitGroup
	wg.Add(2)
	errChan := make(chan error, 2)

	// 客户端到目标服务器 (发送方向)
	go func() {
		defer wg.Done()
		// 使用stats.CopyStreamWithStats替代io.Copy以支持流量统计
		_, err := stats.CopyStreamWithStats(targetConn, clientConn, key, true)
		if err != nil && !isClosedConnError(err) {
			errChan <- fmt.Errorf("客户端到目标服务器转发失败: %v", err)
		}
	}()

	// 目标服务器到客户端 (接收方向)
	go func() {
		defer wg.Done()
		// 使用stats.CopyStreamWithStats替代io.Copy以支持流量统计
		_, err := stats.CopyStreamWithStats(clientConn, targetConn, key, false)
		if err != nil && !isClosedConnError(err) {
			errChan <- fmt.Errorf("目标服务器到客户端转发失败: %v", err)
		}
	}()
	// 等待任一方向出错或完成
	err = <-errChan
	if err != nil {
		log.Printf("连接传输错误: %v", err)
	}
	clientConn.Close()
	targetConn.Close()

	wg.Wait()
	log.Printf("TCP连接已关闭: %s <-> %s", clientConn.RemoteAddr(), targetAddr)
	return nil
}

// sendReply 向客户端发送SOCKS5响应
func sendReply(conn net.Conn, replyCode byte, addr net.Addr) error {
	// 构建响应包
	reply := []byte{socks5Version, replyCode, 0x00}

	// 添加地址信息 (目前仅支持IPv4)
	if addr != nil {
		tcpAddr, ok := addr.(*net.TCPAddr)
		if ok {
			if tcpAddr.IP.To4() != nil {
				// IPv4地址
				reply = append(reply, addrTypeIPv4)
				reply = append(reply, tcpAddr.IP.To4()...)
			} else if tcpAddr.IP.To16() != nil {
				// IPv6地址
				reply = append(reply, addrTypeIPv6)
				reply = append(reply, tcpAddr.IP.To16()...)
			} else {
				// 未知地址类型
				reply = append(reply, addrTypeIPv4, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
			}
			portBytes := make([]byte, 2)
			binary.BigEndian.PutUint16(portBytes, uint16(tcpAddr.Port))
			reply = append(reply, portBytes...)
		} else {
			// 不支持的地址类型，使用0.0.0.0:0
			reply = append(reply, addrTypeIPv4, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
		}
	} else {
		// 无地址信息，使用0.0.0.0:0
		reply = append(reply, addrTypeIPv4, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00)
	}

	// 发送响应
	_, err := conn.Write(reply)
	return err
}

// isClosedConnError 判断错误是否为连接已关闭错误
func isClosedConnError(err error) bool {
	if err == nil {
		return false
	}
	err = errors.Unwrap(err)
	if err == net.ErrClosed {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "connection reset by peer")
}
