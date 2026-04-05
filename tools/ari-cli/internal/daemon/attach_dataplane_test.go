package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/process"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/frame"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestAttachDataPlaneStreamsInputAndOutput(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}
	if err := d.registerAttachMethods(registry, store); err != nil {
		t.Fatalf("registerAttachMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		SessionID: "sess-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "cat"},
	})
	t.Cleanup(func() {
		_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
		waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")
	})

	attachResp := callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 120,
		InitialRows: 40,
	})

	serverConn, clientConn := net.Pipe()
	defer func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.handleAttachDataPlane(ctx, serverConn)

	attachPayload, err := json.Marshal(attachFramePayload{Token: attachResp.Token, Cols: 120, Rows: 40})
	if err != nil {
		t.Fatalf("marshal attach payload: %v", err)
	}
	if err := frame.WriteFrame(clientConn, frame.Frame{Type: frame.TypeAttach, Payload: attachPayload}); err != nil {
		t.Fatalf("write attach frame: %v", err)
	}

	snapshot := readFrameWithTimeout(t, clientConn)
	if snapshot.Type != frame.TypeSnapshot {
		t.Fatalf("first frame type = %d, want %d", snapshot.Type, frame.TypeSnapshot)
	}

	if err := frame.WriteFrame(clientConn, frame.Frame{Type: frame.TypeDataClientToServer, Payload: []byte("ping\n")}); err != nil {
		t.Fatalf("write data frame: %v", err)
	}

	foundOutput := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		msg := readFrameWithTimeout(t, clientConn)
		if msg.Type == frame.TypeDataServerToClient && strings.Contains(string(msg.Payload), "ping") {
			foundOutput = true
			break
		}
	}
	if !foundOutput {
		t.Fatal("did not receive echoed ping output before timeout")
	}

	if err := frame.WriteFrame(clientConn, frame.Frame{Type: frame.TypeDetach, Payload: nil}); err != nil {
		t.Fatalf("write detach frame: %v", err)
	}

	cleanupDeadline := time.Now().Add(500 * time.Millisecond)
	for {
		d.attachMu.Lock()
		_, hasAgent := d.attachByAgent[spawnResp.AgentID]
		_, hasToken := d.attachByToken[attachResp.Token]
		d.attachMu.Unlock()

		if !hasAgent && !hasToken {
			break
		}
		if time.Now().After(cleanupDeadline) {
			t.Fatalf("detach did not clean attach state (hasAgent=%t hasToken=%t)", hasAgent, hasToken)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAttachDataPlaneRejectsUnknownToken(t *testing.T) {
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	serverConn, clientConn := net.Pipe()
	defer func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.handleAttachDataPlane(ctx, serverConn)

	attachPayload, err := json.Marshal(attachFramePayload{Token: "missing-token", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("marshal attach payload: %v", err)
	}
	if err := frame.WriteFrame(clientConn, frame.Frame{Type: frame.TypeAttach, Payload: attachPayload}); err != nil {
		t.Fatalf("write attach frame: %v", err)
	}

	msg := readFrameWithTimeout(t, clientConn)
	if msg.Type != frame.TypeError {
		t.Fatalf("unknown-token response type = %d, want %d", msg.Type, frame.TypeError)
	}
	if got := string(msg.Payload); got != "attach token is not active" {
		t.Fatalf("unknown-token payload = %q, want %q", got, "attach token is not active")
	}
}

func TestAttachDataPlaneRejectsReusedConnectedToken(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}
	if err := d.registerAttachMethods(registry, store); err != nil {
		t.Fatalf("registerAttachMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		SessionID: "sess-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "cat"},
	})
	t.Cleanup(func() {
		_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
		waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")
	})

	attachResp := callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 120,
		InitialRows: 40,
	})

	serverConn1, clientConn1 := net.Pipe()
	defer func() {
		_ = serverConn1.Close()
		_ = clientConn1.Close()
	}()
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	go d.handleAttachDataPlane(ctx1, serverConn1)

	attachPayload, err := json.Marshal(attachFramePayload{Token: attachResp.Token, Cols: 120, Rows: 40})
	if err != nil {
		t.Fatalf("marshal attach payload: %v", err)
	}
	if err := frame.WriteFrame(clientConn1, frame.Frame{Type: frame.TypeAttach, Payload: attachPayload}); err != nil {
		t.Fatalf("write first attach frame: %v", err)
	}
	first := readFrameWithTimeout(t, clientConn1)
	if first.Type != frame.TypeSnapshot {
		t.Fatalf("first attach first frame type = %d, want %d", first.Type, frame.TypeSnapshot)
	}

	serverConn2, clientConn2 := net.Pipe()
	defer func() {
		_ = serverConn2.Close()
		_ = clientConn2.Close()
	}()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go d.handleAttachDataPlane(ctx2, serverConn2)

	if err := frame.WriteFrame(clientConn2, frame.Frame{Type: frame.TypeAttach, Payload: attachPayload}); err != nil {
		t.Fatalf("write second attach frame: %v", err)
	}
	second := readFrameWithTimeout(t, clientConn2)
	if second.Type != frame.TypeError {
		t.Fatalf("second attach frame type = %d, want %d", second.Type, frame.TypeError)
	}
	if got := string(second.Payload); got != "attach token is not active" {
		t.Fatalf("second attach payload = %q, want %q", got, "attach token is not active")
	}
}

func TestAttachDataPlaneRejectsExpiredPendingToken(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}
	if err := d.registerAttachMethods(registry, store); err != nil {
		t.Fatalf("registerAttachMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		SessionID: "sess-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "cat"},
	})
	t.Cleanup(func() {
		_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
		waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")
	})

	attachResp := callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 120,
		InitialRows: 40,
	})

	d.attachMu.Lock()
	session := d.attachByToken[attachResp.Token]
	session.CreatedAt = time.Now().UTC().Add(-(attachPendingSessionTTL + time.Second))
	d.attachByToken[attachResp.Token] = session
	d.attachMu.Unlock()

	serverConn, clientConn := net.Pipe()
	defer func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.handleAttachDataPlane(ctx, serverConn)

	attachPayload, err := json.Marshal(attachFramePayload{Token: attachResp.Token, Cols: 120, Rows: 40})
	if err != nil {
		t.Fatalf("marshal attach payload: %v", err)
	}
	if err := frame.WriteFrame(clientConn, frame.Frame{Type: frame.TypeAttach, Payload: attachPayload}); err != nil {
		t.Fatalf("write attach frame: %v", err)
	}

	msg := readFrameWithTimeout(t, clientConn)
	if msg.Type != frame.TypeError {
		t.Fatalf("expired-token response type = %d, want %d", msg.Type, frame.TypeError)
	}
	if got := string(msg.Payload); got != "attach token is not active" {
		t.Fatalf("expired-token payload = %q, want %q", got, "attach token is not active")
	}
}

func TestAttachDataPlaneSendsAgentExitedFrame(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}
	if err := d.registerAttachMethods(registry, store); err != nil {
		t.Fatalf("registerAttachMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		SessionID: "sess-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "sleep 0.05; exit 7"},
	})

	attachResp := callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 120,
		InitialRows: 40,
	})

	serverConn, clientConn := net.Pipe()
	defer func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.handleAttachDataPlane(ctx, serverConn)

	attachPayload, err := json.Marshal(attachFramePayload{Token: attachResp.Token, Cols: 120, Rows: 40})
	if err != nil {
		t.Fatalf("marshal attach payload: %v", err)
	}
	if err := frame.WriteFrame(clientConn, frame.Frame{Type: frame.TypeAttach, Payload: attachPayload}); err != nil {
		t.Fatalf("write attach frame: %v", err)
	}

	_ = readFrameWithTimeout(t, clientConn) // initial snapshot

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		msg := readFrameWithTimeout(t, clientConn)
		if msg.Type != frame.TypeAgentExited {
			continue
		}

		var exited agentExitedFramePayload
		if err := json.Unmarshal(msg.Payload, &exited); err != nil {
			t.Fatalf("unmarshal agent_exited payload: %v", err)
		}
		if exited.ExitCode != 7 {
			t.Fatalf("agent_exited exit_code = %d, want 7", exited.ExitCode)
		}
		return
	}

	t.Fatal("did not receive agent_exited frame before timeout")
}

func TestAttachDataPlaneReturnsAfterContextCancelWhileClientIdle(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}
	if err := d.registerAttachMethods(registry, store); err != nil {
		t.Fatalf("registerAttachMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		SessionID: "sess-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "cat"},
	})
	t.Cleanup(func() {
		_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
		waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")
	})

	attachResp := callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 120,
		InitialRows: 40,
	})

	serverConn, clientConn := net.Pipe()
	defer func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	handlerDone := make(chan struct{})
	go func() {
		d.handleAttachDataPlane(ctx, serverConn)
		close(handlerDone)
	}()

	attachPayload, err := json.Marshal(attachFramePayload{Token: attachResp.Token, Cols: 120, Rows: 40})
	if err != nil {
		t.Fatalf("marshal attach payload: %v", err)
	}
	if err := frame.WriteFrame(clientConn, frame.Frame{Type: frame.TypeAttach, Payload: attachPayload}); err != nil {
		t.Fatalf("write attach frame: %v", err)
	}

	_ = readFrameWithTimeout(t, clientConn) // snapshot

	cancel()

	select {
	case <-handlerDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("attach data-plane handler did not stop after context cancellation")
	}
}

func TestAttachDataPlaneInitialResizeFailureReturnsErrorFrame(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}
	if err := d.registerAttachMethods(registry, store); err != nil {
		t.Fatalf("registerAttachMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		SessionID: "sess-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "cat"},
	})
	t.Cleanup(func() {
		_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
		waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")
	})

	attachResp := callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 120,
		InitialRows: 40,
	})

	originalResize := resizeAgentProcess
	resizeAgentProcess = func(proc *process.Process, rows, cols uint16) error {
		return errors.New("resize failed")
	}
	t.Cleanup(func() {
		resizeAgentProcess = originalResize
	})

	serverConn, clientConn := net.Pipe()
	defer func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go d.handleAttachDataPlane(ctx, serverConn)

	attachPayload, err := json.Marshal(attachFramePayload{Token: attachResp.Token, Cols: 120, Rows: 40})
	if err != nil {
		t.Fatalf("marshal attach payload: %v", err)
	}
	if err := frame.WriteFrame(clientConn, frame.Frame{Type: frame.TypeAttach, Payload: attachPayload}); err != nil {
		t.Fatalf("write attach frame: %v", err)
	}

	msg := readFrameWithTimeout(t, clientConn)
	if msg.Type != frame.TypeError {
		t.Fatalf("resize-failure response type = %d, want %d", msg.Type, frame.TypeError)
	}
	if got := string(msg.Payload); got != "initialize attach session failed" {
		t.Fatalf("resize-failure payload = %q, want %q", got, "initialize attach session failed")
	}
}

func TestAttachDataPlaneRejectsMismatchedReservedDimensions(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}
	if err := d.registerAttachMethods(registry, store); err != nil {
		t.Fatalf("registerAttachMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		SessionID: "sess-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "cat"},
	})
	t.Cleanup(func() {
		_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
		waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")
	})

	attachResp := callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 120,
		InitialRows: 40,
	})

	serverConn, clientConn := net.Pipe()
	defer func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.handleAttachDataPlane(ctx, serverConn)

	attachPayload, err := json.Marshal(attachFramePayload{Token: attachResp.Token, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("marshal attach payload: %v", err)
	}
	if err := frame.WriteFrame(clientConn, frame.Frame{Type: frame.TypeAttach, Payload: attachPayload}); err != nil {
		t.Fatalf("write attach frame: %v", err)
	}

	msg := readFrameWithTimeout(t, clientConn)
	if msg.Type != frame.TypeError {
		t.Fatalf("mismatched-dimensions response type = %d, want %d", msg.Type, frame.TypeError)
	}
	if got := string(msg.Payload); got != "attach dimensions do not match reserved session" {
		t.Fatalf("mismatched-dimensions payload = %q, want %q", got, "attach dimensions do not match reserved session")
	}
}

func TestAttachDataPlaneRejectsZeroDimensions(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}
	if err := d.registerAttachMethods(registry, store); err != nil {
		t.Fatalf("registerAttachMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		SessionID: "sess-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "cat"},
	})
	t.Cleanup(func() {
		_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
		waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")
	})

	attachResp := callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 120,
		InitialRows: 40,
	})

	serverConn, clientConn := net.Pipe()
	defer func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.handleAttachDataPlane(ctx, serverConn)

	attachPayload, err := json.Marshal(attachFramePayload{Token: attachResp.Token, Cols: 0, Rows: 0})
	if err != nil {
		t.Fatalf("marshal attach payload: %v", err)
	}
	if err := frame.WriteFrame(clientConn, frame.Frame{Type: frame.TypeAttach, Payload: attachPayload}); err != nil {
		t.Fatalf("write attach frame: %v", err)
	}

	msg := readFrameWithTimeout(t, clientConn)
	if msg.Type != frame.TypeError {
		t.Fatalf("zero-dimensions response type = %d, want %d", msg.Type, frame.TypeError)
	}
	if got := string(msg.Payload); got != "attach rows and cols must be greater than zero" {
		t.Fatalf("zero-dimensions payload = %q, want %q", got, "attach rows and cols must be greater than zero")
	}
}

func TestAttachDataPlaneRejectsStoppedAgentAndClearsAttachState(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}
	if err := d.registerAttachMethods(registry, store); err != nil {
		t.Fatalf("registerAttachMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		SessionID: "sess-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "while true; do sleep 1; done"},
	})

	attachResp := callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 120,
		InitialRows: 40,
	})

	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
	waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")

	serverConn, clientConn := net.Pipe()
	defer func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.handleAttachDataPlane(ctx, serverConn)

	attachPayload, err := json.Marshal(attachFramePayload{Token: attachResp.Token, Cols: 120, Rows: 40})
	if err != nil {
		t.Fatalf("marshal attach payload: %v", err)
	}
	if err := frame.WriteFrame(clientConn, frame.Frame{Type: frame.TypeAttach, Payload: attachPayload}); err != nil {
		t.Fatalf("write attach frame: %v", err)
	}

	msg := readFrameWithTimeout(t, clientConn)
	if msg.Type != frame.TypeError {
		t.Fatalf("stopped-agent response type = %d, want %d", msg.Type, frame.TypeError)
	}
	if got := string(msg.Payload); got != "attach token is not active" {
		t.Fatalf("stopped-agent payload = %q, want %q", got, "attach token is not active")
	}

	d.attachMu.Lock()
	_, hasAgent := d.attachByAgent[spawnResp.AgentID]
	_, hasToken := d.attachByToken[attachResp.Token]
	d.attachMu.Unlock()
	if hasAgent || hasToken {
		t.Fatalf("stopped-agent attach state not cleared (hasAgent=%t hasToken=%t)", hasAgent, hasToken)
	}
}

func readFrameWithTimeout(t *testing.T, conn net.Conn) frame.Frame {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline returned error: %v", err)
	}

	msg, err := frame.ReadFrame(conn)
	if err != nil {
		t.Fatalf("ReadFrame returned error: %v", err)
	}

	return msg
}
