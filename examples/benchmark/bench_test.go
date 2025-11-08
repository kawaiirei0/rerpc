package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/kawaiirei0/rerpc"
)

// ===== 测试服务定义 =====

// BenchService 基准测试服务
type BenchService struct{}

type EchoArgs struct {
	Message string `json:"message"`
}

type EchoReply struct {
	Message string `json:"message"`
}

// Echo 回显方法
func (s *BenchService) Echo(ctx context.Context, args *EchoArgs, reply *EchoReply) error {
	reply.Message = args.Message
	return nil
}

type ComputeArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

type ComputeReply struct {
	Result int `json:"result"`
}

// Compute 计算方法
func (s *BenchService) Compute(ctx context.Context, args *ComputeArgs, reply *ComputeReply) error {
	reply.Result = args.A + args.B
	return nil
}

// ===== 对象池性能测试 =====

// BenchmarkObjectPool_WithPool 测试使用对象池的性能
func BenchmarkObjectPool_WithPool(b *testing.B) {
	pool := rerpc.NewObjectPool()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// 从对象池获取 Request
			req := pool.GetRequest()
			req.Jsonrpc = "2.0"
			req.Method = "Test.Method"
			req.ID = 1

			// 归还到对象池
			pool.PutRequest(req)
		}
	})
}

// BenchmarkObjectPool_WithoutPool 测试不使用对象池的性能
func BenchmarkObjectPool_WithoutPool(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// 每次都创建新对象
			req := &rerpc.Request{
				Jsonrpc: "2.0",
				Method:  "Test.Method",
				ID:      1,
			}
			_ = req
		}
	})
}

// BenchmarkObjectPool_Response_WithPool 测试 Response 对象池性能
func BenchmarkObjectPool_Response_WithPool(b *testing.B) {
	pool := rerpc.NewObjectPool()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp := pool.GetResponse()
			resp.Jsonrpc = "2.0"
			resp.Result = json.RawMessage(`{"result":42}`)
			resp.ID = 1

			pool.PutResponse(resp)
		}
	})
}

// BenchmarkObjectPool_Response_WithoutPool 测试不使用 Response 对象池的性能
func BenchmarkObjectPool_Response_WithoutPool(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp := &rerpc.Response{
				Jsonrpc: "2.0",
				Result:  json.RawMessage(`{"result":42}`),
				ID:      1,
			}
			_ = resp
		}
	})
}

// BenchmarkObjectPool_Buffer_WithPool 测试 Buffer 对象池性能
func BenchmarkObjectPool_Buffer_WithPool(b *testing.B) {
	pool := rerpc.NewObjectPool()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.GetBuffer()
			buf.WriteString("test data")
			pool.PutBuffer(buf)
		}
	})
}

// BenchmarkObjectPool_Buffer_WithoutPool 测试不使用 Buffer 对象池的性能
func BenchmarkObjectPool_Buffer_WithoutPool(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := new(bytes.Buffer)
			buf.WriteString("test data")
		}
	})
}

// ===== 编解码性能测试 =====

// BenchmarkCodec_EncodeRequest 测试请求编码性能
func BenchmarkCodec_EncodeRequest(b *testing.B) {
	codec := rerpc.NewJSONCodec(nil)
	req := &rerpc.Request{
		Jsonrpc: "2.0",
		Method:  "Test.Method",
		Params:  json.RawMessage(`{"a":1,"b":2}`),
		ID:      1,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := codec.EncodeRequest(req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCodec_DecodeRequest 测试请求解码性能
func BenchmarkCodec_DecodeRequest(b *testing.B) {
	codec := rerpc.NewJSONCodec(nil)
	data := []byte(`{"jsonrpc":"2.0","method":"Test.Method","params":{"a":1,"b":2},"id":1}` + "\n")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, err := codec.DecodeRequest(data)
		if err != nil {
			b.Fatal(err)
		}
		codec.ReleaseRequest(req)
	}
}

// BenchmarkCodec_EncodeResponse 测试响应编码性能
func BenchmarkCodec_EncodeResponse(b *testing.B) {
	codec := rerpc.NewJSONCodec(nil)
	resp := &rerpc.Response{
		Jsonrpc: "2.0",
		Result:  json.RawMessage(`{"result":42}`),
		ID:      1,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := codec.EncodeResponse(resp)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCodec_DecodeResponse 测试响应解码性能
func BenchmarkCodec_DecodeResponse(b *testing.B) {
	codec := rerpc.NewJSONCodec(nil)
	data := []byte(`{"jsonrpc":"2.0","result":{"result":42},"id":1}` + "\n")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := codec.DecodeResponse(data)
		if err != nil {
			b.Fatal(err)
		}
		codec.ReleaseResponse(resp)
	}
}

// ===== 连接池性能测试 =====

// BenchmarkConnPool_GetPut 测试连接池获取和归还性能
func BenchmarkConnPool_GetPut(b *testing.B) {
	// 启动测试服务器
	server := rerpc.NewServer(100)
	server.Register(new(BenchService))

	go server.Serve("tcp", "localhost:18080")
	defer server.Close()

	time.Sleep(100 * time.Millisecond) // 等待服务器启动

	// 创建连接池
	pool, err := rerpc.NewConnPool(rerpc.ConnPoolConfig{
		Network:     "tcp",
		Address:     "localhost:18080",
		MaxIdle:     10,
		MaxActive:   100,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			conn, err := pool.Get()
			if err != nil {
				b.Fatal(err)
			}
			pool.Put(conn)
		}
	})
}

// BenchmarkConnPool_Concurrent 测试连接池并发性能
func BenchmarkConnPool_Concurrent(b *testing.B) {
	// 启动测试服务器
	server := rerpc.NewServer(100)
	server.Register(new(BenchService))

	go server.Serve("tcp", "localhost:18081")
	defer server.Close()

	time.Sleep(100 * time.Millisecond)

	// 创建连接池
	pool, err := rerpc.NewConnPool(rerpc.ConnPoolConfig{
		Network:     "tcp",
		Address:     "localhost:18081",
		MaxIdle:     20,
		MaxActive:   200,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			conn, err := pool.Get()
			if err != nil {
				b.Fatal(err)
			}

			// 模拟使用连接
			time.Sleep(time.Microsecond)

			pool.Put(conn)
		}
	})
}

// ===== 协程池性能测试 =====

// BenchmarkGoroutinePool_Submit 测试协程池任务提交性能
func BenchmarkGoroutinePool_Submit(b *testing.B) {
	pool := rerpc.NewGoroutinePool(100, 1000)
	defer pool.Close()

	var counter int64

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool.Submit(func() {
			counter++
		})
	}

	// 等待所有任务完成
	pool.Close()
}

// BenchmarkGoroutinePool_vs_RawGoroutine 对比协程池和原生 goroutine
func BenchmarkGoroutinePool_vs_RawGoroutine(b *testing.B) {
	b.Run("WithPool", func(b *testing.B) {
		pool := rerpc.NewGoroutinePool(100, 1000)
		defer pool.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			pool.Submit(func() {
				// 模拟工作
				time.Sleep(time.Microsecond)
			})
		}
		pool.Close()
	})

	b.Run("WithoutPool", func(b *testing.B) {
		var wg sync.WaitGroup

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// 模拟工作
				time.Sleep(time.Microsecond)
			}()
		}
		wg.Wait()
	})
}

// ===== 端到端性能测试 =====

// BenchmarkE2E_SimpleCall 测试端到端简单调用性能
func BenchmarkE2E_SimpleCall(b *testing.B) {
	// 启动服务器
	server := rerpc.NewServer(100)
	server.Register(new(BenchService))

	go server.Serve("tcp", "localhost:18082")
	defer server.Close()

	time.Sleep(100 * time.Millisecond)

	// 创建客户端
	client, err := rerpc.NewClient(rerpc.ClientConfig{
		Network:     "tcp",
		Address:     "localhost:18082",
		MaxIdle:     10,
		MaxActive:   100,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	args := &ComputeArgs{A: 1, B: 2}
	reply := &ComputeReply{}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := client.Call(ctx, "BenchService.Compute", args, reply); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkE2E_ConcurrentCalls 测试端到端并发调用性能
func BenchmarkE2E_ConcurrentCalls(b *testing.B) {
	// 启动服务器
	server := rerpc.NewServer(200)
	server.Register(new(BenchService))

	go server.Serve("tcp", "localhost:18083")
	defer server.Close()

	time.Sleep(100 * time.Millisecond)

	// 创建客户端
	client, err := rerpc.NewClient(rerpc.ClientConfig{
		Network:     "tcp",
		Address:     "localhost:18083",
		MaxIdle:     20,
		MaxActive:   200,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		args := &ComputeArgs{A: 1, B: 2}
		reply := &ComputeReply{}
		ctx := context.Background()

		for pb.Next() {
			if err := client.Call(ctx, "BenchService.Compute", args, reply); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkE2E_LargePayload 测试大负载性能
func BenchmarkE2E_LargePayload(b *testing.B) {
	// 启动服务器
	server := rerpc.NewServer(100)
	server.Register(new(BenchService))

	go server.Serve("tcp", "localhost:18084")
	defer server.Close()

	time.Sleep(100 * time.Millisecond)

	// 创建客户端
	client, err := rerpc.NewClient(rerpc.ClientConfig{
		Network:     "tcp",
		Address:     "localhost:18084",
		MaxIdle:     10,
		MaxActive:   100,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	// 创建大负载（1KB 字符串）
	largeMessage := string(make([]byte, 1024))
	args := &EchoArgs{Message: largeMessage}
	reply := &EchoReply{}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := client.Call(ctx, "BenchService.Echo", args, reply); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkE2E_Throughput 测试吞吐量（QPS）
func BenchmarkE2E_Throughput(b *testing.B) {
	// 启动服务器
	server := rerpc.NewServer(200)
	server.Register(new(BenchService))

	go server.Serve("tcp", "localhost:18085")
	defer server.Close()

	time.Sleep(100 * time.Millisecond)

	// 创建客户端
	client, err := rerpc.NewClient(rerpc.ClientConfig{
		Network:     "tcp",
		Address:     "localhost:18085",
		MaxIdle:     50,
		MaxActive:   500,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	// 并发度：100
	concurrency := 100

	b.ResetTimer()

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			args := &ComputeArgs{A: 1, B: 2}
			reply := &ComputeReply{}
			ctx := context.Background()

			for j := 0; j < b.N/concurrency; j++ {
				if err := client.Call(ctx, "BenchService.Compute", args, reply); err != nil {
					b.Error(err)
					return
				}
			}
		}()
	}

	wg.Wait()
}

// ===== 并发安全测试 =====

// TestConcurrentSafety 测试并发安全性
func TestConcurrentSafety(t *testing.T) {
	// 启动服务器
	server := rerpc.NewServer(200)
	server.Register(new(BenchService))

	go server.Serve("tcp", "localhost:18086")
	defer server.Close()

	time.Sleep(100 * time.Millisecond)

	// 创建客户端
	client, err := rerpc.NewClient(rerpc.ClientConfig{
		Network:     "tcp",
		Address:     "localhost:18086",
		MaxIdle:     20,
		MaxActive:   200,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// 并发调用 1000 次
	concurrency := 100
	callsPerGoroutine := 10

	var wg sync.WaitGroup
	errors := make(chan error, concurrency*callsPerGoroutine)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < callsPerGoroutine; j++ {
				args := &ComputeArgs{A: id, B: j}
				reply := &ComputeReply{}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

				if err := client.Call(ctx, "BenchService.Compute", args, reply); err != nil {
					errors <- err
					cancel()
					return
				}

				// 验证结果
				expected := id + j
				if reply.Result != expected {
					errors <- err
					cancel()
					return
				}

				cancel()
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// 检查错误
	for err := range errors {
		t.Errorf("Concurrent call failed: %v", err)
	}
}
