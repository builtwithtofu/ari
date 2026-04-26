package globaldb

import (
	"context"
	"errors"
	"testing"
)

func TestFinalResponsePersistsAndListsByWorkspace(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "final-response")
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.UpsertAgentProfile(ctx, AgentProfile{ProfileID: "ap_executor", Name: "executor"}); err != nil {
		t.Fatalf("UpsertAgentProfile returned error: %v", err)
	}

	created := FinalResponse{FinalResponseID: "fr_1", RunID: "run_1", WorkspaceID: "ws-1", TaskID: "task-1", ContextPacketID: "ctx_1", ProfileID: "ap_executor", Status: "completed", Text: "Done", EvidenceLinksJSON: `[{"kind":"context_packet","id":"ctx_1"}]`}
	if err := store.UpsertFinalResponse(ctx, created); err != nil {
		t.Fatalf("UpsertFinalResponse returned error: %v", err)
	}

	got, err := store.GetFinalResponseByRunID(ctx, "run_1")
	if err != nil {
		t.Fatalf("GetFinalResponseByRunID returned error: %v", err)
	}
	if got.FinalResponseID != "fr_1" || got.ProfileID != "ap_executor" || got.Text != "Done" || got.EvidenceLinksJSON != `[{"kind":"context_packet","id":"ctx_1"}]` {
		t.Fatalf("final response = %#v, want stored artifact with evidence", got)
	}

	listed, err := store.ListFinalResponses(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListFinalResponses returned error: %v", err)
	}
	if len(listed) != 1 || listed[0].FinalResponseID != "fr_1" {
		t.Fatalf("listed final responses = %#v, want fr_1", listed)
	}
}

func TestFinalResponseRejectsInvalidInput(t *testing.T) {
	store := newMigratedGlobalDBStore(t, "final-response-invalid")
	err := store.UpsertFinalResponse(context.Background(), FinalResponse{FinalResponseID: "fr_1", RunID: "run_1", WorkspaceID: "ws-1", TaskID: "task-1", ContextPacketID: "ctx_1", Status: "complete", Text: "Done", EvidenceLinksJSON: `[]`})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpsertFinalResponse error = %v, want ErrInvalidInput", err)
	}
}
