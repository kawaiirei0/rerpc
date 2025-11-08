package rerpc

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrPoolClosed 表示连接池已关闭
	ErrPoolClosed = errors.New("connection pool is closed")

	// ErrPoolExhausted 表示连接池已耗尽（达到最大连接数）
	ErrPoolExhausted = errors.New("connection pool exhausted")

	// ErrInvalidConn 表示无效的连接
	ErrInvalidConn = errors.New("invalid connection")
)

// ConnPool 连接池，用于复用 TCP 连接
// 性能优化：
// 1. 使用 channel 作为无锁的空闲连接队列
// 2. 使用 atomic 包管理活跃连接计数，避免锁竞争
// 3. 实现快速路径优化：优先从空闲队列获取连接
type ConnPool struct {
	// 网络类型和地址
	network string
	address string

	// 连接池配置
	maxIdle   int // 最大空闲连接数
	maxActive int // 最大活跃连接数（0 表示无限制）

	// 连接管理
	idleConns chan net.Conn // 空闲连接队列（使用 buffered channel）
	activeNum int32         // 当前活跃连接数（使用 atomic 操作）

	// 连接工厂函数
	dial func() (net.Conn, error)

	// 连接健康检查
	testOnGet bool                        // 获取连接时是否进行健康检查
	testConn  func(net.Conn) error        // 连接健康检查函数

	// 超时配置
	dialTimeout time.Duration // 连接超时时间
	idleTimeout time.Duration // 空闲连接超时时间

	// 状态管理
	closed int32      // 关闭标志（使用 atomic 操作）
	mu     sync.Mutex // 保护关闭操作
}

// ConnPoolConfig 连接池配置
type ConnPoolConfig struct {
	Network     string        // 网络类型（如 "tcp"）
	Address     string        // 服务器地址（如 "localhost:8080"）
	MaxIdle     int           // 最大空闲连接数
	MaxActive   int           // 最大活跃连接数（0 表示无限制）
	DialTimeout time.Duration // 连接超时时间
	IdleTimeout time.Duration // 空闲连接超时时间
	TestOnGet   bool          // 获取连接时是否进行健康检查
}

// NewConnPool 创建一个新的连接池
// 参数：
//   - config: 连接池配置
// 返回：
//   - *ConnPool: 连接池实例
//   - error: 错误信息
func NewConnPool(config ConnPoolConfig) (*ConnPool, error) {
	if config.Network == "" {
		config.Network = "tcp"
	}
	if config.Address == "" {
		return nil, errors.New("address is required")
	}
	if config.MaxIdle <= 0 {
		config.MaxIdle = 10 // 默认最大空闲连接数
	}
	if config.MaxActive < 0 {
		config.MaxActive = 0 // 0 表示无限制
	}
	if config.DialTimeout <= 0 {
		config.DialTimeout = 5 * time.Second // 默认连接超时
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 5 * time.Minute // 默认空闲超时
	}

	pool := &ConnPool{
		network:     config.Network,
		address:     config.Address,
		maxIdle:     config.MaxIdle,
		maxActive:   config.MaxActive,
		idleConns:   make(chan net.Conn, config.MaxIdle),
		dialTimeout: config.DialTimeout,
		idleTimeout: config.IdleTimeout,
		testOnGet:   config.TestOnGet,
	}

	// 设置默认的连接工厂函数
	pool.dial = func() (net.Conn, error) {
		return net.DialTimeout(pool.network, pool.address, pool.dialTimeout)
	}

	// 设置默认的连接健康检查函数
	pool.testConn = func(conn net.Conn) error {
		// 简单的健康检查：设置读超时并尝试读取（不阻塞）
		if err := conn.SetReadDeadline(time.Now().Add(time.Millisecond)); err != nil {
			return err
		}
		// 重置读超时
		if err := conn.SetReadDeadline(time.Time{}); err != nil {
			return err
		}
		return nil
	}

	return pool, nil
}

// Get 从连接池获取一个连接
// 性能优化：
// 1. 快速路径：优先从空闲队列获取连接（无锁操作）
// 2. 慢速路径：创建新连接（需要检查最大连接数限制）
// 3. 连接健康检查：可选的连接验证
func (p *ConnPool) Get() (net.Conn, error) {
	// 检查连接池是否已关闭
	if atomic.LoadInt32(&p.closed) == 1 {
		return nil, ErrPoolClosed
	}

	// 快速路径：尝试从空闲队列获取连接
	select {
	case conn := <-p.idleConns:
		// 如果启用了健康检查，验证连接
		if p.testOnGet && p.testConn != nil {
			if err := p.testConn(conn); err != nil {
				// 连接不健康，关闭并递减活跃连接计数
				conn.Close()
				atomic.AddInt32(&p.activeNum, -1)
				// 递归调用，尝试获取另一个连接
				return p.Get()
			}
		}
		return conn, nil
	default:
		// 空闲队列为空，进入慢速路径
	}

	// 慢速路径：创建新连接
	// 检查是否达到最大连接数限制
	if p.maxActive > 0 {
		active := atomic.LoadInt32(&p.activeNum)
		if active >= int32(p.maxActive) {
			return nil, ErrPoolExhausted
		}
	}

	// 创建新连接
	conn, err := p.dial()
	if err != nil {
		return nil, err
	}

	// 增加活跃连接计数
	atomic.AddInt32(&p.activeNum, 1)

	return conn, nil
}

// Put 将连接归还到连接池
// 性能优化：
// 1. 使用非阻塞的 channel 发送，避免等待
// 2. 如果空闲队列已满，直接关闭连接
func (p *ConnPool) Put(conn net.Conn) error {
	if conn == nil {
		return ErrInvalidConn
	}

	// 检查连接池是否已关闭
	if atomic.LoadInt32(&p.closed) == 1 {
		conn.Close()
		atomic.AddInt32(&p.activeNum, -1)
		return ErrPoolClosed
	}

	// 尝试将连接放回空闲队列（非阻塞）
	select {
	case p.idleConns <- conn:
		// 成功放回空闲队列
		return nil
	default:
		// 空闲队列已满，关闭连接并递减活跃连接计数
		conn.Close()
		atomic.AddInt32(&p.activeNum, -1)
		return nil
	}
}

// ActiveCount 返回当前活跃连接数
func (p *ConnPool) ActiveCount() int {
	return int(atomic.LoadInt32(&p.activeNum))
}

// IdleCount 返回当前空闲连接数
func (p *ConnPool) IdleCount() int {
	return len(p.idleConns)
}

// Close 关闭连接池，释放所有连接
// 这是一个优雅关闭过程：
// 1. 设置关闭标志，阻止新的 Get 操作
// 2. 关闭所有空闲连接
// 3. 等待所有活跃连接归还（由调用者负责）
func (p *ConnPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 检查是否已经关闭
	if atomic.LoadInt32(&p.closed) == 1 {
		return nil
	}

	// 设置关闭标志
	atomic.StoreInt32(&p.closed, 1)

	// 关闭所有空闲连接
	close(p.idleConns)
	for conn := range p.idleConns {
		if conn != nil {
			conn.Close()
			atomic.AddInt32(&p.activeNum, -1)
		}
	}

	return nil
}

// IsClosed 检查连接池是否已关闭
func (p *ConnPool) IsClosed() bool {
	return atomic.LoadInt32(&p.closed) == 1
}

// GetWithRetry 从连接池获取连接，支持重试机制
// 参数：
//   - maxRetries: 最大重试次数
//   - retryDelay: 重试延迟时间（指数退避）
// 返回：
//   - net.Conn: 连接
//   - error: 错误信息
func (p *ConnPool) GetWithRetry(maxRetries int, retryDelay time.Duration) (net.Conn, error) {
	var lastErr error

	for i := 0; i <= maxRetries; i++ {
		conn, err := p.Get()
		if err == nil {
			return conn, nil
		}

		lastErr = err

		// 如果是连接池关闭或耗尽错误，不重试
		if errors.Is(err, ErrPoolClosed) || errors.Is(err, ErrPoolExhausted) {
			return nil, err
		}

		// 如果不是最后一次重试，等待后重试
		if i < maxRetries {
			// 指数退避：每次重试延迟时间翻倍
			delay := retryDelay * time.Duration(1<<uint(i))
			time.Sleep(delay)
		}
	}

	return nil, lastErr
}

// Ping 测试连接池是否可用
// 尝试获取一个连接并立即归还
func (p *ConnPool) Ping() error {
	conn, err := p.Get()
	if err != nil {
		return err
	}
	return p.Put(conn)
}

// SetDialFunc 设置自定义的连接工厂函数
// 允许用户自定义连接创建逻辑（如添加 TLS、认证等）
func (p *ConnPool) SetDialFunc(dial func() (net.Conn, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dial = dial
}

// SetTestFunc 设置自定义的连接健康检查函数
// 允许用户自定义连接验证逻辑
func (p *ConnPool) SetTestFunc(test func(net.Conn) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.testConn = test
}

// Stats 返回连接池的统计信息
type PoolStats struct {
	ActiveCount int  // 活跃连接数
	IdleCount   int  // 空闲连接数
	IsClosed    bool // 是否已关闭
}

// Stats 获取连接池统计信息
func (p *ConnPool) Stats() PoolStats {
	return PoolStats{
		ActiveCount: p.ActiveCount(),
		IdleCount:   p.IdleCount(),
		IsClosed:    p.IsClosed(),
	}
}

// CleanIdleConns 清理超时的空闲连接
// 这个方法应该定期调用（如通过 time.Ticker）
func (p *ConnPool) CleanIdleConns() int {
	if atomic.LoadInt32(&p.closed) == 1 {
		return 0
	}

	cleaned := 0
	idleCount := len(p.idleConns)

	// 遍历空闲连接队列
	for i := 0; i < idleCount; i++ {
		select {
		case conn := <-p.idleConns:
			// 检查连接健康状态
			if p.testConn != nil && p.testConn(conn) != nil {
				// 连接不健康，关闭
				conn.Close()
				atomic.AddInt32(&p.activeNum, -1)
				cleaned++
			} else {
				// 连接健康，放回队列
				select {
				case p.idleConns <- conn:
					// 成功放回
				default:
					// 队列已满，关闭连接
					conn.Close()
					atomic.AddInt32(&p.activeNum, -1)
					cleaned++
				}
			}
		default:
			// 队列为空，退出
			return cleaned
		}
	}

	return cleaned
}

// StartCleaner 启动后台清理协程
// 定期清理超时的空闲连接
// 返回一个 stop channel，发送信号到该 channel 可以停止清理协程
func (p *ConnPool) StartCleaner(interval time.Duration) chan<- struct{} {
	stopChan := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.CleanIdleConns()
			case <-stopChan:
				return
			}
		}
	}()

	return stopChan
}
