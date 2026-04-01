package rpc

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

func TestUnixSocketTransportServesRequests(t *testing.T) {
	registry := NewMethodRegistry()
	err := RegisterMethod(registry, Method[echoRequest, echoResponse]{
		Name: "test.echo",
		Handler: func(ctx context.Context, req echoRequest) (echoResponse, error) {
			return echoResponse{Echoed: req.Message}, nil
		},
	})
	if err != nil {
		t.Fatalf("register method: %v", err)
	}

	socketPath := testSocketPath(t)
	transport := NewUnixSocketTransport(socketPath, NewServer(registry))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- transport.Run(ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for unix socket")
		}

		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		stream := jsonrpc2.NewBufferedStream(conn, jsonrpc2.PlainObjectCodec{})
		client := jsonrpc2.NewConn(ctx, stream, jsonrpc2.HandlerWithError(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (interface{}, error) {
			return nil, nil
		}))

		var result echoResponse
		if err := client.Call(ctx, "test.echo", echoRequest{Message: "ping"}, &result); err != nil {
			_ = client.Close()
			_ = conn.Close()
			t.Fatalf("call test.echo: %v", err)
		}

		if result.Echoed != "ping" {
			_ = client.Close()
			_ = conn.Close()
			t.Fatalf("unexpected result: %+v", result)
		}

		_ = client.Close()
		_ = conn.Close()
		break
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("transport run: %v", err)
	}
}

func TestUnixSocketTransportRejectsLiveSocket(t *testing.T) {
	registry := NewMethodRegistry()
	err := RegisterMethod(registry, Method[echoRequest, echoResponse]{
		Name: "test.echo",
		Handler: func(ctx context.Context, req echoRequest) (echoResponse, error) {
			return echoResponse{Echoed: req.Message}, nil
		},
	})
	if err != nil {
		t.Fatalf("register method: %v", err)
	}

	socketPath := testSocketPath(t)

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	transport1 := NewUnixSocketTransport(socketPath, NewServer(registry))
	errCh := make(chan error, 1)
	go func() {
		errCh <- transport1.Run(ctx1)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, dialErr := net.Dial("unix", socketPath)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for first daemon socket: %v", dialErr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	transport2 := NewUnixSocketTransport(socketPath, NewServer(registry))
	err = transport2.Run(context.Background())
	if err == nil {
		t.Fatal("expected second transport run to fail")
	}

	cancel1()
	if runErr := <-errCh; runErr != nil {
		t.Fatalf("first transport run: %v", runErr)
	}
}

func TestUnixSocketTransportRejectsNonSocketPath(t *testing.T) {
	registry := NewMethodRegistry()
	err := RegisterMethod(registry, Method[echoRequest, echoResponse]{
		Name: "test.echo",
		Handler: func(ctx context.Context, req echoRequest) (echoResponse, error) {
			return echoResponse{Echoed: req.Message}, nil
		},
	})
	if err != nil {
		t.Fatalf("register method: %v", err)
	}

	socketPath := testSocketPath(t)
	if writeErr := os.WriteFile(socketPath, []byte("not-a-socket"), 0o644); writeErr != nil {
		t.Fatalf("write fake socket file: %v", writeErr)
	}

	transport := NewUnixSocketTransport(socketPath, NewServer(registry))
	err = transport.Run(context.Background())
	if err == nil {
		t.Fatal("expected transport to fail when path is not a socket")
	}
}

func testSocketPath(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "ari-rpc-")
	if err != nil {
		t.Fatalf("create temp socket dir: %v", err)
	}

	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	return filepath.Join(dir, "s.sock")
}
