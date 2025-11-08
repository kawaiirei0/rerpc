package rerpc

import (
	"encoding/json"
	"fmt"
)

// Codec 定义编解码器接口
// 负责 JSON-RPC 消息的序列化和反序列化
type Codec interface {
	// EncodeRequest 编码请求消息为字节流
	EncodeRequest(req *Request) ([]byte, error)

	// DecodeRequest 从字节流解码请求消息
	DecodeRequest(data []byte) (*Request, error)

	// EncodeResponse 编码响应消息为字节流
	EncodeResponse(resp *Response) ([]byte, error)

	// DecodeResponse 从字节流解码响应消息
	DecodeResponse(data []byte) (*Response, error)
}

// JSONCodec 实现基于 JSON 的编解码器
// 集成对象池以实现零拷贝和对象复用
type JSONCodec struct {
	pool *ObjectPool
}

// NewJSONCodec 创建一个新的 JSON 编解码器
// 如果不提供对象池，将使用默认的全局对象池
func NewJSONCodec(pool *ObjectPool) *JSONCodec {
	if pool == nil {
		pool = defaultPool
	}
	return &JSONCodec{
		pool: pool,
	}
}

// EncodeRequest 编码请求消息
// 性能优化：使用对象池复用 buffer，减少内存分配
func (c *JSONCodec) EncodeRequest(req *Request) ([]byte, error) {
	if req == nil {
		return nil, NewInvalidRequestError("request is nil")
	}

	// 验证请求的必要字段
	if req.Method == "" {
		return nil, NewInvalidRequestError("method is required")
	}

	// 设置协议版本
	if req.Jsonrpc == "" {
		req.Jsonrpc = JSONRPCVersion
	}

	// 从对象池获取 buffer
	buf := c.pool.GetBuffer()
	defer c.pool.PutBuffer(buf)

	// 使用 json.Encoder 直接写入 buffer，避免中间拷贝
	encoder := json.NewEncoder(buf)
	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("encode request failed: %w", err)
	}

	// 复制数据（必须复制，因为 buffer 会被归还到池中）
	data := make([]byte, buf.Len())
	copy(data, buf.Bytes())

	return data, nil
}

// DecodeRequest 解码请求消息
// 性能优化：使用对象池复用 Request 对象，使用 json.RawMessage 延迟解析参数
func (c *JSONCodec) DecodeRequest(data []byte) (*Request, error) {
	if len(data) == 0 {
		return nil, NewInvalidRequestError("empty request data")
	}

	// 从对象池获取 Request 对象
	req := c.pool.GetRequest()

	// 解码 JSON 数据
	if err := json.Unmarshal(data, req); err != nil {
		// 解码失败，归还对象
		c.pool.PutRequest(req)
		return nil, NewParseError(err.Error())
	}

	// 验证协议版本
	if req.Jsonrpc != JSONRPCVersion {
		c.pool.PutRequest(req)
		return nil, NewInvalidRequestError(fmt.Sprintf("invalid jsonrpc version: %s", req.Jsonrpc))
	}

	// 验证方法名
	if req.Method == "" {
		c.pool.PutRequest(req)
		return nil, NewInvalidRequestError("method is required")
	}

	// 注意：调用者负责在使用完毕后归还 Request 对象
	return req, nil
}

// EncodeResponse 编码响应消息
// 性能优化：使用对象池复用 buffer，减少内存分配
func (c *JSONCodec) EncodeResponse(resp *Response) ([]byte, error) {
	if resp == nil {
		return nil, NewInternalError("response is nil")
	}

	// 设置协议版本
	if resp.Jsonrpc == "" {
		resp.Jsonrpc = JSONRPCVersion
	}

	// 验证响应必须有 Result 或 Error，但不能同时存在
	if resp.Result == nil && resp.Error == nil {
		return nil, NewInternalError("response must have either result or error")
	}
	if resp.Result != nil && resp.Error != nil {
		return nil, NewInternalError("response cannot have both result and error")
	}

	// 从对象池获取 buffer
	buf := c.pool.GetBuffer()
	defer c.pool.PutBuffer(buf)

	// 使用 json.Encoder 直接写入 buffer，避免中间拷贝
	encoder := json.NewEncoder(buf)
	if err := encoder.Encode(resp); err != nil {
		return nil, fmt.Errorf("encode response failed: %w", err)
	}

	// 复制数据（必须复制，因为 buffer 会被归还到池中）
	data := make([]byte, buf.Len())
	copy(data, buf.Bytes())

	return data, nil
}

// DecodeResponse 解码响应消息
// 性能优化：使用对象池复用 Response 对象，使用 json.RawMessage 延迟解析结果
func (c *JSONCodec) DecodeResponse(data []byte) (*Response, error) {
	if len(data) == 0 {
		return nil, NewInvalidRequestError("empty response data")
	}

	// 从对象池获取 Response 对象
	resp := c.pool.GetResponse()

	// 解码 JSON 数据
	if err := json.Unmarshal(data, resp); err != nil {
		// 解码失败，归还对象
		c.pool.PutResponse(resp)
		return nil, NewParseError(err.Error())
	}

	// 验证协议版本
	if resp.Jsonrpc != JSONRPCVersion {
		c.pool.PutResponse(resp)
		return nil, NewInvalidRequestError(fmt.Sprintf("invalid jsonrpc version: %s", resp.Jsonrpc))
	}

	// 验证响应必须有 Result 或 Error
	if resp.Result == nil && resp.Error == nil {
		c.pool.PutResponse(resp)
		return nil, NewInvalidRequestError("response must have either result or error")
	}

	// 注意：调用者负责在使用完毕后归还 Response 对象
	return resp, nil
}

// ReleaseRequest 释放（归还）Request 对象到对象池
// 这是一个便捷方法，等同于 pool.PutRequest
func (c *JSONCodec) ReleaseRequest(req *Request) {
	c.pool.PutRequest(req)
}

// ReleaseResponse 释放（归还）Response 对象到对象池
// 这是一个便捷方法，等同于 pool.PutResponse
func (c *JSONCodec) ReleaseResponse(resp *Response) {
	c.pool.PutResponse(resp)
}
