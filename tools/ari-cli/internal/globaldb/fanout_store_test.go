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

func TestProjectFanoutMemberMaterializesLifecycle(t *testing.T) {
	store := newGlobalDBTestStore(t, "fanout-member-projection-lifecycle")
	ctx := context.Background()
	seedHarnessSessionConfigSession(t, store, ctx)
	if err := store.CreateHarnessSession(ctx, HarnessSession{SessionID: "worker-1", WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "fake", Status: "running", Usage: HarnessSessionUsageEphemeral, SourceSessionID: "run-1", SourceAgentID: "agent-1", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateHarnessSession worker returned error: %v", err)
	}
	if err := store.CreateFanoutGroup(ctx, FanoutGroup{FanoutGroupID: "fg-1", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1", RequestAgentMessageID: "request-1", Body: "compare options"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}
	if err := store.ProjectFanoutMember(ctx, FanoutMember{FanoutMemberID: "fm-1", FanoutGroupID: "fg-1", WorkspaceID: "ws-1", WorkerSessionID: "worker-1", TargetProfileID: "agent-2", RequestAgentMessageID: "request-1", Status: "running"}); err != nil {
		t.Fatalf("ProjectFanoutMember running returned error: %v", err)
	}
	if err := store.UpsertFinalResponse(ctx, FinalResponse{FinalResponseID: "fr-worker-1", HarnessSessionID: "worker-1", WorkspaceID: "ws-1", TaskID: "task-1", ContextPacketID: "ctx-1", ProfileID: "agent-2", Status: "completed", Text: "done"}); err != nil {
		t.Fatalf("UpsertFinalResponse returned error: %v", err)
	}
	if err := store.ProjectFanoutMember(ctx, FanoutMember{FanoutMemberID: "fm-1", FanoutGroupID: "fg-1", WorkspaceID: "ws-1", WorkerSessionID: "worker-1", ReplyAgentMessageID: "reply-1", FinalResponseID: "fr-worker-1", Status: "completed"}); err != nil {
		t.Fatalf("ProjectFanoutMember completed returned error: %v", err)
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

func TestProjectFanoutMemberRejectsMissingIdentity(t *testing.T) {
	store := newGlobalDBTestStore(t, "fanout-member-projection-invalid")
	ctx := context.Background()
	if err := store.ProjectFanoutMember(ctx, FanoutMember{FanoutMemberID: "fm-1", FanoutGroupID: "fg-1", WorkspaceID: "ws-1"}); err == nil {
		t.Fatal("ProjectFanoutMember without worker session returned nil error, want ErrInvalidInput")
	}
}

func TestInboxItemProjectionPreservesReadStateAndCounts(t *testing.T) {
	store := newGlobalDBTestStore(t, "inbox-items-projection")
	ctx := context.Background()
	seedHarnessSessionConfigSession(t, store, ctx)

	firstEvent, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-inbox-1", WorkspaceID: "ws-1", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fg-1", CausationID: "reply-1", PayloadJSON: `{"status":"completed"}`, PayloadRefJSON: `{"kind":"final_response","id":"fr-1"}`})
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent first returned error: %v", err)
	}
	if _, err := store.ProjectInboxItem(ctx, InboxItem{InboxItemID: "inbox-fm-1", WorkspaceID: "ws-1", SourceSessionID: "run-1", WorkspaceEventID: firstEvent.EventID, EventType: firstEvent.EventType, FanoutGroupID: "fg-1", FanoutMemberID: "fm-1", WorkerSessionID: "worker-1", FinalResponseID: "fr-1", Kind: "worker_completed", Summary: "done"}); err != nil {
		t.Fatalf("ProjectInboxItem first returned error: %v", err)
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

	secondEvent, err := store.AppendWorkspaceEvent(ctx, WorkspaceEvent{EventID: "we-inbox-2", WorkspaceID: "ws-1", EventType: "worker.completed", SubjectType: "harness_session", SubjectID: "worker-1", ProducerType: "session", ProducerID: "worker-1", CorrelationID: "fg-1", CausationID: "reply-2", PayloadJSON: `{"status":"completed"}`, PayloadRefJSON: `{"kind":"final_response","id":"fr-1"}`})
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent second returned error: %v", err)
	}
	if _, err := store.ProjectInboxItem(ctx, InboxItem{InboxItemID: "inbox-fm-1", WorkspaceID: "ws-1", SourceSessionID: "run-1", WorkspaceEventID: secondEvent.EventID, EventType: secondEvent.EventType, FanoutGroupID: "fg-1", FanoutMemberID: "fm-1", WorkerSessionID: "worker-1", FinalResponseID: "fr-1", Kind: "worker_completed", Summary: "done again"}); err != nil {
		t.Fatalf("ProjectInboxItem second returned error: %v", err)
	}

	item, err := store.GetInboxItem(ctx, "inbox-fm-1")
	if err != nil {
		t.Fatalf("GetInboxItem returned error: %v", err)
	}
	if item.Status != "read" || item.WorkspaceEventID != secondEvent.EventID || item.Summary != "done again" {
		t.Fatalf("item = %#v, want read state preserved with refreshed event evidence", item)
	}
	counts, err = store.CountInboxItems(ctx, "ws-1", "run-1")
	if err != nil {
		t.Fatalf("CountInboxItems second returned error: %v", err)
	}
	if counts.TotalCount != 1 || counts.UnreadCount != 0 || counts.ReadCount != 1 {
		t.Fatalf("counts after reprojection = %#v, want one read item", counts)
	}
}
