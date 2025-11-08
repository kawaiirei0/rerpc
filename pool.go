package rerpc

import (
	"bytes"
	"sync"
)

// ObjectPool 管理所有可复用的对象池
// 使用 sync.Pool 实现零 GC 压力的对象复用
type ObjectPool struct {
	requestPool  *sync.Pool
	responsePool *sync.Pool
	bufferPool   *sync.Pool
}

// NewObjectPool 创建一个新的对象池管理器
func NewObjectPool() *ObjectPool {
	return &ObjectPool{
		requestPool: &sync.Pool{
			New: func() interface{} {
				return &Request{}
			},
		},
		responsePool: &sync.Pool{
			New: func() interface{} {
				return &Response{}
			},
		},
		bufferPool: &sync.Pool{
			New: func() interface{} {
				// 预分配 4KB 缓冲区，适合大多数 RPC 消息
				return bytes.NewBuffer(make([]byte, 0, 4096))
			},
		},
	}
}

// GetRequest 从对象池获取一个 Request 对象
// 使用完毕后必须调用 PutRequest 归还
func (p *ObjectPool) GetRequest() *Request {
	req := p.requestPool.Get().(*Request)
	// 确保对象是干净的状态
	req.Reset()
	return req
}

// PutRequest 将 Request 对象归还到对象池
// 归还前会自动重置对象状态
func (p *ObjectPool) PutRequest(req *Request) {
	if req == nil {
		return
	}
	req.Reset()
	p.requestPool.Put(req)
}

// GetResponse 从对象池获取一个 Response 对象
// 使用完毕后必须调用 PutResponse 归还
func (p *ObjectPool) GetResponse() *Response {
	resp := p.responsePool.Get().(*Response)
	// 确保对象是干净的状态
	resp.Reset()
	return resp
}

// PutResponse 将 Response 对象归还到对象池
// 归还前会自动重置对象状态
func (p *ObjectPool) PutResponse(resp *Response) {
	if resp == nil {
		return
	}
	resp.Reset()
	p.responsePool.Put(resp)
}

// GetBuffer 从对象池获取一个 bytes.Buffer 对象
// 使用完毕后必须调用 PutBuffer 归还
func (p *ObjectPool) GetBuffer() *bytes.Buffer {
	buf := p.bufferPool.Get().(*bytes.Buffer)
	// 重置 buffer 状态
	buf.Reset()
	return buf
}

// PutBuffer 将 bytes.Buffer 对象归还到对象池
// 归还前会自动重置 buffer 状态
func (p *ObjectPool) PutBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	// 如果 buffer 太大（超过 64KB），不放回池中，让 GC 回收
	// 避免池中积累过大的 buffer
	if buf.Cap() > 64*1024 {
		return
	}
	buf.Reset()
	p.bufferPool.Put(buf)
}

// 全局默认对象池实例
// 可以直接使用包级别的函数访问
var defaultPool = NewObjectPool()

// GetRequest 从默认对象池获取 Request
func GetRequest() *Request {
	return defaultPool.GetRequest()
}

// PutRequest 归还 Request 到默认对象池
func PutRequest(req *Request) {
	defaultPool.PutRequest(req)
}

// GetResponse 从默认对象池获取 Response
func GetResponse() *Response {
	return defaultPool.GetResponse()
}

// PutResponse 归还 Response 到默认对象池
func PutResponse(resp *Response) {
	defaultPool.PutResponse(resp)
}

// GetBuffer 从默认对象池获取 Buffer
func GetBuffer() *bytes.Buffer {
	return defaultPool.GetBuffer()
}

// PutBuffer 归还 Buffer 到默认对象池
func PutBuffer(buf *bytes.Buffer) {
	defaultPool.PutBuffer(buf)
}
