package rerpc

import "encoding/json"

// JSON-RPC 2.0 协议版本
const JSONRPCVersion = "2.0"

// Request 表示 JSON-RPC 2.0 请求消息
// 支持对象池复用，使用 Reset() 方法清理状态
type Request struct {
	Jsonrpc string          `json:"jsonrpc"` // 固定为 "2.0"
	Method  string          `json:"method"`  // 要调用的方法名
	Params  json.RawMessage `json:"params,omitempty"` // 方法参数（延迟解析）
	ID      interface{}     `json:"id"` // 请求标识符
}

// Reset 重置 Request 对象状态，用于对象池复用
// 清空所有字段以避免数据污染
func (r *Request) Reset() {
	r.Jsonrpc = ""
	r.Method = ""
	r.Params = nil
	r.ID = nil
}

// Response 表示 JSON-RPC 2.0 响应消息
// 支持对象池复用，使用 Reset() 方法清理状态
type Response struct {
	Jsonrpc string          `json:"jsonrpc"` // 固定为 "2.0"
	Result  json.RawMessage `json:"result,omitempty"` // 调用结果（延迟解析）
	Error   *Error          `json:"error,omitempty"` // 错误信息（如果有）
	ID      interface{}     `json:"id"` // 对应的请求标识符
}

// Reset 重置 Response 对象状态，用于对象池复用
// 清空所有字段以避免数据污染
func (r *Response) Reset() {
	r.Jsonrpc = ""
	r.Result = nil
	r.Error = nil
	r.ID = nil
}

// Error 表示 JSON-RPC 2.0 错误对象
type Error struct {
	Code    int         `json:"code"`    // 错误码
	Message string      `json:"message"` // 错误描述
	Data    interface{} `json:"data,omitempty"` // 额外的错误信息
}

// Error 实现 error 接口
func (e *Error) Error() string {
	return e.Message
}
