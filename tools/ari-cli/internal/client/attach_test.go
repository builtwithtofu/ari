package client

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/frame"
)

func TestOpenAttachSessionSendsAttachAndReadsSnapshot(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	originalDial := attachDialContext
	attachDialContext = func(context.Context, string) (net.Conn, error) {
		return clientConn, nil
	}
	t.Cleanup(func() {
		attachDialContext = originalDial
	})

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		gotAttach, err := frame.ReadFrame(serverConn)
		if err != nil {
			t.Errorf("ReadFrame(attach) returned error: %v", err)
			return
		}
		if gotAttach.Type != frame.TypeAttach {
			t.Errorf("attach frame type = %d, want %d", gotAttach.Type, frame.TypeAttach)
			return
		}

		var payload attachHandshakePayload
		if err := json.Unmarshal(gotAttach.Payload, &payload); err != nil {
			t.Errorf("unmarshal attach payload: %v", err)
			return
		}
		if payload.Token != "tok-1" || payload.Cols != 120 || payload.Rows != 40 {
			t.Errorf("attach payload = %+v, want token tok-1 cols 120 rows 40", payload)
			return
		}

		if err := frame.WriteFrame(serverConn, frame.Frame{Type: frame.TypeSnapshot, Payload: []byte("hello")}); err != nil {
			t.Errorf("WriteFrame(snapshot) returned error: %v", err)
		}
	}()

	session, snapshot, err := OpenAttachSession(context.Background(), "/tmp/ari.sock", AttachConnectRequest{Token: "tok-1", Cols: 120, Rows: 40})
	if err != nil {
		t.Fatalf("OpenAttachSession returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
	})
	if string(snapshot) != "hello" {
		t.Fatalf("snapshot = %q, want %q", string(snapshot), "hello")
	}

	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("server goroutine did not finish")
	}
}

func TestOpenAttachSessionReturnsErrorFrameMessage(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	originalDial := attachDialContext
	attachDialContext = func(context.Context, string) (net.Conn, error) {
		return clientConn, nil
	}
	t.Cleanup(func() {
		attachDialContext = originalDial
	})

	go func() {
		_, _ = frame.ReadFrame(serverConn)
		_ = frame.WriteFrame(serverConn, frame.Frame{Type: frame.TypeError, Payload: []byte("attach token is not active")})
	}()

	_, _, err := OpenAttachSession(context.Background(), "/tmp/ari.sock", AttachConnectRequest{Token: "tok-1", Cols: 120, Rows: 40})
	if err == nil {
		t.Fatal("OpenAttachSession returned nil error for error frame")
	}
	if !errors.Is(err, ErrAttachProtocol) {
		t.Fatalf("OpenAttachSession error = %v, want ErrAttachProtocol", err)
	}
	if err.Error() != "attach protocol error: attach token is not active" {
		t.Fatalf("OpenAttachSession error text = %q, want %q", err.Error(), "attach protocol error: attach token is not active")
	}
}
