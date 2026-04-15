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
	if len(resp.Attention.Items) != 1 || resp.Attention.Items[0].SourceID != "proof_cmd-1" {
		t.Fatalf("attention items = %#v, want failed proof item", resp.Attention.Items)
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
