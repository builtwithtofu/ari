package client

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

type Client struct {
	socketPath string
	timeout    time.Duration
}

func New(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
		timeout:    5 * time.Second,
	}
}

func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	if c == nil {
		return fmt.Errorf("client is required")
	}

	if c.socketPath == "" {
		return fmt.Errorf("socket path is required")
	}

	if result == nil {
		return fmt.Errorf("result is required")
	}

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()

	rpcCtx := ctx
	if _, ok := ctx.Deadline(); !ok && c.timeout > 0 {
		var cancel context.CancelFunc
		rpcCtx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	//nolint:staticcheck // Ari standardizes on PlainObjectCodec framing for local RPC.
	stream := jsonrpc2.NewBufferedStream(conn, jsonrpc2.PlainObjectCodec{})
	rpcConn := jsonrpc2.NewConn(rpcCtx, stream, jsonrpc2.HandlerWithError(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) (interface{}, error) {
		return nil, nil
	}))
	defer func() {
		_ = rpcConn.Close()
	}()

	if err := rpcConn.Call(rpcCtx, method, params, result); err != nil {
		return err
	}

	return nil
}
