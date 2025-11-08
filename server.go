package rerpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Server RPC 服务器
// 集成 ServiceRegistry、GoroutinePool 和 Codec
// 性能优化：
// 1. 使用协程池处理连接，避免 goroutine 爆炸
// 2. 使用对象池复用 Request/Response 对象
// 3. 使用 bufio 减少系统调用
type Server struct {
	registry *ServiceRegistry // 服务注册表
	pool     *GoroutinePool   // 协程池
	codec    Codec            // 编解码器
	listener net.Listener     // TCP 监听器
	mu       sync.Mutex       // 保护 listener 和 shutdown 状态
	shutdown int32            // 关闭标志（原子操作）
	wg       sync.WaitGroup   // 等待所有连接处理完成
}

// NewServer 创建一个新的 RPC 服务器
// workers: 协程池的工作协程数量，用于限制并发连接处理数
// 如果 workers <= 0，默认使用 100
func NewServer(workers int) *Server {
	if workers <= 0 {
		workers = 100
	}

	return &Server{
		registry: NewServiceRegistry(),
		pool:     NewGoroutinePool(workers, workers*2), // 队列大小为 workers 的 2 倍
		codec:    NewJSONCodec(nil),                    // 使用默认对象池
		shutdown: 0,
	}
}

// Register 注册一个服务实例
// service: 服务实例，必须是指针类型
// 服务的所有导出方法都会被注册为 RPC 方法
// 方法签名必须符合：func(ctx context.Context, args *T, reply *R) error
func (s *Server) Register(service interface{}) error {
	return s.registry.Register(service)
}

// RegisterName 使用指定名称注册服务
// name: 服务名称
// service: 服务实例
func (s *Server) RegisterName(name string, service interface{}) error {
	return s.registry.RegisterName(name, service)
}

// Serve 启动 RPC 服务器，监听指定地址
// network: 网络类型，如 "tcp", "tcp4", "tcp6"
// address: 监听地址，如 ":8080", "localhost:8080"
// 此方法会阻塞直到服务器关闭或发生错误
func (s *Server) Serve(network, address string) error {
	// 检查是否已经在运行
	s.mu.Lock()
	if s.listener != nil {
		s.mu.Unlock()
		return fmt.Errorf("server is already running")
	}

	// 创建监听器
	listener, err := net.Listen(network, address)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to listen on %s:%s: %w", network, address, err)
	}

	s.listener = listener
	s.mu.Unlock()

	// 接受连接循环
	for {
		// 检查是否已关闭
		if atomic.LoadInt32(&s.shutdown) == 1 {
			break
		}

		// 接受新连接
		conn, err := listener.Accept()
		if err != nil {
			// 检查是否是因为关闭导致的错误
			if atomic.LoadInt32(&s.shutdown) == 1 {
				break
			}
			// 记录错误但继续接受其他连接
			// 在生产环境中应该使用日志库
			fmt.Printf("accept error: %v\n", err)
			continue
		}

		// 使用协程池处理连接
		// 性能优化：避免为每个连接创建新的 goroutine
		s.wg.Add(1)
		if err := s.pool.Submit(func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}); err != nil {
			// 协程池已关闭或出错，关闭连接
			s.wg.Done()
			conn.Close()
		}
	}

	return nil
}

// handleConn 处理单个客户端连接
// 实现完整的请求处理流程：读取 -> 解码 -> 调用 -> 编码 -> 响应
// 性能优化：
// 1. 使用 bufio 减少系统调用
// 2. 使用对象池复用 Request/Response 对象
// 3. 支持在同一连接上处理多个请求（keep-alive）
func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	// 使用 bufio 包装连接，减少系统调用
	// 性能优化：批量读写，减少 I/O 次数
	reader := bufio.NewReaderSize(conn, 32*1024) // 32KB 读缓冲
	writer := bufio.NewWriterSize(conn, 32*1024) // 32KB 写缓冲

	// 处理连接上的多个请求
	for {
		// 检查服务器是否已关闭
		if atomic.LoadInt32(&s.shutdown) == 1 {
			break
		}

		// 设置读取超时，避免连接长时间占用
		conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

		// 读取一行数据（JSON-RPC 消息以换行符分隔）
		data, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				// 客户端正常关闭连接
				break
			}
			// 读取错误，关闭连接
			fmt.Printf("read error: %v\n", err)
			break
		}

		// 处理请求并生成响应
		respData := s.processRequest(data)

		// 发送响应
		if respData != nil {
			// 设置写入超时
			conn.SetWriteDeadline(time.Now().Add(30 * time.Second))

			// 写入响应数据
			if _, err := writer.Write(respData); err != nil {
				fmt.Printf("write error: %v\n", err)
				break
			}

			// 刷新缓冲区，确保数据发送
			if err := writer.Flush(); err != nil {
				fmt.Printf("flush error: %v\n", err)
				break
			}
		}
	}
}

// processRequest 处理单个请求
// 实现请求解码 -> 服务调用 -> 响应编码的完整流程
// 返回编码后的响应数据
func (s *Server) processRequest(data []byte) []byte {
	// 解码请求
	// 性能优化：使用对象池复用 Request 对象
	req, err := s.codec.DecodeRequest(data)
	if err != nil {
		// 解码失败，返回错误响应
		return s.encodeErrorResponse(nil, err.(*Error))
	}
	defer PutRequest(req)

	// 解析服务名和方法名
	// 格式：ServiceName.MethodName
	serviceName, methodName, err := parseMethod(req.Method)
	if err != nil {
		return s.encodeErrorResponse(req.ID, NewMethodNotFoundError(req.Method))
	}

	// 创建上下文
	// 可以在这里添加超时控制、取消信号等
	ctx := context.Background()

	// 调用服务方法
	// 性能优化：使用缓存的反射信息，避免运行时反射开销
	result, err := s.registry.Call(ctx, serviceName, methodName, req.Params)
	if err != nil {
		// 服务调用失败
		if rpcErr, ok := err.(*Error); ok {
			return s.encodeErrorResponse(req.ID, rpcErr)
		}
		return s.encodeErrorResponse(req.ID, NewInternalError(err.Error()))
	}

	// 编码成功响应
	return s.encodeSuccessResponse(req.ID, result)
}

// encodeSuccessResponse 编码成功响应
func (s *Server) encodeSuccessResponse(id interface{}, result interface{}) []byte {
	// 序列化结果
	resultData, err := json.Marshal(result)
	if err != nil {
		return s.encodeErrorResponse(id, NewInternalError(fmt.Sprintf("failed to marshal result: %v", err)))
	}

	// 创建响应对象
	// 性能优化：使用对象池
	resp := GetResponse()
	defer PutResponse(resp)

	resp.Jsonrpc = JSONRPCVersion
	resp.Result = resultData
	resp.ID = id

	// 编码响应
	data, err := s.codec.EncodeResponse(resp)
	if err != nil {
		return s.encodeErrorResponse(id, NewInternalError(fmt.Sprintf("failed to encode response: %v", err)))
	}

	return data
}

// encodeErrorResponse 编码错误响应
func (s *Server) encodeErrorResponse(id interface{}, rpcErr *Error) []byte {
	// 创建响应对象
	// 性能优化：使用对象池
	resp := GetResponse()
	defer PutResponse(resp)

	resp.Jsonrpc = JSONRPCVersion
	resp.Error = rpcErr
	resp.ID = id

	// 编码响应
	data, err := s.codec.EncodeResponse(resp)
	if err != nil {
		// 编码失败，返回最基本的错误响应
		// 这种情况很少发生，通常是系统级错误
		fallbackResp := fmt.Sprintf(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error"},"id":%v}`+"\n", id)
		return []byte(fallbackResp)
	}

	return data
}

// parseMethod 解析方法名，格式：ServiceName.MethodName
// 返回服务名和方法名
func parseMethod(method string) (string, string, error) {
	// 查找点号分隔符
	dotIndex := -1
	for i := 0; i < len(method); i++ {
		if method[i] == '.' {
			dotIndex = i
			break
		}
	}

	if dotIndex == -1 || dotIndex == 0 || dotIndex == len(method)-1 {
		return "", "", fmt.Errorf("invalid method format: %s", method)
	}

	serviceName := method[:dotIndex]
	methodName := method[dotIndex+1:]

	return serviceName, methodName, nil
}

// Shutdown 优雅关闭服务器
// 停止接受新连接，等待所有正在处理的请求完成
// ctx: 用于控制关闭超时
// 如果 ctx 超时，会强制关闭服务器
func (s *Server) Shutdown(ctx context.Context) error {
	// 使用 CAS 操作设置关闭标志，确保只关闭一次
	if !atomic.CompareAndSwapInt32(&s.shutdown, 0, 1) {
		return fmt.Errorf("server is already shutdown")
	}

	// 关闭监听器，停止接受新连接
	s.mu.Lock()
	var listenerErr error
	if s.listener != nil {
		listenerErr = s.listener.Close()
		s.listener = nil
	}
	s.mu.Unlock()

	// 创建一个 channel 用于等待所有连接处理完成
	done := make(chan struct{})
	go func() {
		// 等待所有连接处理完成
		s.wg.Wait()
		close(done)
	}()

	// 等待所有连接完成或超时
	select {
	case <-done:
		// 所有连接已处理完成
		// 关闭协程池
		s.pool.Close()
		return listenerErr
	case <-ctx.Done():
		// 超时，强制关闭
		// 关闭协程池
		s.pool.Close()
		return fmt.Errorf("shutdown timeout: %w", ctx.Err())
	}
}

// Close 立即关闭服务器，不等待连接处理完成
// 这是一个非优雅的关闭方式，仅在必要时使用
func (s *Server) Close() error {
	// 设置关闭标志
	atomic.StoreInt32(&s.shutdown, 1)

	// 关闭监听器
	s.mu.Lock()
	var err error
	if s.listener != nil {
		err = s.listener.Close()
		s.listener = nil
	}
	s.mu.Unlock()

	// 关闭协程池
	s.pool.Close()

	return err
}

// IsShutdown 检查服务器是否已关闭
func (s *Server) IsShutdown() bool {
	return atomic.LoadInt32(&s.shutdown) == 1
}

// Addr 返回服务器监听的地址
// 如果服务器未启动，返回 nil
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}
