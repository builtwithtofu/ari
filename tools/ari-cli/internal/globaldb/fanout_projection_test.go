package globaldb

import (
	"context"
	"testing"
	"time"
)

func TestEventCoordinatorProjectsFanoutMemberInboxAndDeliveryAtomically(t *testing.T) {
	store := newGlobalDBTestStore(t, "fanout-worker-event-atomic")
	ctx := context.Background()
	seedHarnessSessionConfigSession(t, store, ctx)
	if err := store.CreateHarnessSession(ctx, HarnessSession{SessionID: "worker-1", WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "fake", Status: "running", Usage: HarnessSessionUsageEphemeral, SourceSessionID: "run-1", SourceAgentID: "agent-1", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateHarnessSession worker returned error: %v", err)
	}
	if err := store.CreateFanoutGroup(ctx, FanoutGroup{FanoutGroupID: "fg-atomic", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1", Body: "fan out"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, EventSubscription{SubscriptionID: "sub-atomic", WorkspaceID: "ws-1", OwnerSessionID: "run-1", FilterJSON: `{"event_types":["worker.completed"]}`, DeliveryTargetType: "harness_session", DeliveryTargetID: "run-1", DeliveryPolicyJSON: `{"channel":"harness_session"}`}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}

	event := WorkspaceEvent{WorkspaceID: "ws-1", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fg-atomic", CausationID: "reply-1", PayloadJSON: `{"status":"completed","fanout_group_id":"fg-atomic","fanout_member_id":"fg-atomic-m1","target_profile_id":"agent-2"}`, PayloadRefJSON: `{"kind":"final_response","id":"fr-1"}`}

	stored, err := store.AppendWorkspaceEvent(ctx, event)
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent returned error: %v", err)
	}
	if stored.EventID == "" || stored.Sequence == 0 {
		t.Fatalf("stored event = %#v, want assigned id and sequence", stored)
	}
	members, err := store.ListFanoutMembers(ctx, "fg-atomic")
	if err != nil || len(members) != 1 || members[0].Status != "completed" || members[0].FinalResponseID != "fr-1" {
		t.Fatalf("members = %#v err=%v, want completed member with final response", members, err)
	}
	if members[0].UpdatedAt != stored.CreatedAt.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("member updated_at = %q, want emitted event time %q", members[0].UpdatedAt, stored.CreatedAt.UTC().Format(time.RFC3339Nano))
	}
	item, err := store.GetInboxItem(ctx, "inbox-fg-atomic-m1")
	if err != nil {
		t.Fatalf("GetInboxItem returned error: %v", err)
	}
	if item.WorkspaceEventID != stored.EventID || item.EventType != "worker.completed" {
		t.Fatalf("inbox item = %#v, want linkage to event %q filled by the store", item, stored.EventID)
	}
	due, err := store.ListDuePendingDeliveries(ctx, time.Now().UTC().Add(time.Minute), 10)
	if err != nil || len(due) != 1 || due[0].SubscriptionID != "sub-atomic" {
		t.Fatalf("due deliveries = %#v err=%v, want one delivery from same transaction", due, err)
	}
}

func TestEventCoordinatorRejectsInvalidFanoutProjectionWithoutWriting(t *testing.T) {
	store := newGlobalDBTestStore(t, "fanout-worker-event-invalid")
	ctx := context.Background()
	seedHarnessSessionConfigSession(t, store, ctx)

	event := WorkspaceEvent{WorkspaceID: "ws-1", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fg-x", PayloadJSON: `{"status":"completed","fanout_group_id":"fg-x","fanout_member_id":"fg-x-m1","target_profile_id":"agent-2"}`}

	if _, err := store.AppendWorkspaceEvent(ctx, event); err == nil {
		t.Fatal("AppendWorkspaceEvent with invalid fanout projection returned nil error")
	}
	events, err := store.ListWorkspaceEventsAfterSequence(ctx, "ws-1", 0, 10)
	if err != nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %#v, want nothing written when projection input is invalid", events)
	}
}
