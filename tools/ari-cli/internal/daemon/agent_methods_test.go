package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	_ "modernc.org/sqlite"
)

func TestAgentSpawnSendOutputStopLifecycle(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		SessionID: "sess-1",
		Name:      "harness-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "while read line; do printf 'ack:%s\\n' \"$line\"; done"},
	})

	if spawnResp.AgentID == "" {
		t.Fatal("agent.spawn returned empty agent_id")
	}
	if spawnResp.Status != "running" {
		t.Fatalf("agent.spawn status = %q, want %q", spawnResp.Status, "running")
	}

	sendResp := callMethod[AgentSendResponse](t, registry, "agent.send", AgentSendRequest{
		SessionID: "sess-1",
		AgentID:   spawnResp.AgentID,
		Input:     "ping\n",
	})
	if sendResp.Status != "sent" {
		t.Fatalf("agent.send status = %q, want %q", sendResp.Status, "sent")
	}

	waitForAgentOutput(t, registry, "sess-1", spawnResp.AgentID, "ack:ping")

	outputResp := callMethod[AgentOutputResponse](t, registry, "agent.output", AgentOutputRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
	if !strings.Contains(outputResp.Output, "ack:ping") {
		t.Fatalf("agent.output = %q, want contains %q", outputResp.Output, "ack:ping")
	}

	stopResp := callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
	if stopResp.Status != "stopping" {
		t.Fatalf("agent.stop status = %q, want %q", stopResp.Status, "stopping")
	}

	waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")
}

func TestAgentSpawnUsesSessionPrimaryFolderAsCWD(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		SessionID: "sess-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "pwd"},
	})

	waitForAgentOutput(t, registry, "sess-1", spawnResp.AgentID, workspace)

	outputResp := callMethod[AgentOutputResponse](t, registry, "agent.output", AgentOutputRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
	if !strings.Contains(outputResp.Output, workspace) {
		t.Fatalf("agent.output = %q, want contains workspace %q", outputResp.Output, workspace)
	}

	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
	waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "exited")
}

func TestAgentListAndGetIncludeSpawnedAgent(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		SessionID: "sess-1",
		Name:      "opencode",
		Command:   "/bin/sh",
		Args:      []string{"-c", "while true; do sleep 1; done"},
	})

	listResp := callMethod[AgentListResponse](t, registry, "agent.list", AgentListRequest{SessionID: "sess-1"})
	if len(listResp.Agents) != 1 {
		t.Fatalf("agent.list len = %d, want 1", len(listResp.Agents))
	}
	if listResp.Agents[0].AgentID != spawnResp.AgentID {
		t.Fatalf("agent.list[0].agent_id = %q, want %q", listResp.Agents[0].AgentID, spawnResp.AgentID)
	}

	getResp := callMethod[AgentGetResponse](t, registry, "agent.get", AgentGetRequest{SessionID: "sess-1", AgentID: "opencode"})
	if getResp.AgentID != spawnResp.AgentID {
		t.Fatalf("agent.get agent_id = %q, want %q", getResp.AgentID, spawnResp.AgentID)
	}

	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID})
	waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")
}

func TestAgentSendReturnsAgentNotRunningAfterSelfExit(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		SessionID: "sess-1",
		Command:   "/bin/sh",
		Args:      []string{"-c", "printf done; exit 0"},
	})

	waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "exited")

	spec, ok := registry.Get("agent.send")
	if !ok {
		t.Fatal("agent.send method not registered")
	}
	raw, err := json.Marshal(AgentSendRequest{SessionID: "sess-1", AgentID: spawnResp.AgentID, Input: "late input"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("agent.send returned nil error for exited agent")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("agent.send error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.AgentNotRunning {
		t.Fatalf("agent.send error code = %d, want %d", rpcErr.Code, rpc.AgentNotRunning)
	}
}

func TestAgentSpawnHarnessLauncherUsesDefaultBinaryWhenCommandMissing(t *testing.T) {
	launcher, err := resolveAgentLauncher("opencode")
	if err != nil {
		t.Fatalf("resolveAgentLauncher returned error: %v", err)
	}

	spec, err := launcher.prepare("", []string{"--resume"})
	if err != nil {
		t.Fatalf("launcher.prepare returned error: %v", err)
	}
	if spec.Command != "opencode" {
		t.Fatalf("launcher command = %q, want %q", spec.Command, "opencode")
	}
	if len(spec.Args) != 1 || spec.Args[0] != "--resume" {
		t.Fatalf("launcher args = %v, want [--resume]", spec.Args)
	}
}

func TestAgentSpawnRejectsUnknownHarness(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spec, ok := registry.Get("agent.spawn")
	if !ok {
		t.Fatal("agent.spawn method not registered")
	}

	raw, err := json.Marshal(AgentSpawnRequest{SessionID: "sess-1", Harness: "unknown-harness", Args: []string{"--resume"}})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("agent.spawn returned nil error for unknown harness")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("agent.spawn error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.InvalidParams {
		t.Fatalf("agent.spawn error code = %d, want %d", rpcErr.Code, rpc.InvalidParams)
	}
}

func TestPersistAgentStatusWithRetryHonorsContextCancellation(t *testing.T) {
	originalUpdate := updateAgentStatus
	updateAgentStatus = func(_ *globaldb.Store, ctx context.Context, _ globaldb.UpdateAgentStatusParams) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return errors.New("context was not forwarded")
		}
	}
	t.Cleanup(func() {
		updateAgentStatus = originalUpdate
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := persistAgentStatusWithRetry(ctx, nil, globaldb.UpdateAgentStatusParams{SessionID: "sess-1", AgentID: "agt-1", Status: "running"}, 60*time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("persistAgentStatusWithRetry error = %v, want context.Canceled", err)
	}
}

func waitForAgentStatus(t *testing.T, registry *rpc.MethodRegistry, sessionID, agentID, want string) {
	t.Helper()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		resp := callMethod[AgentGetResponse](t, registry, "agent.get", AgentGetRequest{SessionID: sessionID, AgentID: agentID})
		if resp.Status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("agent %s status did not reach %q before timeout", agentID, want)
}

func waitForAgentOutput(t *testing.T, registry *rpc.MethodRegistry, sessionID, agentID, wantSubstring string) {
	t.Helper()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		resp := callMethod[AgentOutputResponse](t, registry, "agent.output", AgentOutputRequest{SessionID: sessionID, AgentID: agentID})
		if strings.Contains(resp.Output, wantSubstring) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("agent %s output did not contain %q before timeout", agentID, wantSubstring)
}

func newAgentMethodTestStore(t *testing.T) *globaldb.Store {
	t.Helper()

	dbName := fmt.Sprintf("file:agent-method-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dbName)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if _, err := db.Exec(`
CREATE TABLE sessions (
	session_id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	status TEXT NOT NULL DEFAULT 'active',
	vcs_preference TEXT NOT NULL DEFAULT 'auto',
	origin_root TEXT NOT NULL,
	cleanup_policy TEXT NOT NULL DEFAULT 'manual',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE session_folders (
	session_id TEXT NOT NULL,
	folder_path TEXT NOT NULL,
	vcs_type TEXT NOT NULL DEFAULT 'unknown',
	is_primary INTEGER NOT NULL DEFAULT 0,
	added_at TEXT NOT NULL,
	PRIMARY KEY (session_id, folder_path),
	FOREIGN KEY(session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
);
CREATE TABLE agents (
	agent_id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	name TEXT,
	command TEXT NOT NULL,
	args TEXT NOT NULL DEFAULT '[]',
	status TEXT NOT NULL DEFAULT 'running',
	exit_code INTEGER,
	started_at TEXT NOT NULL,
	stopped_at TEXT,
	FOREIGN KEY(session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX agents_session_id_name_uq
	ON agents (session_id, name)
	WHERE name IS NOT NULL;
`); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	store, err := globaldb.NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore returned error: %v", err)
	}

	return store
}
