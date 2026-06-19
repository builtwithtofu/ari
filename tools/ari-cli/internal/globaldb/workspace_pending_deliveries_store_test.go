package globaldb

import (
	"context"
	"testing"
	"time"
)

func TestPendingDeliveryAttemptLifecycleControlsSubscriptionAck(t *testing.T) {
	for _, tc := range []struct {
		name     string
		finish   func(context.Context, *Store, string) (PendingDelivery, error)
		wantRead bool
		wantAck  int64
	}{
		{
			name: "completed delivery advances subscription ack",
			finish: func(ctx context.Context, store *Store, deliveryID string) (PendingDelivery, error) {
				return store.CompletePendingDelivery(ctx, deliveryID)
			},
			wantRead: false,
			wantAck:  1,
		},
		{
			name: "failed delivery leaves subscription unread",
			finish: func(ctx context.Context, store *Store, deliveryID string) (PendingDelivery, error) {
				return store.FailPendingDelivery(ctx, deliveryID, "adapter rejected visible turn")
			},
			wantRead: true,
			wantAck:  0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newGlobalDBTestStore(t, "pending-delivery-attempt")
			ctx := context.Background()
			base := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)

			if err := store.CreateWorkspace(ctx, "ws-delivery-attempt", "ws-delivery-attempt", t.TempDir(), "manual", "auto"); err != nil {
				t.Fatalf("CreateWorkspace returned error: %v", err)
			}
			if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-delivery-attempt", WorkspaceID: "ws-delivery-attempt", OwnerSessionID: "orch-delivery-attempt", FilterJSON: `{"event_types":["worker.completed"]}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "orch-delivery-attempt", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn","max_attempts":3}`, CreatedAt: base, UpdatedAt: base}); err != nil {
				t.Fatalf("CreateEventSubscription returned error: %v", err)
			}
			event, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-delivery-attempt", WorkspaceID: "ws-delivery-attempt", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-delivery-attempt", ProducerType: "session", ProducerID: "worker-delivery-attempt", PayloadRefJSON: `{"kind":"final_response","id":"fr-delivery-attempt"}`, CreatedAt: base.Add(time.Second)})
			if err != nil {
				t.Fatalf("AppendWorkspaceEvent returned error: %v", err)
			}

			due, err := store.ListDuePendingDeliveries(ctx, base.Add(time.Minute), 10)
			if err != nil {
				t.Fatalf("ListDuePendingDeliveries returned error: %v", err)
			}
			if len(due) != 1 || due[0].EventIDs[0] != event.EventID {
				t.Fatalf("due pending deliveries = %#v, want one delivery for %s", due, event.EventID)
			}

			attempted, err := store.ClaimDuePendingDeliveryAttempt(ctx, due[0].DeliveryID, base.Add(time.Minute))
			if err != nil {
				t.Fatalf("ClaimDuePendingDeliveryAttempt returned error: %v", err)
			}
			if attempted.Status != pendingDeliveryStatusAttempted || attempted.Attempts != 1 || attempted.NextAttemptAt != nil || attempted.TerminalAt != nil {
				t.Fatalf("attempted delivery = %#v, want attempted in-flight delivery with one attempt and no retry/terminal time", attempted)
			}
			due, err = store.ListDuePendingDeliveries(ctx, base.Add(2*time.Minute), 10)
			if err != nil {
				t.Fatalf("ListDuePendingDeliveries after attempt returned error: %v", err)
			}
			if len(due) != 0 {
				t.Fatalf("due pending deliveries after claim = %#v, want in-flight attempt hidden from retry queue", due)
			}
			unreadBeforeFinish := readSubscriptionEvents(t, store, "sub-delivery-attempt", 10)
			if len(unreadBeforeFinish) != 1 || unreadBeforeFinish[0].EventID != event.EventID {
				t.Fatalf("subscription events before finish = %#v, want event still unread while attempted", unreadBeforeFinish)
			}

			finished, err := tc.finish(ctx, store, attempted.DeliveryID)
			if err != nil {
				t.Fatalf("finish delivery returned error: %v", err)
			}
			if finished.Attempts != 1 || finished.TerminalAt == nil {
				t.Fatalf("finished delivery = %#v, want terminal outcome preserving one recorded attempt", finished)
			}
			unreadAfterFinish := readSubscriptionEvents(t, store, "sub-delivery-attempt", 10)
			if gotRead := len(unreadAfterFinish) == 1; gotRead != tc.wantRead {
				t.Fatalf("subscription unread after finish = %#v, want unread=%t", unreadAfterFinish, tc.wantRead)
			}
			subscription, err := store.GetEventSubscription(ctx, "sub-delivery-attempt")
			if err != nil {
				t.Fatalf("GetEventSubscription returned error: %v", err)
			}
			if subscription.AckSequence != tc.wantAck || subscription.CursorSequence != tc.wantAck {
				t.Fatalf("subscription cursor/ack = %d/%d, want %d", subscription.CursorSequence, subscription.AckSequence, tc.wantAck)
			}
		})
	}
}

func TestCompletedDeliveryAcksOnlyContiguousDeliveredEvents(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-contiguous-ack")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)

	if err := store.CreateWorkspace(ctx, "ws-contiguous-ack", "ws-contiguous-ack", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-contiguous-ack", WorkspaceID: "ws-contiguous-ack", OwnerSessionID: "orch-contiguous-ack", FilterJSON: `{"event_types":["worker.completed"]}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "orch-contiguous-ack", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn","max_attempts":3}`, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	first, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-contiguous-first", WorkspaceID: "ws-contiguous-ack", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-first", ProducerType: "session", ProducerID: "worker-first", CreatedAt: base.Add(time.Second)})
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent first returned error: %v", err)
	}
	second, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-contiguous-second", WorkspaceID: "ws-contiguous-ack", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-second", ProducerType: "session", ProducerID: "worker-second", CreatedAt: base.Add(2 * time.Second)})
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent second returned error: %v", err)
	}

	secondDelivery, err := store.GetPendingDelivery(ctx, pendingDeliveryIDForSubscriptionEvent("sub-contiguous-ack", second.EventID))
	if err != nil {
		t.Fatalf("GetPendingDelivery second returned error: %v", err)
	}
	if _, err := store.ClaimDuePendingDeliveryAttempt(ctx, secondDelivery.DeliveryID, base.Add(time.Minute)); err != nil {
		t.Fatalf("ClaimDuePendingDeliveryAttempt second returned error: %v", err)
	}
	if _, err := store.CompletePendingDelivery(ctx, secondDelivery.DeliveryID); err != nil {
		t.Fatalf("CompletePendingDelivery second returned error: %v", err)
	}

	subscription, err := store.GetEventSubscription(ctx, "sub-contiguous-ack")
	if err != nil {
		t.Fatalf("GetEventSubscription returned error: %v", err)
	}
	if subscription.CursorSequence != 0 || subscription.AckSequence != 0 {
		t.Fatalf("subscription cursor/ack after second completion = %d/%d, want 0/0 until first event is delivered", subscription.CursorSequence, subscription.AckSequence)
	}
	unread := readSubscriptionEvents(t, store, "sub-contiguous-ack", 10)
	if len(unread) != 2 || unread[0].EventID != first.EventID || unread[1].EventID != second.EventID {
		t.Fatalf("unread events after out-of-order completion = %#v, want both original events", unread)
	}
}

func TestCompletedCustomDeliveryAcksItsEventIDs(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-custom-id-ack")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 12, 30, 0, 0, time.UTC)

	if err := store.CreateWorkspace(ctx, "ws-custom-id-ack", "ws-custom-id-ack", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-custom-id-ack", WorkspaceID: "ws-custom-id-ack", OwnerSessionID: "orch-custom-id-ack", FilterJSON: `{"event_types":["worker.completed"]}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "orch-custom-id-ack", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn"}`, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	event, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-custom-id-ack", WorkspaceID: "ws-custom-id-ack", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-custom-id-ack", ProducerType: "session", ProducerID: "worker-custom-id-ack", CreatedAt: base.Add(time.Second)})
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent returned error: %v", err)
	}
	nextAttempt := base.Add(time.Minute)
	customDelivery, err := store.CreatePendingDelivery(ctx, PendingDelivery{DeliveryID: "pd-custom-manual", WorkspaceID: "ws-custom-id-ack", SubscriptionID: "sub-custom-id-ack", TargetType: "harness_session", TargetID: "orch-custom-id-ack", EventIDs: []string{event.EventID}, NextAttemptAt: &nextAttempt, CreatedAt: base, UpdatedAt: base})
	if err != nil {
		t.Fatalf("CreatePendingDelivery returned error: %v", err)
	}
	if _, err := store.CompletePendingDelivery(ctx, customDelivery.DeliveryID); err != nil {
		t.Fatalf("CompletePendingDelivery returned error: %v", err)
	}

	subscription, err := store.GetEventSubscription(ctx, "sub-custom-id-ack")
	if err != nil {
		t.Fatalf("GetEventSubscription returned error: %v", err)
	}
	if subscription.CursorSequence != event.Sequence || subscription.AckSequence != event.Sequence {
		t.Fatalf("subscription cursor/ack after custom delivery = %d/%d, want %d", subscription.CursorSequence, subscription.AckSequence, event.Sequence)
	}
}

func TestCompletedDeliveryAcksPriorCustomDeliveries(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-prior-custom-ack")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 12, 45, 0, 0, time.UTC)

	if err := store.CreateWorkspace(ctx, "ws-prior-custom-ack", "ws-prior-custom-ack", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-prior-custom-ack", WorkspaceID: "ws-prior-custom-ack", OwnerSessionID: "orch-prior-custom-ack", FilterJSON: `{"event_types":["worker.completed"]}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "orch-prior-custom-ack", CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	first, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-prior-custom-first", WorkspaceID: "ws-prior-custom-ack", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-first", CreatedAt: base.Add(time.Second)})
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent first returned error: %v", err)
	}
	second, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-prior-custom-second", WorkspaceID: "ws-prior-custom-ack", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-second", CreatedAt: base.Add(2 * time.Second)})
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent second returned error: %v", err)
	}
	nextAttempt := base.Add(time.Minute)
	secondCustom, err := store.CreatePendingDelivery(ctx, PendingDelivery{DeliveryID: "pd-prior-custom-second", WorkspaceID: "ws-prior-custom-ack", SubscriptionID: "sub-prior-custom-ack", TargetType: "harness_session", TargetID: "orch-prior-custom-ack", EventIDs: []string{second.EventID}, NextAttemptAt: &nextAttempt, CreatedAt: base, UpdatedAt: base})
	if err != nil {
		t.Fatalf("CreatePendingDelivery custom second returned error: %v", err)
	}
	if _, err := store.CompletePendingDelivery(ctx, secondCustom.DeliveryID); err != nil {
		t.Fatalf("CompletePendingDelivery custom second returned error: %v", err)
	}
	firstDelivery, err := store.GetPendingDelivery(ctx, pendingDeliveryIDForSubscriptionEvent("sub-prior-custom-ack", first.EventID))
	if err != nil {
		t.Fatalf("GetPendingDelivery first returned error: %v", err)
	}
	if _, err := store.CompletePendingDelivery(ctx, firstDelivery.DeliveryID); err != nil {
		t.Fatalf("CompletePendingDelivery first returned error: %v", err)
	}

	subscription, err := store.GetEventSubscription(ctx, "sub-prior-custom-ack")
	if err != nil {
		t.Fatalf("GetEventSubscription returned error: %v", err)
	}
	if subscription.CursorSequence != second.Sequence || subscription.AckSequence != second.Sequence {
		t.Fatalf("subscription cursor/ack after prior custom delivery = %d/%d, want %d", subscription.CursorSequence, subscription.AckSequence, second.Sequence)
	}
}

func TestCompletedDeliveryAckSkipsDeliveryLifecycleEvents(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-skip-lifecycle-ack")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 12, 50, 0, 0, time.UTC)

	if err := store.CreateWorkspace(ctx, "ws-skip-lifecycle-ack", "ws-skip-lifecycle-ack", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-skip-lifecycle-ack", WorkspaceID: "ws-skip-lifecycle-ack", OwnerSessionID: "orch-skip-lifecycle-ack", FilterJSON: `{}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "orch-skip-lifecycle-ack", CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	event, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-skip-lifecycle", WorkspaceID: "ws-skip-lifecycle-ack", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker", CreatedAt: base.Add(time.Second)})
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent returned error: %v", err)
	}
	delivery, err := store.GetPendingDelivery(ctx, pendingDeliveryIDForSubscriptionEvent("sub-skip-lifecycle-ack", event.EventID))
	if err != nil {
		t.Fatalf("GetPendingDelivery returned error: %v", err)
	}
	if _, err := store.ClaimDuePendingDeliveryAttempt(ctx, delivery.DeliveryID, base.Add(time.Minute)); err != nil {
		t.Fatalf("ClaimDuePendingDeliveryAttempt returned error: %v", err)
	}
	if _, err := store.CompletePendingDelivery(ctx, delivery.DeliveryID); err != nil {
		t.Fatalf("CompletePendingDelivery returned error: %v", err)
	}

	subscription, err := store.GetEventSubscription(ctx, "sub-skip-lifecycle-ack")
	if err != nil {
		t.Fatalf("GetEventSubscription returned error: %v", err)
	}
	if subscription.CursorSequence < event.Sequence || subscription.AckSequence < event.Sequence {
		t.Fatalf("subscription cursor/ack = %d/%d, want at least source event sequence %d", subscription.CursorSequence, subscription.AckSequence, event.Sequence)
	}
}

func TestOverdueDeliveriesAreFailedBeforeDueSelection(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-deadline")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 13, 0, 0, 0, time.UTC)

	if err := store.CreateWorkspace(ctx, "ws-deadline", "ws-deadline", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-deadline", WorkspaceID: "ws-deadline", OwnerSessionID: "orch-deadline", FilterJSON: `{}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "orch-deadline", CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	deadline := base.Add(time.Minute)
	nextAttempt := base.Add(30 * time.Second)
	delivery, err := store.CreatePendingDelivery(ctx, PendingDelivery{DeliveryID: "pd-deadline", WorkspaceID: "ws-deadline", SubscriptionID: "sub-deadline", TargetType: "harness_session", TargetID: "orch-deadline", EventIDs: []string{"we-deadline"}, NextAttemptAt: &nextAttempt, DeadlineAt: &deadline, CreatedAt: base, UpdatedAt: base})
	if err != nil {
		t.Fatalf("CreatePendingDelivery returned error: %v", err)
	}

	due, err := store.ListDuePendingDeliveries(ctx, base.Add(2*time.Minute), 10)
	if err != nil {
		t.Fatalf("ListDuePendingDeliveries returned error: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("due deliveries after deadline = %#v, want none", due)
	}
	stored, err := store.GetPendingDelivery(ctx, delivery.DeliveryID)
	if err != nil {
		t.Fatalf("GetPendingDelivery returned error: %v", err)
	}
	if stored.Status != pendingDeliveryStatusFailed || stored.TerminalAt == nil || stored.LastError == "" {
		t.Fatalf("overdue delivery = %#v, want terminal failure before dispatch", stored)
	}
}

func TestTimedOutSubscriptionDeliveriesAreExcludedFromDueSelection(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-timeout-due")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 13, 30, 0, 0, time.UTC)

	if err := store.CreateWorkspace(ctx, "ws-timeout-due", "ws-timeout-due", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	timeoutAt := base.Add(time.Minute)
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-timeout-due", WorkspaceID: "ws-timeout-due", OwnerSessionID: "orch-timeout-due", FilterJSON: `{}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "orch-timeout-due", TimeoutAt: &timeoutAt, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	nextAttempt := base.Add(2 * time.Minute)
	if _, err := store.CreatePendingDelivery(ctx, PendingDelivery{DeliveryID: "pd-timeout-due", WorkspaceID: "ws-timeout-due", SubscriptionID: "sub-timeout-due", TargetType: "harness_session", TargetID: "orch-timeout-due", EventIDs: []string{"we-timeout-due"}, NextAttemptAt: &nextAttempt, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreatePendingDelivery returned error: %v", err)
	}

	due, err := store.ListDuePendingDeliveries(ctx, base.Add(3*time.Minute), 10)
	if err != nil {
		t.Fatalf("ListDuePendingDeliveries returned error: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("due deliveries after subscription timeout = %#v, want none", due)
	}
	scopedDue, err := store.ListDuePendingDeliveriesForScope(ctx, base.Add(3*time.Minute), "ws-timeout-due", "orch-timeout-due", 10)
	if err != nil {
		t.Fatalf("ListDuePendingDeliveriesForScope returned error: %v", err)
	}
	if len(scopedDue) != 0 {
		t.Fatalf("scoped due deliveries after subscription timeout = %#v, want none", scopedDue)
	}
}

func TestStaleAttemptedDeliveriesAreRequeuedBeforeDueSelection(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-stale-attempt")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 14, 0, 0, 0, time.UTC)

	if err := store.CreateWorkspace(ctx, "ws-stale-attempt", "ws-stale-attempt", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-stale-attempt", WorkspaceID: "ws-stale-attempt", OwnerSessionID: "orch-stale-attempt", FilterJSON: `{}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "orch-stale-attempt", CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	nextAttempt := base
	delivery, err := store.CreatePendingDelivery(ctx, PendingDelivery{DeliveryID: "pd-stale-attempt", WorkspaceID: "ws-stale-attempt", SubscriptionID: "sub-stale-attempt", TargetType: "harness_session", TargetID: "orch-stale-attempt", EventIDs: []string{"we-stale-attempt"}, NextAttemptAt: &nextAttempt, CreatedAt: base, UpdatedAt: base})
	if err != nil {
		t.Fatalf("CreatePendingDelivery returned error: %v", err)
	}
	if _, err := store.ClaimDuePendingDeliveryAttempt(ctx, delivery.DeliveryID, base.Add(time.Minute)); err != nil {
		t.Fatalf("ClaimDuePendingDeliveryAttempt returned error: %v", err)
	}

	due, err := store.ListDuePendingDeliveries(ctx, base.Add(time.Minute).Add(pendingDeliveryAttemptLease), 10)
	if err != nil {
		t.Fatalf("ListDuePendingDeliveries returned error: %v", err)
	}
	if len(due) != 1 || due[0].DeliveryID != delivery.DeliveryID || due[0].Status != pendingDeliveryStatusPending || due[0].Attempts != 1 || due[0].LastError == "" {
		t.Fatalf("due deliveries after stale attempt = %#v, want original delivery requeued as pending", due)
	}
}

func TestScopedDueDeliverySelectionDoesNotMutateOtherWorkspaces(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-scoped-readonly")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 14, 30, 0, 0, time.UTC)

	for _, workspaceID := range []string{"ws-scope", "ws-other"} {
		if err := store.CreateWorkspace(ctx, workspaceID, workspaceID, t.TempDir(), "manual", "auto"); err != nil {
			t.Fatalf("CreateWorkspace %s returned error: %v", workspaceID, err)
		}
		if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-" + workspaceID, WorkspaceID: workspaceID, OwnerSessionID: "owner-" + workspaceID, FilterJSON: `{}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "owner-" + workspaceID, CreatedAt: base, UpdatedAt: base}); err != nil {
			t.Fatalf("CreateEventSubscription %s returned error: %v", workspaceID, err)
		}
	}
	nextAttempt := base
	other, err := store.CreatePendingDelivery(ctx, PendingDelivery{DeliveryID: "pd-other", WorkspaceID: "ws-other", SubscriptionID: "sub-ws-other", TargetType: "harness_session", TargetID: "owner-ws-other", EventIDs: []string{"we-other"}, NextAttemptAt: &nextAttempt, CreatedAt: base, UpdatedAt: base})
	if err != nil {
		t.Fatalf("CreatePendingDelivery other returned error: %v", err)
	}
	if _, err := store.ClaimDuePendingDeliveryAttempt(ctx, other.DeliveryID, base.Add(time.Minute)); err != nil {
		t.Fatalf("ClaimDuePendingDeliveryAttempt returned error: %v", err)
	}

	due, err := store.ListDuePendingDeliveriesForScope(ctx, base.Add(time.Minute).Add(pendingDeliveryAttemptLease), "ws-scope", "owner-ws-scope", 10)
	if err != nil {
		t.Fatalf("ListDuePendingDeliveriesForScope returned error: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("scoped due deliveries = %#v, want none", due)
	}
	storedOther, err := store.GetPendingDelivery(ctx, other.DeliveryID)
	if err != nil {
		t.Fatalf("GetPendingDelivery other returned error: %v", err)
	}
	if storedOther.Status != pendingDeliveryStatusAttempted {
		t.Fatalf("other delivery status = %q, want attempted because scoped list is side-effect-free", storedOther.Status)
	}
}

func TestCreatePendingDeliveryRequiresSubscriptionWorkspace(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-subscription-workspace")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 15, 0, 0, 0, time.UTC)

	for _, workspaceID := range []string{"ws-subscription", "ws-delivery"} {
		if err := store.CreateWorkspace(ctx, workspaceID, workspaceID, t.TempDir(), "manual", "auto"); err != nil {
			t.Fatalf("CreateWorkspace %s returned error: %v", workspaceID, err)
		}
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-workspace", WorkspaceID: "ws-subscription", OwnerSessionID: "orch-workspace", FilterJSON: `{}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "orch-workspace", CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	nextAttempt := base
	if _, err := store.CreatePendingDelivery(ctx, PendingDelivery{DeliveryID: "pd-wrong-workspace", WorkspaceID: "ws-delivery", SubscriptionID: "sub-workspace", TargetType: "harness_session", TargetID: "orch-workspace", EventIDs: []string{"we-workspace"}, NextAttemptAt: &nextAttempt, CreatedAt: base, UpdatedAt: base}); err == nil {
		t.Fatalf("CreatePendingDelivery returned nil error, want workspace mismatch rejection")
	}
}
