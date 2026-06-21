package globaldb

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestSubscriptionDeadlineTimerCompletesOnlyTargetStream(t *testing.T) {
	store := newGlobalDBTestStore(t, "subscription-deadline")
	ctx := context.Background()
	base := time.Date(2026, 6, 21, 14, 0, 0, 0, time.UTC)
	if err := store.CreateWorkspace(ctx, "ws-deadline-stream", "deadline stream", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	timeoutAt := base.Add(time.Minute)
	condition := mustMarshalJSON(t, EventSubscriptionCompletionCondition{Mode: "all", SubjectIDs: []string{"worker-a"}, TerminalEventTypes: []string{WorkspaceEventWorkerCompleted}})
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-target", WorkspaceID: "ws-deadline-stream", OwnerSessionID: "owner-target", FilterJSON: `{"event_types":["worker.completed"],"subject_ids":["worker-a"]}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "owner-target", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn"}`, CompletionConditionJSON: condition, TimeoutAt: &timeoutAt, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription target returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-other", WorkspaceID: "ws-deadline-stream", OwnerSessionID: "owner-other", FilterJSON: `{"event_types":["timer.fired"]}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "owner-other", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn"}`, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription other returned error: %v", err)
	}

	fired, err := store.FireDueWorkspaceTimers(ctx, timeoutAt.Add(time.Second), 10)
	if err != nil {
		t.Fatalf("FireDueWorkspaceTimers returned error: %v", err)
	}
	if len(fired) != 1 || fired[0].TargetSubscriptionID != "sub-target" {
		t.Fatalf("fired timers = %#v, want one target subscription deadline", fired)
	}

	result, err := store.ReadEventSubscription(ctx, EventSubscriptionReadRequest{SubscriptionID: "sub-target", Limit: 10})
	if err != nil {
		t.Fatalf("ReadEventSubscription target returned error: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].EventType != WorkspaceEventTimerFired || WorkspaceTimerTargetSubscriptionIDFromEvent(result.Events[0]) != "sub-target" {
		t.Fatalf("target events = %#v, want targeted timer.fired", result.Events)
	}
	if !result.Completion.TimedOut || result.Completion.Status != EventSubscriptionWaitStatusTimeout || result.Completion.MatchedCount != 0 {
		t.Fatalf("completion = %#v, want timeout without counting deadline as worker progress", result.Completion)
	}

	other, err := store.ReadEventSubscription(ctx, EventSubscriptionReadRequest{SubscriptionID: "sub-other", Limit: 10})
	if err != nil {
		t.Fatalf("ReadEventSubscription other returned error: %v", err)
	}
	if len(other.Events) != 0 {
		t.Fatalf("other subscription events = %#v, want targeted timer hidden from non-target stream", other.Events)
	}

	if _, err := store.GetPendingDelivery(ctx, pendingDeliveryIDForSubscriptionEvent("sub-target", result.Events[0].EventID)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deadline pending delivery error = %v, want no delivery for internal subscription deadline", err)
	}
}

func TestUserTargetedTimerWithTimeoutPurposeIsNotSubscriptionDeadline(t *testing.T) {
	store := newGlobalDBTestStore(t, "user-targeted-timer")
	ctx := context.Background()
	base := time.Date(2026, 6, 21, 15, 0, 0, 0, time.UTC)
	if err := store.CreateWorkspace(ctx, "ws-user-timer", "user timer", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-user-timer", WorkspaceID: "ws-user-timer", OwnerSessionID: "owner-user", FilterJSON: `{"event_types":["timer.fired"]}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "owner-user", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn"}`, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	if _, err := store.CreateWorkspaceTimer(ctx, WorkspaceTimer{TimerID: "timer-user-timeout-purpose", WorkspaceID: "ws-user-timer", OwnerSessionID: "owner-user", TargetSubscriptionID: "sub-user-timer", Purpose: workspaceTimerPurposeSubscriptionTimeout, FireAt: base.Add(time.Second), PayloadJSON: `["keep"]`, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateWorkspaceTimer returned error: %v", err)
	}
	if _, err := store.FireWorkspaceTimer(ctx, "timer-user-timeout-purpose"); err != nil {
		t.Fatalf("FireWorkspaceTimer returned error: %v", err)
	}

	result, err := store.ReadEventSubscription(ctx, EventSubscriptionReadRequest{SubscriptionID: "sub-user-timer", Limit: 10})
	if err != nil {
		t.Fatalf("ReadEventSubscription returned error: %v", err)
	}
	if len(result.Events) != 1 || isSubscriptionDeadlineTimerEvent(result.Events[0]) || result.Completion.TimedOut {
		t.Fatalf("subscription result = %#v, want normal targeted timer event", result)
	}
	payload := map[string]any{}
	if err := json.Unmarshal([]byte(result.Events[0].PayloadJSON), &payload); err != nil {
		t.Fatalf("timer fired payload json = %q: %v", result.Events[0].PayloadJSON, err)
	}
	rawPayload, ok := payload["payload"].([]any)
	if !ok || len(rawPayload) != 1 || rawPayload[0] != "keep" {
		t.Fatalf("timer fired payload = %#v, want original non-object payload preserved", payload)
	}

	due, err := store.ListDuePendingDeliveries(ctx, time.Now().UTC().Add(time.Minute), 10)
	if err != nil {
		t.Fatalf("ListDuePendingDeliveries returned error: %v", err)
	}
	if len(due) != 1 || due[0].SubscriptionID != "sub-user-timer" || len(due[0].EventIDs) != 1 || due[0].EventIDs[0] != result.Events[0].EventID {
		t.Fatalf("due deliveries = %#v, want normal delivery for user targeted timer", due)
	}
}
