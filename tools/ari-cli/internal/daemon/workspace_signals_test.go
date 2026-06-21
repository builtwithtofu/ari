package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestWorkspaceSignalSendAppendsWorkspaceEvent(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-signal", "ws-signal", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "agent-signal", WorkspaceID: "ws-signal", Name: "signal", Harness: "fake"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig returned error: %v", err)
	}
	if err := store.CreateHarnessSession(ctx, globaldb.HarnessSession{SessionID: "worker-signal", WorkspaceID: "ws-signal", AgentID: "agent-signal", Harness: "fake", Status: "running", Usage: globaldb.HarnessSessionUsageEphemeral, CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateHarnessSession returned error: %v", err)
	}
	_ = callMethod[WorkspaceEventSubscriptionResponse](t, registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-signal", WorkspaceID: "ws-signal", OwnerSessionID: "owner-signal", FilterJSON: `{"event_types":["signal.sent"],"subject_types":["harness_session"],"subject_ids":["worker-signal"]}`})

	sent := callMethod[WorkspaceSignalResponse](t, registry, "workspace.signals.send", WorkspaceSignalSendRequest{EventID: "signal-1", WorkspaceID: "ws-signal", TargetType: "harness_session", TargetID: "worker-signal", ProducerType: "session", ProducerID: "owner-signal", CorrelationID: "fanout-signal", PayloadJSON: `{"action":"continue"}`})
	if sent.Event.EventID != "signal-1" || sent.Event.EventType != "signal.sent" || sent.Event.SubjectID != "worker-signal" || sent.Event.CorrelationID != "fanout-signal" {
		t.Fatalf("workspace.signals.send = %#v, want signal.sent event", sent)
	}
	events := callMethod[WorkspaceEventsResponse](t, registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-signal", Limit: 10})
	if len(events.Events) != 1 || events.Events[0].EventID != sent.Event.EventID {
		t.Fatalf("workspace.events.next after signal = %#v, want sent signal event", events)
	}
}

func TestWorkspaceSignalSendRejectsCrossWorkspaceScopedTargets(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-signal", "ws-signal", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace ws-signal returned error: %v", err)
	}
	if err := store.CreateWorkspace(ctx, "ws-other", "ws-other", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace ws-other returned error: %v", err)
	}
	if err := store.CreateFanoutGroup(ctx, globaldb.FanoutGroup{FanoutGroupID: "fg-other", WorkspaceID: "ws-other", SourceSessionID: "other-run", SourceAgentID: "agent-other", RequestAgentMessageID: "request-other", Body: "other"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "agent-other", WorkspaceID: "ws-other", Name: "other", Harness: "fake"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig returned error: %v", err)
	}
	if err := store.CreateHarnessSession(ctx, globaldb.HarnessSession{SessionID: "worker-other", WorkspaceID: "ws-other", AgentID: "agent-other", Harness: "fake", Status: "running", Usage: globaldb.HarnessSessionUsageEphemeral, CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateHarnessSession returned error: %v", err)
	}
	now := time.Date(2026, 6, 21, 20, 0, 0, 0, time.UTC)
	if _, err := store.CreateEventSubscription(ctx, globaldb.EventSubscription{SubscriptionID: "sub-other", WorkspaceID: "ws-other", OwnerSessionID: "other-run", FilterJSON: `{"event_types":["worker.completed"]}`, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}

	for _, tc := range []struct {
		name       string
		targetType string
		targetID   string
	}{
		{name: "fanout group", targetType: "fanout_group", targetID: "fg-other"},
		{name: "harness session", targetType: "harness_session", targetID: "worker-other"},
		{name: "event subscription", targetType: "event_subscription", targetID: "sub-other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := callMethodError(registry, "workspace.signals.send", WorkspaceSignalSendRequest{EventID: "signal-" + tc.targetID, WorkspaceID: "ws-signal", TargetType: tc.targetType, TargetID: tc.targetID, ProducerType: "session", ProducerID: "owner-signal", PayloadJSON: `{"action":"continue"}`})
			if data := requireHandlerErrorData(t, err); data["reason"] != "signal_target_scope_mismatch" {
				t.Fatalf("error data = %#v, want signal_target_scope_mismatch", data)
			}
		})
	}
}
