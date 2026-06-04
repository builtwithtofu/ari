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
			unreadBeforeFinish, err := store.ListEventSubscriptionEvents(ctx, "sub-delivery-attempt", 10)
			if err != nil {
				t.Fatalf("ListEventSubscriptionEvents before finish returned error: %v", err)
			}
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
			unreadAfterFinish, err := store.ListEventSubscriptionEvents(ctx, "sub-delivery-attempt", 10)
			if err != nil {
				t.Fatalf("ListEventSubscriptionEvents after finish returned error: %v", err)
			}
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
