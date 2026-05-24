package globaldb

import (
	"context"
	"testing"
	"time"
)

func TestDaemonEventsAppendListAttentionAndClear(t *testing.T) {
	store := newGlobalDBTestStore(t, "daemon-events")
	ctx := context.Background()
	base := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)

	first, err := store.AppendDaemonEvent(ctx, DaemonEvent{EventID: "evt-1", EventType: "session.message.sent", SubjectType: "agent_message", SubjectID: "am-1", PayloadJSON: `{"ok":true}`, AttentionRequired: true, CreatedAt: base})
	if err != nil {
		t.Fatalf("AppendDaemonEvent first returned error: %v", err)
	}
	second, err := store.AppendDaemonEvent(ctx, DaemonEvent{EventID: "evt-2", EventType: "session.lifecycle.completed", SubjectType: "session", SubjectID: "run-1", CreatedAt: base})
	if err != nil {
		t.Fatalf("AppendDaemonEvent second returned error: %v", err)
	}

	events, err := store.ListDaemonEventsAfter(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListDaemonEventsAfter returned error: %v", err)
	}
	if len(events) != 2 || events[0].EventID != first.EventID || events[1].EventID != second.EventID {
		t.Fatalf("events = %#v, want deterministic append order", events)
	}
	afterFirst, err := store.ListDaemonEventsAfter(ctx, first.EventID, 10)
	if err != nil {
		t.Fatalf("ListDaemonEventsAfter cursor returned error: %v", err)
	}
	if len(afterFirst) != 1 || afterFirst[0].EventID != second.EventID {
		t.Fatalf("after first = %#v, want second only", afterFirst)
	}
	if _, err := store.ListDaemonEventsAfter(ctx, "evt-missing", 10); err != ErrNotFound {
		t.Fatalf("ListDaemonEventsAfter missing cursor error = %v, want ErrNotFound", err)
	}

	attention, err := store.ListDaemonAttentionEvents(ctx)
	if err != nil {
		t.Fatalf("ListDaemonAttentionEvents returned error: %v", err)
	}
	if len(attention) != 1 || attention[0].EventID != first.EventID {
		t.Fatalf("attention = %#v, want first event", attention)
	}
	if err := store.ClearDaemonEventAttention(ctx, first.EventID); err != nil {
		t.Fatalf("ClearDaemonEventAttention returned error: %v", err)
	}
	attention, err = store.ListDaemonAttentionEvents(ctx)
	if err != nil {
		t.Fatalf("ListDaemonAttentionEvents after clear returned error: %v", err)
	}
	if len(attention) != 0 {
		t.Fatalf("attention after clear = %#v, want none", attention)
	}
}
