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
	originalReadActive := agentReadActiveSession
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize
	originalRunSession := agentAttachRunSession

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
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
		agentReadActiveSession = originalReadActive
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
		agentAttachRunSession = originalRunSession
	})

	out, err := executeRootCommandWithInput(string([]byte{0x1c}), "agent", "attach", "claude")
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
	originalReadActive := agentReadActiveSession
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize
	originalRunSession := agentAttachRunSession

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	agentAttachRPC = func(_ context.Context, _ string, _ daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		return daemon.AgentAttachResponse{}, io.EOF
	}
	agentAttachTerminalSize = func(_ *cobra.Command) (uint16, uint16) {
		return 120, 40
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentReadActiveSession = originalReadActive
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
		agentAttachRunSession = originalRunSession
	})

	_, err := executeRootCommand("agent", "attach", "claude")
	if err == nil {
		t.Fatal("agent attach returned nil error on disconnect")
	}

	if err.Error() != "Daemon disconnected. Agent may still be running." {
		t.Fatalf("agent attach error = %q, want %q", err.Error(), "Daemon disconnected. Agent may still be running.")
	}
}

func TestIsDaemonDisconnectErrorDoesNotMatchPlainStringEOF(t *testing.T) {
	if isDaemonDisconnectError(errors.New("EOF")) {
		t.Fatal("plain string EOF error unexpectedly classified as daemon disconnect")
	}
}

func TestAgentAttachStoppedAgentError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := agentReadActiveSession
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize
	originalRunSession := agentAttachRunSession

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
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
		agentReadActiveSession = originalReadActive
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
		agentAttachRunSession = originalRunSession
	})

	_, err := executeRootCommand("agent", "attach", "claude")
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
	originalReadActive := agentReadActiveSession
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize
	originalRunSession := agentAttachRunSession

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
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
		agentReadActiveSession = originalReadActive
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
		agentAttachRunSession = originalRunSession
	})

	_, err := executeRootCommand("agent", "attach", "claude")
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
	originalReadActive := agentReadActiveSession
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize
	originalRunSession := agentAttachRunSession

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
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
		agentReadActiveSession = originalReadActive
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
		agentAttachRunSession = originalRunSession
	})

	_, err := executeRootCommandWithInput(string([]byte{0x1c}), "agent", "attach", "claude")
	if err != nil {
		t.Fatalf("execute agent attach: %v", err)
	}
}

func TestAgentAttachRestoresTerminalBeforeFinalStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := agentReadActiveSession
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize
	originalRunSession := agentAttachRunSession
	originalPrepareTerminal := agentAttachPrepareTerminalFn

	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
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
	agentAttachPrepareTerminalFn = func(cmd *cobra.Command, _ context.Context) (func(), error) {
		return func() {
			_, _ = io.WriteString(cmd.OutOrStdout(), "[terminal-restored]\n")
		}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentReadActiveSession = originalReadActive
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
		agentAttachRunSession = originalRunSession
		agentAttachPrepareTerminalFn = originalPrepareTerminal
	})

	out, err := executeRootCommandWithInput(string([]byte{0x1c}), "agent", "attach", "claude")
	if err != nil {
		t.Fatalf("execute agent attach: %v", err)
	}
	if out != "[terminal-restored]\nDetached from agent \"claude\".\n" {
		t.Fatalf("attach output = %q, want %q", out, "[terminal-restored]\\nDetached from agent \"claude\".\\n")
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

func TestRunAttachResizeLoopIgnoresZeroDimensions(t *testing.T) {
	session := &fakeResizeAttachSession{}
	resizeSignals := make(chan os.Signal, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := runAttachResizeLoop(ctx, session, resizeSignals, func() (uint16, uint16) {
		return 0, 0
	})
	defer stop()

	resizeSignals <- syscall.SIGWINCH
	time.Sleep(50 * time.Millisecond)

	if calls := session.callCount(); calls != 0 {
		t.Fatalf("resize loop call count = %d, want 0", calls)
	}
}

func TestRunAttachResizeLoopStopPreventsFurtherResizes(t *testing.T) {
	session := &fakeResizeAttachSession{}
	resizeSignals := make(chan os.Signal, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := runAttachResizeLoop(ctx, session, resizeSignals, func() (uint16, uint16) {
		return 120, 40
	})

	resizeSignals <- syscall.SIGWINCH
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if session.callCount() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if calls := session.callCount(); calls != 1 {
		t.Fatalf("resize loop initial call count = %d, want 1", calls)
	}

	stop()
	resizeSignals <- syscall.SIGWINCH
	time.Sleep(50 * time.Millisecond)

	if calls := session.callCount(); calls != 1 {
		t.Fatalf("resize loop call count after stop = %d, want 1", calls)
	}
}

type fakeResizeAttachSession struct {
	mu          sync.Mutex
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
	s.mu.Lock()
	defer s.mu.Unlock()

	s.resizeCalls = append(s.resizeCalls, frame.Frame{Type: frame.TypeResize, Payload: []byte{byte(cols), byte(rows)}})
	return nil
}

func (s *fakeResizeAttachSession) Close() error {
	return nil
}

func (s *fakeResizeAttachSession) calledWith(cols, rows uint16) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, call := range s.resizeCalls {
		if len(call.Payload) == 2 && call.Payload[0] == byte(cols) && call.Payload[1] == byte(rows) {
			return true
		}
	}
	return false
}

func (s *fakeResizeAttachSession) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.resizeCalls)
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

func TestAgentAttachRunSessionExitWithoutTrailingOutput(t *testing.T) {
	originalOpen := agentAttachOpenSession
	t.Cleanup(func() {
		agentAttachOpenSession = originalOpen
	})

	exitPayload, err := json.Marshal(agentExitedFramePayload{ExitCode: 0})
	if err != nil {
		t.Fatalf("marshal agent exited payload: %v", err)
	}

	session := &fakeStreamAttachSession{
		frames: []frame.Frame{{Type: frame.TypeAgentExited, Payload: exitPayload}},
	}
	agentAttachOpenSession = func(context.Context, string, string, uint16, uint16) (attachFrameSession, []byte, error) {
		return session, []byte("snapshot-only\n"), nil
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
	if outcome.ExitCode == nil || *outcome.ExitCode != 0 {
		t.Fatalf("run outcome exit code = %v, want 0", outcome.ExitCode)
	}
	if got := out.String(); got != "snapshot-only\n" {
		t.Fatalf("attach output = %q, want %q", got, "snapshot-only\n")
	}
}

func TestAgentAttachRestoresTerminalOnRunSessionPanic(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalResolve := commandResolveSessionIdentifier
	originalReadActive := agentReadActiveSession
	originalAttach := agentAttachRPC
	originalSize := agentAttachTerminalSize
	originalRunSession := agentAttachRunSession
	originalPrepareTerminal := agentAttachPrepareTerminalFn

	restoreCalls := 0
	commandResolveSessionIdentifier = func(context.Context, string, string) (string, error) {
		return "sess-1", nil
	}
	agentReadActiveSession = func() (string, error) {
		return "sess-1", nil
	}
	agentAttachRPC = func(_ context.Context, _ string, _ daemon.AgentAttachRequest) (daemon.AgentAttachResponse, error) {
		return daemon.AgentAttachResponse{Token: "tok-1", Status: "pending"}, nil
	}
	agentAttachTerminalSize = func(_ *cobra.Command) (uint16, uint16) {
		return 120, 40
	}
	agentAttachRunSession = func(_ context.Context, _ io.Reader, _ io.Writer, _ string, _ string, _ uint16, _ uint16, _ <-chan os.Signal, _ func() (uint16, uint16)) (attachSessionOutcome, error) {
		panic("attach run panic")
	}
	agentAttachPrepareTerminalFn = func(_ *cobra.Command, _ context.Context) (func(), error) {
		return func() {
			restoreCalls++
		}, nil
	}
	t.Cleanup(func() {
		commandResolveSessionIdentifier = originalResolve
		agentReadActiveSession = originalReadActive
		agentAttachRPC = originalAttach
		agentAttachTerminalSize = originalSize
		agentAttachRunSession = originalRunSession
		agentAttachPrepareTerminalFn = originalPrepareTerminal
	})

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("agent attach did not panic")
		}
		if recovered != "attach run panic" {
			t.Fatalf("recovered panic = %v, want %q", recovered, "attach run panic")
		}
		if restoreCalls != 1 {
			t.Fatalf("terminal restore calls = %d, want 1", restoreCalls)
		}
	}()

	_, _ = executeRootCommand("agent", "attach", "claude")
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

func TestAgentAttachRunSessionReturnsOnContextCancelWhileReadBlocked(t *testing.T) {
	originalOpen := agentAttachOpenSession
	t.Cleanup(func() {
		agentAttachOpenSession = originalOpen
	})

	session := newFakeBlockingAttachSession()
	agentAttachOpenSession = func(context.Context, string, string, uint16, uint16) (attachFrameSession, []byte, error) {
		return session, nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		_, err := agentAttachRunSession(ctx, bytes.NewReader(nil), io.Discard, "/tmp/ari.sock", "tok-1", 120, 40, nil, nil)
		resultCh <- err
	}()

	time.Sleep(25 * time.Millisecond)
	cancel()

	select {
	case err := <-resultCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("agentAttachRunSession error = %v, want context.Canceled", err)
		}
	case <-time.After(500 * time.Millisecond):
		_ = session.Close()
		t.Fatal("agentAttachRunSession did not return after context cancellation")
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
