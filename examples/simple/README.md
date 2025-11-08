# rerpc 简单示例

这个示例展示了如何使用 rerpc 库创建一个简单的 RPC 服务器和客户端。

## 功能说明

### 服务端 (server.go)

实现了一个算术服务 `ArithService`，提供两个方法：

- **Add**: 执行加法运算
- **Multiply**: 执行乘法运算

服务器特性：
- 使用协程池处理并发请求（100 个工作协程）
- 支持优雅关闭（Ctrl+C 触发）
- 自动服务注册和方法发现

### 客户端 (client.go)

演示了多种调用方式：

1. **同步调用**: 使用 `Call()` 方法执行阻塞式调用
2. **异步调用**: 使用 `Go()` 方法执行非阻塞式调用
3. **批量调用**: 使用 `Batch()` 方法并发执行多个调用
4. **统计信息**: 查看连接池和客户端状态
5. **错误处理**: 演示如何处理调用错误

客户端特性：
- 连接池复用 TCP 连接
- 自动重试机制（最多 3 次）
- 超时控制
- 并发安全

## 运行示例

### 1. 启动服务器

```bash
cd examples/simple
go run server.go
```

输出示例：
```
ArithService registered successfully
Available methods:
  - ArithService.Add
  - ArithService.Multiply
Starting RPC server on :8080...
RPC server is running on :8080
```

### 2. 运行客户端

在另一个终端中：

```bash
cd examples/simple
go run client.go
```

输出示例：
```
Connected to RPC server at localhost:8080

=== Example 1: Synchronous Add Call ===
Result: 10 + 20 = 30

=== Example 2: Synchronous Multiply Call ===
Result: 5 * 6 = 30

=== Example 3: Asynchronous Calls ===
Waiting for async calls to complete...
Async call 0: 0 + 1 = 1
Async call 1: 1 + 2 = 3
Async call 2: 2 + 3 = 5
Async call 3: 3 + 4 = 7
Async call 4: 4 + 5 = 9

=== Example 4: Batch Calls ===
Batch call 0 (Add): 100 + 200 = 300
Batch call 1 (Multiply): 10 * 20 = 200
Batch call 2 (Add): 50 + 75 = 125

=== Example 5: Client Statistics ===
Pending calls: 0
Active connections: 1
Idle connections: 1
Client closed: false

=== Example 6: Error Handling ===
Expected error: method ArithService.NonExistent not found

=== All examples completed ===
```

## 代码说明

### 定义服务

```go
// 1. 定义服务结构体
type ArithService struct{}

// 2. 定义参数和返回值类型
type AddArgs struct {
    A int `json:"a"`
    B int `json:"b"`
}

type AddReply struct {
    Result int `json:"result"`
}

// 3. 实现服务方法（必须符合签名规范）
func (s *ArithService) Add(ctx context.Context, args *AddArgs, reply *AddReply) error {
    reply.Result = args.A + args.B
    return nil
}
```

### 启动服务器

```go
// 创建服务器（100 个工作协程）
server := rerpc.NewServer(100)

// 注册服务
server.Register(new(ArithService))

// 启动监听
server.Serve("tcp", ":8080")
```

### 创建客户端

```go
// 创建客户端
client, err := rerpc.NewClient(rerpc.ClientConfig{
    Network:     "tcp",
    Address:     "localhost:8080",
    MaxIdle:     10,
    MaxActive:   100,
    DialTimeout: 5 * time.Second,
})
defer client.Close()
```

### 调用服务

```go
// 同步调用
args := &AddArgs{A: 10, B: 20}
reply := &AddReply{}
ctx := context.WithTimeout(context.Background(), 5*time.Second)
err := client.Call(ctx, "ArithService.Add", args, reply)

// 异步调用
call := client.Go("ArithService.Add", args, reply, nil)
<-call.Done // 等待完成
```

## 性能优化技术

这个简单示例已经应用了以下性能优化：

1. **连接池**: 客户端复用 TCP 连接，避免频繁建立连接
2. **协程池**: 服务器使用固定数量的协程处理请求，避免 goroutine 爆炸
3. **对象池**: 内部使用 sync.Pool 复用 Request/Response 对象
4. **批量处理**: 支持批量调用，减少网络往返次数
5. **异步调用**: 支持非阻塞式调用，提高并发能力

## 注意事项

1. **方法签名**: 服务方法必须符合 `func(ctx context.Context, args *T, reply *R) error` 签名
2. **导出方法**: 只有导出的方法（首字母大写）才会被注册为 RPC 方法
3. **指针参数**: args 和 reply 必须是指针类型
4. **超时控制**: 建议使用 context 设置超时时间
5. **错误处理**: 始终检查返回的错误

## 扩展练习

1. 添加更多算术运算（减法、除法等）
2. 实现带状态的服务（如计数器）
3. 添加参数验证（如除数不能为 0）
4. 实现自定义错误类型
5. 添加日志记录和监控
