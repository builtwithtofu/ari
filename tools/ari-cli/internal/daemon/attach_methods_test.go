package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/process"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/frame"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestAgentAttachReturnsTokenForRunningAgent(t *testing.T) {
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

	if attachResp.Token == "" {
		t.Fatal("agent.attach token = empty, want non-empty")
	}
	if attachResp.Status != "pending" {
		t.Fatalf("agent.attach status = %q, want %q", attachResp.Status, "pending")
	}

	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
	waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")
}

func TestAgentAttachErrorsForStoppedAgent(t *testing.T) {
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
		Args:      []string{"-c", "exit 0"},
	})
	waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "exited")

	spec, ok := registry.Get("agent.attach")
	if !ok {
		t.Fatal("agent.attach method not registered")
	}

	raw, err := json.Marshal(AgentAttachRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID, InitialCols: 100, InitialRows: 30})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("agent.attach returned nil error for stopped agent")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("agent.attach error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.AgentNotRunning {
		t.Fatalf("agent.attach error code = %d, want %d", rpcErr.Code, rpc.AgentNotRunning)
	}
}

func TestAgentAttachRejectsSecondActiveAttach(t *testing.T) {
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
	clientConn := openAttachSession(t, d, attachResp.Token, 120, 40)
	defer func() {
		_ = clientConn.Close()
	}()

	spec, ok := registry.Get("agent.attach")
	if !ok {
		t.Fatal("agent.attach method not registered")
	}
	raw, err := json.Marshal(AgentAttachRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID, InitialCols: 80, InitialRows: 24})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("second agent.attach returned nil error")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("second agent.attach error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.AgentAlreadyAttached {
		t.Fatalf("second agent.attach error code = %d, want %d", rpcErr.Code, rpc.AgentAlreadyAttached)
	}
}

func TestAgentAttachDoesNotResizeDuringTokenReservation(t *testing.T) {
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
	t.Cleanup(func() {
		_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
		waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")
	})

	originalResize := resizeAgentProcess
	called := false
	resizeAgentProcess = func(proc *process.Process, rows, cols uint16) error {
		called = true
		return nil
	}
	t.Cleanup(func() {
		resizeAgentProcess = originalResize
	})

	_ = callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 140,
		InitialRows: 55,
	})

	if called {
		t.Fatal("agent.attach triggered resize during token reservation")
	}
}

func TestAgentDetachRemovesAttachAndAllowsReattach(t *testing.T) {
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
	clientConn := openAttachSession(t, d, attachResp.Token, 120, 40)
	defer func() {
		_ = clientConn.Close()
	}()

	detachResp := callMethod[AgentDetachResponse](t, registry, "agent.detach", AgentDetachRequest{
		SessionID: "sess-1",
		AgentID:   spawnResp.AgentID,
	})
	if detachResp.Status != "detached" {
		t.Fatalf("agent.detach status = %q, want detached", detachResp.Status)
	}

	if err := clientConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err == nil {
		if _, err := frame.ReadFrame(clientConn); err == nil {
			t.Fatal("old attach connection remained open after agent.detach")
		}
	}

	retryResp := callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 80,
		InitialRows: 24,
	})
	if retryResp.Token == "" {
		t.Fatal("reattach token = empty, want non-empty")
	}
}

func TestAgentSendRejectedWhileAttached(t *testing.T) {
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
	clientConn := openAttachSession(t, d, attachResp.Token, 120, 40)
	defer func() {
		_ = clientConn.Close()
	}()

	spec, ok := registry.Get("agent.send")
	if !ok {
		t.Fatal("agent.send method not registered")
	}
	raw, err := json.Marshal(AgentSendRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID, Input: "hello"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("agent.send returned nil error while attached")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("agent.send error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.AgentAlreadyAttached {
		t.Fatalf("agent.send error code = %d, want %d", rpcErr.Code, rpc.AgentAlreadyAttached)
	}
}

func TestAgentSendRejectedWhileAttachReservationIsPending(t *testing.T) {
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

	_ = callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 120,
		InitialRows: 40,
	})

	spec, ok := registry.Get("agent.send")
	if !ok {
		t.Fatal("agent.send method not registered")
	}
	raw, err := json.Marshal(AgentSendRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID, Input: "hello"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("agent.send returned nil error while attach reservation is pending")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("agent.send error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.AgentAlreadyAttached {
		t.Fatalf("agent.send error code = %d, want %d", rpcErr.Code, rpc.AgentAlreadyAttached)
	}
}

func TestAgentSendReturnsNotRunningAfterAttachedAgentStops(t *testing.T) {
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

	attachResp := callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 120,
		InitialRows: 40,
	})
	clientConn := openAttachSession(t, d, attachResp.Token, 120, 40)
	defer func() {
		_ = clientConn.Close()
	}()

	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
	waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")

	spec, ok := registry.Get("agent.send")
	if !ok {
		t.Fatal("agent.send method not registered")
	}
	raw, err := json.Marshal(AgentSendRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID, Input: "hello"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("agent.send returned nil error after stop")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("agent.send error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.AgentNotRunning {
		t.Fatalf("agent.send error code = %d, want %d", rpcErr.Code, rpc.AgentNotRunning)
	}
}

func TestAgentAttachConcurrentOnlyOneSucceeds(t *testing.T) {
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
	t.Cleanup(func() {
		_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
		waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")
	})

	spec, ok := registry.Get("agent.attach")
	if !ok {
		t.Fatal("agent.attach method not registered")
	}

	raw, err := json.Marshal(AgentAttachRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID, InitialCols: 80, InitialRows: 24})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, callErr := spec.Call(context.Background(), raw)
			results <- callErr
		}()
	}
	wg.Wait()
	close(results)

	successes := 0
	alreadyAttached := 0
	for callErr := range results {
		if callErr == nil {
			successes++
			continue
		}
		var rpcErr *rpc.HandlerError
		if !errors.As(callErr, &rpcErr) {
			t.Fatalf("agent.attach error type = %T, want *rpc.HandlerError", callErr)
		}
		if rpcErr.Code == rpc.AgentAlreadyAttached {
			alreadyAttached++
			continue
		}
		t.Fatalf("agent.attach error code = %d, want %d", rpcErr.Code, rpc.AgentAlreadyAttached)
	}

	if successes != 1 || alreadyAttached != 1 {
		t.Fatalf("concurrent attach outcomes success/alreadyAttached = %d/%d, want 1/1", successes, alreadyAttached)
	}
}

func TestAgentAttachPendingReservationAllowsRetryWithNewDimensions(t *testing.T) {
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

	first := callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 80,
		InitialRows: 24,
	})
	second := callMethod[AgentAttachResponse](t, registry, "agent.attach", AgentAttachRequest{
		SessionID:   "sess-1",
		AgentID:     spawnResp.AgentID,
		InitialCols: 120,
		InitialRows: 40,
	})

	if first.Token == second.Token {
		t.Fatal("agent.attach retry returned the same token for new dimensions")
	}

	if _, ok := d.markAttachSessionConnected(first.Token); ok {
		t.Fatal("old pending token stayed active after reservation replacement")
	}
	if _, ok := d.markAttachSessionConnected(second.Token); !ok {
		t.Fatal("new pending token did not become active")
	}
}

func TestClearAttachForTokenDoesNotClearDifferentActiveToken(t *testing.T) {
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	d.attachMu.Lock()
	d.attachByToken["tok-old"] = attachSession{Token: "tok-old", AgentID: "agt-1", CreatedAt: time.Now().UTC(), Connected: true}
	d.attachByToken["tok-new"] = attachSession{Token: "tok-new", AgentID: "agt-1", CreatedAt: time.Now().UTC(), Connected: true}
	d.attachByAgent["agt-1"] = "tok-new"
	d.attachMu.Unlock()

	d.clearAttachForToken("agt-1", "tok-old")

	d.attachMu.RLock()
	defer d.attachMu.RUnlock()

	if active := d.attachByAgent["agt-1"]; active != "tok-new" {
		t.Fatalf("attachByAgent active token = %q, want %q", active, "tok-new")
	}
	if _, ok := d.attachByToken["tok-new"]; !ok {
		t.Fatal("new token missing after old-token cleanup")
	}
	if _, ok := d.attachByToken["tok-old"]; ok {
		t.Fatal("old token still present after old-token cleanup")
	}
}

func openAttachSession(t *testing.T, d *Daemon, token string, cols, rows uint16) net.Conn {
	t.Helper()

	serverConn, clientConn := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	go d.handleAttachDataPlane(ctx, serverConn)

	payload, err := json.Marshal(attachFramePayload{Token: token, Cols: cols, Rows: rows})
	if err != nil {
		t.Fatalf("marshal attach payload: %v", err)
	}
	if err := frame.WriteFrame(clientConn, frame.Frame{Type: frame.TypeAttach, Payload: payload}); err != nil {
		t.Fatalf("write attach frame: %v", err)
	}

	msg := readFrameWithTimeout(t, clientConn)
	if msg.Type != frame.TypeSnapshot {
		t.Fatalf("first data-plane frame type = %d, want %d", msg.Type, frame.TypeSnapshot)
	}

	return clientConn
}
