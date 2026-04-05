package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/frame"
)

var ErrAttachProtocol = errors.New("attach protocol error")

var attachDialContext = func(ctx context.Context, socketPath string) (net.Conn, error) {
	dialer := net.Dialer{}
	return dialer.DialContext(ctx, "unix", socketPath)
}

type AttachConnectRequest struct {
	Token string
	Cols  uint16
	Rows  uint16
}

type attachHandshakePayload struct {
	Token string `json:"token"`
	Cols  uint16 `json:"cols"`
	Rows  uint16 `json:"rows"`
}

type resizePayload struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

type AttachSession struct {
	conn    net.Conn
	writeMu sync.Mutex
}

func OpenAttachSession(ctx context.Context, socketPath string, req AttachConnectRequest) (*AttachSession, []byte, error) {
	if ctx == nil {
		return nil, nil, fmt.Errorf("open attach session: context is required")
	}
	if socketPath == "" {
		return nil, nil, fmt.Errorf("open attach session: socket path is required")
	}
	if req.Token == "" {
		return nil, nil, fmt.Errorf("open attach session: token is required")
	}
	if req.Cols == 0 || req.Rows == 0 {
		return nil, nil, fmt.Errorf("open attach session: cols and rows must be greater than zero")
	}

	conn, err := attachDialContext(ctx, socketPath)
	if err != nil {
		return nil, nil, err
	}

	handshake, err := json.Marshal(attachHandshakePayload(req))
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("open attach session: marshal attach payload: %w", err)
	}

	if err := frame.WriteFrame(conn, frame.Frame{Type: frame.TypeAttach, Payload: handshake}); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("open attach session: write attach frame: %w", err)
	}

	first, err := frame.ReadFrame(conn)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if first.Type == frame.TypeError {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("%w: %s", ErrAttachProtocol, string(first.Payload))
	}
	if first.Type != frame.TypeSnapshot {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("%w: first frame must be snapshot", ErrAttachProtocol)
	}

	session := &AttachSession{conn: conn}
	return session, append([]byte(nil), first.Payload...), nil
}

func (s *AttachSession) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

func (s *AttachSession) ReadFrame() (frame.Frame, error) {
	if s == nil || s.conn == nil {
		return frame.Frame{}, fmt.Errorf("read attach frame: session is required")
	}

	return frame.ReadFrame(s.conn)
}

func (s *AttachSession) SendData(payload []byte) error {
	if s == nil || s.conn == nil {
		return fmt.Errorf("send attach data: session is required")
	}
	if len(payload) == 0 {
		return nil
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	return frame.WriteFrame(s.conn, frame.Frame{Type: frame.TypeDataClientToServer, Payload: payload})
}

func (s *AttachSession) SendResize(cols, rows uint16) error {
	if s == nil || s.conn == nil {
		return fmt.Errorf("send attach resize: session is required")
	}
	if cols == 0 || rows == 0 {
		return fmt.Errorf("send attach resize: cols and rows must be greater than zero")
	}

	payload, err := json.Marshal(resizePayload{Cols: cols, Rows: rows})
	if err != nil {
		return fmt.Errorf("send attach resize: marshal resize payload: %w", err)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	return frame.WriteFrame(s.conn, frame.Frame{Type: frame.TypeResize, Payload: payload})
}

func (s *AttachSession) SendDetach() error {
	if s == nil || s.conn == nil {
		return fmt.Errorf("send attach detach: session is required")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	return frame.WriteFrame(s.conn, frame.Frame{Type: frame.TypeDetach})
}
