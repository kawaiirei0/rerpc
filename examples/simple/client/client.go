package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kawaiirei0/rerpc"
)

// AddArgs 加法参数（与服务器端定义一致）
type AddArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

// AddReply 加法返回值（与服务器端定义一致）
type AddReply struct {
	Result int `json:"result"`
}

// MultiplyArgs 乘法参数（与服务器端定义一致）
type MultiplyArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

// MultiplyReply 乘法返回值（与服务器端定义一致）
type MultiplyReply struct {
	Result int `json:"result"`
}

func main() {
	// 创建 RPC 客户端
	// 配置连接池参数
	client, err := rerpc.NewClient(rerpc.ClientConfig{
		Network:     "tcp",                  // 网络类型
		Address:     "localhost:8080",       // 服务器地址
		MaxIdle:     10,                     // 最大空闲连接数
		MaxActive:   100,                    // 最大活跃连接数
		DialTimeout: 5 * time.Second,        // 连接超时时间
		MaxRetries:  3,                      // 最大重试次数
		RetryDelay:  100 * time.Millisecond, // 重试延迟
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	log.Println("Connected to RPC server at localhost:8080")

	// 示例 1：同步调用 Add 方法
	fmt.Println("\n=== Example 1: Synchronous Add Call ===")
	{
		// 准备参数
		args := &AddArgs{A: 10, B: 20}
		reply := &AddReply{}

		// 创建带超时的 context
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// 执行同步调用
		// 格式：服务名.方法名
		if err := client.Call(ctx, "ArithService.Add", args, reply); err != nil {
			log.Printf("Add call failed: %v", err)
		} else {
			fmt.Printf("Result: %d + %d = %d\n", args.A, args.B, reply.Result)
		}
	}

	// 示例 2：同步调用 Multiply 方法
	fmt.Println("\n=== Example 2: Synchronous Multiply Call ===")
	{
		args := &MultiplyArgs{A: 5, B: 6}
		reply := &MultiplyReply{}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Call(ctx, "ArithService.Multiply", args, reply); err != nil {
			log.Printf("Multiply call failed: %v", err)
		} else {
			fmt.Printf("Result: %d * %d = %d\n", args.A, args.B, reply.Result)
		}
	}

	// 示例 3：异步调用
	fmt.Println("\n=== Example 3: Asynchronous Calls ===")
	{
		// 创建多个异步调用
		calls := make([]*rerpc.Call, 0, 5)

		// 发起 5 个异步加法调用
		for i := 0; i < 5; i++ {
			args := &AddArgs{A: i, B: i + 1}
			reply := &AddReply{}

			// Go 方法返回 *Call 对象，可以通过 Done channel 等待完成
			call := client.Go("ArithService.Add", args, reply, nil)
			calls = append(calls, call)
		}

		// 等待所有异步调用完成
		fmt.Println("Waiting for async calls to complete...")
		for i, call := range calls {
			// 从 Done channel 接收完成通知
			<-call.Done

			if call.Error != nil {
				log.Printf("Async call %d failed: %v", i, call.Error)
			} else {
				reply := call.Reply.(*AddReply)
				args := call.Args.(*AddArgs)
				fmt.Printf("Async call %d: %d + %d = %d\n", i, args.A, args.B, reply.Result)
			}
		}
	}

	// 示例 4：批量调用
	fmt.Println("\n=== Example 4: Batch Calls ===")
	{
		// 准备批量调用
		calls := make([]*rerpc.Call, 0, 3)

		// 添加多个调用
		calls = append(calls, &rerpc.Call{
			ServiceMethod: "ArithService.Add",
			Args:          &AddArgs{A: 100, B: 200},
			Reply:         &AddReply{},
		})

		calls = append(calls, &rerpc.Call{
			ServiceMethod: "ArithService.Multiply",
			Args:          &MultiplyArgs{A: 10, B: 20},
			Reply:         &MultiplyReply{},
		})

		calls = append(calls, &rerpc.Call{
			ServiceMethod: "ArithService.Add",
			Args:          &AddArgs{A: 50, B: 75},
			Reply:         &AddReply{},
		})

		// 执行批量调用（并发执行）
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := client.Batch(ctx, calls); err != nil {
			log.Printf("Batch call failed: %v", err)
		}

		// 打印结果
		for i, call := range calls {
			if call.Error != nil {
				log.Printf("Batch call %d failed: %v", i, call.Error)
			} else {
				switch reply := call.Reply.(type) {
				case *AddReply:
					args := call.Args.(*AddArgs)
					fmt.Printf("Batch call %d (Add): %d + %d = %d\n", i, args.A, args.B, reply.Result)
				case *MultiplyReply:
					args := call.Args.(*MultiplyArgs)
					fmt.Printf("Batch call %d (Multiply): %d * %d = %d\n", i, args.A, args.B, reply.Result)
				}
			}
		}
	}

	// 示例 5：连接池统计
	fmt.Println("\n=== Example 5: Client Statistics ===")
	{
		stats := client.Stats()
		fmt.Printf("Pending calls: %d\n", stats.PendingCalls)
		fmt.Printf("Active connections: %d\n", stats.PoolStats.ActiveCount)
		fmt.Printf("Idle connections: %d\n", stats.PoolStats.IdleCount)
		fmt.Printf("Client closed: %v\n", stats.IsClosed)
	}

	// 示例 6：错误处理
	fmt.Println("\n=== Example 6: Error Handling ===")
	{
		// 调用不存在的方法
		args := &AddArgs{A: 1, B: 2}
		reply := &AddReply{}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := client.Call(ctx, "ArithService.NonExistent", args, reply); err != nil {
			fmt.Printf("Expected error: %v\n", err)
		}
	}

	fmt.Println("\n=== All examples completed ===")
}
