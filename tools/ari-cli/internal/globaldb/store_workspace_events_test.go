package globaldb

import (
	"context"
	"encoding/json"
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

func mustMarshalJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return string(encoded)
}
