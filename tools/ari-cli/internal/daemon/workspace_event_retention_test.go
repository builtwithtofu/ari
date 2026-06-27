package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

// Terminal lifecycle events must stay bounded: result bodies live in the
// final_responses artifact table and events carry only a payload ref.
func TestTerminalWorkspaceEventsReferenceFinalResponsesNotBodies(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	const body = "an unmistakably large worker result body that must never be duplicated into event rows"

	if err := store.CreateFanoutGroup(ctx, globaldb.FanoutGroup{FanoutGroupID: "fg-retention", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1", Body: "fan out"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "retention-worker", WorkspaceID: "ws-1", Name: "worker", Harness: "codex"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig returned error: %v", err)
	}
	if err := store.CreateHarnessSession(ctx, globaldb.HarnessSession{SessionID: "retention-worker-run", WorkspaceID: "ws-1", AgentID: "retention-worker", Harness: "codex", Status: "running", Usage: globaldb.HarnessSessionUsageEphemeral, SourceSessionID: "run-1", SourceAgentID: "agent-1", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateHarnessSession returned error: %v", err)
	}
	if err := store.UpsertFinalResponse(ctx, globaldb.FinalResponse{FinalResponseID: "fr-retention", HarnessSessionID: "retention-worker-run", WorkspaceID: "ws-1", TaskID: "task-retention", ContextPacketID: "ctx-retention", ProfileID: "retention-worker", Status: "completed", Text: body}); err != nil {
		t.Fatalf("UpsertFinalResponse returned error: %v", err)
	}

	member := globaldb.FanoutMember{FanoutMemberID: "fg-retention-m1", FanoutGroupID: "fg-retention", WorkspaceID: "ws-1", WorkerSessionID: "retention-worker-run", TargetProfileID: "retention-worker", RequestAgentMessageID: "request-1", Status: "running"}
	if err := appendFanoutWorkerWorkspaceEvent(ctx, store, member, globaldb.WorkspaceEventWorkerCompleted, "retention-worker-run", "reply-1", "fr-retention", false); err != nil {
		t.Fatalf("appendFanoutWorkerWorkspaceEvent returned error: %v", err)
	}
	if err := newHarnessLifecycle(store).markCompletedWithFinalResponse(ctx, "retention-worker-run", globaldb.FinalResponse{FinalResponseID: "fr-retention", WorkspaceID: "ws-1", TaskID: "task-retention", ContextPacketID: "ctx-retention", ProfileID: "retention-worker", Text: body}); err != nil {
		t.Fatalf("markCompletedWithFinalResponse returned error: %v", err)
	}

	events, err := store.ListWorkspaceEventsAfterSequence(ctx, "ws-1", 0, 100)
	if err != nil {
		t.Fatalf("ListWorkspaceEventsAfterSequence returned error: %v", err)
	}
	checked := map[string]bool{}
	for _, event := range events {
		if event.SubjectID != "retention-worker-run" {
			continue
		}
		switch event.EventType {
		case globaldb.WorkspaceEventWorkerCompleted, globaldb.WorkspaceEventSessionCompleted:
		default:
			continue
		}
		checked[event.EventType] = true
		if strings.Contains(event.PayloadJSON, body) || strings.Contains(event.PayloadRefJSON, body) {
			t.Fatalf("event %s embeds the final response body: payload=%s ref=%s", event.EventType, event.PayloadJSON, event.PayloadRefJSON)
		}
		if id := globaldb.FinalResponseIDFromWorkspaceEventRef(event.PayloadRefJSON); id != "fr-retention" {
			t.Fatalf("event %s payload ref id = %q, want fr-retention", event.EventType, id)
		}
	}
	if !checked[globaldb.WorkspaceEventWorkerCompleted] || !checked[globaldb.WorkspaceEventSessionCompleted] {
		t.Fatalf("checked terminal events = %#v, want both worker.completed and session.completed", checked)
	}
	final, err := getFinalResponse(ctx, store, FinalResponseGetRequest{SessionID: "retention-worker-run"})
	if err != nil {
		t.Fatalf("getFinalResponse returned error: %v", err)
	}
	if final.Text != body {
		t.Fatalf("final response text = %q, want artifact table to hold the body", final.Text)
	}
}
