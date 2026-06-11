package globaldb

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func workspaceEventStringPayloadForTest(t *testing.T, raw string) map[string]string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("payload json %q invalid: %v", raw, err)
	}
	out := make(map[string]string, len(payload))
	for key, value := range payload {
		if text, ok := value.(string); ok {
			out[key] = text
		}
	}
	return out
}

func seedDuePendingDeliveryForEvents(t *testing.T, store *Store, ctx context.Context, base time.Time) PendingDelivery {
	t.Helper()
	if err := store.CreateWorkspace(ctx, "ws-delivery-events", "ws-delivery-events", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-delivery-events", WorkspaceID: "ws-delivery-events", OwnerSessionID: "orch-run", FilterJSON: `{"event_types":["worker.completed"]}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "orch-run", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn","max_attempts":3}`, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-delivery-events", WorkspaceID: "ws-delivery-events", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-run", ProducerType: "session", ProducerID: "worker-run", CreatedAt: base.Add(time.Second)}); err != nil {
		t.Fatalf("AppendWorkspaceEvent returned error: %v", err)
	}
	due, err := store.ListDuePendingDeliveries(ctx, base.Add(time.Minute), 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("ListDuePendingDeliveries = %#v err=%v, want one due delivery", due, err)
	}
	return due[0]
}

func deliveryEventsForSubject(t *testing.T, store *Store, ctx context.Context, workspaceID, deliveryID string) []WorkspaceEvent {
	t.Helper()
	events, err := store.ListWorkspaceEventsAfterSequence(ctx, workspaceID, 0, 100)
	if err != nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned error: %v", err)
	}
	matched := make([]WorkspaceEvent, 0, len(events))
	for _, event := range events {
		if event.SubjectType == "pending_delivery" && event.SubjectID == deliveryID {
			matched = append(matched, event)
		}
	}
	return matched
}

// Delivery state transitions and their delivery.* events are one atomic
// fact: every transition method emits its event in the same transaction, and
// a failed transition leaves no event behind.
func TestPendingDeliveryTransitionsEmitEventsAtomically(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-events")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)
	delivery := seedDuePendingDeliveryForEvents(t, store, ctx, base)

	// Claiming a non-due delivery is not a fact: no row change, no event.
	if _, err := store.ClaimDuePendingDeliveryAttempt(ctx, delivery.DeliveryID, base.Add(-time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-due claim error = %v, want ErrNotFound", err)
	}
	if events := deliveryEventsForSubject(t, store, ctx, delivery.WorkspaceID, delivery.DeliveryID); len(events) != 0 {
		t.Fatalf("events after non-due claim = %#v, want none", events)
	}

	claimed, err := store.ClaimDuePendingDeliveryAttempt(ctx, delivery.DeliveryID, base.Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimDuePendingDeliveryAttempt returned error: %v", err)
	}
	events := deliveryEventsForSubject(t, store, ctx, delivery.WorkspaceID, delivery.DeliveryID)
	if len(events) != 1 || events[0].EventType != "delivery.attempted" {
		t.Fatalf("events after claim = %#v, want exactly one delivery.attempted", events)
	}
	payload := map[string]string{}
	for key, value := range workspaceEventStringPayloadForTest(t, events[0].PayloadJSON) {
		payload[key] = value
	}
	if payload["delivery_id"] != claimed.DeliveryID || payload["subscription_id"] != claimed.SubscriptionID || payload["attempts"] != "1" {
		t.Fatalf("attempted payload = %#v, want delivery identity with attempts", payload)
	}

	retryAt := base.Add(10 * time.Minute)
	if _, err := store.SchedulePendingDeliveryRetry(ctx, delivery.DeliveryID, retryAt, "adapter busy"); err != nil {
		t.Fatalf("SchedulePendingDeliveryRetry returned error: %v", err)
	}
	events = deliveryEventsForSubject(t, store, ctx, delivery.WorkspaceID, delivery.DeliveryID)
	if len(events) != 2 || events[1].EventType != "delivery.retry_scheduled" {
		t.Fatalf("events after retry = %#v, want delivery.retry_scheduled appended", events)
	}
	retryPayload := workspaceEventStringPayloadForTest(t, events[1].PayloadJSON)
	if retryPayload["last_error"] != "adapter busy" || !strings.HasPrefix(retryPayload["next_attempt_at"], "2026-06-11T09:10:00") {
		t.Fatalf("retry payload = %#v, want error and next attempt time", retryPayload)
	}

	completed, err := store.CompletePendingDelivery(ctx, delivery.DeliveryID)
	if err != nil {
		t.Fatalf("CompletePendingDelivery returned error: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("completed status = %q, want completed", completed.Status)
	}
	events = deliveryEventsForSubject(t, store, ctx, delivery.WorkspaceID, delivery.DeliveryID)
	if len(events) != 3 || events[2].EventType != "delivery.completed" || events[2].AttentionRequired {
		t.Fatalf("events after completion = %#v, want delivery.completed without attention", events)
	}
	subscription, err := store.GetEventSubscription(ctx, delivery.SubscriptionID)
	if err != nil {
		t.Fatalf("GetEventSubscription returned error: %v", err)
	}
	if subscription.AckSequence == 0 {
		t.Fatalf("subscription = %#v, want ack advanced in the same completion call", subscription)
	}
}

func TestRecordPendingDeliveryAttemptEmitsRetryEvent(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-record-attempt-event")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 9, 30, 0, 0, time.UTC)
	delivery := seedDuePendingDeliveryForEvents(t, store, ctx, base)
	retryAt := base.Add(5 * time.Minute)

	if _, err := store.RecordPendingDeliveryAttempt(ctx, delivery.DeliveryID, &retryAt, "manual retry"); err != nil {
		t.Fatalf("RecordPendingDeliveryAttempt returned error: %v", err)
	}
	events := deliveryEventsForSubject(t, store, ctx, delivery.WorkspaceID, delivery.DeliveryID)
	if len(events) != 1 || events[0].EventType != "delivery.retry_scheduled" {
		t.Fatalf("events after record attempt = %#v, want delivery.retry_scheduled", events)
	}
	payload := workspaceEventStringPayloadForTest(t, events[0].PayloadJSON)
	if payload["last_error"] != "manual retry" || payload["attempts"] != "1" || !strings.HasPrefix(payload["next_attempt_at"], "2026-06-11T09:35:00") {
		t.Fatalf("record attempt payload = %#v, want retry facts", payload)
	}
}

func TestFailPendingDeliveryEmitsAttentionEventAtomically(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-fail-event")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)
	delivery := seedDuePendingDeliveryForEvents(t, store, ctx, base)

	// Unknown delivery transition leaves no event.
	if _, err := store.FailPendingDelivery(ctx, "pd-unknown", "boom"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown fail error = %v, want ErrNotFound", err)
	}
	if events := deliveryEventsForSubject(t, store, ctx, delivery.WorkspaceID, "pd-unknown"); len(events) != 0 {
		t.Fatalf("events for unknown delivery = %#v, want none", events)
	}

	failed, err := store.FailPendingDelivery(ctx, delivery.DeliveryID, "terminal adapter failure")
	if err != nil {
		t.Fatalf("FailPendingDelivery returned error: %v", err)
	}
	events := deliveryEventsForSubject(t, store, ctx, delivery.WorkspaceID, delivery.DeliveryID)
	if len(events) != 1 || events[0].EventType != "delivery.failed" || !events[0].AttentionRequired {
		t.Fatalf("events after fail = %#v, want one attention-required delivery.failed", events)
	}
	failPayload := workspaceEventStringPayloadForTest(t, events[0].PayloadJSON)
	if failPayload["last_error"] != failed.LastError || failPayload["status"] != "failed" {
		t.Fatalf("fail payload = %#v, want terminal error recorded", failPayload)
	}
}

func TestDeliveryLifecycleEventsDoNotCreateRecursiveDeliveries(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-no-recursion")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 14, 0, 0, 0, time.UTC)

	if err := store.CreateWorkspace(ctx, "ws-no-recursion", "ws-no-recursion", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-no-recursion", WorkspaceID: "ws-no-recursion", OwnerSessionID: "orch-no-recursion", FilterJSON: `{}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "orch-no-recursion", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn"}`, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-no-recursion", WorkspaceID: "ws-no-recursion", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-no-recursion", ProducerType: "session", ProducerID: "worker-no-recursion", CreatedAt: base.Add(time.Second)}); err != nil {
		t.Fatalf("AppendWorkspaceEvent returned error: %v", err)
	}
	due, err := store.ListDuePendingDeliveries(ctx, base.Add(time.Minute), 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("ListDuePendingDeliveries before claim = %#v err=%v, want one original delivery", due, err)
	}
	if _, err := store.ClaimDuePendingDeliveryAttempt(ctx, due[0].DeliveryID, base.Add(time.Minute)); err != nil {
		t.Fatalf("ClaimDuePendingDeliveryAttempt returned error: %v", err)
	}

	due, err = store.ListDuePendingDeliveries(ctx, base.Add(2*time.Minute), 10)
	if err != nil {
		t.Fatalf("ListDuePendingDeliveries after delivery event returned error: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("due deliveries after delivery.attempted = %#v, want no recursive delivery lifecycle work", due)
	}
}
