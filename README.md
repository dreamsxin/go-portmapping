# go-portmapping

## 项目介绍

`go-portmapping` 是一个高性能的多协议端口映射工具，支持TCP、UDP和WebSocket协议，具备流量统计、连接管理和配置热加载功能。该工具设计用于在不同网络环境之间建立安全稳定的端口转发通道，适用于开发调试、网络测试和服务暴露等场景。

## 功能特性

- 多协议支持：同时支持TCP、UDP和WebSocket协议的端口转发
- 流量统计：实时监控并统计各端口的连接数、发送/接收字节数
- 连接管理：支持连接数限制、超时控制和自动清理闲置连接
- 配置热加载：修改配置文件后自动应用新规则，无需重启服务
- 高并发支持：采用goroutine池和资源池化技术，高效处理并发连接
- 详细日志：完善的日志输出，便于问题排查和运行状态监控
- 跨平台：支持Windows、Linux和macOS等主流操作系统
- 容器化支持：提供Docker镜像，方便在容器环境中部署
- 插件机制：支持自定义插件，扩展功能和协议支持
- 性能优化：采用零拷贝技术和内存池机制，提高处理效率
- 安全性：支持TLS加密和访问控制，确保数据安全

## 安装方法

### 前提条件

- Go 1.16+ 开发环境
- Git 版本控制工具

## 源码编译

```bash
# 克隆代码仓库
git clone https://github.com/yourusername/go-portmapping.git
cd go-portmapping

# 编译可执行文件
go build -o portmapping.exe ./cmd/port
```

编译完成后，可执行文件 portmapping.exe 将生成在项目根目录下。

## 配置说明

### 配置文件格式

配置文件采用JSON格式，默认路径为 configs/rules.json，可通过命令行参数或环境变量指定自定义路径。

#### 配置示例

```json
[
  {
    "protocol": "tcp",
    "listenPort": 8080,
    "targetHost": "192.168.1.100",
    "targetPort": 80,
    "enabled": true
  },
  {
    "protocol": "udp",
    "listenPort": 53,
    "targetHost": "8.8.8.8",
    "targetPort": 53,
    "enabled": true
  },
  {
    "protocol": "websocket",
    "listenPort": 8081,
    "targetHost": "localhost",
    "targetPort": 8082,
    "enabled": true
  }
]
```

### 环境变量

`CONFIG_PATH`: 指定配置文件路径，优先级高于默认路径。

## 使用指南

### 基本用法

```bash
# 使用默认配置文件启动
./portmapping.exe

# 指定自定义配置文件
./portmapping.exe -c /path/to/your/rules.json

# 使用环境变量指定配置文件
set CONFIG_PATH=/path/to/your/rules.json
./portmapping.exe
```

## 项目结构

```plaintext
go-portmapping/
├── cmd/port/              # 主程序入口
│   ├── configs/           # 默认配置文件
│   └── main.go            # 程序主函数
├── internal/config/       # 配置管理模块
├── internal/protocols/    # 协议处理模块
│   ├── tcp/               # TCP协议实现
│   ├── udp/               # UDP协议实现
│   └── websocket/         # WebSocket协议实现
└── internal/stats/        # 流量统计模块
```

## 注意事项

- Windows系统兼容性：程序在Windows系统上测试通过，文件路径请使用反斜杠\或正斜杠/
- 权限要求：监听1024以下端口需要管理员权限
- 防火墙设置：确保系统防火墙允许程序监听指定端口
- 性能调优：高并发场景下可调整连接池大小和超时参数
- 安全建议：生产环境中建议限制目标服务器地址和端口范围