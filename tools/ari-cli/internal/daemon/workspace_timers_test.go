package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestWorkspaceTimerRPCCreateAndRuntimeFireProducesWorkspaceEvent(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-timer", "ws-timer", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	_ = callMethod[WorkspaceEventSubscriptionResponse](t, registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-timer", WorkspaceID: "ws-timer", OwnerSessionID: "owner-timer", FilterJSON: `{"event_types":["timer.fired"]}`})

	fireAt := time.Now().Add(-time.Minute).UTC()
	created := callMethod[WorkspaceTimerResponse](t, registry, "workspace.timers.create", WorkspaceTimerCreateRequest{TimerID: "timer-1", WorkspaceID: "ws-timer", OwnerSessionID: "owner-timer", TargetSubscriptionID: "sub-timer", SubjectType: "harness_session", SubjectID: "worker-1", Purpose: "worker-timeout", FireAt: fireAt.Format(time.RFC3339Nano), PayloadJSON: `{"reason":"timeout"}`})
	if created.TimerID != "timer-1" || created.Status != "scheduled" {
		t.Fatalf("workspace.timers.create = %#v, want scheduled timer-1", created)
	}

	runtime := newWorkspaceOrchestrationRuntime(store, &recordingWorkspaceDeliveryDispatcher{})
	if err := runtime.runDueOnce(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("runDueOnce returned error: %v", err)
	}
	stored := callMethod[WorkspaceTimerResponse](t, registry, "workspace.timers.get", WorkspaceTimerGetRequest{TimerID: "timer-1"})
	if stored.Status != "fired" || stored.FiredEventID == "" {
		t.Fatalf("workspace.timers.get = %#v, want persisted fired event id", stored)
	}
	events := callMethod[WorkspaceEventsResponse](t, registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-timer", Limit: 10})
	if len(events.Events) != 1 || events.Events[0].EventID != stored.FiredEventID || events.Events[0].EventType != "timer.fired" || events.Events[0].SubjectType != "timer" || events.Events[0].SubjectID != "timer-1" {
		t.Fatalf("workspace.events.next after timer fire = %#v, want timer.fired event", events)
	}
}

func TestWorkspaceTimerRPCCancelPreventsRuntimeFire(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-timer-cancel", "ws-timer-cancel", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	fireAt := time.Now().Add(time.Minute).UTC()
	_ = callMethod[WorkspaceTimerResponse](t, registry, "workspace.timers.create", WorkspaceTimerCreateRequest{TimerID: "timer-cancel", WorkspaceID: "ws-timer-cancel", OwnerSessionID: "owner-timer", FireAt: fireAt.Format(time.RFC3339Nano), PayloadJSON: `{}`})
	canceled := callMethod[WorkspaceTimerResponse](t, registry, "workspace.timers.cancel", WorkspaceTimerCancelRequest{TimerID: "timer-cancel"})
	if canceled.Status != "canceled" {
		t.Fatalf("workspace.timers.cancel = %#v, want canceled", canceled)
	}
	runtime := newWorkspaceOrchestrationRuntime(store, &recordingWorkspaceDeliveryDispatcher{})
	if err := runtime.runDueOnce(ctx, fireAt.Add(time.Hour)); err != nil {
		t.Fatalf("runDueOnce returned error: %v", err)
	}
	stored := callMethod[WorkspaceTimerResponse](t, registry, "workspace.timers.get", WorkspaceTimerGetRequest{TimerID: "timer-cancel"})
	if stored.Status != "canceled" || stored.FiredEventID != "" {
		t.Fatalf("workspace.timers.get after cancel/runtime = %#v, want canceled without fired event", stored)
	}
}
