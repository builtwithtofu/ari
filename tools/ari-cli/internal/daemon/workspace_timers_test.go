package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestWorkspaceTimerRPCFiresDueTimerAsWorkspaceEvent(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.CreateWorkspace(context.Background(), "ws-timer", "ws-timer", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	_ = callMethod[WorkspaceEventSubscriptionResponse](t, registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-timer", WorkspaceID: "ws-timer", OwnerSessionID: "owner-timer", FilterJSON: `{"event_types":["timer.fired"]}`})

	fireAt := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	created := callMethod[WorkspaceTimerResponse](t, registry, "workspace.timers.create", WorkspaceTimerCreateRequest{TimerID: "timer-1", WorkspaceID: "ws-timer", OwnerSessionID: "owner-timer", SubscriptionID: "sub-timer", SubjectType: "harness_session", SubjectID: "worker-1", Purpose: "worker-timeout", FireAt: fireAt, PayloadJSON: `{"reason":"timeout"}`})
	if created.TimerID != "timer-1" || created.Status != "scheduled" {
		t.Fatalf("workspace.timers.create = %#v, want scheduled timer-1", created)
	}

	fired := callMethod[WorkspaceTimersResponse](t, registry, "workspace.timers.fire_due", WorkspaceTimersFireDueRequest{Now: time.Now().UTC().Format(time.RFC3339Nano), Limit: 10})
	if len(fired.Timers) != 1 || fired.Timers[0].TimerID != "timer-1" || fired.Timers[0].Status != "fired" || fired.Timers[0].FiredEventID == "" {
		t.Fatalf("workspace.timers.fire_due = %#v, want fired timer with event id", fired)
	}
	events := callMethod[WorkspaceEventsResponse](t, registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-timer", Limit: 10})
	if len(events.Events) != 1 || events.Events[0].EventID != fired.Timers[0].FiredEventID || events.Events[0].EventType != "timer.fired" || events.Events[0].SubjectType != "timer" || events.Events[0].SubjectID != "timer-1" {
		t.Fatalf("workspace.events.next after timer fire = %#v, want timer.fired event", events)
	}
	stored := callMethod[WorkspaceTimerResponse](t, registry, "workspace.timers.get", WorkspaceTimerGetRequest{TimerID: "timer-1"})
	if stored.Status != "fired" || stored.FiredEventID != fired.Timers[0].FiredEventID {
		t.Fatalf("workspace.timers.get = %#v, want persisted fired event id", stored)
	}
}

func TestWorkspaceTimerRPCCancelPreventsFire(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.CreateWorkspace(context.Background(), "ws-timer-cancel", "ws-timer-cancel", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	fireAt := time.Now().Add(time.Minute).UTC()
	_ = callMethod[WorkspaceTimerResponse](t, registry, "workspace.timers.create", WorkspaceTimerCreateRequest{TimerID: "timer-cancel", WorkspaceID: "ws-timer-cancel", OwnerSessionID: "owner-timer", FireAt: fireAt.Format(time.RFC3339Nano), PayloadJSON: `{}`})
	canceled := callMethod[WorkspaceTimerResponse](t, registry, "workspace.timers.cancel", WorkspaceTimerCancelRequest{TimerID: "timer-cancel"})
	if canceled.Status != "canceled" {
		t.Fatalf("workspace.timers.cancel = %#v, want canceled", canceled)
	}
	fired := callMethod[WorkspaceTimersResponse](t, registry, "workspace.timers.fire_due", WorkspaceTimersFireDueRequest{Now: fireAt.Add(time.Hour).Format(time.RFC3339Nano), Limit: 10})
	if len(fired.Timers) != 0 {
		t.Fatalf("workspace.timers.fire_due after cancel = %#v, want no fired timers", fired)
	}
}
