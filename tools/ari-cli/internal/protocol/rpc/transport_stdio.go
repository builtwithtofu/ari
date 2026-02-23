package rpc

import (
	"context"
	"io"
	"os"

	"github.com/sourcegraph/jsonrpc2"
)

type StdioTransport struct {
	server *Server
	conn   *jsonrpc2.Conn
}

func NewStdioTransport(server *Server) *StdioTransport {
	return &StdioTransport{server: server}
}

func (t *StdioTransport) Run(ctx context.Context) error {
	stream := jsonrpc2.NewBufferedStream(
		&stdioReadWriteCloser{r: os.Stdin, w: os.Stdout},
		jsonrpc2.PlainObjectCodec{},
	)

	t.conn = jsonrpc2.NewConn(ctx, stream, t.server)

	select {
	case <-t.conn.DisconnectNotify():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type stdioReadWriteCloser struct {
	r io.Reader
	w io.Writer
}

func (s *stdioReadWriteCloser) Read(p []byte) (n int, err error) {
	return s.r.Read(p)
}

func (s *stdioReadWriteCloser) Write(p []byte) (n int, err error) {
	return s.w.Write(p)
}

func (s *stdioReadWriteCloser) Close() error {
	return nil
}
