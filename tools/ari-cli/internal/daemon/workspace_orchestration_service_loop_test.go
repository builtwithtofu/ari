package daemon

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

// Durable timers are daemon-owned: due timers fire from the daemon's
// orchestration service.
func TestWorkspaceOrchestrationServiceFiresDueTimers(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	timer, err := store.CreateWorkspaceTimer(ctx, globaldb.WorkspaceTimer{TimerID: "timer-worker-due", WorkspaceID: "ws-1", OwnerSessionID: "run-1", Purpose: "wake", FireAt: base})
	if err != nil {
		t.Fatalf("CreateWorkspaceTimer returned error: %v", err)
	}

	service := newWorkspaceOrchestrationService(store, &recordingWorkspaceDeliveryDispatcher{})
	if err := service.runDueOnce(ctx, base.Add(time.Minute)); err != nil {
		t.Fatalf("runDueOnce returned error: %v", err)
	}

	fired, err := store.GetWorkspaceTimer(ctx, timer.TimerID)
	if err != nil {
		t.Fatalf("GetWorkspaceTimer returned error: %v", err)
	}
	if fired.Status != "fired" || fired.FiredEventID == "" {
		t.Fatalf("timer after worker tick = %#v, want fired with event evidence", fired)
	}
	events, err := store.ListWorkspaceEventsAfterSequence(ctx, "ws-1", 0, 50)
	if err != nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned error: %v", err)
	}
	found := false
	for _, event := range events {
		if event.EventType == globaldb.WorkspaceEventTimerFired && event.SubjectID == timer.TimerID {
			found = true
		}
	}
	if !found {
		t.Fatalf("workspace events = %#v, want timer.fired for %q", events, timer.TimerID)
	}
}

func TestWorkspaceTimerFireCreatesPendingDeliveryForSubscription(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	base := time.Date(2026, 6, 11, 17, 0, 0, 0, time.UTC)
	if _, err := store.CreateEventSubscription(ctx, globaldb.EventSubscription{SubscriptionID: "sub-timer-delivery", WorkspaceID: "ws-1", OwnerSessionID: "run-1", FilterJSON: `{"event_types":["timer.fired"]}`, DeliveryTargetType: globaldb.WorkspaceEventSubjectHarnessSession, DeliveryTargetID: "run-1", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn"}`, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	timer, err := store.CreateWorkspaceTimer(ctx, globaldb.WorkspaceTimer{TimerID: "timer-delivery", WorkspaceID: "ws-1", OwnerSessionID: "run-1", Purpose: "wake", FireAt: base})
	if err != nil {
		t.Fatalf("CreateWorkspaceTimer returned error: %v", err)
	}

	service := newWorkspaceOrchestrationService(store, &recordingWorkspaceDeliveryDispatcher{})
	if err := service.runDueOnce(ctx, base.Add(time.Minute)); err != nil {
		t.Fatalf("runDueOnce returned error: %v", err)
	}
	fired, err := store.GetWorkspaceTimer(ctx, timer.TimerID)
	if err != nil {
		t.Fatalf("GetWorkspaceTimer returned error: %v", err)
	}
	delivery, err := store.GetPendingDelivery(ctx, "pd-sub-timer-delivery-"+fired.FiredEventID)
	if err != nil {
		t.Fatalf("GetPendingDelivery for fired timer event returned error: %v", err)
	}
	if delivery.Status != "pending" || len(delivery.EventIDs) != 1 || delivery.EventIDs[0] != fired.FiredEventID {
		t.Fatalf("timer pending delivery = %#v, want pending delivery for fired event", delivery)
	}
}

// A transient store failure must not kill the durable due-work service: state
// lives in the database, so the scheduler retries after its error backoff.
func TestWorkspaceOrchestrationServiceContinuesAfterStoreErrors(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	store, err := globaldb.NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore returned error: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close returned error: %v", err)
	}

	service := newWorkspaceOrchestrationService(store, &recordingWorkspaceDeliveryDispatcher{result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}})
	service.errorBackoff = 5 * time.Millisecond
	service.now = func() time.Time { return time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC) }
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := service.run(ctx); err != nil {
		t.Fatalf("workspace orchestration service returned error %v, want loop to outlive store errors", err)
	}
}
