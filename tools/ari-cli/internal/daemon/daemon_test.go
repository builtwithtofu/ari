package daemon

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

func TestDaemonStatusAndStopOverRPC(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "daemon.sock")
	d := New(socketPath, "test-version")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Start(ctx)
	}()

	var status StatusResponse
	callDaemonMethod(t, socketPath, "daemon.status", StatusRequest{}, &status)

	if status.Version != "test-version" {
		t.Fatalf("unexpected version: %q", status.Version)
	}

	if status.PID <= 0 {
		t.Fatalf("expected pid, got %d", status.PID)
	}

	if status.SocketPath != socketPath {
		t.Fatalf("unexpected socket path: %q", status.SocketPath)
	}

	var stop StopResponse
	callDaemonMethod(t, socketPath, "daemon.stop", StopRequest{}, &stop)

	if stop.Status != "stopping" {
		t.Fatalf("unexpected stop status: %q", stop.Status)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("daemon start returned error: %v", err)
	}
}

func callDaemonMethod(t *testing.T, socketPath, method string, params any, result any) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var dialErr error
	var conn net.Conn
	for i := 0; i < 50; i++ {
		conn, dialErr = net.Dial("unix", socketPath)
		if dialErr == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if dialErr != nil {
		t.Fatalf("dial daemon socket: %v", dialErr)
	}
	defer func() {
		_ = conn.Close()
	}()

	//nolint:staticcheck // Ari standardizes on PlainObjectCodec framing for local RPC.
	stream := jsonrpc2.NewBufferedStream(conn, jsonrpc2.PlainObjectCodec{})
	rpcConn := jsonrpc2.NewConn(ctx, stream, jsonrpc2.HandlerWithError(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (interface{}, error) {
		return nil, nil
	}))
	defer func() {
		_ = rpcConn.Close()
	}()

	if err := rpcConn.Call(ctx, method, params, result); err != nil {
		t.Fatalf("call %s: %v", method, err)
	}
}
