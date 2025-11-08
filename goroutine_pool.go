package rerpc

import (
	"errors"
	"sync"
	"sync/atomic"
)

// GoroutinePool 协程池，用于限制并发数量和复用 goroutine
// 性能优化点：
// 1. 固定数量的 worker goroutine，避免频繁创建销毁
// 2. 使用 buffered channel 作为任务队列，平滑流量
// 3. 使用 atomic 标志位管理关闭状态，避免锁竞争
type GoroutinePool struct {
	workers   int           // 工作协程数量
	taskQueue chan func()   // 任务队列
	wg        sync.WaitGroup // 等待所有任务完成
	once      sync.Once     // 确保只初始化一次
	closed    int32         // 关闭标志（原子操作）
}

// NewGoroutinePool 创建一个新的协程池
// workers: 工作协程数量
// queueSize: 任务队列大小，0 表示无缓冲
func NewGoroutinePool(workers int, queueSize int) *GoroutinePool {
	if workers <= 0 {
		workers = 1
	}
	
	pool := &GoroutinePool{
		workers:   workers,
		taskQueue: make(chan func(), queueSize),
		closed:    0,
	}
	
	// 启动固定数量的 worker goroutine
	pool.once.Do(func() {
		for i := 0; i < pool.workers; i++ {
			pool.wg.Add(1)
			go pool.worker()
		}
	})
	
	return pool
}

// worker 工作协程，从任务队列中获取任务并执行
func (p *GoroutinePool) worker() {
	defer p.wg.Done()
	
	// 持续从任务队列中获取任务
	for task := range p.taskQueue {
		if task != nil {
			// 执行任务，捕获 panic 避免 worker 崩溃
			func() {
				defer func() {
					if r := recover(); r != nil {
						// 可以在这里记录日志
						// log.Printf("task panic: %v", r)
					}
				}()
				task()
			}()
		}
	}
}

// Submit 提交任务到协程池
// 如果协程池已关闭，返回 ErrPoolClosed
// 如果任务队列已满，会阻塞直到有空闲位置
func (p *GoroutinePool) Submit(task func()) error {
	if task == nil {
		return errors.New("task cannot be nil")
	}
	
	// 检查协程池是否已关闭（原子操作，无锁）
	if atomic.LoadInt32(&p.closed) == 1 {
		return ErrPoolClosed
	}
	
	// 提交任务到队列
	// 注意：这里可能会阻塞，如果队列已满
	select {
	case p.taskQueue <- task:
		return nil
	default:
		// 再次检查是否关闭，避免在关闭过程中阻塞
		if atomic.LoadInt32(&p.closed) == 1 {
			return ErrPoolClosed
		}
		// 阻塞等待队列有空闲位置
		p.taskQueue <- task
		return nil
	}
}

// Close 关闭协程池，停止接收新任务，等待所有进行中的任务完成
// 这是一个优雅关闭的实现：
// 1. 设置关闭标志，拒绝新任务
// 2. 关闭任务队列，通知 worker 退出
// 3. 等待所有 worker 完成当前任务
func (p *GoroutinePool) Close() {
	// 使用 CAS 操作设置关闭标志，确保只关闭一次
	if !atomic.CompareAndSwapInt32(&p.closed, 0, 1) {
		return // 已经关闭
	}
	
	// 关闭任务队列，worker 会在处理完队列中的任务后退出
	close(p.taskQueue)
	
	// 等待所有 worker 完成
	p.wg.Wait()
}

// IsClosed 检查协程池是否已关闭
func (p *GoroutinePool) IsClosed() bool {
	return atomic.LoadInt32(&p.closed) == 1
}
