package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestWorkspaceDeliveryRPCRetryLifecycle(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.CreateWorkspace(context.Background(), "ws-delivery", "ws-delivery", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	_ = callMethod[WorkspaceEventSubscriptionResponse](t, registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-delivery", WorkspaceID: "ws-delivery", OwnerSessionID: "owner-delivery", FilterJSON: `{"event_types":["worker.completed"]}`})
	event := callMethod[WorkspaceEventResponse](t, registry, "workspace.events.append", WorkspaceEventAppendRequest{EventID: "we-delivery", WorkspaceID: "ws-delivery", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-delivery"})
	now := time.Now().UTC()
	future := now.Add(time.Hour)

	dispatched := callMethod[WorkspaceDeliveryResponse](t, registry, "workspace.deliveries.dispatch", WorkspaceDeliveryDispatchRequest{DeliveryID: "pd-1", WorkspaceID: "ws-delivery", SubscriptionID: "sub-delivery", TargetType: "harness_session", TargetID: "owner-delivery", EventIDs: []string{event.EventID}, DeliveryPolicyJSON: `{"max_attempts":3}`, NextAttemptAt: now.Format(time.RFC3339Nano)})
	if dispatched.Delivery.DeliveryID != "pd-1" || dispatched.Delivery.Status != "pending" || dispatched.Delivery.Attempts != 0 || len(dispatched.Delivery.EventIDs) != 1 {
		t.Fatalf("workspace.deliveries.dispatch = %#v, want pending delivery with event ref", dispatched)
	}
	due := callMethod[WorkspaceDeliveriesResponse](t, registry, "workspace.deliveries.retry_due", WorkspaceDeliveriesRetryDueRequest{Now: now.Format(time.RFC3339Nano), Limit: 10})
	if len(due.Deliveries) != 1 || due.Deliveries[0].DeliveryID != "pd-1" {
		t.Fatalf("workspace.deliveries.retry_due = %#v, want pd-1 due", due)
	}
	attempted := callMethod[WorkspaceDeliveryResponse](t, registry, "workspace.deliveries.record_attempt", WorkspaceDeliveryRecordAttemptRequest{DeliveryID: "pd-1", LastError: "target offline", NextAttemptAt: future.Format(time.RFC3339Nano)})
	if attempted.Delivery.Attempts != 1 || attempted.Delivery.Status != "pending" || attempted.Delivery.LastError != "target offline" || attempted.Delivery.NextAttemptAt == "" {
		t.Fatalf("workspace.deliveries.record_attempt = %#v, want pending retry with error and backoff", attempted)
	}
	due = callMethod[WorkspaceDeliveriesResponse](t, registry, "workspace.deliveries.retry_due", WorkspaceDeliveriesRetryDueRequest{Now: now.Format(time.RFC3339Nano), Limit: 10})
	if len(due.Deliveries) != 0 {
		t.Fatalf("workspace.deliveries.retry_due before backoff = %#v, want none", due)
	}
	due = callMethod[WorkspaceDeliveriesResponse](t, registry, "workspace.deliveries.retry_due", WorkspaceDeliveriesRetryDueRequest{Now: future.Add(time.Second).Format(time.RFC3339Nano), Limit: 10})
	if len(due.Deliveries) != 1 || due.Deliveries[0].Attempts != 1 {
		t.Fatalf("workspace.deliveries.retry_due after backoff = %#v, want retryable pd-1", due)
	}
	completed := callMethod[WorkspaceDeliveryResponse](t, registry, "workspace.deliveries.complete", WorkspaceDeliveryCompleteRequest{DeliveryID: "pd-1"})
	if completed.Delivery.Status != "completed" || completed.Delivery.TerminalAt == "" {
		t.Fatalf("workspace.deliveries.complete = %#v, want terminal completed", completed)
	}
	due = callMethod[WorkspaceDeliveriesResponse](t, registry, "workspace.deliveries.retry_due", WorkspaceDeliveriesRetryDueRequest{Now: future.Add(time.Second).Format(time.RFC3339Nano), Limit: 10})
	if len(due.Deliveries) != 0 {
		t.Fatalf("workspace.deliveries.retry_due after complete = %#v, want none", due)
	}
}

func TestWorkspaceEventSubscriptionAutoDispatchesDeliveryAndCompleteAcks(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.CreateWorkspace(context.Background(), "ws-auto-delivery", "ws-auto-delivery", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}

	_ = callMethod[WorkspaceEventSubscriptionResponse](t, registry, "workspace.events.subscribe", WorkspaceEventSubscribeRequest{SubscriptionID: "sub-auto-delivery", WorkspaceID: "ws-auto-delivery", OwnerSessionID: "owner-auto-delivery", FilterJSON: `{"event_types":["worker.completed"],"correlation_ids":["fg-auto-delivery"]}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "owner-auto-delivery", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn","max_attempts":3}`})
	nonMatching := callMethod[WorkspaceEventResponse](t, registry, "workspace.events.append", WorkspaceEventAppendRequest{EventID: "we-auto-started", WorkspaceID: "ws-auto-delivery", EventType: "worker.started", SubjectType: "harness_session", SubjectID: "worker-auto", CorrelationID: "fg-auto-delivery"})
	completed := callMethod[WorkspaceEventResponse](t, registry, "workspace.events.append", WorkspaceEventAppendRequest{EventID: "we-auto-completed", WorkspaceID: "ws-auto-delivery", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-auto", CorrelationID: "fg-auto-delivery", PayloadRefJSON: `{"kind":"final_response","id":"fr-auto"}`})
	if nonMatching.Sequence != 1 || completed.Sequence != 2 {
		t.Fatalf("event sequences = %d, %d, want 1 and 2", nonMatching.Sequence, completed.Sequence)
	}

	due := callMethod[WorkspaceDeliveriesResponse](t, registry, "workspace.deliveries.retry_due", WorkspaceDeliveriesRetryDueRequest{Now: time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano), Limit: 10})
	if len(due.Deliveries) != 1 {
		t.Fatalf("workspace.deliveries.retry_due = %#v, want one auto-dispatched delivery", due)
	}
	delivery := due.Deliveries[0]
	if delivery.SubscriptionID != "sub-auto-delivery" || delivery.TargetType != "harness_session" || delivery.TargetID != "owner-auto-delivery" || delivery.DeliveryPolicyJSON != `{"channel":"visible_prompt_turn","max_attempts":3}` || delivery.Status != "pending" || len(delivery.EventIDs) != 1 || delivery.EventIDs[0] != completed.EventID {
		t.Fatalf("auto delivery = %#v, want pending delivery for completed event and target", delivery)
	}
	beforeComplete := callMethod[WorkspaceEventsResponse](t, registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-auto-delivery", Limit: 10})
	if len(beforeComplete.Events) != 1 || beforeComplete.Events[0].EventID != completed.EventID {
		t.Fatalf("workspace.events.next before delivery complete = %#v, want completed event unread", beforeComplete)
	}

	completedDelivery := callMethod[WorkspaceDeliveryResponse](t, registry, "workspace.deliveries.complete", WorkspaceDeliveryCompleteRequest{DeliveryID: delivery.DeliveryID})
	if completedDelivery.Delivery.Status != "completed" || completedDelivery.Delivery.TerminalAt == "" {
		t.Fatalf("workspace.deliveries.complete = %#v, want terminal completed delivery", completedDelivery)
	}
	afterComplete := callMethod[WorkspaceEventsResponse](t, registry, "workspace.events.next", WorkspaceEventsNextRequest{SubscriptionID: "sub-auto-delivery", Limit: 10})
	if len(afterComplete.Events) != 0 {
		t.Fatalf("workspace.events.next after delivery complete = %#v, want subscription acked past delivered event", afterComplete)
	}
}
