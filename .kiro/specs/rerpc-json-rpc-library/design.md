# rerpc 设计文档

## Overview

rerpc 是一个轻量级、高性能的 JSON-RPC 工具库，专注于展示 Go 语言中的核心性能优化算法。本设计采用模块化架构，将 RPC 功能分解为独立的组件，每个组件都应用了特定的性能优化技术。

核心设计理念：
- **零拷贝**：减少数据复制次数
- **对象复用**：使用 sync.Pool 减少 GC 压力
- **连接复用**：连接池避免频繁建立连接
- **并发控制**：goroutine 池限制资源消耗
- **反射缓存**：避免重复反射操作

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         rerpc Library                        │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────┐              ┌──────────────┐            │
│  │    Client    │              │    Server    │            │
│  │              │              │              │            │
│  │ - Call()     │              │ - Register() │            │
│  │ - Close()    │              │ - Serve()    │            │
│  └──────┬───────┘              └──────┬───────┘            │
│         │                             │                     │
│         │                             │                     │
│  ┌──────▼──────────────────────────────▼───────┐           │
│  │          Connection Pool                     │           │
│  │  - Get() / Put()                             │           │
│  │  - Health Check                              │           │
│  └──────────────────────────────────────────────┘           │
│                                                               │
│  ┌─────────────────────────────────────────────┐            │
│  │            Codec (JSON-RPC)                  │            │
│  │  - EncodeRequest() / DecodeRequest()         │            │
│  │  - EncodeResponse() / DecodeResponse()       │            │
│  └─────────────────────────────────────────────┘            │
│                                                               │
│  ┌─────────────────────────────────────────────┐            │
│  │          Service Registry                    │            │
│  │  - Register(service)                         │            │
│  │  - Call(method, args)                        │            │
│  │  - Reflection Cache                          │            │
│  └─────────────────────────────────────────────┘            │
│                                                               │
│  ┌─────────────────────────────────────────────┐            │
│  │          Goroutine Pool                      │            │
│  │  - Submit(task)                              │            │
│  │  - Worker Management                         │            │
│  └─────────────────────────────────────────────┘            │
│                                                               │
│  ┌─────────────────────────────────────────────┐            │
│  │          Object Pool (sync.Pool)             │            │
│  │  - Request Pool                              │            │
│  │  - Response Pool                             │            │
│  │  - Buffer Pool                               │            │
│  └─────────────────────────────────────────────┘            │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

## Components and Interfaces

### 1. Protocol - JSON-RPC 消息定义

```go
// Request 表示 JSON-RPC 请求
type Request struct {
    Jsonrpc string          `json:"jsonrpc"` // 固定为 "2.0"
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
    ID      interface{}     `json:"id"`
}

// Response 表示 JSON-RPC 响应
type Response struct {
    Jsonrpc string          `json:"jsonrpc"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *Error          `json:"error,omitempty"`
    ID      interface{}     `json:"id"`
}

// Error 表示 JSON-RPC 错误
type Error struct {
    Code    int         `json:"code"`
    Message string      `json:"message"`
    Data    interface{} `json:"data,omitempty"`
}
```

**性能优化点**：
- 使用 `json.RawMessage` 延迟解析，避免不必要的反序列化
- 对象池复用 Request/Response 结构体

### 2. Codec - 编解码器

```go
type Codec interface {
    // 编码请求
    EncodeRequest(req *Request) ([]byte, error)
    
    // 解码请求
    DecodeRequest(data []byte) (*Request, error)
    
    // 编码响应
    EncodeResponse(resp *Response) ([]byte, error)
    
    // 解码响应
    DecodeResponse(data []byte) (*Response, error)
}

type JSONCodec struct {
    reqPool  *sync.Pool // Request 对象池
    respPool *sync.Pool // Response 对象池
    bufPool  *sync.Pool // Buffer 对象池
}
```

**性能优化算法**：
1. **对象池模式**：使用 `sync.Pool` 复用对象
   ```go
   reqPool: &sync.Pool{
       New: func() interface{} {
           return &Request{}
       },
   }
   ```

2. **Buffer 池**：复用 bytes.Buffer 减少内存分配
   ```go
   bufPool: &sync.Pool{
       New: func() interface{} {
           return new(bytes.Buffer)
       },
   }
   ```

3. **零拷贝技巧**：直接操作 `[]byte`，避免字符串转换

### 3. Connection Pool - 连接池

```go
type ConnPool struct {
    address    string
    maxIdle    int           // 最大空闲连接数
    maxActive  int           // 最大活跃连接数
    idleConns  chan net.Conn // 空闲连接队列
    activeNum  int32         // 当前活跃连接数（原子操作）
    mu         sync.Mutex
}

func (p *ConnPool) Get() (net.Conn, error)
func (p *ConnPool) Put(conn net.Conn) error
func (p *ConnPool) Close() error
```

**性能优化算法**：
1. **Channel 作为队列**：使用 buffered channel 实现无锁的空闲连接队列
2. **原子操作**：使用 `atomic` 包管理活跃连接计数，避免锁竞争
3. **连接健康检查**：定期检查空闲连接的有效性
4. **快速路径优化**：优先从 channel 获取连接，避免创建新连接

```go
// 快速路径：从空闲队列获取
select {
case conn := <-p.idleConns:
    return conn, nil
default:
    // 慢速路径：创建新连接
}
```

### 4. Goroutine Pool - 协程池

```go
type GoroutinePool struct {
    workers   int              // 工作协程数量
    taskQueue chan func()      // 任务队列
    wg        sync.WaitGroup   // 等待所有任务完成
    once      sync.Once        // 确保只初始化一次
    closed    int32            // 关闭标志（原子操作）
}

func (p *GoroutinePool) Submit(task func()) error
func (p *GoroutinePool) Close()
```

**性能优化算法**：
1. **固定数量的 Worker**：避免频繁创建销毁 goroutine
   ```go
   for i := 0; i < p.workers; i++ {
       go p.worker()
   }
   ```

2. **任务队列**：使用 buffered channel 作为任务队列，平滑流量
3. **优雅关闭**：使用 `sync.WaitGroup` 等待所有任务完成
4. **原子标志**：使用 `atomic` 包管理关闭状态

### 5. Service Registry - 服务注册表

```go
type ServiceRegistry struct {
    services map[string]*serviceType // 服务映射
    mu       sync.RWMutex            // 读写锁
}

type serviceType struct {
    name    string                    // 服务名称
    rcvr    reflect.Value             // 服务实例
    methods map[string]*methodType    // 方法映射
}

type methodType struct {
    method    reflect.Method  // 方法反射信息
    ArgType   reflect.Type    // 参数类型
    ReplyType reflect.Type    // 返回类型
}

func (r *ServiceRegistry) Register(service interface{}) error
func (r *ServiceRegistry) Call(ctx context.Context, serviceName, methodName string, args interface{}) (interface{}, error)
```

**性能优化算法**：
1. **反射缓存**：注册时提取并缓存所有反射信息
   ```go
   // 注册时缓存
   for i := 0; i < typ.NumMethod(); i++ {
       method := typ.Method(i)
       // 缓存方法信息
       methods[method.Name] = &methodType{...}
   }
   ```

2. **读写锁优化**：使用 `sync.RWMutex`，读多写少场景下性能更好
3. **类型验证**：注册时验证方法签名，运行时无需检查
4. **快速查找**：使用 map 实现 O(1) 的方法查找

### 6. Server - RPC 服务器

```go
type Server struct {
    registry   *ServiceRegistry
    pool       *GoroutinePool
    codec      Codec
    listener   net.Listener
    mu         sync.Mutex
    shutdown   int32 // 原子操作
}

func NewServer(workers int) *Server
func (s *Server) Register(service interface{}) error
func (s *Server) Serve(network, address string) error
func (s *Server) handleConn(conn net.Conn)
func (s *Server) Shutdown(ctx context.Context) error
```

**性能优化算法**：
1. **协程池处理连接**：避免为每个连接创建 goroutine
2. **流式处理**：使用 `bufio.Reader/Writer` 减少系统调用
3. **批量读取**：一次读取多个请求，减少 I/O 次数
4. **优雅关闭**：使用 context 控制超时

### 7. Client - RPC 客户端

```go
type Client struct {
    connPool *ConnPool
    codec    Codec
    mu       sync.Mutex
    seq      uint64        // 请求序列号（原子递增）
    pending  map[uint64]*Call // 待处理的调用
}

type Call struct {
    ServiceMethod string
    Args          interface{}
    Reply         interface{}
    Error         error
    Done          chan *Call
}

func NewClient(address string, maxIdle, maxActive int) (*Client, error)
func (c *Client) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error
func (c *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call
func (c *Client) Close() error
```

**性能优化算法**：
1. **连接复用**：使用连接池避免频繁建立连接
2. **异步调用**：支持 `Go()` 方法实现异步 RPC
3. **请求管道化**：多个请求可以在同一连接上并发发送
4. **原子序列号**：使用 `atomic.AddUint64` 生成唯一 ID

## Data Models

### 消息流转

```
Client                          Server
  │                               │
  │  1. 获取连接（连接池）          │
  ├──────────────────────────────►│
  │                               │
  │  2. 编码请求（对象池）          │
  │     Request{                  │
  │       Method: "Add",          │
  │       Params: [1, 2]          │
  │     }                         │
  ├──────────────────────────────►│
  │                               │  3. 解码请求（对象池）
  │                               │
  │                               │  4. 查找服务（反射缓存）
  │                               │
  │                               │  5. 调用方法（协程池）
  │                               │     result = service.Add(1, 2)
  │                               │
  │  6. 编码响应（对象池）          │
  │     Response{                 │
  │       Result: 3               │
  │     }                         │
  │◄──────────────────────────────┤
  │                               │
  │  7. 解码响应                   │
  │                               │
  │  8. 归还连接（连接池）          │
  │                               │
```

## Error Handling

### 错误码定义

```go
const (
    ErrCodeParse          = -32700 // 解析错误
    ErrCodeInvalidRequest = -32600 // 无效请求
    ErrCodeMethodNotFound = -32601 // 方法未找到
    ErrCodeInvalidParams  = -32602 // 无效参数
    ErrCodeInternal       = -32603 // 内部错误
)
```

### 错误处理策略

1. **网络错误**：自动重试（最多 3 次），指数退避
2. **超时错误**：使用 context 控制，超时后取消请求
3. **协议错误**：返回标准 JSON-RPC 错误响应
4. **服务错误**：包装为 JSON-RPC 错误，保留原始错误信息

### 错误恢复

```go
// Panic 恢复
defer func() {
    if r := recover(); r != nil {
        err = fmt.Errorf("panic: %v", r)
        // 记录堆栈信息
        log.Printf("stack: %s", debug.Stack())
    }
}()
```

## Testing Strategy

### 1. 单元测试

- **Codec 测试**：验证编解码正确性和性能
- **连接池测试**：验证连接复用和并发安全
- **协程池测试**：验证任务调度和资源限制
- **服务注册测试**：验证反射调用和错误处理

### 2. 基准测试

```go
// 测试对象池性能
func BenchmarkWithPool(b *testing.B)
func BenchmarkWithoutPool(b *testing.B)

// 测试连接池性能
func BenchmarkConnPool(b *testing.B)

// 测试端到端性能
func BenchmarkE2E(b *testing.B)
```

### 3. 并发测试

```go
// 使用 race detector
go test -race ./...

// 并发调用测试
func TestConcurrentCalls(t *testing.T) {
    var wg sync.WaitGroup
    for i := 0; i < 1000; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            client.Call(...)
        }()
    }
    wg.Wait()
}
```

### 4. 性能指标

- **吞吐量**：每秒处理的请求数（QPS）
- **延迟**：P50、P95、P99 延迟
- **内存使用**：GC 次数和内存分配
- **CPU 使用**：CPU 利用率

## Performance Optimization Techniques

### 1. 对象池（sync.Pool）

**原理**：复用对象，减少 GC 压力

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
    req.Reset() // 重置状态
    requestPool.Put(req)
}()
```

**效果**：减少 50-70% 的内存分配

### 2. 连接池

**原理**：复用 TCP 连接，避免三次握手开销

**效果**：减少 80% 的连接建立时间

### 3. Goroutine 池

**原理**：限制并发数，避免 goroutine 爆炸

**效果**：稳定的内存使用，避免 OOM

### 4. 反射缓存

**原理**：注册时提取反射信息，运行时直接使用

```go
// 慢速：每次调用都反射
method := reflect.ValueOf(service).MethodByName(methodName)

// 快速：使用缓存的反射信息
method := cachedMethod.method
```

**效果**：减少 90% 的反射开销

### 5. 零拷贝

**原理**：直接操作 `[]byte`，避免字符串转换

```go
// 慢速：多次拷贝
str := string(data)
json.Unmarshal([]byte(str), &req)

// 快速：直接使用
json.Unmarshal(data, &req)
```

### 6. 批量处理

**原理**：一次处理多个请求，减少系统调用

```go
// 批量读取
reader := bufio.NewReaderSize(conn, 64*1024)

// 批量写入
writer := bufio.NewWriterSize(conn, 64*1024)
```

## Project Structure

```
rerpc/
├── go.mod
├── go.sum
├── README.md
├── codec.go           # 编解码器实现
├── protocol.go        # JSON-RPC 协议定义
├── pool.go            # 对象池实现
├── connpool.go        # 连接池实现
├── goroutine_pool.go  # 协程池实现
├── registry.go        # 服务注册表实现
├── server.go          # 服务器实现
├── client.go          # 客户端实现
├── error.go           # 错误定义
├── examples/
│   ├── simple/        # 简单示例
│   │   ├── server.go
│   │   └── client.go
│   └── benchmark/     # 性能测试
│       └── bench_test.go
└── tests/
    ├── codec_test.go
    ├── pool_test.go
    ├── server_test.go
    └── client_test.go
```

## Implementation Notes

1. **Go 版本**：要求 Go 1.18+（支持泛型，虽然本项目主要使用传统方式）
2. **依赖最小化**：仅使用标准库，展示纯 Go 实现
3. **代码注释**：每个性能优化点都有详细注释说明原理
4. **示例完整**：提供可运行的示例代码
5. **文档详细**：README 包含性能对比数据和使用说明
