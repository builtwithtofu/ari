package globaldb

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestInboxProjectionMaterializesAttentionEvents(t *testing.T) {
	store := newGlobalDBTestStore(t, "inbox-attention-projection")
	ctx := context.Background()
	seedHarnessSessionConfigSession(t, store, ctx)
	base := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)

	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-needs-input", WorkspaceID: "ws-1", EventType: WorkspaceEventSessionNeedsInput, SubjectType: "harness_session", SubjectID: "run-1", ProducerType: "daemon", ProducerID: "test", PayloadJSON: `{"session_id":"run-1","harness":"codex","status":"needs_input"}`, AttentionRequired: true, CreatedAt: base}); err != nil {
		t.Fatalf("AppendWorkspaceEvent needs_input returned error: %v", err)
	}
	if _, err := store.CreateWorkspaceTimer(ctx, WorkspaceTimer{TimerID: "timer-attention", WorkspaceID: "ws-1", OwnerSessionID: "run-1", Purpose: "worker-timeout", FireAt: base.Add(time.Second), PayloadJSON: `{}`}); err != nil {
		t.Fatalf("CreateWorkspaceTimer returned error: %v", err)
	}
	if _, err := store.FireWorkspaceTimer(ctx, "timer-attention"); err != nil {
		t.Fatalf("FireWorkspaceTimer returned error: %v", err)
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-signal", WorkspaceID: "ws-1", EventType: WorkspaceEventSignalSent, SubjectType: "harness_session", SubjectID: "run-1", ProducerType: "session", ProducerID: "worker-1", PayloadJSON: `{"action":"continue"}`, AttentionRequired: true, CreatedAt: base.Add(2 * time.Second)}); err != nil {
		t.Fatalf("AppendWorkspaceEvent signal returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-attention", WorkspaceID: "ws-1", OwnerSessionID: "run-1", FilterJSON: `{"event_types":["worker.completed"]}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "run-1", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn"}`, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	delivery, err := store.CreatePendingDelivery(ctx, PendingDelivery{DeliveryID: "pd-attention", WorkspaceID: "ws-1", SubscriptionID: "sub-attention", TargetType: "harness_session", TargetID: "run-1", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn"}`, EventIDs: []string{"we-needs-input"}, CreatedAt: base, UpdatedAt: base})
	if err != nil {
		t.Fatalf("CreatePendingDelivery returned error: %v", err)
	}
	if _, err := store.FailPendingDelivery(ctx, delivery.DeliveryID, "adapter offline"); err != nil {
		t.Fatalf("FailPendingDelivery returned error: %v", err)
	}

	items, err := store.ListInboxItems(ctx, "ws-1", "run-1")
	if err != nil {
		t.Fatalf("ListInboxItems returned error: %v", err)
	}
	got := map[string]InboxItem{}
	for _, item := range items {
		got[item.Kind] = item
	}
	for _, kind := range []string{"session_needs_input", "signal_sent", "timer_fired", "delivery_failed"} {
		item, ok := got[kind]
		if !ok {
			t.Fatalf("inbox items = %#v, missing kind %q", items, kind)
		}
		if item.Status != inboxItemStatusUnread || !item.AttentionRequired {
			t.Fatalf("item %s = %#v, want unread attention item", kind, item)
		}
	}
	if !strings.Contains(got["delivery_failed"].Summary, "adapter offline") {
		t.Fatalf("delivery_failed summary = %q, want error detail", got["delivery_failed"].Summary)
	}
}
