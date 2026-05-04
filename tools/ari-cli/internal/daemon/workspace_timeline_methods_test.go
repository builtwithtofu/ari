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
	d.recordExecutorRun(AgentSession{AgentSessionID: "run-1", WorkspaceID: "ws-1", Executor: "opencode", Status: "running", StartedAt: "2026-04-25T00:00:01Z"}, []TimelineItem{{ID: "run-1:output", WorkspaceID: "ws-1", RunID: "run-1", SourceKind: "agent_session", SourceID: "run-1", Kind: "terminal_output", Status: "running", CreatedAt: "2026-04-25T00:00:01Z", Text: "agent terminal output"}})

	resp := callMethod[WorkspaceTimelineResponse](t, registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: "ws-1"})
	if len(resp.Items) != 4 {
		t.Fatalf("timeline items len = %d, want 4", len(resp.Items))
	}
	want := []TimelineItem{
		{ID: "cmd-1:lifecycle", SourceKind: "command", SourceID: "cmd-1", Kind: "lifecycle", Status: "exited", Sequence: 1, Text: "just verify"},
		{ID: "cmd-1:output", SourceKind: "command", SourceID: "cmd-1", Kind: "command_output", Status: "completed", Sequence: 2, Text: "command failed"},
		{ID: "proof_cmd-1", SourceKind: "proof", SourceID: "cmd-1", Kind: "proof_result", Status: "failed", Sequence: 3, Text: "just verify"},
		{ID: "run-1:output", SourceKind: "agent_session", SourceID: "run-1", Kind: "run_log_message", Status: "running", Sequence: 4, Text: "agent terminal output"},
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

	d.recordExecutorRun(AgentSession{AgentSessionID: "z-run", WorkspaceID: "ws-1", Status: "running", Executor: "fake", StartedAt: "2026-04-25T00:00:02Z"}, []TimelineItem{{ID: "z-item", WorkspaceID: "ws-1", RunID: "z-run", SourceKind: "executor", SourceID: "z-run", Kind: "agent_text", Status: "completed", Sequence: 1, Text: "z"}})
	d.recordExecutorRun(AgentSession{AgentSessionID: "a-run", WorkspaceID: "ws-1", Status: "running", Executor: "fake", StartedAt: "2026-04-25T00:00:01Z"}, []TimelineItem{{ID: "a-item", WorkspaceID: "ws-1", RunID: "a-run", SourceKind: "executor", SourceID: "a-run", Kind: "agent_text", Status: "completed", Sequence: 1, Text: "a"}})

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

func TestWorkspaceTimelineNormalizesExecutorItemsToSessionRunLogTerms(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	d.recordExecutorRun(AgentSession{AgentSessionID: "run-1", WorkspaceID: "ws-1", Status: "running", Executor: "fake", StartedAt: "2026-04-25T00:00:01Z"}, []TimelineItem{
		{ID: "run-1:lifecycle", WorkspaceID: "ws-1", RunID: "run-1", SourceKind: "executor", SourceID: "run-1", Kind: "lifecycle", Status: "running", Sequence: 1, Text: "started"},
		{ID: "run-1:terminal", WorkspaceID: "ws-1", RunID: "run-1", SourceKind: "executor", SourceID: "run-1", Kind: "terminal_output", Status: "completed", Sequence: 2, Text: "done"},
	})

	resp := callMethod[WorkspaceTimelineResponse](t, registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: "ws-1"})
	if len(resp.Items) != 2 {
		t.Fatalf("timeline items len = %d, want 2", len(resp.Items))
	}
	for _, item := range resp.Items {
		if item.SourceKind != "agent_session" || item.Kind == "terminal_output" || item.SourceKind == "executor" {
			t.Fatalf("timeline item = %#v, want agent_session source and no executor/terminal_output public terms", item)
		}
	}
	if resp.Items[1].Kind != "run_log_message" {
		t.Fatalf("terminal output item = %#v, want run_log_message", resp.Items[1])
	}
}

func TestWorkspaceTimelineIncludesAgentMessageAndContextExcerptActivity(t *testing.T) {
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

	resp := callMethod[WorkspaceTimelineResponse](t, registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: "ws-1"})
	foundExcerpt := false
	foundMessage := false
	for _, item := range resp.Items {
		if item.SourceKind == "context_excerpt" && item.SourceID == "excerpt-1" {
			foundExcerpt = true
		}
		if item.SourceKind == "agent_message" && item.SourceID == "dm-1" {
			foundMessage = true
		}
	}
	if !foundExcerpt || !foundMessage {
		t.Fatalf("timeline items = %#v, want context_excerpt and agent_message activity", resp.Items)
	}
}

func TestWorkspaceTimelineOrdersContextExcerptAndAgentMessageActivityByWorkflow(t *testing.T) {
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
	for _, msg := range []globaldb.RunLogMessage{
		{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "first"}}},
		{MessageID: "msg-2", SessionID: "run-1", Sequence: 2, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-2", Sequence: 1, Kind: "text", Text: "second"}}},
	} {
		if err := store.AppendRunLogMessage(ctx, msg); err != nil {
			t.Fatalf("AppendRunLogMessage(%s) returned error: %v", msg.MessageID, err)
		}
	}
	excerpt1, err := store.CreateContextExcerptFromExplicitIDs(ctx, globaldb.CreateContextExcerptFromExplicitIDsParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "reviewer", MessageIDs: []string{"msg-1"}})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromExplicitIDs excerpt-1 returned error: %v", err)
	}
	if _, err := store.SendAgentMessage(ctx, globaldb.AgentMessageSendParams{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "reviewer", TargetSessionID: "call-1-run", Body: "review first", ContextExcerptIDs: []string{excerpt1.ContextExcerptID}}); err != nil {
		t.Fatalf("SendAgentMessage dm-1 returned error: %v", err)
	}
	excerpt2, err := store.CreateContextExcerptFromExplicitIDs(ctx, globaldb.CreateContextExcerptFromExplicitIDsParams{ContextExcerptID: "excerpt-2", SourceSessionID: "run-1", TargetAgentID: "reviewer", MessageIDs: []string{"msg-2"}})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromExplicitIDs excerpt-2 returned error: %v", err)
	}
	if _, err := store.SendAgentMessage(ctx, globaldb.AgentMessageSendParams{AgentMessageID: "dm-2", SourceSessionID: "run-1", TargetAgentID: "reviewer", TargetSessionID: "call-1-run", Body: "review second", ContextExcerptIDs: []string{excerpt2.ContextExcerptID}}); err != nil {
		t.Fatalf("SendAgentMessage dm-2 returned error: %v", err)
	}

	resp := callMethod[WorkspaceTimelineResponse](t, registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: "ws-1"})
	order := []string{}
	for _, item := range resp.Items {
		if item.SourceKind == "context_excerpt" || item.SourceKind == "agent_message" {
			order = append(order, item.SourceID)
		}
	}
	want := []string{"excerpt-1", "dm-1", "excerpt-2", "dm-2"}
	if len(order) != len(want) {
		t.Fatalf("workflow order = %#v, want %#v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("workflow order = %#v, want %#v", order, want)
		}
	}
}
