package globaldb

import (
	"context"
	"testing"
)

func TestAgentRunTelemetryRollupPreservesUnknownTokens(t *testing.T) {
	store := newGlobalDBTestStore(t, "telemetry-rollup")
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "alpha", "/tmp/origin", "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}

	if err := store.UpsertAgentRunTelemetry(ctx, AgentRunTelemetry{RunID: "run_known", WorkspaceID: "ws-1", TaskID: "task-1", ProfileName: "executor", Harness: "codex", Model: "gpt-5.1-codex", InvocationClass: "agent", Status: "completed", InputTokensKnown: true, InputTokens: int64Ptr(12), OutputTokensKnown: true, OutputTokens: int64Ptr(3), OwnedByAri: true, PortsJSON: `[{"port":5173,"protocol":"tcp","confidence":"detected"}]`, OrphanState: "not_orphaned"}); err != nil {
		t.Fatalf("UpsertAgentRunTelemetry known returned error: %v", err)
	}
	if err := store.UpsertAgentRunTelemetry(ctx, AgentRunTelemetry{RunID: "run_unknown", WorkspaceID: "ws-1", TaskID: "task-2", ProfileName: "executor", Harness: "codex", Model: "gpt-5.1-codex", InvocationClass: "agent", Status: "failed", OwnedByAri: true}); err != nil {
		t.Fatalf("UpsertAgentRunTelemetry unknown returned error: %v", err)
	}

	rollups, err := store.RollupAgentRunTelemetry(ctx, "ws-1")
	if err != nil {
		t.Fatalf("RollupAgentRunTelemetry returned error: %v", err)
	}
	if len(rollups) != 1 {
		t.Fatalf("rollups len = %d, want 1: %#v", len(rollups), rollups)
	}
	rollup := rollups[0]
	if rollup.Runs != 2 || rollup.Completed != 1 || rollup.Failed != 1 {
		t.Fatalf("rollup counts = %#v, want 2 runs 1 completed 1 failed", rollup)
	}
	if !rollup.InputTokens.Known || rollup.InputTokens.Value == nil || *rollup.InputTokens.Value != 12 {
		t.Fatalf("input tokens = %#v, want known total 12", rollup.InputTokens)
	}
	if !rollup.OutputTokens.Known || rollup.OutputTokens.Value == nil || *rollup.OutputTokens.Value != 3 {
		t.Fatalf("output tokens = %#v, want known total 3", rollup.OutputTokens)
	}
	if rollup.EstimatedCost.Known || rollup.EstimatedCost.Value != nil {
		t.Fatalf("estimated cost = %#v, want explicit unknown", rollup.EstimatedCost)
	}
	if rollup.PortsJSON != `[{"port":5173,"protocol":"tcp","confidence":"detected"}]` || rollup.OrphanState != "not_orphaned" {
		t.Fatalf("process rollup = %#v, want ports and orphan state preserved", rollup)
	}
}

func int64Ptr(value int64) *int64 { return &value }
