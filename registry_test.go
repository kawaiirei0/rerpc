package rerpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// 测试服务：算术服务
type ArithService struct{}

type ArithArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

type ArithReply struct {
	Result int `json:"result"`
}

// Add 符合 RPC 规范的方法
func (a *ArithService) Add(ctx context.Context, args *ArithArgs, reply *ArithReply) error {
	reply.Result = args.A + args.B
	return nil
}

// Multiply 符合 RPC 规范的方法
func (a *ArithService) Multiply(ctx context.Context, args *ArithArgs, reply *ArithReply) error {
	reply.Result = args.A * args.B
	return nil
}

// Divide 带错误处理的方法
func (a *ArithService) Divide(ctx context.Context, args *ArithArgs, reply *ArithReply) error {
	if args.B == 0 {
		return fmt.Errorf("division by zero")
	}
	reply.Result = args.A / args.B
	return nil
}

// invalidMethod 不符合规范：未导出
func (a *ArithService) invalidMethod(ctx context.Context, args *ArithArgs, reply *ArithReply) error {
	return nil
}

// InvalidNoContext 不符合规范：缺少 context 参数
func (a *ArithService) InvalidNoContext(args *ArithArgs, reply *ArithReply) error {
	return nil
}

// InvalidNoPointer 不符合规范：参数不是指针
func (a *ArithService) InvalidNoPointer(ctx context.Context, args ArithArgs, reply *ArithReply) error {
	return nil
}

// InvalidNoError 不符合规范：没有返回 error
func (a *ArithService) InvalidNoError(ctx context.Context, args *ArithArgs, reply *ArithReply) {
}

// PanicService 用于测试 panic 恢复
type PanicService struct{}

type PanicArgs struct{}
type PanicReply struct{}

func (p *PanicService) PanicMethod(ctx context.Context, args *PanicArgs, reply *PanicReply) error {
	panic("intentional panic")
}

// 未导出的服务类型
type unexportedService struct{}

func (u *unexportedService) Method(ctx context.Context, args *ArithArgs, reply *ArithReply) error {
	return nil
}

// TestServiceRegistry_Register 测试服务注册功能
func TestServiceRegistry_Register(t *testing.T) {
	tests := []struct {
		name        string
		service     interface{}
		wantErr     bool
		errContains string
	}{
		{
			name:    "注册有效服务",
			service: new(ArithService),
			wantErr: false,
		},
		{
			name:        "注册未导出的服务",
			service:     new(unexportedService),
			wantErr:     true,
			errContains: "not exported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewServiceRegistry()
			err := registry.Register(tt.service)

			if (err != nil) != tt.wantErr {
				t.Errorf("Register() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !contains(err.Error(), tt.errContains) {
					t.Errorf("Register() error = %v, should contain %v", err, tt.errContains)
				}
			}
		})
	}
}

// TestServiceRegistry_RegisterName 测试使用自定义名称注册服务
func TestServiceRegistry_RegisterName(t *testing.T) {
	registry := NewServiceRegistry()

	// 注册服务
	err := registry.RegisterName("CustomArith", new(ArithService))
	if err != nil {
		t.Fatalf("RegisterName() error = %v", err)
	}

	// 验证服务已注册
	services := registry.ListServices()
	if len(services) != 1 || services[0] != "CustomArith" {
		t.Errorf("ListServices() = %v, want [CustomArith]", services)
	}
}

// TestServiceRegistry_RegisterDuplicate 测试重复注册
func TestServiceRegistry_RegisterDuplicate(t *testing.T) {
	registry := NewServiceRegistry()

	// 第一次注册
	err := registry.Register(new(ArithService))
	if err != nil {
		t.Fatalf("First Register() error = %v", err)
	}

	// 第二次注册相同服务
	err = registry.Register(new(ArithService))
	if err == nil {
		t.Error("Register() should return error for duplicate service")
	}
	if !contains(err.Error(), "already registered") {
		t.Errorf("Register() error = %v, should contain 'already registered'", err)
	}
}

// TestServiceRegistry_MethodExtraction 测试方法提取
func TestServiceRegistry_MethodExtraction(t *testing.T) {
	registry := NewServiceRegistry()

	err := registry.Register(new(ArithService))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// 获取服务方法
	methods, exists := registry.GetService("ArithService")
	if !exists {
		t.Fatal("GetService() service not found")
	}

	// 验证只提取了符合规范的方法
	expectedMethods := map[string]bool{
		"Add":      true,
		"Multiply": true,
		"Divide":   true,
	}

	if len(methods) != len(expectedMethods) {
		t.Errorf("GetService() returned %d methods, want %d", len(methods), len(expectedMethods))
	}

	for _, method := range methods {
		if !expectedMethods[method] {
			t.Errorf("Unexpected method: %s", method)
		}
	}

	// 验证不符合规范的方法未被提取
	invalidMethods := []string{"invalidMethod", "InvalidNoContext", "InvalidNoPointer", "InvalidNoError"}
	for _, invalid := range invalidMethods {
		for _, method := range methods {
			if method == invalid {
				t.Errorf("Invalid method %s should not be extracted", invalid)
			}
		}
	}
}

// TestServiceRegistry_Call 测试方法调用
func TestServiceRegistry_Call(t *testing.T) {
	registry := NewServiceRegistry()
	err := registry.Register(new(ArithService))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	tests := []struct {
		name        string
		serviceName string
		methodName  string
		args        interface{}
		wantResult  int
		wantErr     bool
		errCode     int
	}{
		{
			name:        "调用 Add 方法",
			serviceName: "ArithService",
			methodName:  "Add",
			args:        &ArithArgs{A: 10, B: 20},
			wantResult:  30,
			wantErr:     false,
		},
		{
			name:        "调用 Multiply 方法",
			serviceName: "ArithService",
			methodName:  "Multiply",
			args:        &ArithArgs{A: 5, B: 6},
			wantResult:  30,
			wantErr:     false,
		},
		{
			name:        "调用 Divide 方法",
			serviceName: "ArithService",
			methodName:  "Divide",
			args:        &ArithArgs{A: 20, B: 4},
			wantResult:  5,
			wantErr:     false,
		},
		{
			name:        "服务不存在",
			serviceName: "NonExistentService",
			methodName:  "Add",
			args:        &ArithArgs{A: 1, B: 2},
			wantErr:     true,
			errCode:     ErrCodeMethodNotFound,
		},
		{
			name:        "方法不存在",
			serviceName: "ArithService",
			methodName:  "NonExistentMethod",
			args:        &ArithArgs{A: 1, B: 2},
			wantErr:     true,
			errCode:     ErrCodeMethodNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 序列化参数
			argsData, _ := json.Marshal(tt.args)

			// 调用方法
			result, err := registry.Call(context.Background(), tt.serviceName, tt.methodName, argsData)

			if (err != nil) != tt.wantErr {
				t.Errorf("Call() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				// 验证错误类型
				if rpcErr, ok := err.(*Error); ok {
					if rpcErr.Code != tt.errCode {
						t.Errorf("Call() error code = %d, want %d", rpcErr.Code, tt.errCode)
					}
				} else {
					t.Errorf("Call() error type = %T, want *Error", err)
				}
				return
			}

			// 验证结果
			reply, ok := result.(*ArithReply)
			if !ok {
				t.Fatalf("Call() result type = %T, want *ArithReply", result)
			}

			if reply.Result != tt.wantResult {
				t.Errorf("Call() result = %d, want %d", reply.Result, tt.wantResult)
			}
		})
	}
}

// TestServiceRegistry_CallWithError 测试方法返回错误
func TestServiceRegistry_CallWithError(t *testing.T) {
	registry := NewServiceRegistry()
	err := registry.Register(new(ArithService))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// 调用会返回错误的方法（除以零）
	argsData, _ := json.Marshal(&ArithArgs{A: 10, B: 0})
	_, err = registry.Call(context.Background(), "ArithService", "Divide", argsData)

	if err == nil {
		t.Error("Call() should return error for division by zero")
	}

	// 验证错误类型
	if rpcErr, ok := err.(*Error); ok {
		if rpcErr.Code != ErrCodeInternal {
			t.Errorf("Call() error code = %d, want %d", rpcErr.Code, ErrCodeInternal)
		}
		if !contains(rpcErr.Data.(string), "division by zero") {
			t.Errorf("Call() error data = %v, should contain 'division by zero'", rpcErr.Data)
		}
	} else {
		t.Errorf("Call() error type = %T, want *Error", err)
	}
}

// TestServiceRegistry_CallWithInvalidParams 测试无效参数
func TestServiceRegistry_CallWithInvalidParams(t *testing.T) {
	registry := NewServiceRegistry()
	err := registry.Register(new(ArithService))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// 传递无效的 JSON 数据
	invalidJSON := json.RawMessage(`{"invalid": "data"`)

	_, err = registry.Call(context.Background(), "ArithService", "Add", invalidJSON)

	if err == nil {
		t.Error("Call() should return error for invalid JSON")
	}

	// 验证错误类型
	if rpcErr, ok := err.(*Error); ok {
		if rpcErr.Code != ErrCodeInvalidParams {
			t.Errorf("Call() error code = %d, want %d", rpcErr.Code, ErrCodeInvalidParams)
		}
	} else {
		t.Errorf("Call() error type = %T, want *Error", err)
	}
}

// TestServiceRegistry_CallWithPanic 测试 panic 恢复
func TestServiceRegistry_CallWithPanic(t *testing.T) {
	registry := NewServiceRegistry()
	err := registry.Register(new(PanicService))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	argsData, _ := json.Marshal(&PanicArgs{})
	_, err = registry.Call(context.Background(), "PanicService", "PanicMethod", argsData)

	if err == nil {
		t.Error("Call() should return error when method panics")
	}

	// 验证错误类型
	if rpcErr, ok := err.(*Error); ok {
		if rpcErr.Code != ErrCodeInternal {
			t.Errorf("Call() error code = %d, want %d", rpcErr.Code, ErrCodeInternal)
		}
		if !contains(rpcErr.Data.(string), "panic") {
			t.Errorf("Call() error data = %v, should contain 'panic'", rpcErr.Data)
		}
	} else {
		t.Errorf("Call() error type = %T, want *Error", err)
	}
}

// TestServiceRegistry_ConcurrentCalls 测试并发调用
func TestServiceRegistry_ConcurrentCalls(t *testing.T) {
	registry := NewServiceRegistry()
	err := registry.Register(new(ArithService))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	const numGoroutines = 100
	const numCallsPerGoroutine = 10

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numCallsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numCallsPerGoroutine; j++ {
				argsData, _ := json.Marshal(&ArithArgs{A: id, B: j})
				result, err := registry.Call(context.Background(), "ArithService", "Add", argsData)

				if err != nil {
					errors <- err
					continue
				}

				reply := result.(*ArithReply)
				expected := id + j
				if reply.Result != expected {
					errors <- fmt.Errorf("expected %d, got %d", expected, reply.Result)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// 检查是否有错误
	for err := range errors {
		t.Errorf("Concurrent call error: %v", err)
	}
}

// TestServiceRegistry_ListServices 测试列出所有服务
func TestServiceRegistry_ListServices(t *testing.T) {
	registry := NewServiceRegistry()

	// 注册多个服务
	registry.Register(new(ArithService))
	registry.RegisterName("Panic", new(PanicService))

	services := registry.ListServices()

	if len(services) != 2 {
		t.Errorf("ListServices() returned %d services, want 2", len(services))
	}

	expectedServices := map[string]bool{
		"ArithService": true,
		"Panic":        true,
	}

	for _, service := range services {
		if !expectedServices[service] {
			t.Errorf("Unexpected service: %s", service)
		}
	}
}

// TestServiceRegistry_GetService 测试获取服务信息
func TestServiceRegistry_GetService(t *testing.T) {
	registry := NewServiceRegistry()
	registry.Register(new(ArithService))

	// 获取存在的服务
	methods, exists := registry.GetService("ArithService")
	if !exists {
		t.Error("GetService() service should exist")
	}
	if len(methods) == 0 {
		t.Error("GetService() should return methods")
	}

	// 获取不存在的服务
	_, exists = registry.GetService("NonExistent")
	if exists {
		t.Error("GetService() should return false for non-existent service")
	}
}

// 辅助函数：检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || 
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
