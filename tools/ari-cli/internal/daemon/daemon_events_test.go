package daemon

import (
	"context"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestDaemonEventRPCListAttentionAndClear(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if _, err := store.AppendDaemonEvent(context.Background(), globaldb.DaemonEvent{EventID: "evt-1", EventType: daemonEventSessionMessageSent, SubjectType: "agent_message", SubjectID: "am-1", AttentionRequired: true}); err != nil {
		t.Fatalf("AppendDaemonEvent returned error: %v", err)
	}

	attention := callMethod[DaemonEventsResponse](t, registry, "daemon.events.attention", struct{}{})
	if len(attention.Events) != 1 || attention.Events[0].EventID != "evt-1" {
		t.Fatalf("daemon.events.attention = %#v, want evt-1", attention)
	}
	cleared := callMethod[DaemonAttentionClearResponse](t, registry, "daemon.events.attention.clear", DaemonAttentionClearRequest{EventID: "evt-1"})
	if !cleared.Cleared {
		t.Fatalf("clear response = %#v, want cleared", cleared)
	}
	attention = callMethod[DaemonEventsResponse](t, registry, "daemon.events.attention", struct{}{})
	if len(attention.Events) != 0 {
		t.Fatalf("attention after clear = %#v, want empty", attention)
	}
	all := callMethod[DaemonEventsResponse](t, registry, "daemon.events.after", DaemonEventsAfterRequest{})
	if len(all.Events) != 1 || all.Events[0].EventID != "evt-1" || all.Events[0].AttentionClearedAt == "" {
		t.Fatalf("daemon.events.after = %#v, want cleared evt-1", all)
	}
}

func TestSessionMessageSendEmitsAttentionEvent(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "run-1", t.TempDir())
	if err := store.EnsureHarnessSessionConfig(context.Background(), globaldb.HarnessSessionConfig{AgentID: "agent-1", WorkspaceID: "run-1", Name: "planner", Harness: "test"}); err != nil {
		t.Fatalf("EnsureHarnessSessionConfig source returned error: %v", err)
	}
	if err := store.EnsureHarnessSessionConfig(context.Background(), globaldb.HarnessSessionConfig{AgentID: "agent-2", WorkspaceID: "run-1", Name: "reviewer", Harness: "test"}); err != nil {
		t.Fatalf("EnsureHarnessSessionConfig returned error: %v", err)
	}
	if err := store.CreateHarnessSession(context.Background(), globaldb.HarnessSession{SessionID: "run-1", WorkspaceID: "run-1", AgentID: "agent-1", Harness: "test", Status: "running", Usage: globaldb.HarnessSessionUsageSticky}); err != nil {
		t.Fatalf("CreateHarnessSession source returned error: %v", err)
	}
	_ = callMethod[AgentMessageSendResponse](t, registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "am-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "review this", StartSessionID: "run-2"})

	attention := callMethod[DaemonEventsResponse](t, registry, "daemon.events.attention", struct{}{})
	if len(attention.Events) != 1 || attention.Events[0].EventType != daemonEventSessionMessageSent || attention.Events[0].SubjectID != "am-1" {
		t.Fatalf("attention events = %#v, want session message event", attention.Events)
	}
}

func TestLifecycleTransitionsEmitWorkspaceScopedEvents(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.EnsureHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "planner", Harness: "test"}); err != nil {
		t.Fatalf("EnsureHarnessSessionConfig returned error: %v", err)
	}
	for _, sessionID := range []string{"run-completed", "run-failed"} {
		if err := store.CreateHarnessSession(ctx, globaldb.HarnessSession{SessionID: sessionID, WorkspaceID: "ws-1", AgentID: "agent-1", Harness: "test", Status: "running", Usage: globaldb.HarnessSessionUsageEphemeral}); err != nil {
			t.Fatalf("CreateHarnessSession %s returned error: %v", sessionID, err)
		}
	}
	lifecycle := newHarnessLifecycle(store)
	if err := lifecycle.markCompleted(ctx, "run-completed"); err != nil {
		t.Fatalf("markCompleted returned error: %v", err)
	}
	if err := lifecycle.markFailed(ctx, "run-failed"); err != nil {
		t.Fatalf("markFailed returned error: %v", err)
	}
	events, err := store.ListDaemonEventsAfter(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListDaemonEventsAfter returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %#v, want completed and failed events", events)
	}
	for _, event := range events {
		if event.WorkspaceID != "ws-1" || event.SessionID == "" {
			t.Fatalf("event = %#v, want workspace-scoped lifecycle event", event)
		}
	}
}
