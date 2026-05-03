package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestWorkspaceActivityProjectsCommandsAgentsProofsAndVCS(t *testing.T) {
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

	harness := "codex"
	if err := store.CreateAgent(context.Background(), globaldb.CreateAgentParams{
		AgentID:     "ag-1",
		WorkspaceID: "ws-1",
		Command:     "codex",
		Args:        `[]`,
		Status:      "running",
		StartedAt:   "2026-04-25T00:00:01Z",
		Harness:     &harness,
	}); err != nil {
		t.Fatalf("CreateAgent returned error: %v", err)
	}
	d.setAgentOutput("ag-1", "working")

	resp := callMethod[WorkspaceActivityResponse](t, registry, "workspace.activity", WorkspaceActivityRequest{WorkspaceID: "ws-1"})
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
	if len(resp.Agents) != 1 {
		t.Fatalf("agents len = %d, want 1", len(resp.Agents))
	}
	if resp.Agents[0].ID != "ag-1" || resp.Agents[0].Executor != "codex" || resp.Agents[0].OutputSummary != "working" {
		t.Fatalf("agent projection = %#v, want codex agent with output", resp.Agents[0])
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
	if len(resp.Attention.Items) != 2 || resp.Attention.Items[0].SourceID != "proof_cmd-1" || resp.Attention.Items[1].SourceID != "ag-1" {
		t.Fatalf("attention items = %#v, want failed proof and running agent items", resp.Attention.Items)
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

func TestWorkspaceActivityOrdersExecutorRunsDeterministically(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	d.recordExecutorRun(AgentRun{AgentRunID: "z-run", WorkspaceID: "ws-1", Status: "running", Executor: "fake", StartedAt: "2026-04-25T00:00:02Z"}, nil)
	d.recordExecutorRun(AgentRun{AgentRunID: "a-run", WorkspaceID: "ws-1", Status: "running", Executor: "fake", StartedAt: "2026-04-25T00:00:01Z"}, nil)

	resp := callMethod[WorkspaceActivityResponse](t, registry, "workspace.activity", WorkspaceActivityRequest{WorkspaceID: "ws-1"})
	if len(resp.Agents) != 2 {
		t.Fatalf("agents len = %d, want 2", len(resp.Agents))
	}
	if resp.Agents[0].ID != "a-run" || resp.Agents[1].ID != "z-run" {
		t.Fatalf("agents = %#v, want a-run then z-run", resp.Agents)
	}
}

func TestWorkspaceActivityAttentionIncludesRunningAgents(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	harness := "codex"
	if err := store.CreateAgent(context.Background(), globaldb.CreateAgentParams{
		AgentID:     "ag-running",
		WorkspaceID: "ws-1",
		Command:     "codex",
		Args:        `[]`,
		Status:      "running",
		StartedAt:   "2026-04-25T00:00:01Z",
		Harness:     &harness,
	}); err != nil {
		t.Fatalf("CreateAgent returned error: %v", err)
	}

	resp := callMethod[WorkspaceActivityResponse](t, registry, "workspace.activity", WorkspaceActivityRequest{WorkspaceID: "ws-1"})
	if resp.Attention.Level != "running" {
		t.Fatalf("attention level = %q, want running", resp.Attention.Level)
	}
	if len(resp.Attention.Items) != 1 {
		t.Fatalf("attention items len = %d, want 1", len(resp.Attention.Items))
	}
	item := resp.Attention.Items[0]
	if item.Kind != "agent_running" || item.SourceID != "ag-running" {
		t.Fatalf("attention item = %#v, want running agent item", item)
	}
}

func TestWorkspaceActivityAttentionIncludesAuthRequired(t *testing.T) {
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

	resp := callMethod[WorkspaceActivityResponse](t, registry, "workspace.activity", WorkspaceActivityRequest{WorkspaceID: "ws-1"})
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

func TestWorkspaceActivityAttentionTreatsBrokenAuthAsActionRequired(t *testing.T) {
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

			resp := callMethod[WorkspaceActivityResponse](t, registry, "workspace.activity", WorkspaceActivityRequest{WorkspaceID: "ws-1"})
			if resp.Attention.Level != "action-required" {
				t.Fatalf("attention level = %q, want action-required", resp.Attention.Level)
			}
		})
	}
}

func TestWorkspaceActivityAttentionIncludesMixedSourcesWithHighestLevel(t *testing.T) {
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
	if err := store.CreateAgent(context.Background(), globaldb.CreateAgentParams{AgentID: "ag-running", WorkspaceID: "ws-1", Command: "codex", Args: `[]`, Status: "running", StartedAt: "2026-04-25T00:00:01Z"}); err != nil {
		t.Fatalf("CreateAgent returned error: %v", err)
	}

	resp := callMethod[WorkspaceActivityResponse](t, registry, "workspace.activity", WorkspaceActivityRequest{WorkspaceID: "ws-1"})
	if resp.Attention.Level != "action-required" {
		t.Fatalf("attention level = %q, want action-required", resp.Attention.Level)
	}
	want := map[string]string{"proof_failed": "proof_cmd-fail", "auth_required": "codex-default", "agent_running": "ag-running"}
	for _, item := range resp.Attention.Items {
		if want[item.Kind] == item.SourceID {
			delete(want, item.Kind)
		}
	}
	if len(want) != 0 {
		t.Fatalf("attention items = %#v, missing %v", resp.Attention.Items, want)
	}
}

func TestWorkspaceActivityIgnoresUnreferencedAuthSlots(t *testing.T) {
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

	resp := callMethod[WorkspaceActivityResponse](t, registry, "workspace.activity", WorkspaceActivityRequest{WorkspaceID: "ws-1"})
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
