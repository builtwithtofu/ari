package rpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/sourcegraph/jsonrpc2"
)

type UnixSocketTransport struct {
	path   string
	server *Server
}

func NewUnixSocketTransport(path string, server *Server) *UnixSocketTransport {
	return &UnixSocketTransport{path: path, server: server}
}

func (t *UnixSocketTransport) Run(ctx context.Context) error {
	if t.server == nil {
		return fmt.Errorf("unix socket transport server is required")
	}

	if t.path == "" {
		return fmt.Errorf("unix socket path is required")
	}

	if err := os.MkdirAll(filepath.Dir(t.path), 0o755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}

	if err := ensureSocketPathAvailable(t.path); err != nil {
		return err
	}

	if err := os.Remove(t.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(ctx, "unix", t.path)
	if err != nil {
		return fmt.Errorf("listen on unix socket: %w", err)
	}

	unixListener, ok := listener.(*net.UnixListener)
	if !ok {
		_ = listener.Close()
		return fmt.Errorf("listen on unix socket: expected *net.UnixListener")
	}

	unixListener.SetUnlinkOnClose(true)

	defer func() {
		_ = unixListener.Close()
		_ = os.Remove(t.path)
	}()

	go func() {
		<-ctx.Done()
		_ = unixListener.Close()
	}()

	var wg sync.WaitGroup
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			if errors.Is(err, net.ErrClosed) {
				break
			}
			return fmt.Errorf("accept unix connection: %w", err)
		}

		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			//nolint:staticcheck // Ari standardizes on PlainObjectCodec framing for local RPC.
			stream := jsonrpc2.NewBufferedStream(c, jsonrpc2.PlainObjectCodec{})
			rpcConn := jsonrpc2.NewConn(context.Background(), stream, t.server)
			<-rpcConn.DisconnectNotify()
			_ = c.Close()
		}(conn)
	}

	wg.Wait()
	if ctx.Err() != nil {
		return nil
	}

	return nil
}

func ensureSocketPathAvailable(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspect daemon socket path %q: %w", path, err)
	}

	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("daemon socket path exists and is not a socket: %s", path)
	}

	conn, err := net.Dial("unix", path)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("daemon socket already in use: %s", path)
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(opErr.Err, syscall.ENOENT) || errors.Is(opErr.Err, syscall.ECONNREFUSED) {
			return nil
		}

		return fmt.Errorf("dial daemon socket %q: %w", path, err)
	}

	if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
		return nil
	}

	return fmt.Errorf("dial daemon socket %q: %w", path, err)
}
