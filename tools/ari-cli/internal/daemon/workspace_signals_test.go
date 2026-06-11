package daemon

import (
	"context"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestWorkspaceSignalSendAppendsWorkspaceEvent(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.CreateWorkspace(context.Background(), "ws-signal", "ws-signal", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
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
