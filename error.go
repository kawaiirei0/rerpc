package rerpc

// JSON-RPC 2.0 标准错误码
// 参考规范：https://www.jsonrpc.org/specification#error_object
const (
	// ErrCodeParse 表示解析错误
	// 服务端接收到无效的 JSON 数据
	ErrCodeParse = -32700

	// ErrCodeInvalidRequest 表示无效请求
	// 发送的 JSON 不是有效的请求对象
	ErrCodeInvalidRequest = -32600

	// ErrCodeMethodNotFound 表示方法未找到
	// 请求的方法不存在或不可用
	ErrCodeMethodNotFound = -32601

	// ErrCodeInvalidParams 表示无效参数
	// 方法参数无效或缺失
	ErrCodeInvalidParams = -32602

	// ErrCodeInternal 表示内部错误
	// 服务端内部发生错误
	ErrCodeInternal = -32603
)

// 标准错误消息
const (
	ErrMsgParse          = "Parse error"
	ErrMsgInvalidRequest = "Invalid Request"
	ErrMsgMethodNotFound = "Method not found"
	ErrMsgInvalidParams  = "Invalid params"
	ErrMsgInternal       = "Internal error"
)

// NewError 创建一个新的 JSON-RPC 错误
func NewError(code int, message string, data interface{}) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

// NewParseError 创建解析错误
func NewParseError(data interface{}) *Error {
	return NewError(ErrCodeParse, ErrMsgParse, data)
}

// NewInvalidRequestError 创建无效请求错误
func NewInvalidRequestError(data interface{}) *Error {
	return NewError(ErrCodeInvalidRequest, ErrMsgInvalidRequest, data)
}

// NewMethodNotFoundError 创建方法未找到错误
func NewMethodNotFoundError(method string) *Error {
	return NewError(ErrCodeMethodNotFound, ErrMsgMethodNotFound, method)
}

// NewInvalidParamsError 创建无效参数错误
func NewInvalidParamsError(data interface{}) *Error {
	return NewError(ErrCodeInvalidParams, ErrMsgInvalidParams, data)
}

// NewInternalError 创建内部错误
func NewInternalError(data interface{}) *Error {
	return NewError(ErrCodeInternal, ErrMsgInternal, data)
}
