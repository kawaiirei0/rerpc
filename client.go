package rerpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrClientClosed 表示客户端已关闭
	ErrClientClosed = errors.New("client is closed")

	// ErrTimeout 表示请求超时
	ErrTimeout = errors.New("request timeout")

	// ErrNoConnection 表示无法获取连接
	ErrNoConnection = errors.New("failed to get connection")
)

// Call 表示一个正在进行的 RPC 调用
// 支持同步和异步两种调用方式
type Call struct {
	ServiceMethod string      // 服务方法名（格式：Service.Method）
	Args          interface{} // 方法参数
	Reply         interface{} // 方法返回值
	Error         error       // 调用错误
	Done          chan *Call  // 调用完成通知 channel（异步调用使用）
	seq           uint64      // 请求序列号
}

// Client RPC 客户端
// 性能优化：
// 1. 使用连接池复用 TCP 连接
// 2. 使用对象池复用 Request/Response 对象
// 3. 使用 atomic 生成唯一的请求序列号
// 4. 支持请求管道化（多个请求并发发送）
type Client struct {
	connPool *ConnPool          // 连接池
	codec    Codec              // 编解码器
	mu       sync.Mutex         // 保护 pending 和 seq
	seq      uint64             // 请求序列号（原子递增）
	pending  map[uint64]*Call   // 待处理的调用映射
	closed   int32              // 关闭标志（原子操作）
	
	// 重试配置
	maxRetries  int           // 最大重试次数
	retryDelay  time.Duration // 重试延迟（指数退避）
}

// ClientConfig 客户端配置
type ClientConfig struct {
	Network     string        // 网络类型（如 "tcp"）
	Address     string        // 服务器地址（如 "localhost:8080"）
	MaxIdle     int           // 最大空闲连接数
	MaxActive   int           // 最大活跃连接数
	DialTimeout time.Duration // 连接超时时间
	MaxRetries  int           // 最大重试次数
	RetryDelay  time.Duration // 重试延迟
}

// NewClient 创建一个新的 RPC 客户端
// 参数：
//   - config: 客户端配置
// 返回：
//   - *Client: 客户端实例
//   - error: 错误信息
func NewClient(config ClientConfig) (*Client, error) {
	// 设置默认值
	if config.Network == "" {
		config.Network = "tcp"
	}
	if config.Address == "" {
		return nil, errors.New("address is required")
	}
	if config.MaxIdle <= 0 {
		config.MaxIdle = 10
	}
	if config.MaxActive <= 0 {
		config.MaxActive = 100
	}
	if config.DialTimeout <= 0 {
		config.DialTimeout = 5 * time.Second
	}
	if config.MaxRetries < 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = 100 * time.Millisecond
	}

	// 创建连接池
	connPool, err := NewConnPool(ConnPoolConfig{
		Network:     config.Network,
		Address:     config.Address,
		MaxIdle:     config.MaxIdle,
		MaxActive:   config.MaxActive,
		DialTimeout: config.DialTimeout,
		TestOnGet:   true, // 启用连接健康检查
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// 创建客户端
	client := &Client{
		connPool:    connPool,
		codec:       NewJSONCodec(nil), // 使用默认对象池
		pending:     make(map[uint64]*Call),
		maxRetries:  config.MaxRetries,
		retryDelay:  config.RetryDelay,
	}

	return client, nil
}

// nextSeq 生成下一个请求序列号
// 使用 atomic 操作确保线程安全
func (c *Client) nextSeq() uint64 {
	return atomic.AddUint64(&c.seq, 1)
}

// isClosed 检查客户端是否已关闭
func (c *Client) isClosed() bool {
	return atomic.LoadInt32(&c.closed) == 1
}

// Call 执行同步 RPC 调用
// 参数：
//   - ctx: 上下文，用于超时控制和取消
//   - serviceMethod: 服务方法名（格式：Service.Method）
//   - args: 方法参数
//   - reply: 方法返回值（指针类型）
// 返回：
//   - error: 错误信息
//
// 性能优化：
// 1. 从连接池获取连接，避免频繁建立连接
// 2. 使用对象池复用 Request/Response 对象
// 3. 支持 context 超时控制
// 4. 自动重试网络错误
func (c *Client) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	// 检查客户端是否已关闭
	if c.isClosed() {
		return ErrClientClosed
	}

	// 验证参数
	if serviceMethod == "" {
		return errors.New("service method is required")
	}
	if reply == nil {
		return errors.New("reply must not be nil")
	}

	// 创建 Call 对象
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          make(chan *Call, 1), // buffered channel，避免阻塞
	}

	// 执行调用（带重试）
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		// 检查 context 是否已取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 尝试执行调用
		err := c.doCall(ctx, call)
		if err == nil {
			// 调用成功
			return call.Error
		}

		lastErr = err

		// 判断是否需要重试
		if !c.shouldRetry(err) {
			return err
		}

		// 如果不是最后一次尝试，等待后重试
		if attempt < c.maxRetries {
			// 指数退避：每次重试延迟时间翻倍
			delay := c.retryDelay * time.Duration(1<<uint(attempt))
			
			select {
			case <-time.After(delay):
				// 继续重试
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return fmt.Errorf("call failed after %d retries: %w", c.maxRetries, lastErr)
}

// doCall 执行单次 RPC 调用
// 这是 Call 方法的核心实现，不包含重试逻辑
func (c *Client) doCall(ctx context.Context, call *Call) error {
	// 生成请求序列号
	seq := c.nextSeq()
	call.seq = seq

	// 注册待处理的调用
	c.mu.Lock()
	c.pending[seq] = call
	c.mu.Unlock()

	// 确保在函数返回时清理 pending
	defer func() {
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
	}()

	// 从连接池获取连接
	conn, err := c.connPool.Get()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoConnection, err)
	}

	// 确保连接被归还
	defer c.connPool.Put(conn)

	// 编码请求
	req := c.codec.(*JSONCodec).pool.GetRequest()
	defer c.codec.(*JSONCodec).pool.PutRequest(req)

	req.Jsonrpc = JSONRPCVersion
	req.Method = call.ServiceMethod
	req.ID = seq

	// 序列化参数
	if call.Args != nil {
		argsData, err := json.Marshal(call.Args)
		if err != nil {
			return fmt.Errorf("failed to marshal args: %w", err)
		}
		req.Params = argsData
	}

	// 编码请求消息
	reqData, err := c.codec.EncodeRequest(req)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	// 发送请求
	writer := bufio.NewWriter(conn)
	if _, err := writer.Write(reqData); err != nil {
		return fmt.Errorf("failed to write request: %w", err)
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush request: %w", err)
	}

	// 接收响应（带超时控制）
	respChan := make(chan error, 1)
	go func() {
		respChan <- c.receiveResponse(conn, call)
	}()

	// 等待响应或超时
	select {
	case err := <-respChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// receiveResponse 接收并处理响应
func (c *Client) receiveResponse(conn net.Conn, call *Call) error {
	// 读取响应
	reader := bufio.NewReader(conn)
	respData, err := reader.ReadBytes('\n')
	if err != nil {
		if err == io.EOF {
			return errors.New("connection closed by server")
		}
		return fmt.Errorf("failed to read response: %w", err)
	}

	// 解码响应
	resp, err := c.codec.DecodeResponse(respData)
	if err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	defer c.codec.(*JSONCodec).pool.PutResponse(resp)

	// 验证响应 ID
	respID, ok := resp.ID.(float64)
	if !ok {
		return errors.New("invalid response ID type")
	}
	if uint64(respID) != call.seq {
		return fmt.Errorf("response ID mismatch: expected %d, got %d", call.seq, uint64(respID))
	}

	// 处理错误响应
	if resp.Error != nil {
		call.Error = resp.Error
		return nil
	}

	// 反序列化结果
	if resp.Result != nil && call.Reply != nil {
		if err := json.Unmarshal(resp.Result, call.Reply); err != nil {
			return fmt.Errorf("failed to unmarshal result: %w", err)
		}
	}

	return nil
}

// shouldRetry 判断错误是否应该重试
func (c *Client) shouldRetry(err error) bool {
	if err == nil {
		return false
	}

	// 客户端已关闭，不重试
	if errors.Is(err, ErrClientClosed) {
		return false
	}

	// Context 取消或超时，不重试
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// 连接池耗尽，不重试
	if errors.Is(err, ErrPoolExhausted) {
		return false
	}

	// 网络错误，重试
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// 连接错误，重试
	if errors.Is(err, ErrNoConnection) || errors.Is(err, io.EOF) {
		return true
	}

	// 其他错误，不重试
	return false
}

// Go 执行异步 RPC 调用
// 参数：
//   - serviceMethod: 服务方法名（格式：Service.Method）
//   - args: 方法参数
//   - reply: 方法返回值（指针类型）
//   - done: 调用完成通知 channel（可选，如果为 nil 则创建新的）
// 返回：
//   - *Call: Call 对象，可以通过 Done channel 等待调用完成
//
// 性能优化：
// 1. 异步执行，不阻塞调用者
// 2. 支持请求管道化（多个请求并发发送）
// 3. 使用 buffered channel 避免 goroutine 泄漏
func (c *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	// 创建 Call 对象
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
	}

	// 如果没有提供 done channel，创建一个 buffered channel
	if done == nil {
		done = make(chan *Call, 1)
	} else {
		// 确保 done channel 至少有 1 个缓冲，避免 goroutine 泄漏
		if cap(done) == 0 {
			panic("rerpc: done channel is unbuffered")
		}
	}
	call.Done = done

	// 在新的 goroutine 中执行调用
	go func() {
		// 使用默认的超时时间（30 秒）
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// 执行调用
		err := c.Call(ctx, serviceMethod, args, reply)
		if err != nil && call.Error == nil {
			// 如果 Call 返回错误但 call.Error 为空，设置错误
			call.Error = err
		}

		// 通知调用完成
		call.Done <- call
	}()

	return call
}

// GoWithContext 执行异步 RPC 调用（支持自定义 context）
// 参数：
//   - ctx: 上下文，用于超时控制和取消
//   - serviceMethod: 服务方法名（格式：Service.Method）
//   - args: 方法参数
//   - reply: 方法返回值（指针类型）
//   - done: 调用完成通知 channel（可选，如果为 nil 则创建新的）
// 返回：
//   - *Call: Call 对象，可以通过 Done channel 等待调用完成
func (c *Client) GoWithContext(ctx context.Context, serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	// 创建 Call 对象
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
	}

	// 如果没有提供 done channel，创建一个 buffered channel
	if done == nil {
		done = make(chan *Call, 1)
	} else {
		// 确保 done channel 至少有 1 个缓冲，避免 goroutine 泄漏
		if cap(done) == 0 {
			panic("rerpc: done channel is unbuffered")
		}
	}
	call.Done = done

	// 在新的 goroutine 中执行调用
	go func() {
		// 执行调用
		err := c.Call(ctx, serviceMethod, args, reply)
		if err != nil && call.Error == nil {
			// 如果 Call 返回错误但 call.Error 为空，设置错误
			call.Error = err
		}

		// 通知调用完成
		call.Done <- call
	}()

	return call
}

// Batch 批量执行多个 RPC 调用
// 参数：
//   - ctx: 上下文，用于超时控制和取消
//   - calls: 要执行的调用列表
// 返回：
//   - error: 如果有任何调用失败，返回第一个错误
//
// 性能优化：
// 1. 并发执行所有调用
// 2. 使用 WaitGroup 等待所有调用完成
// 3. 支持请求管道化
func (c *Client) Batch(ctx context.Context, calls []*Call) error {
	if len(calls) == 0 {
		return nil
	}

	// 检查客户端是否已关闭
	if c.isClosed() {
		return ErrClientClosed
	}

	// 使用 WaitGroup 等待所有调用完成
	var wg sync.WaitGroup
	errChan := make(chan error, len(calls))

	// 并发执行所有调用
	for _, call := range calls {
		wg.Add(1)
		go func(c *Client, call *Call) {
			defer wg.Done()

			// 执行调用
			err := c.Call(ctx, call.ServiceMethod, call.Args, call.Reply)
			if err != nil {
				call.Error = err
				errChan <- err
			}
		}(c, call)
	}

	// 等待所有调用完成
	wg.Wait()
	close(errChan)

	// 返回第一个错误（如果有）
	for err := range errChan {
		return err
	}

	return nil
}

// Close 关闭客户端，释放所有资源
// 这是一个优雅关闭过程：
// 1. 设置关闭标志，阻止新的调用
// 2. 等待所有待处理的调用完成（可选）
// 3. 关闭连接池
func (c *Client) Close() error {
	// 检查是否已经关闭
	if !atomic.CompareAndSwapInt32(&c.closed, 0, 1) {
		return nil // 已经关闭
	}

	// 关闭连接池
	if err := c.connPool.Close(); err != nil {
		return fmt.Errorf("failed to close connection pool: %w", err)
	}

	// 清理待处理的调用
	c.mu.Lock()
	for seq, call := range c.pending {
		call.Error = ErrClientClosed
		// 通知调用完成（如果有 Done channel）
		if call.Done != nil {
			select {
			case call.Done <- call:
			default:
				// channel 已满或已关闭，忽略
			}
		}
		delete(c.pending, seq)
	}
	c.mu.Unlock()

	return nil
}

// Ping 测试与服务器的连接是否正常
// 通过尝试获取连接并立即归还来验证连接池状态
func (c *Client) Ping() error {
	if c.isClosed() {
		return ErrClientClosed
	}

	return c.connPool.Ping()
}

// Stats 返回客户端的统计信息
type ClientStats struct {
	PendingCalls int       // 待处理的调用数量
	PoolStats    PoolStats // 连接池统计信息
	IsClosed     bool      // 是否已关闭
}

// Stats 获取客户端统计信息
func (c *Client) Stats() ClientStats {
	c.mu.Lock()
	pendingCount := len(c.pending)
	c.mu.Unlock()

	return ClientStats{
		PendingCalls: pendingCount,
		PoolStats:    c.connPool.Stats(),
		IsClosed:     c.isClosed(),
	}
}

// SetMaxRetries 设置最大重试次数
func (c *Client) SetMaxRetries(maxRetries int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxRetries = maxRetries
}

// SetRetryDelay 设置重试延迟时间
func (c *Client) SetRetryDelay(delay time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.retryDelay = delay
}

// GetMaxRetries 获取最大重试次数
func (c *Client) GetMaxRetries() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.maxRetries
}

// GetRetryDelay 获取重试延迟时间
func (c *Client) GetRetryDelay() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.retryDelay
}

// WaitForPending 等待所有待处理的调用完成
// 参数：
//   - timeout: 超时时间（0 表示无限等待）
// 返回：
//   - error: 如果超时返回错误
func (c *Client) WaitForPending(timeout time.Duration) error {
	if timeout == 0 {
		// 无限等待
		for {
			c.mu.Lock()
			count := len(c.pending)
			c.mu.Unlock()

			if count == 0 {
				return nil
			}

			time.Sleep(10 * time.Millisecond)
		}
	}

	// 带超时等待
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		count := len(c.pending)
		c.mu.Unlock()

		if count == 0 {
			return nil
		}

		time.Sleep(10 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for pending calls")
}

// CloseGracefully 优雅关闭客户端
// 等待所有待处理的调用完成后再关闭
// 参数：
//   - timeout: 等待超时时间
// 返回：
//   - error: 错误信息
func (c *Client) CloseGracefully(timeout time.Duration) error {
	// 等待所有待处理的调用完成
	if err := c.WaitForPending(timeout); err != nil {
		// 超时，强制关闭
		return c.Close()
	}

	// 正常关闭
	return c.Close()
}
