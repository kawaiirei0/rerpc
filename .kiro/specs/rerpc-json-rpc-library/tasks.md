# Implementation Plan

- [x] 1. 初始化项目结构和核心协议定义





  - 创建 Go module（rerpc v1.0.0）
  - 实现 JSON-RPC 2.0 协议的消息结构（Request、Response、Error）
  - 定义标准错误码常量
  - 添加消息重置方法以支持对象池复用
  - _Requirements: 1.1, 3.1_

- [x] 2. 实现对象池和编解码器




- [x] 2.1 实现对象池管理器


  - 使用 sync.Pool 创建 Request、Response 和 Buffer 对象池
  - 实现对象获取和归还方法
  - 添加对象重置逻辑确保状态清理
  - _Requirements: 3.2, 4.4, 4.5_

- [x] 2.2 实现 JSON 编解码器


  - 实现 Codec 接口的 JSONCodec 结构
  - 实现 EncodeRequest/DecodeRequest 方法
  - 实现 EncodeResponse/DecodeResponse 方法
  - 集成对象池以复用 Request/Response 对象
  - 使用 json.RawMessage 实现延迟解析
  - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5_

- [ ]* 2.3 编写编解码器单元测试
  - 测试正常消息的编解码
  - 测试错误消息的处理
  - 测试对象池的复用效果
  - _Requirements: 3.2, 3.4_

- [x] 3. 实现连接池




- [x] 3.1 实现连接池核心逻辑


  - 创建 ConnPool 结构体，使用 channel 管理空闲连接
  - 实现 Get() 方法：优先从空闲队列获取，否则创建新连接
  - 实现 Put() 方法：归还连接到空闲队列
  - 使用 atomic 包管理活跃连接计数
  - 实现最大连接数限制
  - _Requirements: 2.4, 4.1, 4.2, 4.3_

- [x] 3.2 实现连接池管理功能


  - 实现 Close() 方法关闭所有连接
  - 添加连接健康检查机制
  - 实现连接超时和重连逻辑
  - _Requirements: 2.4, 4.1_

- [ ]* 3.3 编写连接池测试
  - 测试连接的获取和归还
  - 测试并发场景下的连接池安全性
  - 测试最大连接数限制
  - _Requirements: 4.1, 4.2, 4.3_

- [x] 4. 实现 Goroutine 池





- [x] 4.1 实现协程池核心功能


  - 创建 GoroutinePool 结构体，使用 channel 作为任务队列
  - 实现固定数量的 worker goroutine
  - 实现 Submit() 方法提交任务到队列
  - 使用 sync.WaitGroup 跟踪任务完成状态
  - _Requirements: 1.5, 5.1, 5.2, 5.3, 5.4_

- [x] 4.2 实现协程池优雅关闭


  - 实现 Close() 方法停止接收新任务
  - 等待所有进行中的任务完成
  - 使用 atomic 标志位标记关闭状态
  - _Requirements: 5.5_

- [ ]* 4.3 编写协程池测试
  - 测试任务提交和执行
  - 测试并发任务处理
  - 测试优雅关闭机制
  - _Requirements: 5.1, 5.2, 5.5_

- [x] 5. 实现服务注册表和反射调用




- [x] 5.1 实现服务注册功能


  - 创建 ServiceRegistry 结构体，使用 map 存储服务
  - 实现 Register() 方法注册服务实例
  - 使用反射提取服务的所有导出方法
  - 验证方法签名符合 RPC 规范（context, args, reply, error）
  - 缓存方法的反射信息（Method、ArgType、ReplyType）
  - _Requirements: 6.1, 6.2, 6.4, 6.5_

- [x] 5.2 实现服务方法调用


  - 实现 Call() 方法通过反射调用服务方法
  - 实现方法查找逻辑（服务名.方法名）
  - 处理参数反序列化和结果序列化
  - 实现错误处理和 panic 恢复
  - _Requirements: 6.3, 7.2, 7.4_

- [x] 5.3 编写服务注册表测试






  - 测试服务注册和方法提取
  - 测试方法调用的正确性
  - 测试错误处理和边界情况
  - _Requirements: 6.1, 6.2, 6.3_

- [x] 6. 实现 RPC 服务器




- [x] 6.1 实现服务器核心结构


  - 创建 Server 结构体，集成 ServiceRegistry、GoroutinePool 和 Codec
  - 实现 NewServer() 构造函数
  - 实现 Register() 方法注册服务
  - _Requirements: 1.1, 1.2_

- [x] 6.2 实现服务器监听和连接处理


  - 实现 Serve() 方法启动 TCP 监听
  - 实现 handleConn() 方法处理单个连接
  - 使用协程池处理连接，避免 goroutine 爆炸
  - 实现请求读取、解码、调用、编码、响应的完整流程
  - _Requirements: 1.3, 1.4, 1.5_

- [x] 6.3 实现服务器优雅关闭


  - 实现 Shutdown() 方法支持优雅关闭
  - 使用 context 控制关闭超时
  - 等待所有连接处理完成
  - _Requirements: 7.5_

- [ ]* 6.4 编写服务器测试
  - 测试服务注册和启动
  - 测试请求处理流程
  - 测试并发连接处理
  - 测试优雅关闭
  - _Requirements: 1.1, 1.2, 1.3, 1.4_

- [x] 7. 实现 RPC 客户端




- [x] 7.1 实现客户端核心结构


  - 创建 Client 结构体，集成 ConnPool 和 Codec
  - 实现 NewClient() 构造函数，初始化连接池
  - 使用 map 管理待处理的调用（pending calls）
  - 使用 atomic 生成唯一的请求序列号
  - _Requirements: 2.1, 2.4_

- [x] 7.2 实现同步调用方法


  - 实现 Call() 方法执行同步 RPC 调用
  - 从连接池获取连接
  - 编码请求并发送
  - 等待响应并解码
  - 归还连接到连接池
  - 支持 context 超时控制
  - _Requirements: 2.2, 2.3, 2.4, 2.5_

- [x] 7.3 实现异步调用方法


  - 实现 Go() 方法支持异步 RPC 调用
  - 使用 channel 通知调用完成
  - 支持请求管道化（多个请求并发发送）
  - _Requirements: 2.2, 2.3_

- [x] 7.4 实现客户端关闭和错误处理


  - 实现 Close() 方法关闭客户端和连接池
  - 实现网络错误重试机制（最多 3 次，指数退避）
  - 实现超时错误处理
  - _Requirements: 2.5, 7.3, 7.5_

- [ ]* 7.5 编写客户端测试
  - 测试同步调用
  - 测试异步调用
  - 测试连接池复用
  - 测试错误处理和重试
  - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_

- [x] 8. 创建示例代码和文档




- [x] 8.1 创建简单示例


  - 实现一个简单的算术服务（Add、Multiply）
  - 创建 examples/simple/server.go 启动服务器
  - 创建 examples/simple/client.go 调用服务
  - 添加详细注释说明使用方法
  - _Requirements: 8.1_

- [x] 8.2 创建性能基准测试


  - 创建 examples/benchmark/bench_test.go
  - 实现对象池性能对比测试（使用 vs 不使用）
  - 实现连接池性能对比测试
  - 实现端到端性能测试（吞吐量和延迟）
  - 实现并发测试验证线程安全性
  - _Requirements: 8.2, 8.4, 8.5_

- [x] 8.3 编写 README 文档


  - 编写项目介绍和特性说明
  - 添加快速开始指南
  - 说明各项性能优化技术的原理
  - 包含性能测试结果和对比数据
  - 添加 API 使用文档
  - 添加架构图和流程图
  - _Requirements: 8.1, 8.3, 8.4_

- [x] 9. 集成测试和性能验证




- [x] 9.1 运行完整的端到端测试


  - 启动服务器并注册测试服务
  - 使用客户端执行各种调用场景
  - 验证正常流程和错误处理
  - 验证并发调用的正确性
  - _Requirements: 8.5_



- [ ] 9.2 执行性能基准测试
  - 运行所有 benchmark 测试
  - 收集性能数据（QPS、延迟、内存使用）
  - 对比优化前后的性能差异
  - 将结果更新到 README 文档
  - _Requirements: 8.2, 8.4_

- [ ]* 9.3 运行 race detector 测试
  - 使用 go test -race 检测数据竞争
  - 修复发现的并发问题
  - _Requirements: 8.5_
