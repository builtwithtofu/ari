package rpc

import "fmt"

type RequestEnvelope[T any] struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  T      `json:"params,omitempty"`
}

type ResponseEnvelope[T any] struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  T      `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type HandlerError struct {
	Code    int
	Message string
	Data    any
}

func NewHandlerError(code int, message string, data any) *HandlerError {
	return &HandlerError{Code: code, Message: message, Data: data}
}

func (e *HandlerError) Error() string {
	if e == nil {
		return "rpc handler error"
	}
	if e.Message == "" {
		return fmt.Sprintf("rpc handler error code %d", e.Code)
	}
	return e.Message
}

const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603

	SessionNotFound = -32001
	PlanNotFound    = -32002
	CommandNotFound = -32003
)
