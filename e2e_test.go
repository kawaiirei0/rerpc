package rerpc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// ===== 测试服务定义 =====

// TestService 测试服务
type TestService struct {
	callCount int
	mu        sync.Mutex
}

type AddArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

type AddReply struct {
	Result int `json:"result"`
}

// Add 加法方法
func (s *TestService) Add(ctx context.Context, args *AddArgs, reply *AddReply) error {
	s.mu.Lock()
	s.callCount++
	s.mu.Unlock()
	
	reply.Result = args.A + args.B
	return nil
}

type EchoArgs struct {
	Message string `json:"message"`
}

type EchoReply struct {
	Message string `json:"message"`
}

// Echo 回显方法
func (s *TestService) Echo(ctx context.Context, args *EchoArgs, reply *EchoReply) error {
	reply.Message = args.Message
	return nil
}

// ErrorMethod 返回错误的方法
func (s *TestService) ErrorMethod(ctx context.Context, args *EchoArgs, reply *EchoReply) error {
	return errors.New("intentional error")
}

// GetCallCount 获取调用次数
func (s *TestService) GetCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callCount
}

// ===== 端到端测试 =====

// TestE2E_BasicCall 测试基本的 RPC 调用
func TestE2E_BasicCall(t *testing.T) {
	// 启动服务器
	server := NewServer(10)
	service := &TestService{}
	if err := server.Register(service); err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}
	
	go server.Serve("tcp", "localhost:19001")
	defer server.Close()
	
	time.Sleep(100 * time.Millisecond) // 等待服务器启动
	
	// 创建客户端
	client, err := NewClient(ClientConfig{
		Network:     "tcp",
		Address:     "localhost:19001",
		MaxIdle:     5,
		MaxActive:   10,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	// 执行调用
	args := &AddArgs{A: 10, B: 20}
	reply := &AddReply{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := client.Call(ctx, "TestService.Add", args, reply); err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	
	// 验证结果
	expected := 30
	if reply.Result != expected {
		t.Errorf("Expected result %d, got %d", expected, reply.Result)
	}
}

// TestE2E_MultipleCall 测试多次调用
func TestE2E_MultipleCalls(t *testing.T) {
	// 启动服务器
	server := NewServer(10)
	service := &TestService{}
	if err := server.Register(service); err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}
	
	go server.Serve("tcp", "localhost:19002")
	defer server.Close()
	
	time.Sleep(100 * time.Millisecond)
	
	// 创建客户端
	client, err := NewClient(ClientConfig{
		Network:     "tcp",
		Address:     "localhost:19002",
		MaxIdle:     5,
		MaxActive:   10,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	// 执行多次调用
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		args := &AddArgs{A: i, B: i + 1}
		reply := &AddReply{}
		
		if err := client.Call(ctx, "TestService.Add", args, reply); err != nil {
			t.Fatalf("Call %d failed: %v", i, err)
		}
		
		expected := i + (i + 1)
		if reply.Result != expected {
			t.Errorf("Call %d: expected %d, got %d", i, expected, reply.Result)
		}
	}
	
	// 验证调用次数
	if count := service.GetCallCount(); count != 10 {
		t.Errorf("Expected 10 calls, got %d", count)
	}
}

// TestE2E_ConcurrentCalls 测试并发调用
func TestE2E_ConcurrentCalls(t *testing.T) {
	// 启动服务器
	server := NewServer(50)
	service := &TestService{}
	if err := server.Register(service); err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}
	
	go server.Serve("tcp", "localhost:19003")
	defer server.Close()
	
	time.Sleep(100 * time.Millisecond)
	
	// 创建客户端
	client, err := NewClient(ClientConfig{
		Network:     "tcp",
		Address:     "localhost:19003",
		MaxIdle:     10,
		MaxActive:   50,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	// 并发调用
	concurrency := 50
	callsPerGoroutine := 10
	
	var wg sync.WaitGroup
	errChan := make(chan error, concurrency*callsPerGoroutine)
	
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			ctx := context.Background()
			for j := 0; j < callsPerGoroutine; j++ {
				args := &AddArgs{A: id, B: j}
				reply := &AddReply{}
				
				if err := client.Call(ctx, "TestService.Add", args, reply); err != nil {
					errChan <- err
					return
				}
				
				expected := id + j
				if reply.Result != expected {
					errChan <- errors.New("result mismatch")
					return
				}
			}
		}(i)
	}
	
	wg.Wait()
	close(errChan)
	
	// 检查错误
	for err := range errChan {
		t.Errorf("Concurrent call failed: %v", err)
	}
	
	// 验证调用次数
	expectedCalls := concurrency * callsPerGoroutine
	if count := service.GetCallCount(); count != expectedCalls {
		t.Errorf("Expected %d calls, got %d", expectedCalls, count)
	}
}

// TestE2E_ErrorHandling 测试错误处理
func TestE2E_ErrorHandling(t *testing.T) {
	// 启动服务器
	server := NewServer(10)
	service := &TestService{}
	if err := server.Register(service); err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}
	
	go server.Serve("tcp", "localhost:19004")
	defer server.Close()
	
	time.Sleep(100 * time.Millisecond)
	
	// 创建客户端
	client, err := NewClient(ClientConfig{
		Network:     "tcp",
		Address:     "localhost:19004",
		MaxIdle:     5,
		MaxActive:   10,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	ctx := context.Background()
	
	// 测试 1: 调用不存在的方法
	t.Run("MethodNotFound", func(t *testing.T) {
		args := &AddArgs{A: 1, B: 2}
		reply := &AddReply{}
		
		err := client.Call(ctx, "TestService.NonExistent", args, reply)
		if err == nil {
			t.Error("Expected error for non-existent method")
		}
		
		// 检查是否是 RPC 错误
		var rpcErr *Error
		if errors.As(err, &rpcErr) {
			if rpcErr.Code != ErrCodeMethodNotFound {
				t.Errorf("Expected error code %d, got %d", ErrCodeMethodNotFound, rpcErr.Code)
			}
		}
	})
	
	// 测试 2: 服务方法返回错误
	t.Run("ServiceError", func(t *testing.T) {
		args := &EchoArgs{Message: "test"}
		reply := &EchoReply{}
		
		err := client.Call(ctx, "TestService.ErrorMethod", args, reply)
		if err == nil {
			t.Error("Expected error from ErrorMethod")
		}
	})
	
	// 测试 3: 无效的服务名格式
	t.Run("InvalidServiceName", func(t *testing.T) {
		args := &AddArgs{A: 1, B: 2}
		reply := &AddReply{}
		
		err := client.Call(ctx, "InvalidFormat", args, reply)
		if err == nil {
			t.Error("Expected error for invalid service name format")
		}
	})
}

// TestE2E_Timeout 测试超时处理
func TestE2E_Timeout(t *testing.T) {
	// 启动服务器
	server := NewServer(10)
	service := &TestService{}
	if err := server.Register(service); err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}
	
	go server.Serve("tcp", "localhost:19005")
	defer server.Close()
	
	time.Sleep(100 * time.Millisecond)
	
	// 创建客户端
	client, err := NewClient(ClientConfig{
		Network:     "tcp",
		Address:     "localhost:19005",
		MaxIdle:     5,
		MaxActive:   10,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	// 使用非常短的超时
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	
	time.Sleep(10 * time.Millisecond) // 确保超时
	
	args := &AddArgs{A: 1, B: 2}
	reply := &AddReply{}
	
	err = client.Call(ctx, "TestService.Add", args, reply)
	if err == nil {
		t.Error("Expected timeout error")
	}
	
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}
}

// TestE2E_AsyncCalls 测试异步调用
func TestE2E_AsyncCalls(t *testing.T) {
	// 启动服务器
	server := NewServer(10)
	service := &TestService{}
	if err := server.Register(service); err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}
	
	go server.Serve("tcp", "localhost:19006")
	defer server.Close()
	
	time.Sleep(100 * time.Millisecond)
	
	// 创建客户端
	client, err := NewClient(ClientConfig{
		Network:     "tcp",
		Address:     "localhost:19006",
		MaxIdle:     5,
		MaxActive:   10,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	// 发起多个异步调用
	calls := make([]*Call, 0, 10)
	for i := 0; i < 10; i++ {
		args := &AddArgs{A: i, B: i + 1}
		reply := &AddReply{}
		
		call := client.Go("TestService.Add", args, reply, nil)
		calls = append(calls, call)
	}
	
	// 等待所有调用完成
	for i, call := range calls {
		<-call.Done
		
		if call.Error != nil {
			t.Errorf("Async call %d failed: %v", i, call.Error)
			continue
		}
		
		reply := call.Reply.(*AddReply)
		args := call.Args.(*AddArgs)
		expected := args.A + args.B
		
		if reply.Result != expected {
			t.Errorf("Async call %d: expected %d, got %d", i, expected, reply.Result)
		}
	}
}

// TestE2E_BatchCalls 测试批量调用
func TestE2E_BatchCalls(t *testing.T) {
	// 启动服务器
	server := NewServer(10)
	service := &TestService{}
	if err := server.Register(service); err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}
	
	go server.Serve("tcp", "localhost:19007")
	defer server.Close()
	
	time.Sleep(100 * time.Millisecond)
	
	// 创建客户端
	client, err := NewClient(ClientConfig{
		Network:     "tcp",
		Address:     "localhost:19007",
		MaxIdle:     5,
		MaxActive:   10,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	// 准备批量调用
	calls := make([]*Call, 0, 5)
	for i := 0; i < 5; i++ {
		calls = append(calls, &Call{
			ServiceMethod: "TestService.Add",
			Args:          &AddArgs{A: i * 10, B: i * 10 + 5},
			Reply:         &AddReply{},
		})
	}
	
	// 执行批量调用
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := client.Batch(ctx, calls); err != nil {
		t.Fatalf("Batch call failed: %v", err)
	}
	
	// 验证结果
	for i, call := range calls {
		if call.Error != nil {
			t.Errorf("Batch call %d failed: %v", i, call.Error)
			continue
		}
		
		reply := call.Reply.(*AddReply)
		args := call.Args.(*AddArgs)
		expected := args.A + args.B
		
		if reply.Result != expected {
			t.Errorf("Batch call %d: expected %d, got %d", i, expected, reply.Result)
		}
	}
}

// TestE2E_ConnectionPoolReuse 测试连接池复用
func TestE2E_ConnectionPoolReuse(t *testing.T) {
	// 启动服务器
	server := NewServer(10)
	service := &TestService{}
	if err := server.Register(service); err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}
	
	go server.Serve("tcp", "localhost:19008")
	defer server.Close()
	
	time.Sleep(100 * time.Millisecond)
	
	// 创建客户端（小连接池）
	client, err := NewClient(ClientConfig{
		Network:     "tcp",
		Address:     "localhost:19008",
		MaxIdle:     2,
		MaxActive:   5,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	ctx := context.Background()
	
	// 执行多次调用，验证连接复用
	for i := 0; i < 20; i++ {
		args := &AddArgs{A: i, B: 1}
		reply := &AddReply{}
		
		if err := client.Call(ctx, "TestService.Add", args, reply); err != nil {
			t.Fatalf("Call %d failed: %v", i, err)
		}
	}
	
	// 检查连接池统计
	stats := client.Stats()
	
	// 活跃连接应该小于等于 MaxActive
	if stats.PoolStats.ActiveCount > 5 {
		t.Errorf("Active connections %d exceeds MaxActive 5", stats.PoolStats.ActiveCount)
	}
	
	// 应该有空闲连接（说明连接被复用）
	if stats.PoolStats.IdleCount == 0 {
		t.Error("Expected idle connections, got 0")
	}
}

// TestE2E_ServerShutdown 测试服务器优雅关闭
func TestE2E_ServerShutdown(t *testing.T) {
	// 启动服务器
	server := NewServer(10)
	service := &TestService{}
	if err := server.Register(service); err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}
	
	go server.Serve("tcp", "localhost:19009")
	
	time.Sleep(100 * time.Millisecond)
	
	// 创建客户端
	client, err := NewClient(ClientConfig{
		Network:     "tcp",
		Address:     "localhost:19009",
		MaxIdle:     5,
		MaxActive:   10,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	
	// 执行一次调用确保服务器正常
	ctx := context.Background()
	args := &AddArgs{A: 1, B: 2}
	reply := &AddReply{}
	
	if err := client.Call(ctx, "TestService.Add", args, reply); err != nil {
		t.Fatalf("Initial call failed: %v", err)
	}
	
	// 关闭客户端连接
	client.Close()
	
	// 优雅关闭服务器
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Server shutdown failed: %v", err)
	}
	
	// 验证服务器已关闭
	if !server.IsShutdown() {
		t.Error("Server should be shutdown")
	}
}

// TestE2E_LargePayload 测试大负载传输
func TestE2E_LargePayload(t *testing.T) {
	// 启动服务器
	server := NewServer(10)
	service := &TestService{}
	if err := server.Register(service); err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}
	
	go server.Serve("tcp", "localhost:19010")
	defer server.Close()
	
	time.Sleep(100 * time.Millisecond)
	
	// 创建客户端
	client, err := NewClient(ClientConfig{
		Network:     "tcp",
		Address:     "localhost:19010",
		MaxIdle:     5,
		MaxActive:   10,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	// 创建大负载（10KB 字符串）
	largeMessage := string(make([]byte, 10*1024))
	args := &EchoArgs{Message: largeMessage}
	reply := &EchoReply{}
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := client.Call(ctx, "TestService.Echo", args, reply); err != nil {
		t.Fatalf("Large payload call failed: %v", err)
	}
	
	// 验证数据完整性
	if reply.Message != largeMessage {
		t.Error("Large payload data mismatch")
	}
}

// TestE2E_JSONEncoding 测试 JSON 编码正确性
func TestE2E_JSONEncoding(t *testing.T) {
	// 启动服务器
	server := NewServer(10)
	service := &TestService{}
	if err := server.Register(service); err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}
	
	go server.Serve("tcp", "localhost:19011")
	defer server.Close()
	
	time.Sleep(100 * time.Millisecond)
	
	// 创建客户端
	client, err := NewClient(ClientConfig{
		Network:     "tcp",
		Address:     "localhost:19011",
		MaxIdle:     5,
		MaxActive:   10,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	ctx := context.Background()
	
	// 测试特殊字符
	specialChars := []string{
		"Hello, 世界!",
		`{"nested": "json"}`,
		"Line1\nLine2\nLine3",
		"Tab\tSeparated\tValues",
		`Quote: "test"`,
	}
	
	for i, msg := range specialChars {
		args := &EchoArgs{Message: msg}
		reply := &EchoReply{}
		
		if err := client.Call(ctx, "TestService.Echo", args, reply); err != nil {
			t.Errorf("Call %d failed: %v", i, err)
			continue
		}
		
		if reply.Message != msg {
			t.Errorf("Call %d: message mismatch\nExpected: %q\nGot: %q", i, msg, reply.Message)
		}
	}
}

// TestE2E_StressTest 压力测试
func TestE2E_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}
	
	// 启动服务器
	server := NewServer(100)
	service := &TestService{}
	if err := server.Register(service); err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}
	
	go server.Serve("tcp", "localhost:19012")
	defer server.Close()
	
	time.Sleep(100 * time.Millisecond)
	
	// 创建客户端
	client, err := NewClient(ClientConfig{
		Network:     "tcp",
		Address:     "localhost:19012",
		MaxIdle:     20,
		MaxActive:   100,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	
	// 压力测试：1000 个并发调用
	concurrency := 100
	callsPerGoroutine := 100
	totalCalls := concurrency * callsPerGoroutine
	
	var wg sync.WaitGroup
	errors := make(chan error, totalCalls)
	successCount := int32(0)
	
	startTime := time.Now()
	
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			ctx := context.Background()
			for j := 0; j < callsPerGoroutine; j++ {
				args := &AddArgs{A: id, B: j}
				reply := &AddReply{}
				
				if err := client.Call(ctx, "TestService.Add", args, reply); err != nil {
					errors <- err
					return
				}
				
				// 使用原子操作增加成功计数
				_ = reply // 避免未使用变量警告
				successCount++
			}
		}(i)
	}
	
	wg.Wait()
	close(errors)
	
	duration := time.Since(startTime)
	
	// 检查错误
	errorCount := 0
	for err := range errors {
		t.Logf("Call failed: %v", err)
		errorCount++
	}
	
	// 计算 QPS
	qps := float64(totalCalls) / duration.Seconds()
	
	t.Logf("Stress test results:")
	t.Logf("  Total calls: %d", totalCalls)
	t.Logf("  Successful: %d", successCount)
	t.Logf("  Failed: %d", errorCount)
	t.Logf("  Duration: %v", duration)
	t.Logf("  QPS: %.2f", qps)
	
	// 验证成功率
	successRate := float64(successCount) / float64(totalCalls) * 100
	if successRate < 99.0 {
		t.Errorf("Success rate %.2f%% is below 99%%", successRate)
	}
}
