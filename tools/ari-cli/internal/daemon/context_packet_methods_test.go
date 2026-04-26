package daemon

import (
	"context"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestContextProjectBuildsDeterministicInspectablePacket(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	workspaceRoot := t.TempDir()
	if err := makeGitRoot(workspaceRoot); err != nil {
		t.Fatalf("makeGitRoot returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", workspaceRoot)
	if err := store.CreateWorkspaceCommandDefinition(context.Background(), globaldb.CreateWorkspaceCommandDefinitionParams{
		CommandID:   "cmddef-1",
		WorkspaceID: "ws-1",
		Name:        "verify",
		Command:     "just",
		Args:        `["verify"]`,
	}); err != nil {
		t.Fatalf("CreateWorkspaceCommandDefinition returned error: %v", err)
	}
	if err := store.CreateCommand(context.Background(), globaldb.CreateCommandParams{
		CommandID:   "cmd-1",
		WorkspaceID: "ws-1",
		Command:     "just verify",
		Args:        `[]`,
		Status:      "exited",
		ExitCode:    intPtr(1),
		StartedAt:   "2026-04-25T00:00:00Z",
	}); err != nil {
		t.Fatalf("CreateCommand returned error: %v", err)
	}
	d.setCommandOutput("cmd-1", "verify failed")

	req := ContextProjectRequest{WorkspaceID: "ws-1", TaskID: "task-1", Goal: "Expose control plane", Constraints: []string{"JJ first"}}
	first := callMethod[ContextProjectResponse](t, registry, "context.project", req)
	second := callMethod[ContextProjectResponse](t, registry, "context.project", req)
	if first.Packet.PacketHash == "" {
		t.Fatal("packet_hash is empty")
	}
	if first.Packet.PacketHash != second.Packet.PacketHash {
		t.Fatalf("packet hash changed: %q != %q", first.Packet.PacketHash, second.Packet.PacketHash)
	}
	if first.Packet.TaskID != "task-1" || first.Packet.WorkspaceID != "ws-1" {
		t.Fatalf("packet ids = task %q workspace %q, want task-1/ws-1", first.Packet.TaskID, first.Packet.WorkspaceID)
	}
	if len(first.Packet.Sections) != 6 {
		t.Fatalf("sections len = %d, want 6", len(first.Packet.Sections))
	}
	if first.Packet.Sections[0].Name != "goal" || first.Packet.Sections[0].Content != "Expose control plane" {
		t.Fatalf("goal section = %#v, want exact goal", first.Packet.Sections[0])
	}
	if len(first.Packet.IncludedCommandIDs) != 1 || first.Packet.IncludedCommandIDs[0] != "cmddef-1" {
		t.Fatalf("included command ids = %#v, want [cmddef-1]", first.Packet.IncludedCommandIDs)
	}
	if len(first.Packet.IncludedProofIDs) != 1 || first.Packet.IncludedProofIDs[0] != "proof_cmd-1" {
		t.Fatalf("included proof ids = %#v, want [proof_cmd-1]", first.Packet.IncludedProofIDs)
	}
	if len(first.Packet.Omissions) != 1 || first.Packet.Omissions[0].Kind != "logs" || first.Packet.Omissions[0].SourceID != "cmd-1" {
		t.Fatalf("omissions = %#v, want log omission for cmd-1", first.Packet.Omissions)
	}
}

func TestContextProjectRequiresTaskFields(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	spec, ok := registry.Get("context.project")
	if !ok {
		t.Fatal("context.project method not registered")
	}
	_, err := spec.Call(context.Background(), []byte(`{"workspace_id":"ws-1","task_id":"","goal":""}`))
	if err == nil {
		t.Fatal("context.project returned nil error for missing task fields")
	}
}

func TestStableHashReturnsMarshalError(t *testing.T) {
	_, err := stableHash(struct {
		Bad func()
	}{Bad: func() {}})
	if err == nil {
		t.Fatal("stableHash returned nil error for unmarshalable input")
	}
	if err.Error() != "stable hash marshal failed: json: unsupported type: func()" {
		t.Fatalf("stableHash error = %q, want marshal error", err.Error())
	}
}
