package flow

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/adavies/opencode-gaia/tools/gaia-cli/internal/lifecycle"
)

func TestStartCreatesFlowSnapshotAndEvent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	result, err := Start(repoRoot, StartInput{
		SessionID: "s-flow-start",
		StreamID:  "feature-x",
		Mode:      lifecycle.ModeSupervised,
		Risk:      lifecycle.RiskLow,
		Scope:     "Add deterministic flow contract",
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if result.Policy.State != lifecycle.StatePlanningReady {
		t.Fatalf("expected planning_ready, got %s", result.Policy.State)
	}

	if result.Flow.LastCommand != CommandStart {
		t.Fatalf("expected start command, got %s", result.Flow.LastCommand)
	}

	if result.Flow.Iteration != 0 {
		t.Fatalf("expected iteration 0, got %d", result.Flow.Iteration)
	}

	if result.Flow.LastSequence != 1 {
		t.Fatalf("expected sequence 1, got %d", result.Flow.LastSequence)
	}

	if result.NextCommand != "ari flow execute --session s-flow-start" {
		t.Fatalf("unexpected next command: %s", result.NextCommand)
	}

	if _, err := os.Stat(filepath.Join(repoRoot, ".gaia", "flows", "s-flow-start.events.ndjson")); err != nil {
		t.Fatalf("expected event log file: %v", err)
	}
}

func TestIterateIncrementsIterationAndTransitionsToReady(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if _, err := Start(repoRoot, StartInput{
		SessionID: "s-flow-iterate",
		StreamID:  "feature-x",
		Mode:      lifecycle.ModeSupervised,
		Risk:      lifecycle.RiskLow,
		Scope:     "",
	}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	result, err := Iterate(repoRoot, IterateInput{
		SessionID: "s-flow-iterate",
		Scope:     "Clarify acceptance checks",
		Note:      "first planning loop",
	})
	if err != nil {
		t.Fatalf("Iterate returned error: %v", err)
	}

	if result.Policy.State != lifecycle.StatePlanningReady {
		t.Fatalf("expected planning_ready after iterate, got %s", result.Policy.State)
	}

	if result.Flow.Iteration != 1 {
		t.Fatalf("expected iteration 1, got %d", result.Flow.Iteration)
	}

	if result.Flow.LastCommand != CommandIterate {
		t.Fatalf("expected iterate command, got %s", result.Flow.LastCommand)
	}

	if result.Flow.LastSequence != 2 {
		t.Fatalf("expected sequence 2, got %d", result.Flow.LastSequence)
	}

	eventPath := filepath.Join(repoRoot, ".gaia", "flows", "s-flow-iterate.events.ndjson")
	eventCount := countLines(t, eventPath)
	if eventCount != 2 {
		t.Fatalf("expected 2 flow events, got %d", eventCount)
	}
}

func TestExecuteRequiresApprovalForMediumRisk(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if _, err := Start(repoRoot, StartInput{
		SessionID: "s-flow-approval",
		StreamID:  "urgent-bug",
		Mode:      lifecycle.ModeSupervised,
		Risk:      lifecycle.RiskMedium,
		Scope:     "Apply hotfix safely",
	}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	result, err := Execute(repoRoot, ExecuteInput{SessionID: "s-flow-approval", Approve: false})
	if !errors.Is(err, lifecycle.ErrApprovalRequired) {
		t.Fatalf("expected ErrApprovalRequired, got %v", err)
	}

	if result.Policy.State != lifecycle.StateBlockedWaitingHuman {
		t.Fatalf("expected blocked_waiting_human, got %s", result.Policy.State)
	}

	if result.NextCommand != "ari flow execute --session s-flow-approval --approve" {
		t.Fatalf("unexpected next command: %s", result.NextCommand)
	}
}

func TestContinueRequiresExecutingState(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if _, err := Start(repoRoot, StartInput{
		SessionID: "s-flow-continue",
		StreamID:  "feature-x",
		Mode:      lifecycle.ModeSupervised,
		Risk:      lifecycle.RiskLow,
		Scope:     "Implement one small unit",
	}); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	_, err := Continue(repoRoot, "s-flow-continue")
	if !errors.Is(err, ErrContinueStateNotReady) {
		t.Fatalf("expected ErrContinueStateNotReady, got %v", err)
	}

	if _, err := Execute(repoRoot, ExecuteInput{SessionID: "s-flow-continue", Approve: false}); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	result, err := Continue(repoRoot, "s-flow-continue")
	if err != nil {
		t.Fatalf("Continue returned error: %v", err)
	}

	if result.Policy.State != lifecycle.StateExecuting {
		t.Fatalf("expected executing state, got %s", result.Policy.State)
	}

	if result.NextCommand != "ari flow continue --session s-flow-continue" {
		t.Fatalf("unexpected next command: %s", result.NextCommand)
	}
}

func countLines(t *testing.T, path string) int {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open file failed: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count += 1
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scan file failed: %v", err)
	}

	return count
}
