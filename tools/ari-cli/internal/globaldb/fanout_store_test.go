package globaldb

import (
	"context"
	"testing"
)

func TestFanoutMembersWorkspaceIndexSupportsWorkspaceProjection(t *testing.T) {
	store := newGlobalDBTestStore(t, "fanout-members-workspace-index")
	ctx := context.Background()
	var indexName string
	if err := store.db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'fanout_members_workspace_idx'`).Scan(&indexName); err != nil {
		t.Fatalf("fanout_members workspace index lookup returned error: %v", err)
	}
	if indexName != "fanout_members_workspace_idx" {
		t.Fatalf("workspace index name = %q, want fanout_members_workspace_idx", indexName)
	}
}

func TestFanoutProjectionMaterializesLifecycleFromWorkspaceEvents(t *testing.T) {
	store := newGlobalDBTestStore(t, "fanout-member-projection-lifecycle")
	ctx := context.Background()
	seedHarnessSessionConfigSession(t, store, ctx)
	if err := store.CreateHarnessSession(ctx, HarnessSession{SessionID: "worker-1", WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "fake", Status: "running", Usage: HarnessSessionUsageEphemeral, SourceSessionID: "run-1", SourceAgentID: "agent-1", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateHarnessSession worker returned error: %v", err)
	}
	if err := store.CreateFanoutGroup(ctx, FanoutGroup{FanoutGroupID: "fg-1", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1", RequestAgentMessageID: "request-1", Body: "compare options"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-fanout-started", WorkspaceID: "ws-1", EventType: WorkspaceEventWorkerStarted, SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fg-1", CausationID: "request-1", PayloadJSON: `{"fanout_member_id":"fm-1","fanout_group_id":"fg-1","target_profile_id":"agent-2"}`}); err != nil {
		t.Fatalf("AppendWorkspaceEvent started returned error: %v", err)
	}
	if err := store.UpsertFinalResponse(ctx, FinalResponse{FinalResponseID: "fr-worker-1", HarnessSessionID: "worker-1", WorkspaceID: "ws-1", TaskID: "task-1", ContextPacketID: "ctx-1", ProfileID: "agent-2", Status: "completed", Text: "done"}); err != nil {
		t.Fatalf("UpsertFinalResponse returned error: %v", err)
	}
	if _, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-fanout-completed", WorkspaceID: "ws-1", EventType: WorkspaceEventWorkerCompleted, SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fg-1", CausationID: "reply-1", PayloadJSON: `{"fanout_member_id":"fm-1","fanout_group_id":"fg-1"}`, PayloadRefJSON: `{"kind":"final_response","id":"fr-worker-1"}`}); err != nil {
		t.Fatalf("AppendWorkspaceEvent completed returned error: %v", err)
	}

	members, err := store.ListFanoutMembers(ctx, "fg-1")
	if err != nil {
		t.Fatalf("ListFanoutMembers returned error: %v", err)
	}
	if len(members) != 1 || members[0].Status != "completed" || members[0].WorkerSessionID != "worker-1" || members[0].FinalResponseID != "fr-worker-1" || members[0].ReplyAgentMessageID != "reply-1" {
		t.Fatalf("members = %#v, want completed worker linked to final response", members)
	}
	if members[0].RequestAgentMessageID != "request-1" || members[0].TargetProfileID != "agent-2" {
		t.Fatalf("members = %#v, want terminal projection to preserve request linkage and target profile", members)
	}
}

func TestInboxProjectionPreservesReadStateAndCounts(t *testing.T) {
	store := newGlobalDBTestStore(t, "inbox-items-projection")
	ctx := context.Background()
	seedHarnessSessionConfigSession(t, store, ctx)
	if err := store.CreateFanoutGroup(ctx, FanoutGroup{FanoutGroupID: "fg-1", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1", RequestAgentMessageID: "request-1", Body: "compare options"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}

	firstEvent, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-inbox-1", WorkspaceID: "ws-1", EventType: WorkspaceEventWorkerCompleted, SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fg-1", CausationID: "reply-1", PayloadJSON: `{"fanout_member_id":"fm-1","fanout_group_id":"fg-1","target_profile_id":"agent-2"}`, PayloadRefJSON: `{"kind":"final_response","id":"fr-1"}`})
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent first returned error: %v", err)
	}

	counts, err := store.CountInboxItems(ctx, "ws-1", "run-1")
	if err != nil {
		t.Fatalf("CountInboxItems first returned error: %v", err)
	}
	if counts.TotalCount != 1 || counts.UnreadCount != 1 || counts.ReadCount != 0 {
		t.Fatalf("counts = %#v, want one unread item", counts)
	}
	marked, err := store.MarkInboxItemsRead(ctx, "ws-1", "run-1", []string{"inbox-fm-1"})
	if err != nil {
		t.Fatalf("MarkInboxItemsRead returned error: %v", err)
	}
	if marked != 1 {
		t.Fatalf("marked = %d, want one row", marked)
	}

	secondEvent, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-inbox-2", WorkspaceID: "ws-1", EventType: WorkspaceEventWorkerCompleted, SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fg-1", CausationID: "reply-2", PayloadJSON: `{"fanout_member_id":"fm-1","fanout_group_id":"fg-1","target_profile_id":"agent-2"}`, PayloadRefJSON: `{"kind":"final_response","id":"fr-1"}`})
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent second returned error: %v", err)
	}

	item, err := store.GetInboxItem(ctx, "inbox-fm-1")
	if err != nil {
		t.Fatalf("GetInboxItem returned error: %v", err)
	}
	if item.Status != "read" || item.WorkspaceEventID != secondEvent.EventID || item.EventType != secondEvent.EventType || item.Kind != "worker_completed" {
		t.Fatalf("item = %#v, want read state preserved with refreshed event evidence", item)
	}
	if item.WorkspaceEventID == firstEvent.EventID {
		t.Fatalf("item = %#v, want refreshed event evidence", item)
	}
	counts, err = store.CountInboxItems(ctx, "ws-1", "run-1")
	if err != nil {
		t.Fatalf("CountInboxItems second returned error: %v", err)
	}
	if counts.TotalCount != 1 || counts.UnreadCount != 0 || counts.ReadCount != 1 {
		t.Fatalf("counts after reprojection = %#v, want one read item", counts)
	}
}
