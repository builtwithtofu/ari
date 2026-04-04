package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
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

func TestUnixSocketTransportStopsWithOpenConnection(t *testing.T) {
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
	errCh := make(chan error, 1)
	go func() {
		errCh <- transport.Run(ctx)
	}()

	var conn net.Conn
	deadline := time.Now().Add(2 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for unix socket")
		}

		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	// Keep connection open, cancel transport, and verify Run exits promptly.
	cancel()

	select {
	case runErr := <-errCh:
		if runErr != nil {
			t.Fatalf("transport run: %v", runErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("transport did not stop while connection was still open")
	}

	_ = conn.Close()
}

func TestUnixSocketTransportRoutesFrameConnectionsByFirstByte(t *testing.T) {
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

	var routed atomic.Bool
	transport := NewUnixSocketTransportWithFrameRouter(socketPath, NewServer(registry), func(ctx context.Context, conn net.Conn, firstByte byte) {
		_ = ctx
		if firstByte != 0x01 {
			t.Errorf("firstByte = 0x%02x, want 0x01", firstByte)
		}
		routed.Store(true)
		_, _ = io.Copy(io.Discard, conn)
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- transport.Run(ctx)
	}()
	defer func() {
		cancel()
		<-errCh
	}()

	conn := waitForDial(t, socketPath)
	defer func() {
		_ = conn.Close()
	}()

	if _, err := conn.Write([]byte{0x01, 0xAA, 0xBB}); err != nil {
		t.Fatalf("write frame bytes: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for !routed.Load() {
		if time.Now().After(deadline) {
			t.Fatal("frame router was not called")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUnixSocketTransportRejectsInvalidFirstByte(t *testing.T) {
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
	transport := NewUnixSocketTransportWithFrameRouter(socketPath, NewServer(registry), nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- transport.Run(ctx)
	}()
	defer func() {
		cancel()
		<-errCh
	}()

	conn := waitForDial(t, socketPath)
	defer func() {
		_ = conn.Close()
	}()

	if _, err := conn.Write([]byte{0x20}); err != nil {
		t.Fatalf("write invalid first byte: %v", err)
	}

	reader := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, err = reader.ReadByte()
	if err == nil {
		t.Fatal("read returned nil error, want connection close error")
	}
}

func TestUnixSocketTransportAcceptsJSONWithLeadingWhitespace(t *testing.T) {
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
	transport := NewUnixSocketTransportWithFrameRouter(socketPath, NewServer(registry), nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- transport.Run(ctx)
	}()
	defer func() {
		cancel()
		<-errCh
	}()

	conn := waitForDial(t, socketPath)
	defer func() {
		_ = conn.Close()
	}()

	request := []byte("\n{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"test.echo\",\"params\":{\"message\":\"ping\"}}")
	if _, err := conn.Write(request); err != nil {
		t.Fatalf("write json-rpc request: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read json-rpc response: %v", err)
	}
	if n == 0 {
		t.Fatal("read zero bytes for json-rpc response")
	}

	var response ResponseEnvelope[echoResponse]
	if err := json.Unmarshal(buf[:n], &response); err != nil {
		t.Fatalf("unmarshal json-rpc response: %v", err)
	}
	if response.Error != nil {
		t.Fatalf("json-rpc response error = %+v, want nil", *response.Error)
	}
	if response.Result.Echoed != "ping" {
		t.Fatalf("json-rpc echoed result = %q, want %q", response.Result.Echoed, "ping")
	}
}

func waitForDial(t *testing.T, socketPath string) net.Conn {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for socket %s", socketPath)
		}

		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			return conn
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func TestListenUnixSocketKeepsLiveSocketPath(t *testing.T) {
	socketPath := testSocketPath(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listenConfig := net.ListenConfig{}
	liveListener, err := listenConfig.Listen(ctx, "unix", socketPath)
	if err != nil {
		t.Fatalf("listen live socket: %v", err)
	}
	defer func() {
		_ = liveListener.Close()
	}()

	_, err = listenUnixSocket(context.Background(), socketPath)
	if err == nil {
		t.Fatal("expected listenUnixSocket to fail for active live socket")
	}

	if _, dialErr := net.Dial("unix", socketPath); dialErr != nil {
		t.Fatalf("live socket should remain reachable: %v", dialErr)
	}
}

func TestListenUnixSocketRecoversStaleSocketPath(t *testing.T) {
	socketPath := testSocketPath(t)

	ctx, cancel := context.WithCancel(context.Background())
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(ctx, "unix", socketPath)
	if err != nil {
		t.Fatalf("listen initial socket: %v", err)
	}

	_ = listener.Close()
	cancel()

	recovered, err := listenUnixSocket(context.Background(), socketPath)
	if err != nil {
		t.Fatalf("listenUnixSocket should recover stale socket path: %v", err)
	}
	defer func() {
		_ = recovered.Close()
	}()

	if _, dialErr := net.Dial("unix", socketPath); dialErr != nil {
		t.Fatalf("recovered socket should be reachable: %v", dialErr)
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
