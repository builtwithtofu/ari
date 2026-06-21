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

	event := WorkspaceEvent{WorkspaceID: "ws-1", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fg-atomic", CausationID: "reply-1", PayloadJSON: `{"status":"completed","fanout_group_id":"fg-atomic","fanout_member_id":"fg-atomic-m1","source_session_id":"run-1","target_profile_id":"agent-2"}`, PayloadRefJSON: `{"kind":"final_response","id":"fr-1"}`}

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

func TestFanoutProjectionRebuildDeletesStaleRowsAndMergesReplay(t *testing.T) {
	store := newGlobalDBTestStore(t, "fanout-rebuild")
	ctx := context.Background()
	base := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	seedHarnessSessionConfigSession(t, store, ctx)
	for _, sessionID := range []string{"worker-1", "stale-worker"} {
		if err := store.CreateHarnessSession(ctx, HarnessSession{SessionID: sessionID, WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "fake", Status: "running", Usage: HarnessSessionUsageEphemeral, CWD: t.TempDir()}); err != nil {
			t.Fatalf("CreateHarnessSession %s returned error: %v", sessionID, err)
		}
	}
	if err := store.CreateFanoutGroup(ctx, FanoutGroup{FanoutGroupID: "fg-1", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1", RequestAgentMessageID: "request-1", Body: "compare"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}
	if err := upsertFanoutMemberWithQueries(ctx, store.sqlcQueries(), FanoutMember{FanoutMemberID: "fm-stale", FanoutGroupID: "fg-1", WorkspaceID: "ws-1", WorkerSessionID: "stale-worker", Status: "running", CreatedAt: base.Format(time.RFC3339Nano), UpdatedAt: base.Format(time.RFC3339Nano)}); err != nil {
		t.Fatalf("upsert stale fanout member returned error: %v", err)
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-started", WorkspaceID: "ws-1", EventType: WorkspaceEventWorkerStarted, SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fg-1", CausationID: "request-1", PayloadJSON: `{"fanout_member_id":"fm-1","fanout_group_id":"fg-1","source_session_id":"run-1","target_profile_id":"agent-2"}`, CreatedAt: base.Add(time.Second)}); err != nil {
		t.Fatalf("AppendWorkspaceEvent started returned error: %v", err)
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-completed", WorkspaceID: "ws-1", EventType: WorkspaceEventWorkerCompleted, SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fg-1", CausationID: "reply-1", PayloadJSON: `{"fanout_member_id":"fm-1","fanout_group_id":"fg-1","source_session_id":"run-1"}`, PayloadRefJSON: `{"kind":"final_response","id":"fr-1"}`, CreatedAt: base.Add(2 * time.Second)}); err != nil {
		t.Fatalf("AppendWorkspaceEvent completed returned error: %v", err)
	}

	if err := (FanoutProjection{}).Rebuild(ctx, store, "ws-1"); err != nil {
		t.Fatalf("FanoutProjection.Rebuild returned error: %v", err)
	}
	members, err := store.ListFanoutMembersByWorkspace(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListFanoutMembersByWorkspace returned error: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("fanout members = %#v, want stale row removed", members)
	}
	got := members[0]
	if got.FanoutMemberID != "fm-1" || got.WorkerSessionID != "worker-1" || got.TargetProfileID != "agent-2" || got.RequestAgentMessageID != "request-1" || got.ReplyAgentMessageID != "reply-1" || got.FinalResponseID != "fr-1" || got.Status != "completed" {
		t.Fatalf("rebuilt fanout member = %#v, want merged started and completed fields", got)
	}
}

func TestFanoutProjectionEnforcesUniqueWorkerSession(t *testing.T) {
	store := newGlobalDBTestStore(t, "fanout-unique-worker")
	ctx := context.Background()
	base := time.Date(2026, 6, 18, 12, 30, 0, 0, time.UTC)
	seedHarnessSessionConfigSession(t, store, ctx)
	if err := store.CreateHarnessSession(ctx, HarnessSession{SessionID: "worker-1", WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "fake", Status: "running", Usage: HarnessSessionUsageEphemeral, CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateHarnessSession returned error: %v", err)
	}
	if err := store.CreateFanoutGroup(ctx, FanoutGroup{FanoutGroupID: "fg-1", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1", RequestAgentMessageID: "request-1", Body: "compare"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}
	if err := upsertFanoutMemberWithQueries(ctx, store.sqlcQueries(), FanoutMember{FanoutMemberID: "fm-1", FanoutGroupID: "fg-1", WorkspaceID: "ws-1", WorkerSessionID: "worker-1", Status: "running", CreatedAt: base.Format(time.RFC3339Nano), UpdatedAt: base.Format(time.RFC3339Nano)}); err != nil {
		t.Fatalf("upsert first fanout member returned error: %v", err)
	}
	if err := upsertFanoutMemberWithQueries(ctx, store.sqlcQueries(), FanoutMember{FanoutMemberID: "fm-2", FanoutGroupID: "fg-1", WorkspaceID: "ws-1", WorkerSessionID: "worker-1", Status: "running", CreatedAt: base.Format(time.RFC3339Nano), UpdatedAt: base.Format(time.RFC3339Nano)}); err == nil {
		t.Fatal("upsert duplicate worker_session_id returned nil error, want unique constraint")
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
