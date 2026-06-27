package globaldb

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Delivery state transitions and their delivery.* events are one atomic fact:
// every transition method emits its event in the same transaction, and a failed
// transition leaves no event behind.
func TestPendingDeliveryTransitionsEmitEventsAtomically(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-events")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)
	_, _, delivery := seedDueWorkerDeliveryFixture(t, store, ctx, "events", base)

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
	requireDeliveryEvent(t, lastDeliveryEvent(t, store, ctx, delivery), WorkspaceEventDeliveryAttempted, claimed, false)

	retryAt := base.Add(10 * time.Minute)
	retry, err := store.SchedulePendingDeliveryRetry(ctx, delivery.DeliveryID, retryAt, "adapter busy")
	if err != nil {
		t.Fatalf("SchedulePendingDeliveryRetry returned error: %v", err)
	}
	requireDeliveryEvent(t, lastDeliveryEvent(t, store, ctx, delivery), WorkspaceEventDeliveryRetryScheduled, retry, false)
	if retry.LastError != "adapter busy" || retry.NextAttemptAt == nil || !retry.NextAttemptAt.Equal(retryAt) {
		t.Fatalf("retry delivery = %#v, want retry facts on row", retry)
	}

	completed, err := store.CompletePendingDelivery(ctx, delivery.DeliveryID)
	if err != nil {
		t.Fatalf("CompletePendingDelivery returned error: %v", err)
	}
	requireDeliveryEvent(t, lastDeliveryEvent(t, store, ctx, delivery), WorkspaceEventDeliveryCompleted, completed, false)
	if completed.TerminalAt == nil {
		t.Fatalf("completed delivery = %#v, want terminal timestamp", completed)
	}
	subscription, err := store.GetEventSubscription(ctx, delivery.SubscriptionID)
	if err != nil {
		t.Fatalf("GetEventSubscription returned error: %v", err)
	}
	if subscription.AckSequence == 0 {
		t.Fatalf("subscription = %#v, want ack advanced in the same completion call", subscription)
	}
}

func TestFailPendingDeliveryEmitsAttentionEventAtomically(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-fail-event")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)
	_, _, delivery := seedDueWorkerDeliveryFixture(t, store, ctx, "fail-event", base)

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
	requireDeliveryEvent(t, lastDeliveryEvent(t, store, ctx, delivery), WorkspaceEventDeliveryFailed, failed, true)
}

func TestDeliveryLifecycleEventsDoNotCreateRecursiveDeliveries(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-no-recursion")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 14, 0, 0, 0, time.UTC)
	_, _, delivery := seedDueWorkerDeliveryFixture(t, store, ctx, "no-recursion", base)

	if _, err := store.ClaimDuePendingDeliveryAttempt(ctx, delivery.DeliveryID, base.Add(time.Minute)); err != nil {
		t.Fatalf("ClaimDuePendingDeliveryAttempt returned error: %v", err)
	}
	due, err := store.ListDuePendingDeliveries(ctx, base.Add(2*time.Minute), 10)
	if err != nil {
		t.Fatalf("ListDuePendingDeliveries after delivery event returned error: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("due deliveries after delivery.attempted = %#v, want no recursive delivery lifecycle work", due)
	}
}

func deliveryEventsForSubject(t *testing.T, store *Store, ctx context.Context, workspaceID, deliveryID string) []WorkspaceEvent {
	t.Helper()
	events, err := store.ListWorkspaceEventsAfterSequence(ctx, workspaceID, 0, 100)
	if err != nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned error: %v", err)
	}
	matched := make([]WorkspaceEvent, 0, len(events))
	for _, event := range events {
		decoded, ok := DecodeDeliveryWorkspaceEvent(event)
		if ok && decoded.DeliveryID == deliveryID {
			matched = append(matched, event)
		}
	}
	return matched
}

func lastDeliveryEvent(t *testing.T, store *Store, ctx context.Context, delivery PendingDelivery) WorkspaceEvent {
	t.Helper()
	events := deliveryEventsForSubject(t, store, ctx, delivery.WorkspaceID, delivery.DeliveryID)
	if len(events) == 0 {
		t.Fatalf("no delivery events found for %s", delivery.DeliveryID)
	}
	return events[len(events)-1]
}

func requireDeliveryEvent(t *testing.T, event WorkspaceEvent, eventType string, delivery PendingDelivery, attentionRequired bool) {
	t.Helper()
	if event.EventType != eventType || event.SubjectType != WorkspaceEventSubjectPendingDelivery || event.SubjectID != delivery.DeliveryID || event.ProducerType != WorkspaceEventProducerDaemon || event.ProducerID != WorkspaceEventProducerWorkspaceDelivery || event.AttentionRequired != attentionRequired {
		t.Fatalf("delivery event = %#v, want %s for %s attention=%t", event, eventType, delivery.DeliveryID, attentionRequired)
	}
	decoded, ok := DecodeDeliveryWorkspaceEvent(event)
	if !ok || decoded.DeliveryID != delivery.DeliveryID || decoded.SubscriptionID != delivery.SubscriptionID || decoded.TargetType != delivery.TargetType || decoded.TargetID != delivery.TargetID {
		t.Fatalf("decoded delivery event ok=%v decoded=%#v, want delivery %#v", ok, decoded, delivery)
	}
	if eventType != WorkspaceEventDeliveryCompleted && decoded.LastError != delivery.LastError {
		t.Fatalf("decoded delivery last_error = %q, want %q", decoded.LastError, delivery.LastError)
	}
}
