package globaldb

import (
	"context"
	"encoding/json"
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

func readSubscriptionEvents(t *testing.T, store *Store, subscriptionID string, limit int) []WorkspaceEvent {
	t.Helper()
	result, err := store.ReadEventSubscription(context.Background(), EventSubscriptionReadRequest{SubscriptionID: subscriptionID, Limit: limit})
	if err != nil {
		t.Fatalf("ReadEventSubscription returned error: %v", err)
	}
	return result.Events
}

func mustMarshalJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return string(encoded)
}

type workspaceEventSubscriptionFixtureOptions struct {
	ownerSessionID     string
	deliveryTargetID   string
	deliveryTargetType string
	deliveryPolicyJSON string
	timeoutAt          *time.Time
}

type workspaceEventSubscriptionFixtureOption func(*workspaceEventSubscriptionFixtureOptions)

func withFixtureOwnerSession(ownerSessionID string) workspaceEventSubscriptionFixtureOption {
	return func(opts *workspaceEventSubscriptionFixtureOptions) {
		opts.ownerSessionID = ownerSessionID
	}
}

func withFixtureDeliveryTarget(targetType, targetID string) workspaceEventSubscriptionFixtureOption {
	return func(opts *workspaceEventSubscriptionFixtureOptions) {
		opts.deliveryTargetType = targetType
		opts.deliveryTargetID = targetID
	}
}

func withFixtureDeliveryPolicy(policyJSON string) workspaceEventSubscriptionFixtureOption {
	return func(opts *workspaceEventSubscriptionFixtureOptions) {
		opts.deliveryPolicyJSON = policyJSON
	}
}

func withFixtureTimeout(timeoutAt time.Time) workspaceEventSubscriptionFixtureOption {
	return func(opts *workspaceEventSubscriptionFixtureOptions) {
		opts.timeoutAt = &timeoutAt
	}
}

func createWorkspaceFixture(t *testing.T, store *Store, ctx context.Context, workspaceID string) {
	t.Helper()
	if err := store.CreateWorkspace(ctx, workspaceID, workspaceID, t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
}

func createEventSubscriptionFixture(t *testing.T, store *Store, ctx context.Context, workspaceID, subscriptionID string, filter EventSubscriptionFilter, base time.Time, options ...workspaceEventSubscriptionFixtureOption) EventSubscription {
	t.Helper()
	opts := workspaceEventSubscriptionFixtureOptions{ownerSessionID: "owner-" + subscriptionID, deliveryTargetType: WorkspaceEventSubjectHarnessSession, deliveryTargetID: "owner-" + subscriptionID, deliveryPolicyJSON: fixtureDeliveryPolicyJSON(t, 3)}
	for _, option := range options {
		option(&opts)
	}
	subscription, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: subscriptionID, WorkspaceID: workspaceID, OwnerSessionID: opts.ownerSessionID, FilterJSON: mustMarshalJSON(t, filter), DeliveryTargetType: opts.deliveryTargetType, DeliveryTargetID: opts.deliveryTargetID, DeliveryPolicyJSON: opts.deliveryPolicyJSON, TimeoutAt: opts.timeoutAt, CreatedAt: base, UpdatedAt: base})
	if err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	return subscription
}

func appendWorkerEventFixture(t *testing.T, store *Store, ctx context.Context, workspaceID, eventID, eventType, workerSessionID string, createdAt time.Time) WorkspaceEvent {
	t.Helper()
	event := NewFanoutWorkerWorkspaceEvent(FanoutWorkerWorkspaceEventParams{WorkspaceID: workspaceID, EventType: eventType, WorkerSessionID: workerSessionID, ProducerID: workerSessionID, FanoutGroupID: "fg-" + workerSessionID, FanoutMemberID: "fm-" + workerSessionID, SourceSessionID: "owner-" + workerSessionID, TargetProfileID: "agent-" + workerSessionID, RequestAgentMessageID: "request-" + workerSessionID, FinalResponseID: "fr-" + workerSessionID})
	event.EventID = eventID
	event.CreatedAt = createdAt
	created, err := store.AppendWorkspaceEvent(ctx, event)
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent returned error: %v", err)
	}
	return created
}

func seedDueWorkerDeliveryFixture(t *testing.T, store *Store, ctx context.Context, suffix string, base time.Time) (EventSubscription, WorkspaceEvent, PendingDelivery) {
	t.Helper()
	workspaceID := "ws-delivery-" + suffix
	createWorkspaceFixture(t, store, ctx, workspaceID)
	subscription := createEventSubscriptionFixture(t, store, ctx, workspaceID, "sub-delivery-"+suffix, EventSubscriptionFilter{EventTypes: []string{WorkspaceEventWorkerCompleted}}, base, withFixtureOwnerSession("orch-"+suffix), withFixtureDeliveryTarget(WorkspaceEventSubjectHarnessSession, "orch-"+suffix))
	event := appendWorkerEventFixture(t, store, ctx, workspaceID, "we-delivery-"+suffix, WorkspaceEventWorkerCompleted, "worker-"+suffix, base.Add(time.Second))
	delivery := requireDueDeliveryFixture(t, store, ctx, workspaceID, event.EventID, base.Add(time.Minute))
	return subscription, event, delivery
}

func requireDueDeliveryFixture(t *testing.T, store *Store, ctx context.Context, workspaceID, eventID string, now time.Time) PendingDelivery {
	t.Helper()
	due, err := store.ListDuePendingDeliveries(ctx, now, 10)
	if err != nil {
		t.Fatalf("ListDuePendingDeliveries returned error: %v", err)
	}
	if len(due) != 1 || due[0].WorkspaceID != workspaceID || len(due[0].EventIDs) != 1 || due[0].EventIDs[0] != eventID {
		t.Fatalf("due deliveries = %#v, want one delivery for %s", due, eventID)
	}
	return due[0]
}

func requireDeliveryForEventFixture(t *testing.T, store *Store, ctx context.Context, subscriptionID, eventID string) PendingDelivery {
	t.Helper()
	delivery, err := store.GetPendingDelivery(ctx, pendingDeliveryIDForSubscriptionEvent(subscriptionID, eventID))
	if err != nil {
		t.Fatalf("GetPendingDelivery for subscription event returned error: %v", err)
	}
	return delivery
}

func createPendingDeliveryFixture(t *testing.T, store *Store, ctx context.Context, delivery PendingDelivery) PendingDelivery {
	t.Helper()
	if delivery.TargetType == "" {
		delivery.TargetType = WorkspaceEventSubjectHarnessSession
	}
	if delivery.TargetID == "" {
		delivery.TargetID = "owner-" + delivery.SubscriptionID
	}
	if len(delivery.EventIDs) == 0 {
		delivery.EventIDs = []string{"we-" + delivery.DeliveryID}
	}
	created, err := store.CreatePendingDelivery(ctx, delivery)
	if err != nil {
		t.Fatalf("CreatePendingDelivery returned error: %v", err)
	}
	return created
}

func fixtureDeliveryPolicyJSON(t *testing.T, maxAttempts int64) string {
	t.Helper()
	policy := map[string]any{"channel": "visible_prompt_turn"}
	if maxAttempts > 0 {
		policy["max_attempts"] = maxAttempts
	}
	return mustMarshalJSON(t, policy)
}
