package globaldb

import (
	"context"
	"testing"
	"time"
)

func TestPendingDeliveryAttemptLifecycleControlsSubscriptionAck(t *testing.T) {
	for _, tc := range []struct {
		name     string
		suffix   string
		finish   func(context.Context, *Store, string) (PendingDelivery, error)
		wantRead bool
	}{
		{
			name:   "completed delivery advances subscription ack",
			suffix: "attempt-completed",
			finish: func(ctx context.Context, store *Store, deliveryID string) (PendingDelivery, error) {
				return store.CompletePendingDelivery(ctx, deliveryID)
			},
			wantRead: false,
		},
		{
			name:   "failed delivery leaves subscription unread",
			suffix: "attempt-failed",
			finish: func(ctx context.Context, store *Store, deliveryID string) (PendingDelivery, error) {
				return store.FailPendingDelivery(ctx, deliveryID, "adapter rejected visible turn")
			},
			wantRead: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newGlobalDBTestStore(t, "pending-delivery-attempt")
			ctx := context.Background()
			base := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
			_, event, delivery := seedDueWorkerDeliveryFixture(t, store, ctx, tc.suffix, base)

			attempted, err := store.ClaimDuePendingDeliveryAttempt(ctx, delivery.DeliveryID, base.Add(time.Minute))
			if err != nil {
				t.Fatalf("ClaimDuePendingDeliveryAttempt returned error: %v", err)
			}
			if attempted.Status != pendingDeliveryStatusAttempted || attempted.Attempts != 1 || attempted.NextAttemptAt != nil || attempted.TerminalAt != nil {
				t.Fatalf("attempted delivery = %#v, want attempted in-flight delivery with one attempt and no retry/terminal time", attempted)
			}
			due, err := store.ListDuePendingDeliveries(ctx, base.Add(2*time.Minute), 10)
			if err != nil {
				t.Fatalf("ListDuePendingDeliveries after attempt returned error: %v", err)
			}
			if len(due) != 0 {
				t.Fatalf("due pending deliveries after claim = %#v, want in-flight attempt hidden from retry queue", due)
			}
			unreadBeforeFinish := readSubscriptionEvents(t, store, delivery.SubscriptionID, 10)
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
			unreadAfterFinish := readSubscriptionEvents(t, store, delivery.SubscriptionID, 10)
			if gotRead := len(unreadAfterFinish) == 1; gotRead != tc.wantRead {
				t.Fatalf("subscription unread after finish = %#v, want unread=%t", unreadAfterFinish, tc.wantRead)
			}
			subscription, err := store.GetEventSubscription(ctx, delivery.SubscriptionID)
			if err != nil {
				t.Fatalf("GetEventSubscription returned error: %v", err)
			}
			if tc.wantRead && (subscription.AckSequence != 0 || subscription.CursorSequence != 0) {
				t.Fatalf("subscription cursor/ack = %d/%d, want unread source event", subscription.CursorSequence, subscription.AckSequence)
			}
			if !tc.wantRead && (subscription.AckSequence < event.Sequence || subscription.CursorSequence < event.Sequence) {
				t.Fatalf("subscription cursor/ack = %d/%d, want delivered through source event sequence %d", subscription.CursorSequence, subscription.AckSequence, event.Sequence)
			}
		})
	}
}

func TestCompletedDeliveryAcksOnlyContiguousDeliveredEvents(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-contiguous-ack")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)

	createWorkspaceFixture(t, store, ctx, "ws-contiguous-ack")
	createEventSubscriptionFixture(t, store, ctx, "ws-contiguous-ack", "sub-contiguous-ack", EventSubscriptionFilter{EventTypes: []string{WorkspaceEventWorkerCompleted}}, base, withFixtureOwnerSession("orch-contiguous-ack"), withFixtureDeliveryTarget(WorkspaceEventSubjectHarnessSession, "orch-contiguous-ack"))
	first := appendWorkerEventFixture(t, store, ctx, "ws-contiguous-ack", "we-contiguous-first", WorkspaceEventWorkerCompleted, "worker-first", base.Add(time.Second))
	second := appendWorkerEventFixture(t, store, ctx, "ws-contiguous-ack", "we-contiguous-second", WorkspaceEventWorkerCompleted, "worker-second", base.Add(2*time.Second))

	secondDelivery := requireDeliveryForEventFixture(t, store, ctx, "sub-contiguous-ack", second.EventID)
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

	createWorkspaceFixture(t, store, ctx, "ws-custom-id-ack")
	createEventSubscriptionFixture(t, store, ctx, "ws-custom-id-ack", "sub-custom-id-ack", EventSubscriptionFilter{EventTypes: []string{WorkspaceEventWorkerCompleted}}, base, withFixtureOwnerSession("orch-custom-id-ack"), withFixtureDeliveryTarget(WorkspaceEventSubjectHarnessSession, "orch-custom-id-ack"), withFixtureDeliveryPolicy(fixtureDeliveryPolicyJSON(t, 0)))
	event := appendWorkerEventFixture(t, store, ctx, "ws-custom-id-ack", "we-custom-id-ack", WorkspaceEventWorkerCompleted, "worker-custom-id-ack", base.Add(time.Second))
	nextAttempt := base.Add(time.Minute)
	customDelivery, err := store.CreatePendingDelivery(ctx, PendingDelivery{DeliveryID: "pd-custom-manual", WorkspaceID: "ws-custom-id-ack", SubscriptionID: "sub-custom-id-ack", TargetType: WorkspaceEventSubjectHarnessSession, TargetID: "orch-custom-id-ack", EventIDs: []string{event.EventID}, NextAttemptAt: &nextAttempt, CreatedAt: base, UpdatedAt: base})
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

	createWorkspaceFixture(t, store, ctx, "ws-prior-custom-ack")
	createEventSubscriptionFixture(t, store, ctx, "ws-prior-custom-ack", "sub-prior-custom-ack", EventSubscriptionFilter{EventTypes: []string{WorkspaceEventWorkerCompleted}}, base, withFixtureOwnerSession("orch-prior-custom-ack"), withFixtureDeliveryTarget(WorkspaceEventSubjectHarnessSession, "orch-prior-custom-ack"))
	first := appendWorkerEventFixture(t, store, ctx, "ws-prior-custom-ack", "we-prior-custom-first", WorkspaceEventWorkerCompleted, "worker-first", base.Add(time.Second))
	second := appendWorkerEventFixture(t, store, ctx, "ws-prior-custom-ack", "we-prior-custom-second", WorkspaceEventWorkerCompleted, "worker-second", base.Add(2*time.Second))
	nextAttempt := base.Add(time.Minute)
	secondCustom, err := store.CreatePendingDelivery(ctx, PendingDelivery{DeliveryID: "pd-prior-custom-second", WorkspaceID: "ws-prior-custom-ack", SubscriptionID: "sub-prior-custom-ack", TargetType: WorkspaceEventSubjectHarnessSession, TargetID: "orch-prior-custom-ack", EventIDs: []string{second.EventID}, NextAttemptAt: &nextAttempt, CreatedAt: base, UpdatedAt: base})
	if err != nil {
		t.Fatalf("CreatePendingDelivery custom second returned error: %v", err)
	}
	if _, err := store.CompletePendingDelivery(ctx, secondCustom.DeliveryID); err != nil {
		t.Fatalf("CompletePendingDelivery custom second returned error: %v", err)
	}
	firstDelivery := requireDeliveryForEventFixture(t, store, ctx, "sub-prior-custom-ack", first.EventID)
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

	createWorkspaceFixture(t, store, ctx, "ws-skip-lifecycle-ack")
	createEventSubscriptionFixture(t, store, ctx, "ws-skip-lifecycle-ack", "sub-skip-lifecycle-ack", EventSubscriptionFilter{}, base, withFixtureOwnerSession("orch-skip-lifecycle-ack"), withFixtureDeliveryTarget(WorkspaceEventSubjectHarnessSession, "orch-skip-lifecycle-ack"))
	event := appendWorkerEventFixture(t, store, ctx, "ws-skip-lifecycle-ack", "we-skip-lifecycle", WorkspaceEventWorkerCompleted, "worker", base.Add(time.Second))
	delivery := requireDeliveryForEventFixture(t, store, ctx, "sub-skip-lifecycle-ack", event.EventID)
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

func TestFailExpiredPendingDeliveriesMarksOverdueDeliveries(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-deadline")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 13, 0, 0, 0, time.UTC)

	createWorkspaceFixture(t, store, ctx, "ws-deadline")
	createEventSubscriptionFixture(t, store, ctx, "ws-deadline", "sub-deadline", EventSubscriptionFilter{}, base, withFixtureOwnerSession("orch-deadline"), withFixtureDeliveryTarget(WorkspaceEventSubjectHarnessSession, "orch-deadline"))
	deadline := base.Add(time.Minute)
	nextAttempt := base.Add(30 * time.Second)
	delivery := createPendingDeliveryFixture(t, store, ctx, PendingDelivery{DeliveryID: "pd-deadline", WorkspaceID: "ws-deadline", SubscriptionID: "sub-deadline", TargetID: "orch-deadline", EventIDs: []string{"we-deadline"}, NextAttemptAt: &nextAttempt, DeadlineAt: &deadline, CreatedAt: base, UpdatedAt: base})

	failed, err := store.FailExpiredPendingDeliveries(ctx, base.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("FailExpiredPendingDeliveries returned error: %v", err)
	}
	if len(failed) != 1 || failed[0].DeliveryID != delivery.DeliveryID {
		t.Fatalf("failed deliveries = %#v, want expired delivery", failed)
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

	createWorkspaceFixture(t, store, ctx, "ws-timeout-due")
	timeoutAt := base.Add(time.Minute)
	createEventSubscriptionFixture(t, store, ctx, "ws-timeout-due", "sub-timeout-due", EventSubscriptionFilter{}, base, withFixtureOwnerSession("orch-timeout-due"), withFixtureDeliveryTarget(WorkspaceEventSubjectHarnessSession, "orch-timeout-due"), withFixtureTimeout(timeoutAt))
	nextAttempt := base.Add(2 * time.Minute)
	createPendingDeliveryFixture(t, store, ctx, PendingDelivery{DeliveryID: "pd-timeout-due", WorkspaceID: "ws-timeout-due", SubscriptionID: "sub-timeout-due", TargetID: "orch-timeout-due", EventIDs: []string{"we-timeout-due"}, NextAttemptAt: &nextAttempt, CreatedAt: base, UpdatedAt: base})

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

func TestFailTimedOutSubscriptionDeliveriesMarksPendingDeliveries(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-timeout-fail")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 13, 35, 0, 0, time.UTC)
	createWorkspaceFixture(t, store, ctx, "ws-timeout-fail")
	timeoutAt := base.Add(time.Minute)
	createEventSubscriptionFixture(t, store, ctx, "ws-timeout-fail", "sub-timeout-fail", EventSubscriptionFilter{}, base, withFixtureOwnerSession("orch-timeout-fail"), withFixtureDeliveryTarget(WorkspaceEventSubjectHarnessSession, "orch-timeout-fail"), withFixtureTimeout(timeoutAt))
	nextAttempt := base.Add(30 * time.Second)
	delivery := createPendingDeliveryFixture(t, store, ctx, PendingDelivery{DeliveryID: "pd-timeout-fail", WorkspaceID: "ws-timeout-fail", SubscriptionID: "sub-timeout-fail", TargetID: "orch-timeout-fail", EventIDs: []string{"we-timeout-fail"}, NextAttemptAt: &nextAttempt, CreatedAt: base, UpdatedAt: base})

	failed, err := store.FailTimedOutSubscriptionDeliveries(ctx, timeoutAt.Add(time.Second))
	if err != nil {
		t.Fatalf("FailTimedOutSubscriptionDeliveries returned error: %v", err)
	}
	if len(failed) != 1 || failed[0].DeliveryID != delivery.DeliveryID || failed[0].Status != pendingDeliveryStatusFailed || failed[0].LastError == "" {
		t.Fatalf("failed deliveries = %#v, want timed-out pending delivery failed", failed)
	}
	requireDeliveryEvent(t, lastDeliveryEvent(t, store, ctx, delivery), WorkspaceEventDeliveryFailed, failed[0], true)
}

func TestFailExpiredPendingDeliveriesMarksAttemptedOverdueDeliveries(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-attempted-deadline")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 13, 30, 0, 0, time.UTC)

	createWorkspaceFixture(t, store, ctx, "ws-attempted-deadline")
	createEventSubscriptionFixture(t, store, ctx, "ws-attempted-deadline", "sub-attempted-deadline", EventSubscriptionFilter{}, base, withFixtureOwnerSession("orch-attempted-deadline"), withFixtureDeliveryTarget(WorkspaceEventSubjectHarnessSession, "orch-attempted-deadline"))
	deadline := base.Add(time.Minute)
	nextAttempt := base.Add(30 * time.Second)
	delivery := createPendingDeliveryFixture(t, store, ctx, PendingDelivery{DeliveryID: "pd-attempted-deadline", WorkspaceID: "ws-attempted-deadline", SubscriptionID: "sub-attempted-deadline", TargetID: "orch-attempted-deadline", EventIDs: []string{"we-attempted-deadline"}, NextAttemptAt: &nextAttempt, DeadlineAt: &deadline, CreatedAt: base, UpdatedAt: base})
	if _, err := store.ClaimDuePendingDeliveryAttempt(ctx, delivery.DeliveryID, nextAttempt); err != nil {
		t.Fatalf("ClaimDuePendingDeliveryAttempt returned error: %v", err)
	}

	failed, err := store.FailExpiredPendingDeliveries(ctx, base.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("FailExpiredPendingDeliveries returned error: %v", err)
	}
	if len(failed) != 1 || failed[0].DeliveryID != delivery.DeliveryID || failed[0].Status != pendingDeliveryStatusFailed {
		t.Fatalf("failed deliveries = %#v, want attempted expired delivery failed", failed)
	}
}

func TestRequeueStalePendingDeliveryAttemptsMakesInterruptedAttemptDue(t *testing.T) {
	store := newGlobalDBTestStore(t, "pending-delivery-stale-attempt")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 14, 0, 0, 0, time.UTC)

	createWorkspaceFixture(t, store, ctx, "ws-stale-attempt")
	createEventSubscriptionFixture(t, store, ctx, "ws-stale-attempt", "sub-stale-attempt", EventSubscriptionFilter{}, base, withFixtureOwnerSession("orch-stale-attempt"), withFixtureDeliveryTarget(WorkspaceEventSubjectHarnessSession, "orch-stale-attempt"))
	nextAttempt := base
	delivery := createPendingDeliveryFixture(t, store, ctx, PendingDelivery{DeliveryID: "pd-stale-attempt", WorkspaceID: "ws-stale-attempt", SubscriptionID: "sub-stale-attempt", TargetID: "orch-stale-attempt", EventIDs: []string{"we-stale-attempt"}, NextAttemptAt: &nextAttempt, CreatedAt: base, UpdatedAt: base})
	if _, err := store.ClaimDuePendingDeliveryAttempt(ctx, delivery.DeliveryID, base.Add(time.Minute)); err != nil {
		t.Fatalf("ClaimDuePendingDeliveryAttempt returned error: %v", err)
	}

	requeued, err := store.RequeueStalePendingDeliveryAttempts(ctx, base.Add(time.Minute).Add(pendingDeliveryAttemptLease))
	if err != nil {
		t.Fatalf("RequeueStalePendingDeliveryAttempts returned error: %v", err)
	}
	if requeued != 1 {
		t.Fatalf("requeued = %d, want 1", requeued)
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
		createWorkspaceFixture(t, store, ctx, workspaceID)
		createEventSubscriptionFixture(t, store, ctx, workspaceID, "sub-"+workspaceID, EventSubscriptionFilter{}, base, withFixtureOwnerSession("owner-"+workspaceID), withFixtureDeliveryTarget(WorkspaceEventSubjectHarnessSession, "owner-"+workspaceID))
	}
	nextAttempt := base
	other := createPendingDeliveryFixture(t, store, ctx, PendingDelivery{DeliveryID: "pd-other", WorkspaceID: "ws-other", SubscriptionID: "sub-ws-other", TargetID: "owner-ws-other", EventIDs: []string{"we-other"}, NextAttemptAt: &nextAttempt, CreatedAt: base, UpdatedAt: base})
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
		createWorkspaceFixture(t, store, ctx, workspaceID)
	}
	createEventSubscriptionFixture(t, store, ctx, "ws-subscription", "sub-workspace", EventSubscriptionFilter{}, base, withFixtureOwnerSession("orch-workspace"), withFixtureDeliveryTarget(WorkspaceEventSubjectHarnessSession, "orch-workspace"))
	nextAttempt := base
	if _, err := store.CreatePendingDelivery(ctx, PendingDelivery{DeliveryID: "pd-wrong-workspace", WorkspaceID: "ws-delivery", SubscriptionID: "sub-workspace", TargetType: WorkspaceEventSubjectHarnessSession, TargetID: "orch-workspace", EventIDs: []string{"we-workspace"}, NextAttemptAt: &nextAttempt, CreatedAt: base, UpdatedAt: base}); err == nil {
		t.Fatalf("CreatePendingDelivery returned nil error, want workspace mismatch rejection")
	}
}
