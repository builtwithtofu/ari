package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/sourcegraph/jsonrpc2"
)

// Server handles JSON-RPC requests.
type Server struct {
	registry *MethodRegistry
}

// NewServer creates a new JSON-RPC server.
func NewServer(registry *MethodRegistry) *Server {
	return &Server{registry: registry}
}

// Handle implements jsonrpc2.Handler.
func (s *Server) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {
	if req == nil || req.Method == "" {
		s.sendError(ctx, conn, jsonrpc2.ID{}, InvalidRequest, "Invalid Request", "method is required")
		return
	}

	envelope := RequestEnvelope[json.RawMessage]{
		JSONRPC: "2.0",
		ID:      req.ID,
		Method:  req.Method,
	}

	if req.Params != nil {
		envelope.Params = *req.Params
		if !json.Valid(envelope.Params) {
			s.sendError(ctx, conn, req.ID, ParseError, "Parse error", "invalid params JSON")
			return
		}
	}

	if s.registry == nil {
		s.sendError(ctx, conn, req.ID, InternalError, "Internal error", "method registry is not configured")
		return
	}

	method, ok := s.registry.Get(envelope.Method)
	if !ok {
		s.sendError(ctx, conn, req.ID, MethodNotFound, "Method not found", envelope.Method)
		return
	}

	result, err := method.Call(ctx, envelope.Params)
	if err != nil {
		if req.Notif {
			return
		}

		if errors.Is(err, ErrInvalidMethodParams) {
			s.sendError(ctx, conn, req.ID, InvalidParams, "Invalid params", err.Error())
			return
		}

		var handlerErr *HandlerError
		if errors.As(err, &handlerErr) {
			s.sendError(ctx, conn, req.ID, handlerErr.Code, handlerErr.Message, handlerErr.Data)
			return
		}

		s.sendError(ctx, conn, req.ID, InternalError, "Internal error", fmt.Sprintf("%v", err))
		return
	}

	if req.Notif {
		return
	}

	s.sendResult(ctx, conn, req.ID, result)
}

func (s *Server) sendError(ctx context.Context, conn *jsonrpc2.Conn, id jsonrpc2.ID, code int, message string, data any) {
	if conn == nil {
		return
	}

	resp := ResponseEnvelope[any]{
		JSONRPC: "2.0",
		ID:      id,
		Error: &Error{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}

	var rawData *json.RawMessage
	if resp.Error.Data != nil {
		raw, _ := json.Marshal(resp.Error.Data)
		rawData = (*json.RawMessage)(&raw)
	}

	err := conn.ReplyWithError(ctx, id, &jsonrpc2.Error{
		Code:    int64(resp.Error.Code),
		Message: resp.Error.Message,
		Data:    rawData,
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
		return
	}
}

func (s *Server) sendResult(ctx context.Context, conn *jsonrpc2.Conn, id jsonrpc2.ID, result any) {
	if conn == nil {
		return
	}

	resp := ResponseEnvelope[any]{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}

	err := conn.Reply(ctx, id, resp.Result)
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
		return
	}
}
