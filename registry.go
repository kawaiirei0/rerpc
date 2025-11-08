package rerpc

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"
	"unicode"
	"unicode/utf8"
)

// ServiceRegistry 服务注册表
// 使用 map 存储服务名称到服务实例的映射
// 性能优化：使用 sync.RWMutex 支持并发读取
type ServiceRegistry struct {
	services map[string]*serviceType // 服务名称 -> 服务类型
	mu       sync.RWMutex            // 读写锁，读多写少场景优化
}

// NewServiceRegistry 创建新的服务注册表
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		services: make(map[string]*serviceType),
	}
}

// serviceType 表示一个已注册的服务
// 性能优化：缓存反射信息，避免运行时重复反射
type serviceType struct {
	name    string                 // 服务名称
	rcvr    reflect.Value          // 服务实例的反射值
	typ     reflect.Type           // 服务类型
	methods map[string]*methodType // 方法名 -> 方法类型
}

// methodType 表示一个服务方法
// 性能优化：预先提取并缓存所有反射信息
type methodType struct {
	method    reflect.Method // 方法的反射信息
	ArgType   reflect.Type   // 参数类型
	ReplyType reflect.Type   // 返回值类型
}

// Register 注册一个服务实例
// 使用反射提取服务的所有导出方法，并验证方法签名
// 方法签名必须符合：func(ctx context.Context, args *T, reply *R) error
func (r *ServiceRegistry) Register(service interface{}) error {
	return r.register(service, "", false)
}

// RegisterName 使用指定名称注册服务
func (r *ServiceRegistry) RegisterName(name string, service interface{}) error {
	return r.register(service, name, true)
}

// register 内部注册方法
func (r *ServiceRegistry) register(service interface{}, name string, useName bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s := new(serviceType)
	s.typ = reflect.TypeOf(service)
	s.rcvr = reflect.ValueOf(service)

	// 获取服务名称
	sname := name
	if !useName {
		sname = reflect.Indirect(s.rcvr).Type().Name()
	}
	if sname == "" {
		return fmt.Errorf("rerpc.Register: no service name for type %s", s.typ.String())
	}

	// 验证服务名称是否导出
	if !useName && !isExported(sname) {
		return fmt.Errorf("rerpc.Register: type %s is not exported", sname)
	}

	// 检查服务是否已注册
	if _, present := r.services[sname]; present {
		return fmt.Errorf("rerpc.Register: service %s already registered", sname)
	}

	s.name = sname
	s.methods = make(map[string]*methodType)

	// 使用反射提取所有导出方法
	// 性能优化：在注册时一次性提取并缓存所有方法信息
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mtype := method.Type

		// 验证方法签名
		if err := r.validateMethod(method.Name, mtype); err != nil {
			continue // 跳过不符合规范的方法
		}

		// 缓存方法信息
		s.methods[method.Name] = &methodType{
			method:    method,
			ArgType:   mtype.In(2).Elem(), // 第2个参数是 *T，取 Elem() 得到 T
			ReplyType: mtype.In(3).Elem(), // 第3个参数是 *R，取 Elem() 得到 R
		}
	}

	if len(s.methods) == 0 {
		return fmt.Errorf("rerpc.Register: type %s has no exported methods of suitable type", sname)
	}

	// 注册服务
	r.services[sname] = s
	return nil
}

// validateMethod 验证方法签名是否符合 RPC 规范
// 规范：func(ctx context.Context, args *T, reply *R) error
// 参数：
//   - receiver (索引 0)
//   - context.Context (索引 1)
//   - *T 参数指针 (索引 2)
//   - *R 返回值指针 (索引 3)
// 返回值：error
func (r *ServiceRegistry) validateMethod(mname string, mtype reflect.Type) error {
	// 检查方法是否导出
	if !isExported(mname) {
		return fmt.Errorf("method %s is not exported", mname)
	}

	// 检查参数数量：receiver + context + args + reply = 4
	if mtype.NumIn() != 4 {
		return fmt.Errorf("method %s has wrong number of ins: %d", mname, mtype.NumIn())
	}

	// 检查第一个参数是否为 context.Context
	ctxType := mtype.In(1)
	if !ctxType.Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) {
		return fmt.Errorf("method %s first argument is not context.Context", mname)
	}

	// 检查第二个参数是否为指针
	argType := mtype.In(2)
	if argType.Kind() != reflect.Ptr {
		return fmt.Errorf("method %s args type not a pointer: %s", mname, argType)
	}

	// 检查第三个参数是否为指针
	replyType := mtype.In(3)
	if replyType.Kind() != reflect.Ptr {
		return fmt.Errorf("method %s reply type not a pointer: %s", mname, replyType)
	}

	// 检查返回值数量：必须只有一个 error
	if mtype.NumOut() != 1 {
		return fmt.Errorf("method %s has wrong number of outs: %d", mname, mtype.NumOut())
	}

	// 检查返回值类型是否为 error
	if returnType := mtype.Out(0); returnType != reflect.TypeOf((*error)(nil)).Elem() {
		return fmt.Errorf("method %s returns %s not error", mname, returnType.String())
	}

	return nil
}

// isExported 判断名称是否导出（首字母大写）
func isExported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// Call 通过反射调用服务方法
// serviceName: 服务名称
// methodName: 方法名称
// args: 参数（JSON 编码的数据）
// 返回：结果（JSON 编码）和错误
// 性能优化：使用缓存的反射信息，避免运行时反射开销
func (r *ServiceRegistry) Call(ctx context.Context, serviceName, methodName string, args json.RawMessage) (interface{}, error) {
	// 查找服务
	// 使用读锁，支持并发调用
	r.mu.RLock()
	service, ok := r.services[serviceName]
	r.mu.RUnlock()

	if !ok {
		return nil, NewMethodNotFoundError(fmt.Sprintf("service %s not found", serviceName))
	}

	// 查找方法
	// 性能优化：O(1) 的 map 查找
	method, ok := service.methods[methodName]
	if !ok {
		return nil, NewMethodNotFoundError(fmt.Sprintf("%s.%s", serviceName, methodName))
	}

	// 调用方法并处理 panic
	return r.call(ctx, service, method, args)
}

// call 执行实际的方法调用
// 包含 panic 恢复机制，确保服务稳定性
func (r *ServiceRegistry) call(ctx context.Context, service *serviceType, method *methodType, argsData json.RawMessage) (result interface{}, err error) {
	// Panic 恢复
	// 捕获方法执行中的 panic，转换为错误返回
	defer func() {
		if r := recover(); r != nil {
			err = NewInternalError(fmt.Sprintf("panic: %v\nstack: %s", r, debug.Stack()))
		}
	}()

	// 创建参数实例
	// 使用反射创建参数类型的新实例
	argv := reflect.New(method.ArgType)

	// 反序列化参数
	// 处理参数解析错误
	if len(argsData) > 0 {
		if err := json.Unmarshal(argsData, argv.Interface()); err != nil {
			return nil, NewInvalidParamsError(fmt.Sprintf("failed to unmarshal args: %v", err))
		}
	}

	// 创建返回值实例
	replyv := reflect.New(method.ReplyType)

	// 调用方法
	// 参数：receiver, context, args, reply
	// 性能优化：使用缓存的 Method，避免 MethodByName 查找
	returnValues := method.method.Func.Call([]reflect.Value{
		service.rcvr,
		reflect.ValueOf(ctx),
		argv,
		replyv,
	})

	// 检查返回的错误
	errInter := returnValues[0].Interface()
	if errInter != nil {
		return nil, NewInternalError(errInter.(error).Error())
	}

	// 返回结果
	// 返回 reply 的值（去掉指针）
	return replyv.Interface(), nil
}

// GetService 获取已注册的服务信息（用于调试）
func (r *ServiceRegistry) GetService(name string) (methods []string, exists bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	service, ok := r.services[name]
	if !ok {
		return nil, false
	}

	methods = make([]string, 0, len(service.methods))
	for name := range service.methods {
		methods = append(methods, name)
	}
	return methods, true
}

// ListServices 列出所有已注册的服务
func (r *ServiceRegistry) ListServices() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	services := make([]string, 0, len(r.services))
	for name := range r.services {
		services = append(services, name)
	}
	return services
}
