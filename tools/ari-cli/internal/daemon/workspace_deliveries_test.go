package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestWorkspaceDeliveryRPCGetInspectsPendingDelivery(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	ctx := context.Background()
	base := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	if err := store.CreateWorkspace(ctx, "ws-delivery", "ws-delivery", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, globaldb.EventSubscription{SubscriptionID: "sub-delivery", WorkspaceID: "ws-delivery", OwnerSessionID: "owner-delivery", FilterJSON: `{"event_types":["worker.completed"]}`, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	nextAttemptAt := base.Add(time.Minute)
	created, err := store.CreatePendingDelivery(ctx, globaldb.PendingDelivery{DeliveryID: "pd-1", WorkspaceID: "ws-delivery", SubscriptionID: "sub-delivery", TargetType: "harness_session", TargetID: "owner-delivery", EventIDs: []string{"we-delivery"}, DeliveryPolicyJSON: `{"max_attempts":3}`, NextAttemptAt: &nextAttemptAt, CreatedAt: base, UpdatedAt: base})
	if err != nil {
		t.Fatalf("CreatePendingDelivery returned error: %v", err)
	}

	got := callMethod[WorkspaceDeliveryResponse](t, registry, "workspace.deliveries.get", WorkspaceDeliveryGetRequest{DeliveryID: created.DeliveryID})
	if got.Delivery.DeliveryID != "pd-1" || got.Delivery.WorkspaceID != "ws-delivery" || got.Delivery.SubscriptionID != "sub-delivery" || got.Delivery.Status != "pending" || len(got.Delivery.EventIDs) != 1 || got.Delivery.EventIDs[0] != "we-delivery" || got.Delivery.NextAttemptAt == "" {
		t.Fatalf("workspace.deliveries.get = %#v, want inspected pending delivery", got)
	}
}

func TestWorkspaceEventSubscriptionAutoDispatchesDeliveryThroughRuntimeAndCompleteAcks(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-auto-delivery", "ws-auto-delivery", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}

	_ = callMethod[WorkspaceEventSubscriptionResponse](t, registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-auto-delivery", WorkspaceID: "ws-auto-delivery", OwnerSessionID: "owner-auto-delivery", FilterJSON: `{"event_types":["worker.completed"],"correlation_ids":["fg-auto-delivery"]}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "owner-auto-delivery", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn","max_attempts":3}`})
	nonMatching := appendWorkspaceEventForTest(t, store, globaldb.WorkspaceEvent{EventID: "we-auto-started", WorkspaceID: "ws-auto-delivery", EventType: "worker.started", SubjectType: "harness_session", SubjectID: "worker-auto", CorrelationID: "fg-auto-delivery"})
	completed := appendWorkspaceEventForTest(t, store, globaldb.WorkspaceEvent{EventID: "we-auto-completed", WorkspaceID: "ws-auto-delivery", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-auto", CorrelationID: "fg-auto-delivery", PayloadRefJSON: `{"kind":"final_response","id":"fr-auto"}`})
	if nonMatching.Sequence != 1 || completed.Sequence != 2 {
		t.Fatalf("event sequences = %d, %d, want 1 and 2", nonMatching.Sequence, completed.Sequence)
	}

	dueBefore, err := store.ListDuePendingDeliveriesForScope(ctx, time.Now().UTC().Add(time.Minute), "ws-auto-delivery", "owner-auto-delivery", 10)
	if err != nil {
		t.Fatalf("ListDuePendingDeliveriesForScope returned error: %v", err)
	}
	if len(dueBefore) != 1 || dueBefore[0].SubscriptionID != "sub-auto-delivery" || dueBefore[0].TargetID != "owner-auto-delivery" || len(dueBefore[0].EventIDs) != 1 || dueBefore[0].EventIDs[0] != completed.EventID {
		t.Fatalf("due delivery = %#v, want pending delivery for completed event and target", dueBefore)
	}
	beforeComplete := callMethod[WorkspaceEventsResponse](t, registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-auto-delivery", Limit: 10})
	if len(beforeComplete.Events) != 1 || beforeComplete.Events[0].EventID != completed.EventID {
		t.Fatalf("workspace.events.next before delivery complete = %#v, want completed event unread", beforeComplete)
	}

	service := newWorkspaceOrchestrationService(store, &recordingWorkspaceDeliveryDispatcher{result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}})
	if err := service.runDueOnce(ctx, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("runDueOnce returned error: %v", err)
	}
	afterComplete := callMethod[WorkspaceEventsResponse](t, registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-auto-delivery", Limit: 10})
	if len(afterComplete.Events) != 0 {
		t.Fatalf("workspace.events.next after runtime delivery complete = %#v, want subscription acked past delivered event", afterComplete)
	}
}
