package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kawaiirei0/rerpc"
)

// ArithService 算术服务
// 提供基本的算术运算功能
type ArithService struct{}

// AddArgs 加法参数
type AddArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

// AddReply 加法返回值
type AddReply struct {
	Result int `json:"result"`
}

// Add 执行加法运算
// 方法签名符合 RPC 规范：func(ctx context.Context, args *T, reply *R) error
func (s *ArithService) Add(ctx context.Context, args *AddArgs, reply *AddReply) error {
	// 执行加法运算
	reply.Result = args.A + args.B

	// 记录日志
	log.Printf("Add called: %d + %d = %d", args.A, args.B, reply.Result)

	return nil
}

// MultiplyArgs 乘法参数
type MultiplyArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

// MultiplyReply 乘法返回值
type MultiplyReply struct {
	Result int `json:"result"`
}

// Multiply 执行乘法运算
// 方法签名符合 RPC 规范：func(ctx context.Context, args *T, reply *R) error
func (s *ArithService) Multiply(ctx context.Context, args *MultiplyArgs, reply *MultiplyReply) error {
	// 执行乘法运算
	reply.Result = args.A * args.B

	// 记录日志
	log.Printf("Multiply called: %d * %d = %d", args.A, args.B, reply.Result)

	return nil
}

func main() {
	// 创建 RPC 服务器
	// 参数：协程池大小（100 个工作协程）
	server := rerpc.NewServer(100)

	// 注册算术服务
	// 服务名称将自动设置为 "ArithService"
	arithService := new(ArithService)
	if err := server.Register(arithService); err != nil {
		log.Fatalf("Failed to register service: %v", err)
	}

	log.Println("ArithService registered successfully")
	log.Println("Available methods:")
	log.Println("  - ArithService.Add")
	log.Println("  - ArithService.Multiply")

	// 启动服务器（在单独的 goroutine 中）
	go func() {
		log.Println("Starting RPC server on :8080...")
		if err := server.Serve("tcp", ":8080"); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)
	log.Println("RPC server is running on :8080")

	// 等待中断信号以优雅关闭服务器
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	log.Println("\nReceived interrupt signal, shutting down...")

	// 优雅关闭服务器
	// 设置 5 秒超时
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	} else {
		log.Println("Server shutdown successfully")
	}

	fmt.Println("Goodbye!")
}
