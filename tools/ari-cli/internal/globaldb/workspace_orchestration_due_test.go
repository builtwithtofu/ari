package globaldb

import (
	"context"
	"testing"
	"time"
)

func TestNextWorkspaceOrchestrationDueAtChoosesEarliestDurableWork(t *testing.T) {
	store := newGlobalDBTestStore(t, "workspace-orchestration-due")
	ctx := context.Background()
	base := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	if err := store.CreateWorkspace(ctx, "ws-due", "ws-due", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-due", WorkspaceID: "ws-due", OwnerSessionID: "owner-due", FilterJSON: `{}`, DeliveryTargetType: WorkspaceEventSubjectHarnessSession, DeliveryTargetID: "owner-due", CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}

	if next, ok, err := store.NextWorkspaceOrchestrationDueAt(ctx, base); err != nil || ok || !next.IsZero() {
		t.Fatalf("empty next due = %s ok=%v err=%v, want none", next, ok, err)
	}

	timerAt := base.Add(10 * time.Minute)
	if _, err := store.CreateWorkspaceTimer(ctx, WorkspaceTimer{TimerID: "timer-due", WorkspaceID: "ws-due", FireAt: timerAt, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateWorkspaceTimer returned error: %v", err)
	}
	next, ok, err := store.NextWorkspaceOrchestrationDueAt(ctx, base)
	if err != nil || !ok || !next.Equal(timerAt) {
		t.Fatalf("timer next due = %s ok=%v err=%v, want %s", next, ok, err, timerAt)
	}

	deliveryAt := base.Add(5 * time.Minute)
	if _, err := store.CreatePendingDelivery(ctx, PendingDelivery{DeliveryID: "pd-due", WorkspaceID: "ws-due", SubscriptionID: "sub-due", TargetType: WorkspaceEventSubjectHarnessSession, TargetID: "owner-due", EventIDs: []string{"we-due"}, NextAttemptAt: &deliveryAt, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreatePendingDelivery returned error: %v", err)
	}
	next, ok, err = store.NextWorkspaceOrchestrationDueAt(ctx, base)
	if err != nil || !ok || !next.Equal(deliveryAt) {
		t.Fatalf("delivery next due = %s ok=%v err=%v, want %s", next, ok, err, deliveryAt)
	}
}
