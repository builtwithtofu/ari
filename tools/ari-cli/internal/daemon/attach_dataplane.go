package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/frame"
)

type attachFramePayload struct {
	Token string `json:"token"`
	Cols  uint16 `json:"cols"`
	Rows  uint16 `json:"rows"`
}

type resizeFramePayload struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

type agentExitedFramePayload struct {
	ExitCode int `json:"exit_code"`
}

var writeAllBytesToAgentProcess = writeAllBytes

func (d *Daemon) routeFrameConnection(ctx context.Context, conn net.Conn, firstByte byte) {
	if !frame.IsValidType(frame.Type(firstByte)) {
		_ = conn.Close()
		return
	}
	d.handleAttachDataPlane(ctx, conn)
}

func (d *Daemon) handleAttachDataPlane(ctx context.Context, conn net.Conn) {
	writer := &frameWriter{conn: conn}
	writeProtocolError := func(message string) {
		_ = writer.write(frame.Frame{Type: frame.TypeError, Payload: []byte(message)})
	}

	first, err := frame.ReadFrame(conn)
	if err != nil {
		return
	}
	if first.Type != frame.TypeAttach {
		writeProtocolError("first frame must be attach")
		return
	}

	var attachPayload attachFramePayload
	if err := json.Unmarshal(first.Payload, &attachPayload); err != nil {
		writeProtocolError("invalid attach payload")
		return
	}
	if attachPayload.Token == "" {
		writeProtocolError("attach token is required")
		return
	}

	session, ok := d.markAttachSessionConnected(attachPayload.Token)
	if !ok {
		writeProtocolError("attach token is not active")
		return
	}

	proc, ok := d.getAgentProcess(session.AgentID)
	if !ok {
		d.clearAttachForAgent(session.AgentID)
		writeProtocolError("agent is not running")
		return
	}

	if attachPayload.Rows == 0 || attachPayload.Cols == 0 {
		d.clearAttachForToken(session.AgentID, session.Token)
		writeProtocolError("attach rows and cols must be greater than zero")
		return
	}

	if attachPayload.Rows != session.InitialRows || attachPayload.Cols != session.InitialCols {
		d.clearAttachForToken(session.AgentID, session.Token)
		writeProtocolError("attach dimensions do not match reserved session")
		return
	}

	if err := resizeAgentProcess(proc, session.InitialRows, session.InitialCols); err != nil {
		d.clearAttachForToken(session.AgentID, session.Token)
		writeProtocolError("initialize attach session failed")
		return
	}
	d.setAttachConnection(session.AgentID, conn)
	defer d.clearAttachConnectionIfCurrent(session.AgentID, conn)

	snapshot, updates, unsubscribe := proc.SnapshotAndSubscribe()
	defer unsubscribe()
	defer d.clearAttachForToken(session.AgentID, session.Token)
	if err := writer.write(frame.Frame{Type: frame.TypeSnapshot, Payload: snapshot}); err != nil {
		return
	}

	stopCh := make(chan struct{})
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			_ = conn.SetReadDeadline(time.Now())
			close(stopCh)
		})
	}
	defer stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				stop()
				return
			case <-stopCh:
				return
			case chunk, ok := <-updates:
				if !ok {
					return
				}
				if err := writer.write(frame.Frame{Type: frame.TypeDataServerToClient, Payload: chunk}); err != nil {
					stop()
					return
				}
			}
		}
	}()

	go func() {
		select {
		case <-ctx.Done():
			stop()
			return
		case <-stopCh:
			return
		case <-proc.Done():
		}

		result, waitErr := proc.Wait()
		if waitErr != nil {
			stop()
			return
		}
		payload, _ := json.Marshal(agentExitedFramePayload{ExitCode: result.ExitCode})
		_ = writer.write(frame.Frame{Type: frame.TypeAgentExited, Payload: payload})
		stop()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		default:
		}

		msg, err := frame.ReadFrame(conn)
		if err != nil {
			return
		}

		switch msg.Type {
		case frame.TypeDataClientToServer:
			if err := writeAllBytesToAgentProcess(proc, msg.Payload); err != nil {
				return
			}
		case frame.TypeResize:
			var resize resizeFramePayload
			if err := json.Unmarshal(msg.Payload, &resize); err != nil {
				_ = writer.write(frame.Frame{Type: frame.TypeError, Payload: []byte("invalid resize payload")})
				return
			}
			if resize.Rows == 0 || resize.Cols == 0 {
				_ = writer.write(frame.Frame{Type: frame.TypeError, Payload: []byte("resize rows and cols must be greater than zero")})
				continue
			}
			if err := resizeAgentProcess(proc, resize.Rows, resize.Cols); err != nil {
				_ = writer.write(frame.Frame{Type: frame.TypeError, Payload: []byte("agent is not running")})
				return
			}
		case frame.TypeAttach:
			_ = writer.write(frame.Frame{Type: frame.TypeError, Payload: []byte("attach frame is only valid as the first frame")})
			return
		case frame.TypeSnapshot, frame.TypeError, frame.TypeAgentExited:
			_ = writer.write(frame.Frame{Type: frame.TypeError, Payload: []byte(fmt.Sprintf("unsupported client frame type: 0x%02x", byte(msg.Type)))})
			return
		case frame.TypeDataServerToClient:
			_ = writer.write(frame.Frame{Type: frame.TypeError, Payload: []byte("client cannot send server-to-client data frames")})
			return
		case frame.TypeDetach:
			return
		default:
			_ = writer.write(frame.Frame{Type: frame.TypeError, Payload: []byte(fmt.Sprintf("unknown frame type: 0x%02x", byte(msg.Type)))})
			return
		}
	}
}

type frameWriter struct {
	mu   sync.Mutex
	conn net.Conn
}

func (w *frameWriter) write(msg frame.Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return frame.WriteFrame(w.conn, msg)
}
