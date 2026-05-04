package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestWorkspaceStatusProjectsCommandsAgentsProofsAndVCS(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	workspaceRoot := t.TempDir()
	if err := makeJJMarker(workspaceRoot); err != nil {
		t.Fatalf("makeJJMarker returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", workspaceRoot)

	finishedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := store.CreateCommand(context.Background(), globaldb.CreateCommandParams{
		CommandID:   "cmd-1",
		WorkspaceID: "ws-1",
		Command:     "just verify",
		Args:        `[]`,
		Status:      "exited",
		ExitCode:    intPtr(1),
		StartedAt:   "2026-04-25T00:00:00Z",
		FinishedAt:  &finishedAt,
	}); err != nil {
		t.Fatalf("CreateCommand returned error: %v", err)
	}
	d.setCommandOutput("cmd-1", "unit test failed\nfull log")

	d.recordExecutorRun(AgentSession{AgentSessionID: "run-1", WorkspaceID: "ws-1", Executor: "codex", Status: "running", StartedAt: "2026-04-25T00:00:01Z"}, nil)

	resp := callMethod[WorkspaceStatusResponse](t, registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	if resp.WorkspaceID != "ws-1" {
		t.Fatalf("workspace_id = %q, want ws-1", resp.WorkspaceID)
	}
	if resp.WorkspaceName != "ws-1" {
		t.Fatalf("workspace_name = %q, want ws-1", resp.WorkspaceName)
	}
	if resp.VCS.Backend != "jj" {
		t.Fatalf("vcs.backend = %q, want jj", resp.VCS.Backend)
	}
	if len(resp.Processes) != 1 {
		t.Fatalf("processes len = %d, want 1", len(resp.Processes))
	}
	if resp.Processes[0].ID != "cmd-1" || resp.Processes[0].Kind != "command" || resp.Processes[0].Status != "exited" {
		t.Fatalf("process projection = %#v, want command cmd-1 exited", resp.Processes[0])
	}
	if resp.Processes[0].OutputSummary != "unit test failed" {
		t.Fatalf("process output summary = %q, want first output line", resp.Processes[0].OutputSummary)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(resp.Sessions))
	}
	if resp.Sessions[0].ID != "run-1" || resp.Sessions[0].Executor != "codex" || resp.Sessions[0].Status != "running" {
		t.Fatalf("session projection = %#v, want codex run", resp.Sessions[0])
	}
	if len(resp.Proofs) != 1 {
		t.Fatalf("proofs len = %d, want 1", len(resp.Proofs))
	}
	if resp.Proofs[0].ID != "proof_cmd-1" || resp.Proofs[0].Status != "failed" || resp.Proofs[0].Command != "just verify" {
		t.Fatalf("proof projection = %#v, want failed command proof", resp.Proofs[0])
	}
	if resp.Attention.Level != "action-required" {
		t.Fatalf("attention level = %q, want action-required", resp.Attention.Level)
	}
	if len(resp.Attention.Items) != 2 || resp.Attention.Items[0].SourceID != "proof_cmd-1" || resp.Attention.Items[1].SourceID != "run-1" {
		t.Fatalf("attention items = %#v, want failed proof and running agent items", resp.Attention.Items)
	}
}

func TestWorkspaceStatusJSONUsesAgentMessageTerminology(t *testing.T) {
	resp := WorkspaceStatusResponse{
		WorkspaceID:     "ws-1",
		Sessions:        []SessionActivity{{ID: "run-1", Status: "running", Executor: "codex"}},
		ContextExcerpts: []ContextExcerptActivity{{ContextExcerptID: "excerpt-1", SelectorType: "last_n", ItemCount: 1, TargetAgentID: "reviewer"}},
		AgentMessages:   []AgentMessageActivity{{AgentMessageID: "msg-1", Status: "delivered", SourceSessionID: "run-1", TargetAgentID: "reviewer", ContextExcerptCount: 1}},
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	got := string(raw)
	for _, want := range []string{`"sessions"`, `"context_excerpts"`, `"context_excerpt_id"`, `"agent_messages"`, `"agent_message_id"`, `"context_excerpt_count"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("workspace status json = %s, want %s", got, want)
		}
	}
	for _, stale := range []string{`"agents"`, `"message_shares"`, `"share_id"`, `"direct_messages"`, `"direct_message_id"`} {
		if strings.Contains(got, stale) {
			t.Fatalf("workspace status json = %s, want no stale field %s", got, stale)
		}
	}
}

func TestWorkspaceDiffUsesPrimaryFolderFirst(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	gitRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
		t.Fatalf("create git marker: %v", err)
	}
	jjRoot := t.TempDir()
	if err := makeJJMarker(jjRoot); err != nil {
		t.Fatalf("makeJJMarker returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", gitRoot)
	if err := store.AddFolder(context.Background(), "ws-1", jjRoot, "jj", true); err != nil {
		t.Fatalf("AddFolder primary jj root returned error: %v", err)
	}

	resp := callMethod[WorkspaceDiffResponse](t, registry, "workspace.diff", WorkspaceDiffRequest{WorkspaceID: "ws-1"})
	if resp.Diff.Backend != "jj" {
		t.Fatalf("diff backend = %q, want primary folder jj backend", resp.Diff.Backend)
	}
}

func TestWorkspaceStatusOrdersExecutorRunsDeterministically(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	d.recordExecutorRun(AgentSession{AgentSessionID: "z-run", WorkspaceID: "ws-1", Status: "running", Executor: "fake", StartedAt: "2026-04-25T00:00:02Z"}, nil)
	d.recordExecutorRun(AgentSession{AgentSessionID: "a-run", WorkspaceID: "ws-1", Status: "running", Executor: "fake", StartedAt: "2026-04-25T00:00:01Z"}, nil)

	resp := callMethod[WorkspaceStatusResponse](t, registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	if len(resp.Sessions) != 2 {
		t.Fatalf("sessions len = %d, want 2", len(resp.Sessions))
	}
	if resp.Sessions[0].ID != "a-run" || resp.Sessions[1].ID != "z-run" {
		t.Fatalf("sessions = %#v, want a-run then z-run", resp.Sessions)
	}
}

func TestWorkspaceStatusProjectsPersistedAgentSessionsAfterDaemonRestart(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.CreateAgentSessionConfig(context.Background(), globaldb.AgentSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "executor", Harness: "codex"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig returned error: %v", err)
	}
	if err := store.CreateAgentSession(context.Background(), globaldb.AgentSession{SessionID: "run-1", WorkspaceID: "ws-1", AgentID: "agent-1", Harness: "codex", Status: "running", Usage: "durable"}); err != nil {
		t.Fatalf("CreateAgentSession returned error: %v", err)
	}

	resp := callMethod[WorkspaceStatusResponse](t, registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	if len(resp.Sessions) != 1 || resp.Sessions[0].ID != "run-1" || resp.Sessions[0].Executor != "codex" || resp.Sessions[0].Status != "running" {
		t.Fatalf("sessions = %#v, want persisted running normalized session", resp.Sessions)
	}
	if resp.Attention.Level != "running" || len(resp.Attention.Items) != 1 || resp.Attention.Items[0].SourceID != "run-1" {
		t.Fatalf("attention = %#v, want persisted running run to bubble attention", resp.Attention)
	}
}

func TestWorkspaceStatusProjectsMessageWorkflows(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	ctx := context.Background()
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "executor", WorkspaceID: "ws-1", Name: "executor", Harness: "codex"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig executor returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "reviewer", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig reviewer returned error: %v", err)
	}
	if err := store.CreateAgentSession(ctx, globaldb.AgentSession{SessionID: "run-1", WorkspaceID: "ws-1", AgentID: "executor", Harness: "codex", Status: "waiting", Usage: "durable"}); err != nil {
		t.Fatalf("CreateAgentSession source returned error: %v", err)
	}
	if err := store.CreateAgentSession(ctx, globaldb.AgentSession{SessionID: "call-1-run", WorkspaceID: "ws-1", AgentID: "reviewer", Harness: "opencode", Status: "running", Usage: "ephemeral", SourceSessionID: "run-1", SourceAgentID: "executor"}); err != nil {
		t.Fatalf("CreateAgentSession ephemeral returned error: %v", err)
	}
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "please review"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	excerpt, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "reviewer", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}
	if _, err := store.SendAgentMessage(ctx, globaldb.AgentMessageSendParams{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "reviewer", TargetSessionID: "call-1-run", Body: "review this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}}); err != nil {
		t.Fatalf("SendAgentMessage returned error: %v", err)
	}

	resp := callMethod[WorkspaceStatusResponse](t, registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	if len(resp.Sessions) != 2 {
		t.Fatalf("sessions = %#v, want sticky waiting and ephemeral running sessions", resp.Sessions)
	}
	byID := map[string]SessionActivity{}
	for _, session := range resp.Sessions {
		byID[session.ID] = session
	}
	if byID["run-1"].Status != "waiting" || byID["call-1-run"].Usage != "ephemeral" || byID["call-1-run"].SourceSessionID != "run-1" {
		t.Fatalf("sessions = %#v, want waiting source and running ephemeral call metadata", resp.Sessions)
	}
	if len(resp.ContextExcerpts) != 1 || resp.ContextExcerpts[0].ContextExcerptID != "excerpt-1" || resp.ContextExcerpts[0].SelectorType != "last_n" || resp.ContextExcerpts[0].ItemCount != 1 {
		t.Fatalf("context excerpts = %#v, want excerpt history summary", resp.ContextExcerpts)
	}
	if len(resp.AgentMessages) != 1 || resp.AgentMessages[0].AgentMessageID != "dm-1" || resp.AgentMessages[0].ContextExcerptCount != 1 || resp.AgentMessages[0].Status != "delivered" {
		t.Fatalf("agent messages = %#v, want delivered agent message history", resp.AgentMessages)
	}
	wantAttention := map[string]string{"session_waiting": "run-1", "ephemeral_running": "call-1-run"}
	for _, item := range resp.Attention.Items {
		if wantAttention[item.Kind] == item.SourceID {
			delete(wantAttention, item.Kind)
		}
	}
	if len(wantAttention) != 0 {
		t.Fatalf("attention = %#v, missing %v", resp.Attention, wantAttention)
	}
}

func TestWorkspaceStatusAttentionIncludesRunningSessions(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "profile-1", WorkspaceID: "ws-1", Name: "executor", Harness: "codex"}); err != nil {
		t.Fatalf("UpsertAgentProfile returned error: %v", err)
	}
	if err := store.EnsureAgentSessionConfig(context.Background(), globaldb.AgentSessionConfig{AgentID: "profile-1", WorkspaceID: "ws-1", Name: "executor", Harness: "codex"}); err != nil {
		t.Fatalf("EnsureAgentSessionConfig returned error: %v", err)
	}
	if err := store.CreateAgentSession(context.Background(), globaldb.AgentSession{SessionID: "session-running", WorkspaceID: "ws-1", AgentID: "profile-1", Harness: "codex", Status: "running", Usage: globaldb.AgentSessionUsageSticky}); err != nil {
		t.Fatalf("CreateAgentSession returned error: %v", err)
	}

	resp := callMethod[WorkspaceStatusResponse](t, registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	if resp.Attention.Level != "running" {
		t.Fatalf("attention level = %q, want running", resp.Attention.Level)
	}
	if len(resp.Attention.Items) != 1 {
		t.Fatalf("attention items len = %d, want 1", len(resp.Attention.Items))
	}
	item := resp.Attention.Items[0]
	if item.Kind != "session_running" || item.SourceID != "session-running" {
		t.Fatalf("attention item = %#v, want running session item", item)
	}
}

func TestWorkspaceStatusIgnoresLegacyAgentRows(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.CreateAgent(context.Background(), globaldb.CreateAgentParams{AgentID: "legacy-agent", WorkspaceID: "ws-1", Command: "codex", Args: `[]`, Status: "running", StartedAt: "2026-04-25T00:00:01Z"}); err != nil {
		t.Fatalf("CreateAgent returned error: %v", err)
	}

	resp := callMethod[WorkspaceStatusResponse](t, registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	if len(resp.Sessions) != 0 || len(resp.Attention.Items) != 0 || resp.Attention.Level != "none" {
		t.Fatalf("status = %#v, want legacy agent row ignored by workspace.status", resp)
	}
}

func TestWorkspaceStatusAttentionIncludesAuthRequired(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "codex-default", Harness: "codex", Label: "default", Status: "auth_required"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}
	if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "ap-helper", WorkspaceID: "ws-1", Name: "helper", Harness: "codex", AuthSlotID: "codex-default"}); err != nil {
		t.Fatalf("UpsertAgentProfile returned error: %v", err)
	}

	resp := callMethod[WorkspaceStatusResponse](t, registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	if resp.Attention.Level != "auth" {
		t.Fatalf("attention level = %q, want auth", resp.Attention.Level)
	}
	if len(resp.Attention.Items) != 1 {
		t.Fatalf("attention items len = %d, want 1", len(resp.Attention.Items))
	}
	item := resp.Attention.Items[0]
	if item.Kind != "auth_required" || item.SourceID != "codex-default" {
		t.Fatalf("attention item = %#v, want auth-required item", item)
	}
}

func TestWorkspaceStatusAttentionTreatsBrokenAuthAsActionRequired(t *testing.T) {
	for _, status := range []string{"auth_failed", "not_installed"} {
		t.Run(status, func(t *testing.T) {
			store := newCommandMethodTestStore(t)
			registry := rpc.NewMethodRegistry()
			d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

			if err := d.registerMethods(registry, store); err != nil {
				t.Fatalf("registerMethods returned error: %v", err)
			}
			seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
			if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "codex-default", Harness: "codex", Label: "default", Status: status}); err != nil {
				t.Fatalf("UpsertAuthSlot returned error: %v", err)
			}
			if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "ap-helper", WorkspaceID: "ws-1", Name: "helper", Harness: "codex", AuthSlotID: "codex-default"}); err != nil {
				t.Fatalf("UpsertAgentProfile returned error: %v", err)
			}

			resp := callMethod[WorkspaceStatusResponse](t, registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
			if resp.Attention.Level != "action-required" {
				t.Fatalf("attention level = %q, want action-required", resp.Attention.Level)
			}
		})
	}
}

func TestWorkspaceStatusAttentionIncludesMixedSourcesWithHighestLevel(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.CreateCommand(context.Background(), globaldb.CreateCommandParams{CommandID: "cmd-fail", WorkspaceID: "ws-1", Command: "just test", Args: `[]`, Status: "exited", ExitCode: intPtr(1), StartedAt: "2026-04-25T00:00:00Z"}); err != nil {
		t.Fatalf("CreateCommand returned error: %v", err)
	}
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "codex-default", Harness: "codex", Label: "default", Status: "auth_required"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}
	if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "ap-helper", WorkspaceID: "ws-1", Name: "helper", Harness: "codex", AuthSlotID: "codex-default"}); err != nil {
		t.Fatalf("UpsertAgentProfile returned error: %v", err)
	}
	if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "profile-1", WorkspaceID: "ws-1", Name: "executor", Harness: "codex"}); err != nil {
		t.Fatalf("UpsertAgentProfile executor returned error: %v", err)
	}
	if err := store.EnsureAgentSessionConfig(context.Background(), globaldb.AgentSessionConfig{AgentID: "profile-1", WorkspaceID: "ws-1", Name: "executor", Harness: "codex"}); err != nil {
		t.Fatalf("EnsureAgentSessionConfig returned error: %v", err)
	}
	if err := store.CreateAgentSession(context.Background(), globaldb.AgentSession{SessionID: "session-running", WorkspaceID: "ws-1", AgentID: "profile-1", Harness: "codex", Status: "running", Usage: globaldb.AgentSessionUsageSticky}); err != nil {
		t.Fatalf("CreateAgentSession returned error: %v", err)
	}

	resp := callMethod[WorkspaceStatusResponse](t, registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	if resp.Attention.Level != "action-required" {
		t.Fatalf("attention level = %q, want action-required", resp.Attention.Level)
	}
	want := map[string]string{"proof_failed": "proof_cmd-fail", "auth_required": "codex-default", "session_running": "session-running"}
	for _, item := range resp.Attention.Items {
		if want[item.Kind] == item.SourceID {
			delete(want, item.Kind)
		}
	}
	if len(want) != 0 {
		t.Fatalf("attention items = %#v, missing %v", resp.Attention.Items, want)
	}
}

func TestWorkspaceStatusIgnoresUnreferencedAuthSlots(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "unused-slot", Harness: "codex", Label: "unused", Status: "auth_required"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	resp := callMethod[WorkspaceStatusResponse](t, registry, "workspace.status", WorkspaceStatusRequest{WorkspaceID: "ws-1"})
	if resp.Attention.Level != "none" || len(resp.Attention.Items) != 0 {
		t.Fatalf("attention = %#v, want no workspace auth attention from unreferenced slot", resp.Attention)
	}
}

func TestWorkspaceProjectionMethodsRejectMissingWorkspaceID(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	spec, ok := registry.Get("workspace.diff")
	if !ok {
		t.Fatal("workspace.diff method not registered")
	}
	_, err := spec.Call(context.Background(), []byte(`{"workspace_id":""}`))
	if err == nil {
		t.Fatal("workspace.diff returned nil error for missing workspace_id")
	}
}

func TestWorkspaceProofsProjectsCommandStatuses(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	workspaceRoot := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "ws-1", workspaceRoot)
	if err := store.CreateCommand(context.Background(), globaldb.CreateCommandParams{
		CommandID:   "cmd-pass",
		WorkspaceID: "ws-1",
		Command:     "go test ./...",
		Args:        `[]`,
		Status:      "exited",
		ExitCode:    intPtr(0),
		StartedAt:   "2026-04-25T00:00:00Z",
	}); err != nil {
		t.Fatalf("CreateCommand returned error: %v", err)
	}

	resp := callMethod[WorkspaceProofsResponse](t, registry, "workspace.proofs", WorkspaceProofsRequest{WorkspaceID: "ws-1"})
	if len(resp.Proofs) != 1 {
		t.Fatalf("proofs len = %d, want 1", len(resp.Proofs))
	}
	if resp.Proofs[0].Status != "passed" {
		t.Fatalf("proof status = %q, want passed", resp.Proofs[0].Status)
	}
}

func makeJJMarker(root string) error {
	return os.MkdirAll(filepath.Join(root, ".jj"), 0o755)
}

func intPtr(value int) *int {
	return &value
}
