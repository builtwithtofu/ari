package globaldb

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestWorkspaceEventSubscriptionContract(t *testing.T) {
	store := newGlobalDBTestStore(t, "workspace-events")
	ctx := context.Background()
	base := time.Date(2026, 6, 5, 20, 0, 0, 0, time.UTC)

	for _, workspaceID := range []string{"ws-1", "ws-2"} {
		if err := store.CreateWorkspace(ctx, workspaceID, workspaceID, t.TempDir(), "manual", "auto"); err != nil {
			t.Fatalf("CreateWorkspace %s returned error: %v", workspaceID, err)
		}
	}

	started, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-started", WorkspaceID: "ws-1", EventType: "worker.started", SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "orch-1", CorrelationID: "fanout-1", CausationID: "request-1", PayloadJSON: `{"status":"running"}`, CreatedAt: base})
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent started returned error: %v", err)
	}
	completed, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-completed", WorkspaceID: "ws-1", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fanout-1", CausationID: started.EventID, PayloadJSON: `{"status":"completed"}`, PayloadRefJSON: `{"kind":"final_response","id":"fr-1"}`, AttentionRequired: true, CreatedAt: base.Add(time.Second)})
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent completed returned error: %v", err)
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-other-workspace", WorkspaceID: "ws-2", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-2", CorrelationID: "fanout-1", PayloadJSON: `{}`, CreatedAt: base.Add(2 * time.Second)}); err != nil {
		t.Fatalf("AppendWorkspaceEvent other workspace returned error: %v", err)
	}

	if started.Sequence != 1 || completed.Sequence != 2 {
		t.Fatalf("sequences = %d, %d, want 1, 2", started.Sequence, completed.Sequence)
	}

	workspaceEvents, err := store.ListWorkspaceEventsAfterSequence(ctx, "ws-1", 0, 10)
	if err != nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned error: %v", err)
	}
	if len(workspaceEvents) != 2 || workspaceEvents[0].EventID != started.EventID || workspaceEvents[1].EventID != completed.EventID {
		t.Fatalf("workspace events = %#v, want ws-1 started/completed only", workspaceEvents)
	}
	if workspaceEvents[1].PayloadRefJSON != `{"kind":"final_response","id":"fr-1"}` || !workspaceEvents[1].AttentionRequired {
		t.Fatalf("completed event = %#v, want payload ref and attention flag", workspaceEvents[1])
	}

	filter := mustMarshalJSON(t, EventSubscriptionFilter{EventTypes: []string{"worker.completed"}, CorrelationIDs: []string{"fanout-1"}})
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-1", WorkspaceID: "ws-1", OwnerSessionID: "orch-1", FilterJSON: filter, CompletionConditionJSON: `{"mode":"each"}`}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}

	matching, err := store.ListEventSubscriptionEvents(ctx, "sub-1", 10)
	if err != nil {
		t.Fatalf("ListEventSubscriptionEvents returned error: %v", err)
	}
	if len(matching) != 1 || matching[0].EventID != completed.EventID {
		t.Fatalf("subscription events = %#v, want only completed event", matching)
	}

	if err := store.AckEventSubscription(ctx, "sub-1", completed.Sequence); err != nil {
		t.Fatalf("AckEventSubscription returned error: %v", err)
	}
	matching, err = store.ListEventSubscriptionEvents(ctx, "sub-1", 10)
	if err != nil {
		t.Fatalf("ListEventSubscriptionEvents after ack returned error: %v", err)
	}
	if len(matching) != 0 {
		t.Fatalf("subscription events after ack = %#v, want none", matching)
	}

	subscription, err := store.GetEventSubscription(ctx, "sub-1")
	if err != nil {
		t.Fatalf("GetEventSubscription returned error: %v", err)
	}
	if subscription.CursorSequence != completed.Sequence || subscription.AckSequence != completed.Sequence {
		t.Fatalf("subscription cursor/ack = %d/%d, want %d", subscription.CursorSequence, subscription.AckSequence, completed.Sequence)
	}
}

func TestCreateEventSubscriptionBackfillsExistingEventDeliveries(t *testing.T) {
	store := newGlobalDBTestStore(t, "workspace-events-subscription-backfill")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 16, 0, 0, 0, time.UTC)

	if err := store.CreateWorkspace(ctx, "ws-backfill", "ws-backfill", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	matching, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-backfill-match", WorkspaceID: "ws-backfill", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CreatedAt: base.Add(time.Second)})
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent matching returned error: %v", err)
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-backfill-delivery", WorkspaceID: "ws-backfill", EventType: "delivery.completed", SubjectType: "pending_delivery", SubjectID: "pd-1", ProducerType: "daemon", ProducerID: "workspace_delivery_worker", CreatedAt: base.Add(2 * time.Second)}); err != nil {
		t.Fatalf("AppendWorkspaceEvent delivery returned error: %v", err)
	}

	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-backfill", WorkspaceID: "ws-backfill", OwnerSessionID: "orch-backfill", FilterJSON: `{"event_types":["worker.completed"]}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "orch-backfill", CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}

	due, err := store.ListDuePendingDeliveries(ctx, base.Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("ListDuePendingDeliveries returned error: %v", err)
	}
	if len(due) != 1 || due[0].SubscriptionID != "sub-backfill" || len(due[0].EventIDs) != 1 || due[0].EventIDs[0] != matching.EventID {
		t.Fatalf("due deliveries = %#v, want backfilled delivery for matching existing event only", due)
	}
}

func TestWorkspaceEventValidation(t *testing.T) {
	store := newGlobalDBTestStore(t, "workspace-events-validation")
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-1", "ws-1", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}

	for _, tc := range []struct {
		name  string
		event WorkspaceEvent
	}{
		{name: "missing workspace", event: WorkspaceEvent{EventType: "worker.completed", SubjectType: "session", SubjectID: "run-1"}},
		{name: "invalid payload", event: WorkspaceEvent{WorkspaceID: "ws-1", EventType: "worker.completed", SubjectType: "session", SubjectID: "run-1", PayloadJSON: `{invalid`}},
		{name: "invalid payload ref", event: WorkspaceEvent{WorkspaceID: "ws-1", EventType: "worker.completed", SubjectType: "session", SubjectID: "run-1", PayloadRefJSON: `{invalid`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.AppendWorkspaceEvent(ctx, tc.event); err == nil {
				t.Fatalf("AppendWorkspaceEvent returned nil error, want validation failure")
			}
		})
	}
}

func TestWorkspaceEventTimestampParseFailuresSurface(t *testing.T) {
	store := newGlobalDBTestStore(t, "workspace-events-invalid-timestamps")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 16, 30, 0, 0, time.UTC)

	for _, workspaceID := range []string{"ws-invalid-timestamps", "ws-invalid-subscription-timestamps"} {
		if err := store.CreateWorkspace(ctx, workspaceID, workspaceID, t.TempDir(), "manual", "auto"); err != nil {
			t.Fatalf("CreateWorkspace %s returned error: %v", workspaceID, err)
		}
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-invalid-timestamp", WorkspaceID: "ws-invalid-timestamps", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-invalid-timestamp", CreatedAt: base}); err != nil {
		t.Fatalf("AppendWorkspaceEvent returned error: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE workspace_events SET created_at = 'not-a-time' WHERE event_id = 'we-invalid-timestamp'`); err != nil {
		t.Fatalf("corrupt workspace event timestamp: %v", err)
	}
	if _, err := store.ListWorkspaceEventsAfterSequence(ctx, "ws-invalid-timestamps", 0, 10); err == nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned nil error, want timestamp parse failure")
	}

	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-invalid-timestamp", WorkspaceID: "ws-invalid-subscription-timestamps", OwnerSessionID: "orch-invalid-timestamp", FilterJSON: `{}`, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE event_subscriptions SET updated_at = 'not-a-time' WHERE subscription_id = 'sub-invalid-timestamp'`); err != nil {
		t.Fatalf("corrupt event subscription timestamp: %v", err)
	}
	if _, err := store.GetEventSubscription(ctx, "sub-invalid-timestamp"); err == nil {
		t.Fatalf("GetEventSubscription returned nil error, want timestamp parse failure")
	}
}

func TestWorkspaceEventSchemaEnforcesWorkspaceScopedReferences(t *testing.T) {
	store := newGlobalDBTestStore(t, "workspace-events-scoped-fks")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 17, 0, 0, 0, time.UTC)
	if _, err := store.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	for _, workspaceID := range []string{"ws-fk-a", "ws-fk-b"} {
		if err := store.CreateWorkspace(ctx, workspaceID, workspaceID, t.TempDir(), "manual", "auto"); err != nil {
			t.Fatalf("CreateWorkspace %s returned error: %v", workspaceID, err)
		}
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-fk", WorkspaceID: "ws-fk-a", OwnerSessionID: "orch-fk", FilterJSON: `{}`, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-fk", WorkspaceID: "ws-fk-a", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-fk", CreatedAt: base}); err != nil {
		t.Fatalf("AppendWorkspaceEvent returned error: %v", err)
	}

	if _, err := store.db.ExecContext(ctx, `INSERT INTO pending_deliveries (delivery_id, workspace_id, subscription_id, target_type, target_id, event_ids_json, created_at, updated_at) VALUES ('pd-cross-fk', 'ws-fk-b', 'sub-fk', 'harness_session', 'orch-fk', '[]', ?, ?)`, base.Format(time.RFC3339Nano), base.Format(time.RFC3339Nano)); err == nil {
		t.Fatalf("cross-workspace pending delivery insert succeeded, want FK failure")
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO workspace_timers (timer_id, workspace_id, subscription_id, fire_at, created_at, updated_at) VALUES ('timer-cross-fk', 'ws-fk-b', 'sub-fk', ?, ?, ?)`, base.Format(time.RFC3339Nano), base.Format(time.RFC3339Nano), base.Format(time.RFC3339Nano)); err == nil {
		t.Fatalf("cross-workspace timer insert succeeded, want FK failure")
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO inbox_items (inbox_item_id, workspace_id, source_session_id, workspace_event_id, event_type, kind, created_at, updated_at) VALUES ('inbox-cross-fk', 'ws-fk-b', 'orch-fk', 'we-fk', 'worker.completed', 'workspace_event', ?, ?)`, base.Format(time.RFC3339Nano), base.Format(time.RFC3339Nano)); err == nil {
		t.Fatalf("cross-workspace inbox item insert succeeded, want FK failure")
	}
}

func TestWorkspaceEventSequenceAllocationIsConcurrentSafe(t *testing.T) {
	store := newGlobalDBTestStore(t, "workspace-events-concurrent-sequences")
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-concurrent", "ws-concurrent", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}

	const count = 25
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: fmt.Sprintf("we-concurrent-%02d", i), WorkspaceID: "ws-concurrent", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: fmt.Sprintf("worker-%02d", i)})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("AppendWorkspaceEvent concurrent returned error: %v", err)
		}
	}

	events, err := store.ListWorkspaceEventsAfterSequence(ctx, "ws-concurrent", 0, count)
	if err != nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned error: %v", err)
	}
	if len(events) != count {
		t.Fatalf("events len = %d, want %d", len(events), count)
	}
	for i, event := range events {
		if event.Sequence != int64(i+1) {
			t.Fatalf("event[%d].Sequence = %d, want %d", i, event.Sequence, i+1)
		}
	}
}

func TestCancelEventSubscriptionFailsPendingDeliveries(t *testing.T) {
	store := newGlobalDBTestStore(t, "workspace-events-cancel-deliveries")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 15, 0, 0, 0, time.UTC)

	if err := store.CreateWorkspace(ctx, "ws-cancel-deliveries", "ws-cancel-deliveries", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-cancel-deliveries", WorkspaceID: "ws-cancel-deliveries", OwnerSessionID: "orch-cancel", FilterJSON: `{}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "orch-cancel", CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-cancel-deliveries", WorkspaceID: "ws-cancel-deliveries", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-cancel", CreatedAt: base.Add(time.Second)}); err != nil {
		t.Fatalf("AppendWorkspaceEvent returned error: %v", err)
	}
	delivery, err := store.GetPendingDelivery(ctx, pendingDeliveryIDForSubscriptionEvent("sub-cancel-deliveries", "we-cancel-deliveries"))
	if err != nil {
		t.Fatalf("GetPendingDelivery returned error: %v", err)
	}

	if _, err := store.CancelEventSubscription(ctx, "sub-cancel-deliveries"); err != nil {
		t.Fatalf("CancelEventSubscription returned error: %v", err)
	}
	due, err := store.ListDuePendingDeliveries(ctx, base.Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("ListDuePendingDeliveries returned error: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("due deliveries after subscription cancel = %#v, want none", due)
	}
	stored, err := store.GetPendingDelivery(ctx, delivery.DeliveryID)
	if err != nil {
		t.Fatalf("GetPendingDelivery after cancel returned error: %v", err)
	}
	if stored.Status != pendingDeliveryStatusFailed || stored.TerminalAt == nil {
		t.Fatalf("pending delivery after subscription cancel = %#v, want terminal failure", stored)
	}
}

func TestTimedOutSubscriptionsDoNotCreateDeliveries(t *testing.T) {
	store := newGlobalDBTestStore(t, "workspace-events-timeout-deliveries")
	ctx := context.Background()
	base := time.Date(2026, 6, 11, 16, 0, 0, 0, time.UTC)

	if err := store.CreateWorkspace(ctx, "ws-timeout-deliveries", "ws-timeout-deliveries", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	timeoutAt := base.Add(time.Minute)
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-timeout-deliveries", WorkspaceID: "ws-timeout-deliveries", OwnerSessionID: "orch-timeout", FilterJSON: `{}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "orch-timeout", TimeoutAt: &timeoutAt, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-timeout-deliveries", WorkspaceID: "ws-timeout-deliveries", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-timeout", CreatedAt: base.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("AppendWorkspaceEvent returned error: %v", err)
	}
	due, err := store.ListDuePendingDeliveries(ctx, base.Add(3*time.Minute), 10)
	if err != nil {
		t.Fatalf("ListDuePendingDeliveries returned error: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("due deliveries after subscription timeout = %#v, want none", due)
	}
	matches, err := store.ListEventSubscriptionEvents(ctx, "sub-timeout-deliveries", 10)
	if err != nil {
		t.Fatalf("ListEventSubscriptionEvents returned error: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("subscription events after timeout = %#v, want none", matches)
	}
}

func mustMarshalJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return string(encoded)
}
