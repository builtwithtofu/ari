package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestFanoutWorkerWorkspaceEventsAreSoleWritePathForFanoutMembers(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateFanoutGroup(ctx, globaldb.FanoutGroup{FanoutGroupID: "fg-proj", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1", RequestAgentMessageID: "request-root", Body: "fan out"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "worker-profile", WorkspaceID: "ws-1", Name: "worker", Harness: "codex"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig worker returned error: %v", err)
	}
	if err := store.CreateHarnessSession(ctx, globaldb.HarnessSession{SessionID: "worker-proj-run", WorkspaceID: "ws-1", AgentID: "worker-profile", Harness: "codex", Status: "running", Usage: globaldb.HarnessSessionUsageEphemeral, SourceSessionID: "run-1", SourceAgentID: "agent-1", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateHarnessSession worker returned error: %v", err)
	}
	member := globaldb.FanoutMember{FanoutMemberID: "fg-proj-m1", FanoutGroupID: "fg-proj", WorkspaceID: "ws-1", WorkerSessionID: "worker-proj-run", TargetProfileID: "worker-profile", RequestAgentMessageID: "request-1", Status: "running"}

	if err := appendFanoutWorkerWorkspaceEvent(ctx, store, member, workspaceEventWorkerStarted, "run-1", "request-1", "", false); err != nil {
		t.Fatalf("appendFanoutWorkerWorkspaceEvent started returned error: %v", err)
	}
	stored, err := store.ListFanoutMembers(ctx, "fg-proj")
	if err != nil {
		t.Fatalf("ListFanoutMembers returned error: %v", err)
	}
	if len(stored) != 1 || stored[0].FanoutMemberID != "fg-proj-m1" || stored[0].Status != "running" || stored[0].RequestAgentMessageID != "request-1" || stored[0].TargetProfileID != "worker-profile" {
		t.Fatalf("stored members after started event = %#v, want event append to materialize the running member", stored)
	}

	if err := appendFanoutWorkerWorkspaceEvent(ctx, store, member, workspaceEventWorkerCompleted, "worker-proj-run", "reply-1", "fr-proj", false); err != nil {
		t.Fatalf("appendFanoutWorkerWorkspaceEvent completed returned error: %v", err)
	}
	got, err := store.GetFanoutMemberByWorkerSession(ctx, "worker-proj-run")
	if err != nil {
		t.Fatalf("GetFanoutMemberByWorkerSession returned error: %v", err)
	}
	if got.Status != "completed" || got.ReplyAgentMessageID != "reply-1" || got.FinalResponseID != "fr-proj" || got.RequestAgentMessageID != "request-1" {
		t.Fatalf("member after completed event = %#v, want terminal state materialized with preserved request linkage", got)
	}
}

// Event append and projection writes are one atomic unit: when any part of a
// fanout worker fact cannot be recorded (here: the fanout group is missing so
// the inbox projection is impossible), nothing lands in event history either.
func TestFanoutWorkerEventAppendLeavesNothingBehindWhenProjectionFails(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateHarnessSession(ctx, globaldb.HarnessSession{SessionID: "worker-orphan-run", WorkspaceID: "ws-1", AgentID: "agent-1", Harness: "codex", Status: "running", Usage: globaldb.HarnessSessionUsageEphemeral, SourceSessionID: "run-1", SourceAgentID: "agent-1", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateHarnessSession worker returned error: %v", err)
	}
	member := globaldb.FanoutMember{FanoutMemberID: "fg-missing-m1", FanoutGroupID: "fg-missing", WorkspaceID: "ws-1", WorkerSessionID: "worker-orphan-run", TargetProfileID: "agent-1", Status: "running"}

	if err := appendFanoutWorkerWorkspaceEvent(ctx, store, member, workspaceEventWorkerCompleted, "worker-orphan-run", "reply-1", "fr-orphan", false); err == nil {
		t.Fatal("appendFanoutWorkerWorkspaceEvent with missing fanout group returned nil error")
	}
	events, err := store.ListWorkspaceEventsAfterSequence(ctx, "ws-1", 0, 50)
	if err != nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned error: %v", err)
	}
	for _, event := range events {
		if event.SubjectID == "worker-orphan-run" {
			t.Fatalf("event %#v persisted despite failed projection, want atomic rollback", event)
		}
	}
	if members, err := store.ListFanoutMembers(ctx, "fg-missing"); err != nil || len(members) != 0 {
		t.Fatalf("fanout members = %#v err=%v, want no member rows from failed append", members, err)
	}
}

// The workspace boundary holds for the event/timer/delivery tool surface:
// objects owned by another workspace or session are not readable or mutable
// through a scoped tool call.
func TestAriWorkspaceToolsRejectCrossWorkspaceObjects(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateWorkspace(ctx, "ws-2", "other", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace ws-2 returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, globaldb.EventSubscription{SubscriptionID: "sub-other-ws", WorkspaceID: "ws-2", OwnerSessionID: "other-run", FilterJSON: `{}`}); err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	if _, err := store.CreateEventSubscription(ctx, globaldb.EventSubscription{SubscriptionID: "sub-other-owner", WorkspaceID: "ws-1", OwnerSessionID: "other-run", FilterJSON: `{}`}); err != nil {
		t.Fatalf("CreateEventSubscription owner returned error: %v", err)
	}
	if _, err := store.CreateWorkspaceTimer(ctx, globaldb.WorkspaceTimer{TimerID: "timer-other-ws", WorkspaceID: "ws-2", OwnerSessionID: "other-run", FireAt: time.Now().Add(time.Hour).UTC()}); err != nil {
		t.Fatalf("CreateWorkspaceTimer returned error: %v", err)
	}
	if _, err := store.CreatePendingDelivery(ctx, globaldb.PendingDelivery{DeliveryID: "delivery-other-ws", WorkspaceID: "ws-2", SubscriptionID: "sub-other-ws", TargetType: "harness_session", TargetID: "other-run", DeliveryPolicyJSON: `{"channel":"harness_session"}`, EventIDs: []string{"we-other-1"}}); err != nil {
		t.Fatalf("CreatePendingDelivery returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	scope := AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", WithinDefaultScope: true}

	for _, tc := range []struct {
		tool       string
		input      map[string]any
		wantReason string
	}{
		{tool: "ari.workspace.events.next", input: map[string]any{"subscription_id": "sub-other-ws"}, wantReason: "subscription_scope_mismatch"},
		{tool: "ari.workspace.events.next", input: map[string]any{"subscription_id": "sub-other-owner"}, wantReason: "subscription_scope_mismatch"},
		{tool: "ari.workspace.events.ack", input: map[string]any{"subscription_id": "sub-other-ws", "sequence": int64(1)}, wantReason: "subscription_scope_mismatch"},
		{tool: "ari.workspace.timers.get", input: map[string]any{"timer_id": "timer-other-ws"}, wantReason: "timer_scope_mismatch"},
		{tool: "ari.workspace.timers.cancel", input: map[string]any{"timer_id": "timer-other-ws"}, wantReason: "timer_scope_mismatch"},
		{tool: "ari.workspace.deliveries.get", input: map[string]any{"delivery_id": "delivery-other-ws"}, wantReason: "delivery_scope_mismatch"},
	} {
		err := callMethodError(registry, "ari.tool.call", AriToolCallRequest{Name: tc.tool, Scope: scope, Input: tc.input})
		if data := requireHandlerErrorData(t, err); data["reason"] != tc.wantReason {
			t.Fatalf("%s(%#v) error data = %#v, want reason %q", tc.tool, tc.input, data, tc.wantReason)
		}
	}
}

func TestFanoutMemberRebuildFromWorkspaceEventsMatchesMaterializedRows(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateFanoutGroup(ctx, globaldb.FanoutGroup{FanoutGroupID: "fg-rebuild", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1", RequestAgentMessageID: "request-root", Body: "fan out"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "worker-profile", WorkspaceID: "ws-1", Name: "worker", Harness: "codex"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig worker returned error: %v", err)
	}
	for i, terminal := range []string{workspaceEventWorkerCompleted, workspaceEventWorkerFailed} {
		workerSessionID := "worker-rebuild-" + string(rune('a'+i))
		if err := store.CreateHarnessSession(ctx, globaldb.HarnessSession{SessionID: workerSessionID, WorkspaceID: "ws-1", AgentID: "worker-profile", Harness: "codex", Status: "running", Usage: globaldb.HarnessSessionUsageEphemeral, SourceSessionID: "run-1", SourceAgentID: "agent-1", CWD: t.TempDir()}); err != nil {
			t.Fatalf("CreateHarnessSession %s returned error: %v", workerSessionID, err)
		}
		member := globaldb.FanoutMember{FanoutMemberID: "fg-rebuild-m" + workerSessionID, FanoutGroupID: "fg-rebuild", WorkspaceID: "ws-1", WorkerSessionID: workerSessionID, TargetProfileID: "worker-profile", RequestAgentMessageID: "request-" + workerSessionID, Status: "running"}
		if err := appendFanoutWorkerWorkspaceEvent(ctx, store, member, workspaceEventWorkerStarted, "run-1", member.RequestAgentMessageID, "", false); err != nil {
			t.Fatalf("appendFanoutWorkerWorkspaceEvent started returned error: %v", err)
		}
		if err := appendFanoutWorkerWorkspaceEvent(ctx, store, member, terminal, workerSessionID, "reply-"+workerSessionID, "fr-"+workerSessionID, terminal == workspaceEventWorkerFailed); err != nil {
			t.Fatalf("appendFanoutWorkerWorkspaceEvent terminal returned error: %v", err)
		}
	}
	materialized, err := store.ListFanoutMembers(ctx, "fg-rebuild")
	if err != nil {
		t.Fatalf("ListFanoutMembers returned error: %v", err)
	}
	rebuilt, err := globaldb.FanoutProjection{}.MembersFromWorkspaceEvents(ctx, store, "ws-1", "fg-rebuild")
	if err != nil {
		t.Fatalf("FanoutProjection.MembersFromWorkspaceEvents returned error: %v", err)
	}
	if len(rebuilt) != len(materialized) || len(rebuilt) != 2 {
		t.Fatalf("rebuilt members = %#v, materialized = %#v, want replay to reproduce the projection", rebuilt, materialized)
	}
	rebuiltByID := map[string]globaldb.FanoutMember{}
	for _, member := range rebuilt {
		rebuiltByID[member.FanoutMemberID] = member
	}
	for _, member := range materialized {
		replayed, ok := rebuiltByID[member.FanoutMemberID]
		if !ok {
			t.Fatalf("materialized member %q missing from replay %#v", member.FanoutMemberID, rebuilt)
		}
		if replayed.Status != member.Status || replayed.WorkerSessionID != member.WorkerSessionID || replayed.FinalResponseID != member.FinalResponseID || replayed.ReplyAgentMessageID != member.ReplyAgentMessageID || replayed.RequestAgentMessageID != member.RequestAgentMessageID {
			t.Fatalf("replayed member %q = %#v, want to match materialized %#v", member.FanoutMemberID, replayed, member)
		}
	}
}
