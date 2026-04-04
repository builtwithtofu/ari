package rpc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/frame"
	"github.com/sourcegraph/jsonrpc2"
)

type FrameRouter func(ctx context.Context, conn net.Conn, firstByte byte)

type UnixSocketTransport struct {
	path        string
	server      *Server
	frameRouter FrameRouter
}

func NewUnixSocketTransport(path string, server *Server) *UnixSocketTransport {
	return &UnixSocketTransport{path: path, server: server}
}

func NewUnixSocketTransportWithFrameRouter(path string, server *Server, frameRouter FrameRouter) *UnixSocketTransport {
	return &UnixSocketTransport{path: path, server: server, frameRouter: frameRouter}
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

	listener, err := listenUnixSocket(ctx, t.path)
	if err != nil {
		return err
	}

	unixListener, ok := listener.(*net.UnixListener)
	if !ok {
		_ = listener.Close()
		return fmt.Errorf("listen on unix socket: expected *net.UnixListener")
	}

	unixListener.SetUnlinkOnClose(true)

	defer func() {
		_ = unixListener.Close()
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

			closeOnCancelDone := make(chan struct{})
			go func() {
				select {
				case <-ctx.Done():
					_ = c.Close()
				case <-closeOnCancelDone:
				}
			}()
			defer close(closeOnCancelDone)

			reader := bufio.NewReader(c)
			firstByte, err := readFirstNonSpaceByte(reader)
			if err != nil {
				_ = c.Close()
				return
			}
			if err := reader.UnreadByte(); err != nil {
				_ = c.Close()
				return
			}

			conn := &bufferedConn{Conn: c, reader: reader}

			if firstByte == '{' {
				//nolint:staticcheck // Ari standardizes on PlainObjectCodec framing for local RPC.
				stream := jsonrpc2.NewBufferedStream(conn, jsonrpc2.PlainObjectCodec{})
				rpcConn := jsonrpc2.NewConn(ctx, stream, t.server)
				select {
				case <-rpcConn.DisconnectNotify():
				case <-ctx.Done():
					_ = rpcConn.Close()
				}
				_ = conn.Close()
				return
			}

			if frame.IsValidType(frame.Type(firstByte)) && t.frameRouter != nil {
				t.frameRouter(ctx, conn, firstByte)
				_ = conn.Close()
				return
			}

			_ = conn.Close()
		}(conn)
	}

	wg.Wait()
	if ctx.Err() != nil {
		return nil
	}

	return nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func readFirstNonSpaceByte(reader *bufio.Reader) (byte, error) {
	for {
		value, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}

		switch value {
		case ' ', '\n', '\r', '\t':
			continue
		default:
			return value, nil
		}
	}
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

func listenUnixSocket(ctx context.Context, path string) (net.Listener, error) {
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(ctx, "unix", path)
	if err == nil {
		return listener, nil
	}

	if !errors.Is(err, syscall.EADDRINUSE) {
		return nil, fmt.Errorf("listen on unix socket: %w", err)
	}

	if staleErr := ensureSocketPathAvailable(path); staleErr != nil {
		return nil, staleErr
	}

	if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale socket: %w", removeErr)
	}

	listener, err = listenConfig.Listen(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on unix socket: %w", err)
	}

	return listener, nil
}
