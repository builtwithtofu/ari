package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

func TestWorkspaceDeliveryWorkerAttemptsDueDeliveries(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		result                    func(time.Time) WorkspaceDeliveryAttemptResult
		wantOutcomeStatus         WorkspaceDeliveryWorkerOutcomeStatus
		wantDeliveryStatus        string
		wantUnread                bool
		wantAck                   int64
		wantLastError             string
		wantNextAttempt           func(time.Time) *time.Time
		wantDeliveryEventTypes    []string
		wantDeliveryEventStatuses []string
	}{
		{
			name: "completed attempt completes delivery and advances ack",
			result: func(time.Time) WorkspaceDeliveryAttemptResult {
				return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}
			},
			wantOutcomeStatus:         WorkspaceDeliveryWorkerOutcomeCompleted,
			wantDeliveryStatus:        "completed",
			wantUnread:                false,
			wantAck:                   1,
			wantDeliveryEventTypes:    []string{"delivery.attempted", "delivery.completed"},
			wantDeliveryEventStatuses: []string{"attempted", "completed"},
		},
		{
			name: "terminal failure leaves subscription unread",
			result: func(time.Time) WorkspaceDeliveryAttemptResult {
				return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptFailed, LastError: "adapter rejected visible turn"}
			},
			wantOutcomeStatus:         WorkspaceDeliveryWorkerOutcomeFailed,
			wantDeliveryStatus:        "failed",
			wantUnread:                true,
			wantAck:                   0,
			wantLastError:             "adapter rejected visible turn",
			wantDeliveryEventTypes:    []string{"delivery.attempted", "delivery.failed"},
			wantDeliveryEventStatuses: []string{"attempted", "failed"},
		},
		{
			name: "retryable failure requeues without advancing ack",
			result: func(now time.Time) WorkspaceDeliveryAttemptResult {
				nextAttempt := now.Add(5 * time.Second)
				return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: "adapter offline", NextAttemptAt: &nextAttempt}
			},
			wantOutcomeStatus:         WorkspaceDeliveryWorkerOutcomeRetry,
			wantDeliveryStatus:        "pending",
			wantUnread:                true,
			wantAck:                   0,
			wantLastError:             "adapter offline",
			wantDeliveryEventTypes:    []string{"delivery.attempted", "delivery.retry_scheduled"},
			wantDeliveryEventStatuses: []string{"attempted", "retry"},
			wantNextAttempt: func(now time.Time) *time.Time {
				nextAttempt := now.Add(5 * time.Second)
				return &nextAttempt
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newCommandMethodTestStore(t)
			ctx := context.Background()
			base := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
			now := base.Add(time.Minute)
			delivery, event := seedDueWorkspaceDelivery(t, store, strings.ReplaceAll(tc.name, " ", "-"), base)
			dispatcher := &recordingWorkspaceDeliveryDispatcher{result: tc.result(now)}

			outcomes, err := runWorkspaceDeliveryWorkerOnce(ctx, store, dispatcher, now, 10)
			if err != nil {
				t.Fatalf("runWorkspaceDeliveryWorkerOnce returned error: %v", err)
			}
			if len(outcomes) != 1 || outcomes[0].DeliveryID != delivery.DeliveryID || outcomes[0].Status != tc.wantOutcomeStatus {
				t.Fatalf("worker outcomes = %#v, want one %s outcome for %s", outcomes, tc.wantOutcomeStatus, delivery.DeliveryID)
			}

			attempts := dispatcher.Attempts()
			if len(attempts) != 1 {
				t.Fatalf("dispatcher attempts = %#v, want one attempt", attempts)
			}
			if attempts[0].Delivery.DeliveryID != delivery.DeliveryID || attempts[0].Delivery.Status != "attempted" || attempts[0].Delivery.Attempts != 1 || attempts[0].Delivery.NextAttemptAt != nil {
				t.Fatalf("dispatcher attempt = %#v, want claimed attempted delivery", attempts[0])
			}

			finished, err := store.GetPendingDelivery(ctx, delivery.DeliveryID)
			if err != nil {
				t.Fatalf("GetPendingDelivery returned error: %v", err)
			}
			if finished.Status != tc.wantDeliveryStatus || finished.Attempts != 1 || finished.LastError != tc.wantLastError {
				t.Fatalf("finished delivery = %#v, want status=%s attempts=1 last_error=%q", finished, tc.wantDeliveryStatus, tc.wantLastError)
			}
			wantNextAttempt := timePtrValue(tc.wantNextAttempt, now)
			if !sameOptionalTime(finished.NextAttemptAt, wantNextAttempt) {
				t.Fatalf("finished next_attempt_at = %v, want %v", finished.NextAttemptAt, wantNextAttempt)
			}

			unread := readWorkspaceDeliverySubscriptionEvents(t, ctx, store, delivery.SubscriptionID, 10)
			if gotUnread := len(unread) == 1 && unread[0].EventID == event.EventID; gotUnread != tc.wantUnread {
				t.Fatalf("subscription unread = %#v, want unread=%t", unread, tc.wantUnread)
			}
			subscription, err := store.GetEventSubscription(ctx, delivery.SubscriptionID)
			if err != nil {
				t.Fatalf("GetEventSubscription returned error: %v", err)
			}
			if subscription.CursorSequence != tc.wantAck || subscription.AckSequence != tc.wantAck {
				t.Fatalf("subscription cursor/ack = %d/%d, want %d", subscription.CursorSequence, subscription.AckSequence, tc.wantAck)
			}

			deliveryEvents := listDeliveryWorkspaceEvents(t, store, delivery.WorkspaceID, event.Sequence)
			if gotTypes := workspaceEventTypes(deliveryEvents); !sameStringSlice(gotTypes, tc.wantDeliveryEventTypes) {
				t.Fatalf("delivery event types = %#v, want %#v", gotTypes, tc.wantDeliveryEventTypes)
			}
			for i, deliveryEvent := range deliveryEvents {
				if deliveryEvent.SubjectType != "pending_delivery" || deliveryEvent.SubjectID != delivery.DeliveryID || deliveryEvent.ProducerType != "daemon" {
					t.Fatalf("delivery event = %#v, want pending delivery subject and daemon producer", deliveryEvent)
				}
				payload := globaldb.WorkspaceEventStringPayload(deliveryEvent.PayloadJSON)
				if payload["delivery_id"] != delivery.DeliveryID || payload["subscription_id"] != delivery.SubscriptionID || payload["status"] != tc.wantDeliveryEventStatuses[i] {
					t.Fatalf("delivery event payload = %#v, want delivery/subscription with status %q", payload, tc.wantDeliveryEventStatuses[i])
				}
			}
		})
	}
}

func TestWorkspaceDeliveryWorkerLoopAttemptsDueDeliveriesOnTicks(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	now := base.Add(time.Minute)
	delivery, _ := seedDueWorkspaceDelivery(t, store, "loop", base)
	dispatcher := &recordingWorkspaceDeliveryDispatcher{result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}}
	ticks := make(chan time.Time, 1)
	ticks <- now
	close(ticks)

	if err := runWorkspaceDeliveryWorkerLoop(ctx, store, dispatcher, ticks, 10); err != nil {
		t.Fatalf("runWorkspaceDeliveryWorkerLoop returned error: %v", err)
	}
	if attempts := dispatcher.Attempts(); len(attempts) != 1 || attempts[0].Delivery.DeliveryID != delivery.DeliveryID {
		t.Fatalf("dispatcher attempts = %#v, want one loop-driven attempt for %s", attempts, delivery.DeliveryID)
	}
	finished, err := store.GetPendingDelivery(ctx, delivery.DeliveryID)
	if err != nil {
		t.Fatalf("GetPendingDelivery returned error: %v", err)
	}
	if finished.Status != "completed" {
		t.Fatalf("finished delivery status = %q, want completed", finished.Status)
	}
}

func TestWorkspaceDeliveryWorkerFailsRetryAfterMaxAttempts(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 8, 11, 0, 0, 0, time.UTC)
	now := base.Add(time.Minute)
	delivery, event := seedDueWorkspaceDelivery(t, store, "max-attempts", base)
	retryAt := base.Add(10 * time.Second)
	for range 2 {
		if _, err := store.RecordPendingDeliveryAttempt(ctx, delivery.DeliveryID, &retryAt, "previous failure"); err != nil {
			t.Fatalf("RecordPendingDeliveryAttempt returned error: %v", err)
		}
	}
	dispatcher := &recordingWorkspaceDeliveryDispatcher{result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: "adapter still offline"}}

	outcomes, err := runWorkspaceDeliveryWorkerOnce(ctx, store, dispatcher, now, 10)
	if err != nil {
		t.Fatalf("runWorkspaceDeliveryWorkerOnce returned error: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != WorkspaceDeliveryWorkerOutcomeFailed || outcomes[0].LastError != "adapter still offline" {
		t.Fatalf("worker outcomes = %#v, want terminal failed after max attempts", outcomes)
	}
	finished, err := store.GetPendingDelivery(ctx, delivery.DeliveryID)
	if err != nil {
		t.Fatalf("GetPendingDelivery returned error: %v", err)
	}
	if finished.Status != "failed" || finished.Attempts != 3 || finished.LastError != "adapter still offline" {
		t.Fatalf("finished delivery = %#v, want failed after third attempt", finished)
	}
	unread := readWorkspaceDeliverySubscriptionEvents(t, ctx, store, delivery.SubscriptionID, 10)
	if len(unread) != 1 || unread[0].EventID != event.EventID {
		t.Fatalf("subscription unread = %#v, want original event still unread", unread)
	}
	if gotTypes := workspaceEventTypes(listDeliveryWorkspaceEvents(t, store, delivery.WorkspaceID, event.Sequence)); !sameStringSlice(gotTypes, []string{"delivery.retry_scheduled", "delivery.retry_scheduled", "delivery.attempted", "delivery.failed"}) {
		t.Fatalf("delivery event types = %#v, want retry records then attempted and failed", gotTypes)
	}
}

func readWorkspaceDeliverySubscriptionEvents(t *testing.T, ctx context.Context, store *globaldb.Store, subscriptionID string, limit int) []globaldb.WorkspaceEvent {
	t.Helper()
	result, err := store.ReadEventSubscription(ctx, globaldb.EventSubscriptionReadRequest{SubscriptionID: subscriptionID, Limit: limit})
	if err != nil {
		t.Fatalf("ReadEventSubscription returned error: %v", err)
	}
	return result.Events
}

func listDeliveryWorkspaceEvents(t *testing.T, store *globaldb.Store, workspaceID string, afterSequence int64) []globaldb.WorkspaceEvent {
	t.Helper()
	events, err := store.ListWorkspaceEventsAfterSequence(context.Background(), workspaceID, afterSequence, 20)
	if err != nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned error: %v", err)
	}
	deliveryEvents := make([]globaldb.WorkspaceEvent, 0, len(events))
	for _, event := range events {
		if strings.HasPrefix(event.EventType, "delivery.") {
			deliveryEvents = append(deliveryEvents, event)
		}
	}
	return deliveryEvents
}

func workspaceEventTypes(events []globaldb.WorkspaceEvent) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.EventType)
	}
	return types
}

func sameStringSlice(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func seedDueWorkspaceDelivery(t *testing.T, store *globaldb.Store, suffix string, base time.Time) (globaldb.PendingDelivery, globaldb.WorkspaceEvent) {
	t.Helper()
	ctx := context.Background()
	workspaceID := "ws-worker-" + suffix
	subscriptionID := "sub-worker-" + suffix
	if err := store.CreateWorkspace(ctx, workspaceID, workspaceID, t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, globaldb.EventSubscription{SubscriptionID: subscriptionID, WorkspaceID: workspaceID, OwnerSessionID: "owner-" + suffix, FilterJSON: `{"event_types":["worker.completed"]}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "owner-" + suffix, DeliveryPolicyJSON: `{"channel":"visible_prompt_turn","max_attempts":3}`, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	event, err := store.AppendWorkspaceEvent(ctx, globaldb.WorkspaceEvent{EventID: "we-worker-" + suffix, WorkspaceID: workspaceID, EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-" + suffix, ProducerType: "session", ProducerID: "worker-" + suffix, PayloadRefJSON: `{"kind":"final_response","id":"fr-worker"}`, CreatedAt: base.Add(time.Second)})
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
	return due[0], event
}

type recordingWorkspaceDeliveryDispatcher struct {
	mu       sync.Mutex
	result   WorkspaceDeliveryAttemptResult
	attempts []WorkspaceDeliveryAttempt
}

func (d *recordingWorkspaceDeliveryDispatcher) AttemptWorkspaceDelivery(ctx context.Context, attempt WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error) {
	if ctx == nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("context is required")
	}
	d.mu.Lock()
	d.attempts = append(d.attempts, attempt)
	d.mu.Unlock()
	return d.result, nil
}

func (d *recordingWorkspaceDeliveryDispatcher) Attempts() []WorkspaceDeliveryAttempt {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]WorkspaceDeliveryAttempt(nil), d.attempts...)
}

func timePtrValue(fn func(time.Time) *time.Time, now time.Time) *time.Time {
	if fn == nil {
		return nil
	}
	return fn(now)
}

func sameOptionalTime(got, want *time.Time) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return got.Equal(*want)
}
