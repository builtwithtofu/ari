package plan

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/world"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE plans (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			status TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// mockStepRunner is a test double for StepRunner
type mockStepRunner struct {
	executedSteps []string
	shouldFail    map[string]error
}

func newMockStepRunner() *mockStepRunner {
	return &mockStepRunner{
		executedSteps: []string{},
		shouldFail:    make(map[string]error),
	}
}

func (m *mockStepRunner) Run(ctx context.Context, step Step) error {
	if err, ok := m.shouldFail[step.StepID]; ok {
		return err
	}
	m.executedSteps = append(m.executedSteps, step.StepID)
	return nil
}

func (m *mockStepRunner) setFail(stepID string, err error) {
	m.shouldFail[stepID] = err
}

// captureEmitter captures emitted events for testing
type captureEmitter struct {
	output *bytes.Buffer
}

func newCaptureEmitter() *captureEmitter {
	return &captureEmitter{
		output: &bytes.Buffer{},
	}
}

func (c *captureEmitter) EmitEvent(event protocol.Event) error {
	data, _ := json.Marshal(event)
	c.output.Write(data)
	c.output.WriteByte('\n')
	return nil
}

func TestExecutor_Run_LinearChain(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	// Create plan in DB
	plan := &Plan{
		PlanID:    "test-linear",
		Goal:      "Test linear execution",
		Status:    PlanStatusApproved,
		CreatedAt: now(),
		UpdatedAt: now(),
		Steps: []Step{
			{StepID: "A", Type: StepTypeCode, Description: "Step A", Status: StepStatusApproved},
			{StepID: "B", Type: StepTypeCode, Description: "Step B", Status: StepStatusApproved, DependsOn: []string{"A"}},
			{StepID: "C", Type: StepTypeCode, Description: "Step C", Status: StepStatusApproved, DependsOn: []string{"B"}},
		},
	}

	content, _ := json.Marshal(plan)
	_, err := q.CreatePlan(ctx, world.CreatePlanParams{
		ID:        plan.PlanID,
		Title:     plan.Goal,
		Status:    string(plan.Status),
		Content:   string(content),
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	runner := newMockStepRunner()
	emitter := protocol.NewEmitter()
	executor := NewExecutor(q, emitter, runner)

	result, err := executor.Run(ctx, plan)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.StepsRun != 3 {
		t.Fatalf("expected 3 steps run, got %d", result.StepsRun)
	}
	if result.StepsFailed != 0 {
		t.Fatalf("expected 0 steps failed, got %d", result.StepsFailed)
	}

	// Verify execution order
	expectedOrder := []string{"A", "B", "C"}
	if len(runner.executedSteps) != len(expectedOrder) {
		t.Fatalf("expected %d executed steps, got %d", len(expectedOrder), len(runner.executedSteps))
	}
	for i, stepID := range expectedOrder {
		if runner.executedSteps[i] != stepID {
			t.Fatalf("expected step %d to be %q, got %q", i, stepID, runner.executedSteps[i])
		}
	}

	// Verify plan status updated
	updatedPlan, err := q.GetPlan(ctx, plan.PlanID)
	if err != nil {
		t.Fatalf("get updated plan: %v", err)
	}
	if updatedPlan.Status != string(PlanStatusCompleted) {
		t.Fatalf("expected plan status %q, got %q", PlanStatusCompleted, updatedPlan.Status)
	}
}

func TestExecutor_Run_DiamondPattern(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	// Create diamond pattern plan:
	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	plan := &Plan{
		PlanID:    "test-diamond",
		Goal:      "Test diamond execution",
		Status:    PlanStatusApproved,
		CreatedAt: now(),
		UpdatedAt: now(),
		Steps: []Step{
			{StepID: "A", Type: StepTypeCode, Description: "Step A", Status: StepStatusApproved},
			{StepID: "B", Type: StepTypeCode, Description: "Step B", Status: StepStatusApproved, DependsOn: []string{"A"}},
			{StepID: "C", Type: StepTypeCode, Description: "Step C", Status: StepStatusApproved, DependsOn: []string{"A"}},
			{StepID: "D", Type: StepTypeCode, Description: "Step D", Status: StepStatusApproved, DependsOn: []string{"B", "C"}},
		},
	}

	content, _ := json.Marshal(plan)
	_, err := q.CreatePlan(ctx, world.CreatePlanParams{
		ID:        plan.PlanID,
		Title:     plan.Goal,
		Status:    string(plan.Status),
		Content:   string(content),
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	runner := newMockStepRunner()
	emitter := protocol.NewEmitter()
	executor := NewExecutor(q, emitter, runner)

	result, err := executor.Run(ctx, plan)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.StepsRun != 4 {
		t.Fatalf("expected 4 steps run, got %d", result.StepsRun)
	}

	// Verify A comes before B and C
	indexA := -1
	indexB := -1
	indexC := -1
	indexD := -1
	for i, stepID := range runner.executedSteps {
		switch stepID {
		case "A":
			indexA = i
		case "B":
			indexB = i
		case "C":
			indexC = i
		case "D":
			indexD = i
		}
	}

	if indexA == -1 || indexB == -1 || indexC == -1 || indexD == -1 {
		t.Fatal("not all steps were executed")
	}

	// A must come before B and C
	if indexA >= indexB || indexA >= indexC {
		t.Fatal("A must come before B and C")
	}

	// B and C must come before D
	if indexB >= indexD || indexC >= indexD {
		t.Fatal("B and C must come before D")
	}
}

func TestExecutor_Run_StepFailure(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	plan := &Plan{
		PlanID:    "test-failure",
		Goal:      "Test step failure",
		Status:    PlanStatusApproved,
		CreatedAt: now(),
		UpdatedAt: now(),
		Steps: []Step{
			{StepID: "A", Type: StepTypeCode, Description: "Step A", Status: StepStatusApproved},
			{StepID: "B", Type: StepTypeCode, Description: "Step B", Status: StepStatusApproved, DependsOn: []string{"A"}},
			{StepID: "C", Type: StepTypeCode, Description: "Step C", Status: StepStatusApproved, DependsOn: []string{"B"}},
		},
	}

	content, _ := json.Marshal(plan)
	_, err := q.CreatePlan(ctx, world.CreatePlanParams{
		ID:        plan.PlanID,
		Title:     plan.Goal,
		Status:    string(plan.Status),
		Content:   string(content),
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	runner := newMockStepRunner()
	expectedErr := errors.New("step B failed")
	runner.setFail("B", expectedErr)

	emitter := protocol.NewEmitter()
	executor := NewExecutor(q, emitter, runner)

	result, err := executor.Run(ctx, plan)

	// We expect an error return
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.StepsRun != 2 {
		t.Fatalf("expected 2 steps run (A and attempted B), got %d", result.StepsRun)
	}
	if result.StepsFailed != 1 {
		t.Fatalf("expected 1 step failed, got %d", result.StepsFailed)
	}
	if !strings.Contains(err.Error(), "step \"B\" failed") {
		t.Fatalf("expected error to contain 'step \"B\" failed', got %q", err.Error())
	}

	// Verify only A was executed
	if len(runner.executedSteps) != 1 || runner.executedSteps[0] != "A" {
		t.Fatalf("expected only A to be executed, got %v", runner.executedSteps)
	}

	// Verify plan status is failed
	updatedPlan, err := q.GetPlan(ctx, plan.PlanID)
	if err != nil {
		t.Fatalf("get updated plan: %v", err)
	}
	if updatedPlan.Status != string(PlanStatusFailed) {
		t.Fatalf("expected plan status %q, got %q", PlanStatusFailed, updatedPlan.Status)
	}

	// Verify step B has error recorded in content
	var planContent Plan
	if err := json.Unmarshal([]byte(updatedPlan.Content), &planContent); err != nil {
		t.Fatalf("unmarshal plan content: %v", err)
	}
	for _, step := range planContent.Steps {
		if step.StepID == "B" {
			if step.Status != StepStatusFailed {
				t.Fatalf("expected step B status to be %q, got %q", StepStatusFailed, step.Status)
			}
			if step.Error != expectedErr.Error() {
				t.Fatalf("expected step B error %q, got %q", expectedErr.Error(), step.Error)
			}
		}
	}
}

func TestExecutor_Run_TopologicalSortError(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	// Create plan with circular dependency
	plan := &Plan{
		PlanID:    "test-cycle",
		Goal:      "Test cycle detection",
		Status:    PlanStatusApproved,
		CreatedAt: now(),
		UpdatedAt: now(),
		Steps: []Step{
			{StepID: "A", Type: StepTypeCode, Description: "Step A", Status: StepStatusApproved, DependsOn: []string{"C"}},
			{StepID: "B", Type: StepTypeCode, Description: "Step B", Status: StepStatusApproved, DependsOn: []string{"A"}},
			{StepID: "C", Type: StepTypeCode, Description: "Step C", Status: StepStatusApproved, DependsOn: []string{"B"}},
		},
	}

	content, _ := json.Marshal(plan)
	_, err := q.CreatePlan(ctx, world.CreatePlanParams{
		ID:        plan.PlanID,
		Title:     plan.Goal,
		Status:    string(plan.Status),
		Content:   string(content),
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	runner := newMockStepRunner()
	emitter := protocol.NewEmitter()
	executor := NewExecutor(q, emitter, runner)

	result, err := executor.Run(ctx, plan)

	if err == nil {
		t.Fatal("expected error for cycle, got nil")
	}
	if result.Success {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), "topological sort failed") {
		t.Fatalf("expected error to contain 'topological sort failed', got %q", err.Error())
	}
}

func TestExecutor_Run_StepStatusUpdates(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	plan := &Plan{
		PlanID:    "test-status",
		Goal:      "Test status updates",
		Status:    PlanStatusApproved,
		CreatedAt: now(),
		UpdatedAt: now(),
		Steps: []Step{
			{StepID: "A", Type: StepTypeCode, Description: "Step A", Status: StepStatusApproved},
		},
	}

	content, _ := json.Marshal(plan)
	_, err := q.CreatePlan(ctx, world.CreatePlanParams{
		ID:        plan.PlanID,
		Title:     plan.Goal,
		Status:    string(plan.Status),
		Content:   string(content),
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	runner := newMockStepRunner()
	emitter := protocol.NewEmitter()
	executor := NewExecutor(q, emitter, runner)

	_, err = executor.Run(ctx, plan)
	if err != nil {
		t.Fatalf("run executor: %v", err)
	}

	// Verify final plan content has updated timestamps
	updatedPlan, err := q.GetPlan(ctx, plan.PlanID)
	if err != nil {
		t.Fatalf("get updated plan: %v", err)
	}

	var planContent Plan
	if err := json.Unmarshal([]byte(updatedPlan.Content), &planContent); err != nil {
		t.Fatalf("unmarshal plan content: %v", err)
	}

	// Verify step has timestamps
	if len(planContent.Steps) != 1 {
		t.Fatal("expected 1 step")
	}
	step := planContent.Steps[0]
	if step.StartedAt == "" {
		t.Error("expected step to have started_at timestamp")
	}
	if step.CompletedAt == "" {
		t.Error("expected step to have completed_at timestamp")
	}
	if step.Status != StepStatusCompleted {
		t.Fatalf("expected step status %q, got %q", StepStatusCompleted, step.Status)
	}
}

func TestExecutor_Run_NoEmitter(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	plan := &Plan{
		PlanID:    "test-no-emitter",
		Goal:      "Test without emitter",
		Status:    PlanStatusApproved,
		CreatedAt: now(),
		UpdatedAt: now(),
		Steps: []Step{
			{StepID: "A", Type: StepTypeCode, Description: "Step A", Status: StepStatusApproved},
		},
	}

	content, _ := json.Marshal(plan)
	_, err := q.CreatePlan(ctx, world.CreatePlanParams{
		ID:        plan.PlanID,
		Title:     plan.Goal,
		Status:    string(plan.Status),
		Content:   string(content),
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	runner := newMockStepRunner()
	// Pass nil emitter
	executor := NewExecutor(q, nil, runner)

	result, err := executor.Run(ctx, plan)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.StepsRun != 1 {
		t.Fatalf("expected 1 step run, got %d", result.StepsRun)
	}
}

func TestExecutor_Run_EmptyPlan(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	plan := &Plan{
		PlanID:    "test-empty",
		Goal:      "Test empty plan",
		Status:    PlanStatusApproved,
		CreatedAt: now(),
		UpdatedAt: now(),
		Steps:     []Step{},
	}

	content, _ := json.Marshal(plan)
	_, err := q.CreatePlan(ctx, world.CreatePlanParams{
		ID:        plan.PlanID,
		Title:     plan.Goal,
		Status:    string(plan.Status),
		Content:   string(content),
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	runner := newMockStepRunner()
	emitter := protocol.NewEmitter()
	executor := NewExecutor(q, emitter, runner)

	result, err := executor.Run(ctx, plan)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.StepsRun != 0 {
		t.Fatalf("expected 0 steps run, got %d", result.StepsRun)
	}

	// Verify plan is marked as completed
	updatedPlan, err := q.GetPlan(ctx, plan.PlanID)
	if err != nil {
		t.Fatalf("get updated plan: %v", err)
	}
	if updatedPlan.Status != string(PlanStatusCompleted) {
		t.Fatalf("expected plan status %q, got %q", PlanStatusCompleted, updatedPlan.Status)
	}
}
