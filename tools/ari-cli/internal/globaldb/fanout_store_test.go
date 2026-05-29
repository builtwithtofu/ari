package globaldb

import (
	"context"
	"testing"
)

func TestFanoutGroupMembersAndStickyInboxLifecycle(t *testing.T) {
	store := newGlobalDBTestStore(t, "fanout-inbox-lifecycle")
	ctx := context.Background()
	seedHarnessSessionConfigSession(t, store, ctx)
	if err := store.CreateHarnessSession(ctx, HarnessSession{SessionID: "worker-1", WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "fake", Status: "running", Usage: HarnessSessionUsageEphemeral, SourceSessionID: "run-1", SourceAgentID: "agent-1", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateHarnessSession worker returned error: %v", err)
	}

	if err := store.CreateFanoutGroup(ctx, FanoutGroup{FanoutGroupID: "fg-1", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1", RequestAgentMessageID: "request-1", Body: "compare options"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}
	if err := store.AddFanoutMember(ctx, FanoutMember{FanoutMemberID: "fm-1", FanoutGroupID: "fg-1", WorkspaceID: "ws-1", WorkerSessionID: "worker-1", TargetProfileID: "agent-2", RequestAgentMessageID: "request-1"}); err != nil {
		t.Fatalf("AddFanoutMember returned error: %v", err)
	}
	if err := store.UpsertFinalResponse(ctx, FinalResponse{FinalResponseID: "fr-worker-1", HarnessSessionID: "worker-1", WorkspaceID: "ws-1", TaskID: "task-1", ContextPacketID: "ctx-1", ProfileID: "agent-2", Status: "completed", Text: "done"}); err != nil {
		t.Fatalf("UpsertFinalResponse returned error: %v", err)
	}
	if err := store.UpdateFanoutMemberStatus(ctx, "fm-1", "completed", "reply-1", "fr-worker-1"); err != nil {
		t.Fatalf("UpdateFanoutMemberStatus returned error: %v", err)
	}
	if err := store.CreateStickyInboxItem(ctx, StickyInboxItem{InboxItemID: "inbox-1", WorkspaceID: "ws-1", TargetSessionID: "run-1", FanoutGroupID: "fg-1", FanoutMemberID: "fm-1", WorkerSessionID: "worker-1", FinalResponseID: "fr-worker-1", Kind: "worker_completed", Summary: "done"}); err != nil {
		t.Fatalf("CreateStickyInboxItem returned error: %v", err)
	}

	members, err := store.ListFanoutMembers(ctx, "fg-1")
	if err != nil {
		t.Fatalf("ListFanoutMembers returned error: %v", err)
	}
	if len(members) != 1 || members[0].Status != "completed" || members[0].WorkerSessionID != "worker-1" || members[0].FinalResponseID != "fr-worker-1" {
		t.Fatalf("members = %#v, want completed worker linked to final response", members)
	}
	items, err := store.ListStickyInboxItems(ctx, "ws-1", "run-1")
	if err != nil {
		t.Fatalf("ListStickyInboxItems returned error: %v", err)
	}
	if len(items) != 1 || items[0].Kind != "worker_completed" || items[0].Status != "unread" || items[0].FanoutGroupID != "fg-1" || items[0].WorkerSessionID != "worker-1" || items[0].FinalResponseID != "fr-worker-1" {
		t.Fatalf("items = %#v, want sticky-visible unread worker result with durable links", items)
	}
}

func TestStickyInboxPreservesStoppedWorkerAsInspectable(t *testing.T) {
	store := newGlobalDBTestStore(t, "fanout-inbox-stopped")
	ctx := context.Background()
	seedHarnessSessionConfigSession(t, store, ctx)
	if err := store.CreateHarnessSession(ctx, HarnessSession{SessionID: "worker-stopped", WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "fake", Status: "stopped", Usage: HarnessSessionUsageEphemeral, SourceSessionID: "run-1", SourceAgentID: "agent-1", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateHarnessSession worker returned error: %v", err)
	}
	if err := store.CreateFanoutGroup(ctx, FanoutGroup{FanoutGroupID: "fg-stopped", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}
	if err := store.AddFanoutMember(ctx, FanoutMember{FanoutMemberID: "fm-stopped", FanoutGroupID: "fg-stopped", WorkspaceID: "ws-1", WorkerSessionID: "worker-stopped", TargetProfileID: "agent-2", Status: "stopped"}); err != nil {
		t.Fatalf("AddFanoutMember returned error: %v", err)
	}
	if err := store.CreateStickyInboxItem(ctx, StickyInboxItem{InboxItemID: "inbox-stopped", WorkspaceID: "ws-1", TargetSessionID: "run-1", FanoutGroupID: "fg-stopped", FanoutMemberID: "fm-stopped", WorkerSessionID: "worker-stopped", Kind: "worker_stopped", Summary: "stopped by workspace suspend"}); err != nil {
		t.Fatalf("CreateStickyInboxItem returned error: %v", err)
	}

	items, err := store.ListStickyInboxItems(ctx, "ws-1", "run-1")
	if err != nil {
		t.Fatalf("ListStickyInboxItems returned error: %v", err)
	}
	if len(items) != 1 || items[0].Kind != "worker_stopped" || items[0].Status != "unread" || items[0].WorkerSessionID != "worker-stopped" {
		t.Fatalf("items = %#v, want stopped worker visible for explicit continuation", items)
	}
}

func TestFanoutMemberStatusUpdatePreservesExistingInboxReadState(t *testing.T) {
	store := newGlobalDBTestStore(t, "fanout-inbox-read-preserved")
	ctx := context.Background()
	seedHarnessSessionConfigSession(t, store, ctx)
	if err := store.CreateHarnessSession(ctx, HarnessSession{SessionID: "worker-1", WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "fake", Status: "running", Usage: HarnessSessionUsageEphemeral, SourceSessionID: "run-1", SourceAgentID: "agent-1", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateHarnessSession worker returned error: %v", err)
	}
	if err := store.CreateFanoutGroup(ctx, FanoutGroup{FanoutGroupID: "fg-1", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}
	if err := store.AddFanoutMember(ctx, FanoutMember{FanoutMemberID: "fm-1", FanoutGroupID: "fg-1", WorkspaceID: "ws-1", WorkerSessionID: "worker-1", TargetProfileID: "agent-2"}); err != nil {
		t.Fatalf("AddFanoutMember returned error: %v", err)
	}
	if err := store.UpdateFanoutMemberStatusAndInboxByWorkerSession(ctx, "worker-1", "completed", "reply-1", "fr-1", "done"); err != nil {
		t.Fatalf("UpdateFanoutMemberStatusAndInboxByWorkerSession first returned error: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE sticky_inbox_items SET status = 'read' WHERE inbox_item_id = 'inbox-fm-1'`); err != nil {
		t.Fatalf("mark inbox read returned error: %v", err)
	}
	if err := store.UpdateFanoutMemberStatusAndInboxByWorkerSession(ctx, "worker-1", "completed", "reply-1", "fr-1", "done again"); err != nil {
		t.Fatalf("UpdateFanoutMemberStatusAndInboxByWorkerSession second returned error: %v", err)
	}
	items, err := store.ListStickyInboxItems(ctx, "ws-1", "run-1")
	if err != nil {
		t.Fatalf("ListStickyInboxItems returned error: %v", err)
	}
	if len(items) != 1 || items[0].Status != "read" || items[0].Summary != "done again" {
		t.Fatalf("items = %#v, want read state preserved and summary updated", items)
	}
}
