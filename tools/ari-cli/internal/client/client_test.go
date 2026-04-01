package client

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type pingRequest struct {
	Message string `json:"message"`
}

type pingResponse struct {
	Message string `json:"message"`
}

func TestClientCall(t *testing.T) {
	registry := rpc.NewMethodRegistry()
	err := rpc.RegisterMethod(registry, rpc.Method[pingRequest, pingResponse]{
		Name: "test.ping",
		Handler: func(ctx context.Context, req pingRequest) (pingResponse, error) {
			_ = ctx
			return pingResponse(req), nil
		},
	})
	if err != nil {
		t.Fatalf("register method: %v", err)
	}

	socketPath := filepath.Join(t.TempDir(), "rpc.sock")
	transport := rpc.NewUnixSocketTransport(socketPath, rpc.NewServer(registry))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- transport.Run(ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		c := New(socketPath)
		var out pingResponse
		err := c.Call(context.Background(), "test.ping", pingRequest{Message: "pong"}, &out)
		if err == nil {
			if out.Message != "pong" {
				t.Fatalf("unexpected response: %+v", out)
			}
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("client call failed before deadline: %v", err)
		}

		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("transport run: %v", err)
	}
}

func TestClientCallRejectsNilResult(t *testing.T) {
	c := New("/tmp/ari-missing.sock")
	err := c.Call(context.Background(), "test.ping", pingRequest{Message: "pong"}, nil)
	if err == nil {
		t.Fatal("expected error for nil result")
	}

	if err.Error() != "result is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}
