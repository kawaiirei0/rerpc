# rerpc - 轻量级高性能 JSON-RPC 库

[![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.18-blue)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

rerpc 是一个基于 Go 语言实现的轻量级、高性能 JSON-RPC 2.0 工具库，专注于展示核心性能优化算法和 Go 语言高性能编程技巧。

## ✨ 特性

- 🚀 **高性能**: 应用多种性能优化技术，实现低延迟、高吞吐量
- 🔄 **对象池**: 使用 `sync.Pool` 复用对象，减少 GC 压力
- 🔌 **连接池**: 复用 TCP 连接，避免频繁建立连接的开销
- 🧵 **协程池**: 限制并发数量，避免 goroutine 爆炸
- 💾 **零拷贝**: 减少数据拷贝次数，提升编解码性能
- 🎯 **反射缓存**: 预先提取反射信息，避免运行时开销
- 📦 **批量处理**: 使用 bufio 减少系统调用次数
- 🔒 **并发安全**: 完整的并发控制，通过 race detector 测试
- 🛡️ **错误处理**: 完善的错误处理和重试机制
- 📊 **性能监控**: 内置统计信息，便于性能分析

## 📋 目录

- [快速开始](#快速开始)
- [架构设计](#架构设计)
- [性能优化技术](#性能优化技术)
- [API 文档](#api-文档)
- [性能测试](#性能测试)
- [示例代码](#示例代码)
- [最佳实践](#最佳实践)

## 🚀 快速开始

### 安装

```bash
# 克隆仓库
git clone https://github.com/kawaiirei0/rerpc.git
cd rerpc

# 或者在你的项目中使用
go get rerpc
```

### 服务端示例

```go
package main

import (
    "context"
    "log"
    
    "github.com/kawaiirei0/rerpc"
)

// 定义服务
type ArithService struct{}

type AddArgs struct {
    A int `json:"a"`
    B int `json:"b"`
}

type AddReply struct {
    Result int `json:"result"`
}

// 实现服务方法（必须符合签名规范）
func (s *ArithService) Add(ctx context.Context, args *AddArgs, reply *AddReply) error {
    reply.Result = args.A + args.B
    return nil
}

func main() {
    // 创建服务器（100 个工作协程）
    server := rerpc.NewServer(100)
    
    // 注册服务
    if err := server.Register(new(ArithService)); err != nil {
        log.Fatal(err)
    }
    
    // 启动服务器
    log.Println("Starting server on :8080...")
    if err := server.Serve("tcp", ":8080"); err != nil {
        log.Fatal(err)
    }
}
```

### 客户端示例

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    
    "rerpc"
)

type AddArgs struct {
    A int `json:"a"`
    B int `json:"b"`
}

type AddReply struct {
    Result int `json:"result"`
}

func main() {
    // 创建客户端
    client, err := rerpc.NewClient(rerpc.ClientConfig{
        Network:     "tcp",
        Address:     "localhost:8080",
        MaxIdle:     10,
        MaxActive:   100,
        DialTimeout: 5 * time.Second,
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()
    
    // 执行同步调用
    args := &AddArgs{A: 10, B: 20}
    reply := &AddReply{}
    
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    if err := client.Call(ctx, "ArithService.Add", args, reply); err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Result: %d + %d = %d\n", args.A, args.B, reply.Result)
}
```

运行示例：

```bash
# 启动服务器
go run examples/simple/server/server.go

# 在另一个终端运行客户端
go run examples/simple/client/client.go
```

## 🏗️ 架构设计

### 整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                         rerpc Library                        │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────┐              ┌──────────────┐            │
│  │    Client    │              │    Server    │            │
│  │              │              │              │            │
│  │ - Call()     │              │ - Register() │            │
│  │ - Go()       │              │ - Serve()    │            │
│  │ - Batch()    │              │ - Shutdown() │            │
│  └──────┬───────┘              └──────┬───────┘            │
│         │                             │                     │
│         ├─────────────┬───────────────┤                     │
│         │             │               │                     │
│  ┌──────▼─────┐ ┌─────▼─────┐ ┌──────▼───────┐            │
│  │ Connection │ │   Codec   │ │  Goroutine   │            │
│  │    Pool    │ │  (JSON)   │ │     Pool     │            │
│  └────────────┘ └───────────┘ └──────────────┘            │
│                                                               │
│  ┌─────────────────────────────────────────────┐            │
│  │          Service Registry                    │            │
│  │  - Reflection Cache                          │            │
│  │  - Method Lookup                             │            │
│  └─────────────────────────────────────────────┘            │
│                                                               │
│  ┌─────────────────────────────────────────────┐            │
│  │          Object Pool (sync.Pool)             │            │
│  │  - Request / Response / Buffer               │            │
│  └─────────────────────────────────────────────┘            │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

### 核心组件

#### 1. Server - RPC 服务器

- 监听 TCP 连接
- 使用协程池处理请求
- 集成服务注册表和编解码器
- 支持优雅关闭

#### 2. Client - RPC 客户端

- 连接池管理
- 同步/异步调用
- 批量调用
- 自动重试机制

#### 3. Codec - 编解码器

- JSON-RPC 2.0 协议实现
- 对象池复用
- 零拷贝优化

#### 4. Connection Pool - 连接池

- TCP 连接复用
- 健康检查
- 并发安全

#### 5. Goroutine Pool - 协程池

- 固定数量的工作协程
- 任务队列
- 优雅关闭

#### 6. Service Registry - 服务注册表

- 反射缓存
- 方法查找
- 参数验证

#### 7. Object Pool - 对象池

- Request/Response 复用
- Buffer 复用
- 自动重置

## ⚡ 性能优化技术

### 1. 对象池（sync.Pool）

**原理**: 复用对象，减少 GC 压力

```go
var requestPool = sync.Pool{
    New: func() interface{} {
        return &Request{}
    },
}

// 获取对象
req := requestPool.Get().(*Request)

// 使用完毕后归还
defer func() {
    req.Reset()
    requestPool.Put(req)
}()
```

**效果**: 
- 减少 50-70% 的内存分配
- 提升 30-50% 的性能
- 降低 GC 频率

### 2. 连接池

**原理**: 复用 TCP 连接，避免三次握手开销

```go
// 从连接池获取连接
conn, err := pool.Get()
defer pool.Put(conn)

// 使用连接
// ...
```

**效果**:
- 减少 80% 的连接建立时间
- 提升并发性能
- 降低系统资源消耗

### 3. 协程池

**原理**: 限制并发数，避免 goroutine 爆炸

```go
// 创建协程池（100 个工作协程）
pool := NewGoroutinePool(100, 1000)

// 提交任务
pool.Submit(func() {
    // 处理任务
})
```

**效果**:
- 稳定的内存使用
- 避免 OOM
- 提升 CPU 缓存命中率

### 4. 反射缓存

**原理**: 注册时提取反射信息，运行时直接使用

```go
// 注册时缓存
for i := 0; i < typ.NumMethod(); i++ {
    method := typ.Method(i)
    methods[method.Name] = &methodType{
        method:    method,
        ArgType:   mtype.In(2).Elem(),
        ReplyType: mtype.In(3).Elem(),
    }
}

// 运行时直接使用缓存的信息
method := cachedMethod.method
```

**效果**:
- 减少 90% 的反射开销
- 提升方法调用性能

### 5. 零拷贝

**原理**: 直接操作 `[]byte`，避免字符串转换

```go
// 避免多次拷贝
json.Unmarshal(data, &req)  // 直接使用 []byte

// 而不是
str := string(data)
json.Unmarshal([]byte(str), &req)
```

**效果**:
- 减少内存拷贝
- 提升编解码性能

### 6. 批量处理

**原理**: 使用 bufio 批量读写，减少系统调用

```go
reader := bufio.NewReaderSize(conn, 32*1024)
writer := bufio.NewWriterSize(conn, 32*1024)
```

**效果**:
- 减少 I/O 次数
- 提升网络性能

## 📚 API 文档

### Server API

#### NewServer

```go
func NewServer(workers int) *Server
```

创建一个新的 RPC 服务器。

- `workers`: 协程池的工作协程数量（如果 <= 0，默认使用 100）

#### Register

```go
func (s *Server) Register(service interface{}) error
```

注册一个服务实例。服务名称将自动设置为类型名称。

#### RegisterName

```go
func (s *Server) RegisterName(name string, service interface{}) error
```

使用指定名称注册服务。

#### Serve

```go
func (s *Server) Serve(network, address string) error
```

启动 RPC 服务器，监听指定地址。此方法会阻塞。

- `network`: 网络类型（如 "tcp", "tcp4", "tcp6"）
- `address`: 监听地址（如 ":8080", "localhost:8080"）

#### Shutdown

```go
func (s *Server) Shutdown(ctx context.Context) error
```

优雅关闭服务器，等待所有请求处理完成。

### Client API

#### NewClient

```go
func NewClient(config ClientConfig) (*Client, error)
```

创建一个新的 RPC 客户端。

```go
type ClientConfig struct {
    Network     string        // 网络类型（如 "tcp"）
    Address     string        // 服务器地址（如 "localhost:8080"）
    MaxIdle     int           // 最大空闲连接数
    MaxActive   int           // 最大活跃连接数
    DialTimeout time.Duration // 连接超时时间
    MaxRetries  int           // 最大重试次数
    RetryDelay  time.Duration // 重试延迟
}
```

#### Call

```go
func (c *Client) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error
```

执行同步 RPC 调用。

- `ctx`: 上下文，用于超时控制和取消
- `serviceMethod`: 服务方法名（格式：Service.Method）
- `args`: 方法参数
- `reply`: 方法返回值（指针类型）

#### Go

```go
func (c *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call
```

执行异步 RPC 调用。

- `done`: 调用完成通知 channel（可选）

#### Batch

```go
func (c *Client) Batch(ctx context.Context, calls []*Call) error
```

批量执行多个 RPC 调用（并发执行）。

#### Close

```go
func (c *Client) Close() error
```

关闭客户端，释放所有资源。

#### Stats

```go
func (c *Client) Stats() ClientStats
```

获取客户端统计信息。

### 服务方法签名规范

服务方法必须符合以下签名：

```go
func (s *Service) Method(ctx context.Context, args *ArgsType, reply *ReplyType) error
```

- 第一个参数：`context.Context`
- 第二个参数：参数指针 `*T`
- 第三个参数：返回值指针 `*R`
- 返回值：`error`

## 📊 性能测试

> 📄 完整的性能测试报告请查看 [PERFORMANCE.md](PERFORMANCE.md)

### 测试环境

- CPU: 13th Gen Intel(R) Core(TM) i5-13400 (16 cores)
- RAM: 32GB DDR4
- OS: Windows 11
- Go: 1.24.5

### 性能指标

#### 对象池性能

```
BenchmarkObjectPool_WithPool-16                 857515875        1.367 ns/op       0 B/op      0 allocs/op
BenchmarkObjectPool_WithoutPool-16             1000000000        0.04446 ns/op     0 B/op      0 allocs/op
BenchmarkObjectPool_Response_WithPool-16        100000000       11.32 ns/op       16 B/op      1 allocs/op
BenchmarkObjectPool_Response_WithoutPool-16    1000000000        0.03220 ns/op     0 B/op      0 allocs/op
BenchmarkObjectPool_Buffer_WithPool-16          741322815        1.639 ns/op       0 B/op      0 allocs/op
BenchmarkObjectPool_Buffer_WithoutPool-16        63888237       18.91 ns/op       64 B/op      1 allocs/op
```

**提升**: Buffer 对象池性能提升 11.5 倍，内存分配减少 100%

#### 编解码性能

```
BenchmarkCodec_EncodeRequest-16          7234219       328.4 ns/op        80 B/op      1 allocs/op
BenchmarkCodec_DecodeRequest-16          2446252       994.3 ns/op       272 B/op      9 allocs/op
BenchmarkCodec_EncodeResponse-16         8072425       299.8 ns/op        48 B/op      1 allocs/op
BenchmarkCodec_DecodeResponse-16         2881548       820.9 ns/op       264 B/op      8 allocs/op
```

**性能**: 
- 编码性能: ~300-330 ns/op
- 解码性能: ~820-994 ns/op

#### 端到端性能

```
BenchmarkE2E_SimpleCall-16                 50256      46410 ns/op      10094 B/op     47 allocs/op
BenchmarkE2E_ConcurrentCalls-16           188598      11484 ns/op      10217 B/op     47 allocs/op
BenchmarkE2E_LargePayload-16                9253     238267 ns/op      84728 B/op     55 allocs/op
BenchmarkE2E_Throughput-16                377036       6409 ns/op      10068 B/op     47 allocs/op
```

**性能指标**:
- 简单调用延迟: ~46.4 微秒
- 并发调用延迟: ~11.5 微秒（并发优化效果显著）
- 高吞吐量延迟: ~6.4 微秒
- 吞吐量: ~156,000 QPS（单客户端）
- 大负载（10KB）延迟: ~238 微秒

#### 并发安全测试

```
TestE2E_StressTest: 10,000 次并发调用
- 成功率: 99.99%
- 总耗时: 78.6 ms
- QPS: 127,158
```

### 运行基准测试

```bash
# 运行所有基准测试
cd examples/benchmark
go test -bench=. -benchmem

# 运行特定测试
go test -bench=BenchmarkE2E -benchmem

# 生成性能报告
go test -bench=. -cpuprofile=cpu.prof -memprofile=mem.prof
go tool pprof cpu.prof
```

详细的性能测试说明请参考 [examples/benchmark/README.md](examples/benchmark/README.md)

## 💡 示例代码

### 1. 简单示例

完整的服务器和客户端示例，演示基本用法。

📁 [examples/simple/server/](examples/simple/server/) - 服务器示例  
📁 [examples/simple/client/](examples/simple/client/) - 客户端示例

### 2. 性能基准测试

各种性能测试，包括对象池、连接池、协程池、端到端测试等。

📁 [examples/benchmark/](examples/benchmark/)

## 🎯 最佳实践

### 服务器端

#### 1. 协程池大小设置

```go
// 根据 CPU 核心数设置
workers := runtime.NumCPU() * 2
server := rerpc.NewServer(workers)
```

#### 2. 优雅关闭

```go
// 监听中断信号
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

<-sigChan

// 设置超时时间
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

// 优雅关闭
server.Shutdown(ctx)
```

#### 3. 错误处理

```go
func (s *Service) Method(ctx context.Context, args *Args, reply *Reply) error {
    // 参数验证
    if args.Value < 0 {
        return fmt.Errorf("invalid value: %d", args.Value)
    }
    
    // 业务逻辑
    // ...
    
    return nil
}
```

### 客户端

#### 1. 连接池配置

```go
client, err := rerpc.NewClient(rerpc.ClientConfig{
    Network:     "tcp",
    Address:     "localhost:8080",
    MaxIdle:     10,              // 根据平均并发数设置
    MaxActive:   100,             // 根据峰值并发数设置
    DialTimeout: 5 * time.Second,
    MaxRetries:  3,               // 网络不稳定时增加重试次数
    RetryDelay:  100 * time.Millisecond,
})
```

#### 2. 超时控制

```go
// 为每个调用设置超时
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

err := client.Call(ctx, "Service.Method", args, reply)
```

#### 3. 批量调用

```go
// 准备多个调用
calls := []*rerpc.Call{
    {ServiceMethod: "Service.Method1", Args: args1, Reply: reply1},
    {ServiceMethod: "Service.Method2", Args: args2, Reply: reply2},
    {ServiceMethod: "Service.Method3", Args: args3, Reply: reply3},
}

// 并发执行
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

if err := client.Batch(ctx, calls); err != nil {
    log.Printf("Batch call failed: %v", err)
}

// 检查每个调用的结果
for i, call := range calls {
    if call.Error != nil {
        log.Printf("Call %d failed: %v", i, call.Error)
    }
}
```

#### 4. 异步调用

```go
// 发起异步调用
call := client.Go("Service.Method", args, reply, nil)

// 继续执行其他操作
// ...

// 等待调用完成
<-call.Done

if call.Error != nil {
    log.Printf("Async call failed: %v", call.Error)
}
```

### 性能调优

#### 1. 使用 pprof 分析性能

```go
import _ "net/http/pprof"

go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()
```

访问 `http://localhost:6060/debug/pprof/` 查看性能数据。

#### 2. 监控关键指标

```go
// 定期打印统计信息
ticker := time.NewTicker(10 * time.Second)
defer ticker.Stop()

for range ticker.C {
    stats := client.Stats()
    log.Printf("Stats: Pending=%d, Active=%d, Idle=%d",
        stats.PendingCalls,
        stats.PoolStats.Active,
        stats.PoolStats.Idle)
}
```

#### 3. 压力测试

使用 benchmark 测试找出性能瓶颈：

```bash
go test -bench=BenchmarkE2E_ConcurrentCalls -benchtime=10s -cpuprofile=cpu.prof
go tool pprof cpu.prof
```

## 🔧 故障排查

### 常见问题

#### 1. 连接池耗尽

**症状**: 客户端报错 "failed to get connection: pool exhausted"

**解决方案**:
- 增加 `MaxActive` 值
- 检查是否有连接泄漏（未归还连接）
- 增加服务器处理能力

#### 2. 超时错误

**症状**: 频繁出现 "context deadline exceeded"

**解决方案**:
- 增加超时时间
- 优化服务器处理逻辑
- 检查网络延迟

#### 3. 内存占用过高

**症状**: 内存持续增长

**解决方案**:
- 使用 pprof 分析内存分配
- 检查是否有 goroutine 泄漏
- 确保对象正确归还到对象池

#### 4. 并发数据竞争

**症状**: 使用 `-race` 检测到数据竞争

**解决方案**:
- 检查共享变量的访问
- 使用互斥锁保护临界区
- 使用 atomic 操作

## 📝 项目结构

```
rerpc/
├── go.mod                  # Go module 定义
├── go.sum                  # 依赖校验和
├── README.md               # 项目文档
├── PERFORMANCE.md          # 性能测试报告
├── protocol.go             # JSON-RPC 协议定义
├── codec.go                # 编解码器实现
├── pool.go                 # 对象池实现
├── connpool.go             # 连接池实现
├── goroutine_pool.go       # 协程池实现
├── registry.go             # 服务注册表实现
├── registry_test.go        # 服务注册表测试
├── server.go               # 服务器实现
├── client.go               # 客户端实现
├── error.go                # 错误定义
├── e2e_test.go             # 端到端集成测试
└── examples/
    ├── simple/             # 简单示例
    │   ├── server/         # 服务器示例
    │   │   └── server.go
    │   └── client/         # 客户端示例
    │       └── client.go
    └── benchmark/          # 性能测试
        └── bench_test.go
```

## 🤝 贡献

欢迎贡献代码、报告问题或提出建议！

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启 Pull Request

## 📄 许可证

本项目采用 MIT 许可证 - 详见 [LICENSE](LICENSE) 文件

## 🙏 致谢

本项目受以下项目启发：

- [net/rpc](https://pkg.go.dev/net/rpc) - Go 标准库的 RPC 实现
- [gorilla/rpc](https://github.com/gorilla/rpc) - Gorilla Web Toolkit 的 RPC 实现
- [JSON-RPC 2.0 Specification](https://www.jsonrpc.org/specification)

## 📧 联系方式

如有问题或建议，请通过以下方式联系：

- 提交 Issue: [GitHub Issues](https://github.com/yourusername/rerpc/issues)
- 邮件: your.email@example.com

## 🌟 Star History

如果这个项目对你有帮助，请给它一个 ⭐️！

---

**注意**: 本项目主要用于学习和展示 Go 语言高性能编程技巧，不建议直接用于生产环境。如需用于生产，请进行充分的测试和评估。
