package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/frame"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/spf13/cobra"
)

func TestAgentAttachDetachViaCtrlBackslash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize
	originalRunSession := agentAttachRunSession

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentAttachRPC = func(_ context.Context, _ string, _ daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		return daemon.AgentAttachResponse{Token: "tok-1", Status: "pending"}, nil
	}
	agentAttachTerminalSize = func(_ *cobra.Command) (uint16, uint16) {
		return 120, 40
	}
	agentAttachRunSession = func(_ context.Context, _ io.Reader, _ io.Writer, _ string, _ string, _ uint16, _ uint16, _ <-chan os.Signal, _ func() (uint16, uint16)) (attachSessionOutcome, error) {
		return attachSessionOutcome{Detached: true}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
		agentAttachRunSession = originalRunSession
	})

	out, err := executeRootCommandWithInput(string([]byte{0x1c}), "agent", "attach", "alpha", "claude")
	if err != nil {
		t.Fatalf("execute agent attach: %v", err)
	}

	if out != "Detached from agent \"claude\".\n" {
		t.Fatalf("attach output = %q, want %q", out, "Detached from agent \"claude\".\n")
	}
}

func TestAgentAttachDaemonDisconnectMessage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize
	originalRunSession := agentAttachRunSession

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentAttachRPC = func(_ context.Context, _ string, _ daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		return daemon.AgentAttachResponse{}, errors.New("EOF")
	}
	agentAttachTerminalSize = func(_ *cobra.Command) (uint16, uint16) {
		return 120, 40
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
		agentAttachRunSession = originalRunSession
	})

	_, err := executeRootCommand("agent", "attach", "alpha", "claude")
	if err == nil {
		t.Fatal("agent attach returned nil error on disconnect")
	}

	if err.Error() != "Daemon disconnected. Agent may still be running." {
		t.Fatalf("agent attach error = %q, want %q", err.Error(), "Daemon disconnected. Agent may still be running.")
	}
}

func TestAgentAttachStoppedAgentError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize
	originalRunSession := agentAttachRunSession

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentAttachRPC = func(_ context.Context, _ string, _ daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		return daemon.AgentAttachResponse{}, &jsonrpc2.Error{Code: int64(rpc.AgentNotRunning), Message: "agent is not running"}
	}
	agentAttachTerminalSize = func(_ *cobra.Command) (uint16, uint16) {
		return 120, 40
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
		agentAttachRunSession = originalRunSession
	})

	_, err := executeRootCommand("agent", "attach", "alpha", "claude")
	if err == nil {
		t.Fatal("agent attach returned nil error for stopped agent")
	}
	if err.Error() != "Agent is not running" {
		t.Fatalf("agent attach error = %q, want %q", err.Error(), "Agent is not running")
	}
}

func TestAgentAttachActiveWriterError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize
	originalRunSession := agentAttachRunSession

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentAttachRPC = func(_ context.Context, _ string, _ daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		return daemon.AgentAttachResponse{}, &jsonrpc2.Error{Code: int64(rpc.AgentAlreadyAttached), Message: "agent already has an active attach session"}
	}
	agentAttachTerminalSize = func(_ *cobra.Command) (uint16, uint16) {
		return 120, 40
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
		agentAttachRunSession = originalRunSession
	})

	_, err := executeRootCommand("agent", "attach", "alpha", "claude")
	if err == nil {
		t.Fatal("agent attach returned nil error for active writer")
	}
	if err.Error() != "Agent already has an active attach session" {
		t.Fatalf("agent attach error = %q, want %q", err.Error(), "Agent already has an active attach session")
	}
}

func TestAgentAttachRunSessionUsesCommandContextWithoutTimeout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize
	originalRunSession := agentAttachRunSession

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentAttachRPC = func(_ context.Context, _ string, _ daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		return daemon.AgentAttachResponse{Token: "tok-1", Status: "pending"}, nil
	}
	agentAttachTerminalSize = func(_ *cobra.Command) (uint16, uint16) {
		return 120, 40
	}
	agentAttachRunSession = func(ctx context.Context, _ io.Reader, _ io.Writer, _ string, _ string, _ uint16, _ uint16, _ <-chan os.Signal, _ func() (uint16, uint16)) (attachSessionOutcome, error) {
		if _, hasDeadline := ctx.Deadline(); hasDeadline {
			return attachSessionOutcome{}, errors.New("attach run session context unexpectedly has timeout deadline")
		}
		return attachSessionOutcome{Detached: true}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
		agentAttachRunSession = originalRunSession
	})

	_, err := executeRootCommandWithInput(string([]byte{0x1c}), "agent", "attach", "alpha", "claude")
	if err != nil {
		t.Fatalf("execute agent attach: %v", err)
	}
}

func TestRunAttachResizeLoopForwardsSIGWINCH(t *testing.T) {
	session := &fakeResizeAttachSession{}
	resizeSignals := make(chan os.Signal, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := runAttachResizeLoop(ctx, session, resizeSignals, func() (uint16, uint16) {
		return 132, 41
	})
	defer stop()

	resizeSignals <- syscall.SIGWINCH

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if session.calledWith(132, 41) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("resize loop did not forward SIGWINCH dimensions")
}

type fakeResizeAttachSession struct {
	resizeCalls []frame.Frame
}

func (s *fakeResizeAttachSession) ReadFrame() (frame.Frame, error) {
	return frame.Frame{}, io.EOF
}

func (s *fakeResizeAttachSession) SendData([]byte) error {
	return nil
}

func (s *fakeResizeAttachSession) SendDetach() error {
	return nil
}

func (s *fakeResizeAttachSession) SendResize(cols, rows uint16) error {
	s.resizeCalls = append(s.resizeCalls, frame.Frame{Type: frame.TypeResize, Payload: []byte{byte(cols), byte(rows)}})
	return nil
}

func (s *fakeResizeAttachSession) Close() error {
	return nil
}

func (s *fakeResizeAttachSession) calledWith(cols, rows uint16) bool {
	for _, call := range s.resizeCalls {
		if len(call.Payload) == 2 && call.Payload[0] == byte(cols) && call.Payload[1] == byte(rows) {
			return true
		}
	}
	return false
}

func TestAgentAttachRunSessionStreamsDataBeforeAgentExit(t *testing.T) {
	originalOpen := agentAttachOpenSession
	t.Cleanup(func() {
		agentAttachOpenSession = originalOpen
	})

	exitPayload, err := json.Marshal(agentExitedFramePayload{ExitCode: 7})
	if err != nil {
		t.Fatalf("marshal agent exited payload: %v", err)
	}

	session := &fakeStreamAttachSession{
		frames: []frame.Frame{
			{Type: frame.TypeDataServerToClient, Payload: []byte("live-output\n")},
			{Type: frame.TypeAgentExited, Payload: exitPayload},
		},
	}
	agentAttachOpenSession = func(context.Context, string, string, uint16, uint16) (attachFrameSession, []byte, error) {
		return session, []byte("snapshot\n"), nil
	}

	reader, writer := io.Pipe()
	defer func() {
		_ = writer.Close()
	}()
	var out bytes.Buffer

	outcome, err := agentAttachRunSession(context.Background(), reader, &out, "/tmp/ari.sock", "tok-1", 120, 40, nil, nil)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("agentAttachRunSession returned error: %v", err)
	}
	if outcome.ExitCode == nil || *outcome.ExitCode != 7 {
		t.Fatalf("run outcome exit code = %v, want 7", outcome.ExitCode)
	}
	if got := out.String(); got != "snapshot\nlive-output\n" {
		t.Fatalf("attach output = %q, want %q", got, "snapshot\nlive-output\n")
	}
}

func TestAgentAttachRunSessionDetachClosesIdleRead(t *testing.T) {
	originalOpen := agentAttachOpenSession
	t.Cleanup(func() {
		agentAttachOpenSession = originalOpen
	})

	session := newFakeBlockingAttachSession()
	agentAttachOpenSession = func(context.Context, string, string, uint16, uint16) (attachFrameSession, []byte, error) {
		return session, nil, nil
	}

	input := bytes.NewReader([]byte("abc\x1cdef"))
	outcome, err := agentAttachRunSession(context.Background(), input, io.Discard, "/tmp/ari.sock", "tok-1", 120, 40, nil, nil)
	if err != nil {
		t.Fatalf("agentAttachRunSession returned error: %v", err)
	}
	if !outcome.Detached {
		t.Fatal("attach run session did not report detach outcome")
	}
	if !session.detachSent {
		t.Fatal("attach run session did not send detach frame")
	}
	if got := string(session.sentData); got != "abc" {
		t.Fatalf("attach run session data = %q, want %q", got, "abc")
	}
}

func TestAgentAttachRunSessionLocalInputEOFDropsInputOnly(t *testing.T) {
	originalOpen := agentAttachOpenSession
	t.Cleanup(func() {
		agentAttachOpenSession = originalOpen
	})

	exitPayload, err := json.Marshal(agentExitedFramePayload{ExitCode: 0})
	if err != nil {
		t.Fatalf("marshal agent exited payload: %v", err)
	}
	session := &fakeStreamAttachSession{frames: []frame.Frame{{Type: frame.TypeAgentExited, Payload: exitPayload}}}
	agentAttachOpenSession = func(context.Context, string, string, uint16, uint16) (attachFrameSession, []byte, error) {
		return session, nil, nil
	}

	reader := bytes.NewReader(nil)
	outcome, err := agentAttachRunSession(context.Background(), reader, io.Discard, "/tmp/ari.sock", "tok-1", 120, 40, nil, nil)
	if err != nil {
		t.Fatalf("agentAttachRunSession returned error: %v", err)
	}
	if outcome.ExitCode == nil || *outcome.ExitCode != 0 {
		t.Fatalf("attach run session exit outcome = %v, want 0", outcome.ExitCode)
	}
}

type fakeStreamAttachSession struct {
	frames []frame.Frame
	index  int
}

type fakeBlockingAttachSession struct {
	sentData   []byte
	detachSent bool
	closedCh   chan struct{}
	once       sync.Once
}

func newFakeBlockingAttachSession() *fakeBlockingAttachSession {
	return &fakeBlockingAttachSession{closedCh: make(chan struct{})}
}

func (s *fakeStreamAttachSession) ReadFrame() (frame.Frame, error) {
	if s.index >= len(s.frames) {
		return frame.Frame{}, io.EOF
	}
	frameValue := s.frames[s.index]
	s.index++
	return frameValue, nil
}

func (s *fakeStreamAttachSession) SendData([]byte) error {
	return nil
}

func (s *fakeStreamAttachSession) SendDetach() error {
	return nil
}

func (s *fakeStreamAttachSession) SendResize(uint16, uint16) error {
	return nil
}

func (s *fakeStreamAttachSession) Close() error {
	return nil
}

func (s *fakeBlockingAttachSession) ReadFrame() (frame.Frame, error) {
	<-s.closedCh
	return frame.Frame{}, io.EOF
}

func (s *fakeBlockingAttachSession) SendData(payload []byte) error {
	s.sentData = append(s.sentData, payload...)
	return nil
}

func (s *fakeBlockingAttachSession) SendDetach() error {
	s.detachSent = true
	return nil
}

func (s *fakeBlockingAttachSession) SendResize(uint16, uint16) error {
	return nil
}

func (s *fakeBlockingAttachSession) Close() error {
	s.once.Do(func() {
		close(s.closedCh)
	})
	return nil
}
