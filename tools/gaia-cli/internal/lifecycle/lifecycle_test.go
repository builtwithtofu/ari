package lifecycle

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestStartPlanReadyWhenScopePresent(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	policy, err := StartPlan(repoRoot, StartPlanInput{
		SessionID: "s-ready",
		StreamID:  "feature-x",
		Mode:      ModeSupervised,
		Risk:      RiskLow,
		Scope:     "Implement hello world flag",
	})
	if err != nil {
		t.Fatalf("StartPlan returned error: %v", err)
	}

	if policy.State != StatePlanningReady {
		t.Fatalf("expected %s, got %s", StatePlanningReady, policy.State)
	}

	if policy.NextCommand != "ari plan execute --session s-ready" {
		t.Fatalf("unexpected next command: %s", policy.NextCommand)
	}

	if policy.Path != filepath.Join(repoRoot, ".gaia", "lifecycle", "s-ready.json") {
		t.Fatalf("unexpected lifecycle path: %s", policy.Path)
	}
}

func TestStartPlanNeedsInputWhenScopeMissing(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	policy, err := StartPlan(repoRoot, StartPlanInput{
		SessionID: "s-needs-input",
		StreamID:  "feature-x",
		Mode:      ModeSupervised,
		Risk:      RiskLow,
		Scope:     "",
	})
	if err != nil {
		t.Fatalf("StartPlan returned error: %v", err)
	}

	if policy.State != StatePlanningNeedsInput {
		t.Fatalf("expected %s, got %s", StatePlanningNeedsInput, policy.State)
	}
}

func TestExecutePlanRequiresApprovalForMediumRisk(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if _, err := StartPlan(repoRoot, StartPlanInput{
		SessionID: "s-approve",
		StreamID:  "urgent-bug",
		Mode:      ModeSupervised,
		Risk:      RiskMedium,
		Scope:     "Apply hotfix safely",
	}); err != nil {
		t.Fatalf("StartPlan returned error: %v", err)
	}

	policy, err := ExecutePlan(repoRoot, "s-approve", false)
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("expected ErrApprovalRequired, got %v", err)
	}

	if policy.State != StateBlockedWaitingHuman {
		t.Fatalf("expected %s, got %s", StateBlockedWaitingHuman, policy.State)
	}
}

func TestExecutePlanTransitionsToExecuting(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if _, err := StartPlan(repoRoot, StartPlanInput{
		SessionID: "s-execute",
		StreamID:  "feature-x",
		Mode:      ModeSupervised,
		Risk:      RiskLow,
		Scope:     "Implement one small unit",
	}); err != nil {
		t.Fatalf("StartPlan returned error: %v", err)
	}

	policy, err := ExecutePlan(repoRoot, "s-execute", false)
	if err != nil {
		t.Fatalf("ExecutePlan returned error: %v", err)
	}

	if policy.State != StateExecuting {
		t.Fatalf("expected %s, got %s", StateExecuting, policy.State)
	}
}

func TestContinueWorkRejectsWhenPlanNotExecuted(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if _, err := StartPlan(repoRoot, StartPlanInput{
		SessionID: "s-continue",
		StreamID:  "feature-x",
		Mode:      ModeSupervised,
		Risk:      RiskLow,
		Scope:     "Implement small task",
	}); err != nil {
		t.Fatalf("StartPlan returned error: %v", err)
	}

	_, err := ContinueWork(repoRoot, "s-continue")
	if !errors.Is(err, ErrPlanNotExecuting) {
		t.Fatalf("expected ErrPlanNotExecuting, got %v", err)
	}
}
