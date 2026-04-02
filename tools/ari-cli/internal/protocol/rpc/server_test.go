package rpc

import (
	"context"
	"net"
	"testing"

	"github.com/sourcegraph/jsonrpc2"
)

type echoRequest struct {
	Message string `json:"message"`
}

type echoResponse struct {
	Echoed string `json:"echoed"`
}

func TestServerDispatchesRegisteredHandler(t *testing.T) {
	registry := NewMethodRegistry()

	err := RegisterMethod(registry, Method[echoRequest, echoResponse]{
		Name:        "test.echo",
		Description: "Echo request",
		Handler: func(ctx context.Context, req echoRequest) (echoResponse, error) {
			if req.Message == "" {
				t.Fatalf("expected request message")
			}
			return echoResponse{Echoed: req.Message}, nil
		},
	})
	if err != nil {
		t.Fatalf("register method: %v", err)
	}

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	server := NewServer(registry)
	ctx := context.Background()
	serverRPC, clientRPC := newRPCPair(ctx, t, serverConn, clientConn, server)

	t.Cleanup(func() {
		_ = serverRPC.Close()
		_ = clientRPC.Close()
	})

	var result echoResponse
	if err := clientRPC.Call(ctx, "test.echo", echoRequest{Message: "hello"}, &result); err != nil {
		t.Fatalf("call test.echo: %v", err)
	}

	if result.Echoed != "hello" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestServerReturnsMethodNotFound(t *testing.T) {
	registry := NewMethodRegistry()
	server := NewServer(registry)
	ctx := context.Background()

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	serverRPC, clientRPC := newRPCPair(ctx, t, serverConn, clientConn, server)
	t.Cleanup(func() {
		_ = serverRPC.Close()
		_ = clientRPC.Close()
	})

	err := clientRPC.Call(ctx, "test.missing", map[string]string{"x": "1"}, &echoResponse{})
	if err == nil {
		t.Fatal("expected method not found error")
	}

	rpcErr, ok := err.(*jsonrpc2.Error)
	if !ok {
		t.Fatalf("expected jsonrpc2 error, got %T", err)
	}

	if rpcErr.Code != int64(MethodNotFound) {
		t.Fatalf("unexpected error code: %d", rpcErr.Code)
	}
}

func TestServerReturnsInvalidParams(t *testing.T) {
	registry := NewMethodRegistry()
	err := RegisterMethod(registry, Method[echoRequest, echoResponse]{
		Name: "test.echo",
		Handler: func(ctx context.Context, req echoRequest) (echoResponse, error) {
			_ = ctx
			return echoResponse{Echoed: req.Message}, nil
		},
	})
	if err != nil {
		t.Fatalf("register method: %v", err)
	}

	server := NewServer(registry)
	ctx := context.Background()

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	serverRPC, clientRPC := newRPCPair(ctx, t, serverConn, clientConn, server)
	t.Cleanup(func() {
		_ = serverRPC.Close()
		_ = clientRPC.Close()
	})

	err = clientRPC.Call(ctx, "test.echo", "wrong-shape", &echoResponse{})
	if err == nil {
		t.Fatal("expected invalid params error")
	}

	rpcErr, ok := err.(*jsonrpc2.Error)
	if !ok {
		t.Fatalf("expected jsonrpc2 error, got %T", err)
	}

	if rpcErr.Code != int64(InvalidParams) {
		t.Fatalf("unexpected error code: %d", rpcErr.Code)
	}
}

func TestServerPreservesCustomHandlerErrorCode(t *testing.T) {
	registry := NewMethodRegistry()
	err := RegisterMethod(registry, Method[echoRequest, echoResponse]{
		Name: "test.custom_error",
		Handler: func(ctx context.Context, req echoRequest) (echoResponse, error) {
			_ = ctx
			_ = req
			return echoResponse{}, NewHandlerError(SessionNotFound, "session not found", "sess-missing")
		},
	})
	if err != nil {
		t.Fatalf("register method: %v", err)
	}

	server := NewServer(registry)
	ctx := context.Background()

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	serverRPC, clientRPC := newRPCPair(ctx, t, serverConn, clientConn, server)
	t.Cleanup(func() {
		_ = serverRPC.Close()
		_ = clientRPC.Close()
	})

	err = clientRPC.Call(ctx, "test.custom_error", echoRequest{Message: "x"}, &echoResponse{})
	if err == nil {
		t.Fatal("expected session not found error")
	}

	rpcErr, ok := err.(*jsonrpc2.Error)
	if !ok {
		t.Fatalf("expected jsonrpc2 error, got %T", err)
	}

	if rpcErr.Code != int64(SessionNotFound) {
		t.Fatalf("unexpected error code: %d", rpcErr.Code)
	}
	if rpcErr.Message != "session not found" {
		t.Fatalf("unexpected error message: %q", rpcErr.Message)
	}
}

func newRPCPair(ctx context.Context, t *testing.T, serverConn net.Conn, clientConn net.Conn, server *Server) (*jsonrpc2.Conn, *jsonrpc2.Conn) {
	t.Helper()

	//nolint:staticcheck // Ari standardizes on PlainObjectCodec framing for local RPC.
	serverStream := jsonrpc2.NewBufferedStream(serverConn, jsonrpc2.PlainObjectCodec{})
	//nolint:staticcheck // Ari standardizes on PlainObjectCodec framing for local RPC.
	clientStream := jsonrpc2.NewBufferedStream(clientConn, jsonrpc2.PlainObjectCodec{})

	serverRPC := jsonrpc2.NewConn(ctx, serverStream, server)
	clientRPC := jsonrpc2.NewConn(ctx, clientStream, jsonrpc2.HandlerWithError(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (interface{}, error) {
		return nil, nil
	}))

	return serverRPC, clientRPC
}
