package daemon

import (
	"context"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestWorkspaceTimelineMapsCommandAgentAndProofOutput(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
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
	d.setCommandOutput("cmd-1", "command failed")
	if err := store.CreateAgent(context.Background(), globaldb.CreateAgentParams{
		AgentID:     "ag-1",
		WorkspaceID: "ws-1",
		Command:     "opencode",
		Args:        `[]`,
		Status:      "running",
		StartedAt:   "2026-04-25T00:00:01Z",
	}); err != nil {
		t.Fatalf("CreateAgent returned error: %v", err)
	}
	d.setAgentOutput("ag-1", "agent terminal output")

	resp := callMethod[WorkspaceTimelineResponse](t, registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: "ws-1"})
	if len(resp.Items) != 4 {
		t.Fatalf("timeline items len = %d, want 4", len(resp.Items))
	}
	want := []TimelineItem{
		{ID: "cmd-1:lifecycle", SourceKind: "command", SourceID: "cmd-1", Kind: "lifecycle", Status: "exited", Sequence: 1, Text: "just verify"},
		{ID: "cmd-1:output", SourceKind: "command", SourceID: "cmd-1", Kind: "command_output", Status: "completed", Sequence: 2, Text: "command failed"},
		{ID: "proof_cmd-1", SourceKind: "proof", SourceID: "cmd-1", Kind: "proof_result", Status: "failed", Sequence: 3, Text: "just verify"},
		{ID: "ag-1:output", SourceKind: "agent", SourceID: "ag-1", Kind: "terminal_output", Status: "running", Sequence: 4, Text: "agent terminal output"},
	}
	for i := range want {
		got := resp.Items[i]
		if got.ID != want[i].ID || got.SourceKind != want[i].SourceKind || got.SourceID != want[i].SourceID || got.Kind != want[i].Kind || got.Status != want[i].Status || got.Sequence != want[i].Sequence || got.Text != want[i].Text {
			t.Fatalf("timeline item %d = %#v, want %#v", i, got, want[i])
		}
	}
}

func TestWorkspaceTimelineOrdersExecutorItemsDeterministically(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	d.recordExecutorRun(AgentRun{AgentRunID: "z-run", WorkspaceID: "ws-1", Status: "running", Executor: "fake", StartedAt: "2026-04-25T00:00:02Z"}, []TimelineItem{{ID: "z-item", WorkspaceID: "ws-1", RunID: "z-run", SourceKind: "executor", SourceID: "z-run", Kind: "agent_text", Status: "completed", Sequence: 1, Text: "z"}})
	d.recordExecutorRun(AgentRun{AgentRunID: "a-run", WorkspaceID: "ws-1", Status: "running", Executor: "fake", StartedAt: "2026-04-25T00:00:01Z"}, []TimelineItem{{ID: "a-item", WorkspaceID: "ws-1", RunID: "a-run", SourceKind: "executor", SourceID: "a-run", Kind: "agent_text", Status: "completed", Sequence: 1, Text: "a"}})

	resp := callMethod[WorkspaceTimelineResponse](t, registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: "ws-1"})
	if len(resp.Items) != 2 {
		t.Fatalf("timeline items len = %d, want 2", len(resp.Items))
	}
	if resp.Items[0].RunID != "a-run" || resp.Items[0].Sequence != 1 {
		t.Fatalf("first executor item = %#v, want a-run sequence 1", resp.Items[0])
	}
	if resp.Items[1].RunID != "z-run" || resp.Items[1].Sequence != 2 {
		t.Fatalf("second executor item = %#v, want z-run sequence 2", resp.Items[1])
	}
}
