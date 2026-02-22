package plan

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/world"
	_ "modernc.org/sqlite"
)

func TestSaveStepStatus_SavesStepToDB(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	// Create initial plan
	plan := &Plan{
		PlanID:    "test-save-step",
		Goal:      "Test save step status",
		Status:    PlanStatusApproved,
		CreatedAt: now(),
		UpdatedAt: now(),
		Steps: []Step{
			{StepID: "A", Type: StepTypeCode, Description: "Step A", Status: StepStatusApproved},
			{StepID: "B", Type: StepTypeCode, Description: "Step B", Status: StepStatusApproved},
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

	executor := NewExecutor(q, protocol.NewEmitter(), newMockStepRunner())

	// Update step A to completed
	updatedStep := Step{
		StepID:      "A",
		Type:        StepTypeCode,
		Description: "Step A",
		Status:      StepStatusCompleted,
		StartedAt:   now(),
		CompletedAt: now(),
	}

	err = executor.SaveStepStatus(ctx, plan.PlanID, updatedStep)
	if err != nil {
		t.Fatalf("save step status: %v", err)
	}

	// Verify step was saved
	loadedPlan, err := executor.LoadPlanWithStatus(ctx, plan.PlanID)
	if err != nil {
		t.Fatalf("load plan: %v", err)
	}

	if len(loadedPlan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(loadedPlan.Steps))
	}

	stepA := loadedPlan.Steps[0]
	if stepA.StepID != "A" {
		t.Fatalf("expected step ID A, got %s", stepA.StepID)
	}
	if stepA.Status != StepStatusCompleted {
		t.Fatalf("expected status %q, got %q", StepStatusCompleted, stepA.Status)
	}
	if stepA.StartedAt == "" {
		t.Error("expected step to have started_at")
	}
	if stepA.CompletedAt == "" {
		t.Error("expected step to have completed_at")
	}

	// Step B should remain unchanged
	stepB := loadedPlan.Steps[1]
	if stepB.Status != StepStatusApproved {
		t.Fatalf("expected step B status unchanged, got %q", stepB.Status)
	}
}

func TestSaveStepStatus_UpdatesPlanTimestamp(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	originalTime := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	plan := &Plan{
		PlanID:    "test-timestamp",
		Goal:      "Test timestamp update",
		Status:    PlanStatusExecuting,
		CreatedAt: originalTime,
		UpdatedAt: originalTime,
		Steps: []Step{
			{StepID: "A", Type: StepTypeCode, Description: "Step A", Status: StepStatusExecuting},
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

	executor := NewExecutor(q, protocol.NewEmitter(), newMockStepRunner())

	updatedStep := Step{
		StepID:      "A",
		Type:        StepTypeCode,
		Description: "Step A",
		Status:      StepStatusCompleted,
		CompletedAt: now(),
	}

	err = executor.SaveStepStatus(ctx, plan.PlanID, updatedStep)
	if err != nil {
		t.Fatalf("save step status: %v", err)
	}

	// Check updated_at was updated
	dbPlan, err := q.GetPlan(ctx, plan.PlanID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}

	if dbPlan.UpdatedAt == originalTime {
		t.Error("expected updated_at to change, but it remained the same")
	}

	updatedTime, _ := time.Parse(time.RFC3339, dbPlan.UpdatedAt)
	originalParsed, _ := time.Parse(time.RFC3339, originalTime)
	if !updatedTime.After(originalParsed) {
		t.Error("expected updated_at to be after original time")
	}
}

func TestSaveStepStatus_PlanNotFound(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	executor := NewExecutor(q, protocol.NewEmitter(), newMockStepRunner())

	step := Step{
		StepID:      "A",
		Type:        StepTypeCode,
		Description: "Step A",
		Status:      StepStatusCompleted,
	}

	err := executor.SaveStepStatus(ctx, "non-existent-plan", step)
	if err == nil {
		t.Fatal("expected error for non-existent plan")
	}
}

func TestSavePlanStatus_UpdatesStatus(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	plan := &Plan{
		PlanID:    "test-save-status",
		Goal:      "Test save plan status",
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

	executor := NewExecutor(q, protocol.NewEmitter(), newMockStepRunner())

	// Update plan status to executing
	plan.Status = PlanStatusExecuting
	err = executor.SavePlanStatus(ctx, plan, PlanStatusExecuting)
	if err != nil {
		t.Fatalf("save plan status: %v", err)
	}

	// Verify status was saved
	dbPlan, err := q.GetPlan(ctx, plan.PlanID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}

	if dbPlan.Status != string(PlanStatusExecuting) {
		t.Fatalf("expected status %q, got %q", PlanStatusExecuting, dbPlan.Status)
	}

	// Verify in-memory plan was updated
	if plan.Status != PlanStatusExecuting {
		t.Fatalf("expected in-memory status to be updated")
	}
}

func TestSavePlanStatus_AllStatusTransitions(t *testing.T) {
	statusTransitions := []struct {
		from PlanStatus
		to   PlanStatus
	}{
		{PlanStatusApproved, PlanStatusExecuting},
		{PlanStatusExecuting, PlanStatusCompleted},
		{PlanStatusExecuting, PlanStatusFailed},
		{PlanStatusPlanned, PlanStatusApproved},
		{PlanStatusApproved, PlanStatusRejected},
	}

	for _, tc := range statusTransitions {
		t.Run(string(tc.from)+"_to_"+string(tc.to), func(t *testing.T) {
			db := setupTestDB(t)
			q := world.New(db)
			ctx := context.Background()

			planID := "test-transition-" + string(tc.from) + "-" + string(tc.to)
			plan := &Plan{
				PlanID:    planID,
				Goal:      "Test transition",
				Status:    tc.from,
				CreatedAt: now(),
				UpdatedAt: now(),
				Steps:     []Step{},
			}

			content, _ := json.Marshal(plan)
			_, err := q.CreatePlan(ctx, world.CreatePlanParams{
				ID:        plan.PlanID,
				Title:     plan.Goal,
				Status:    string(tc.from),
				Content:   string(content),
				CreatedAt: plan.CreatedAt,
				UpdatedAt: plan.UpdatedAt,
			})
			if err != nil {
				t.Fatalf("create plan: %v", err)
			}

			executor := NewExecutor(q, protocol.NewEmitter(), newMockStepRunner())

			err = executor.SavePlanStatus(ctx, plan, tc.to)
			if err != nil {
				t.Fatalf("save plan status: %v", err)
			}

			dbPlan, err := q.GetPlan(ctx, plan.PlanID)
			if err != nil {
				t.Fatalf("get plan: %v", err)
			}

			if dbPlan.Status != string(tc.to) {
				t.Fatalf("expected status %q, got %q", tc.to, dbPlan.Status)
			}
		})
	}
}

func TestLoadPlanWithStatus_LoadsFromDB(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	plan := &Plan{
		PlanID:    "test-load",
		Goal:      "Test load plan",
		Status:    PlanStatusExecuting,
		CreatedAt: now(),
		UpdatedAt: now(),
		Steps: []Step{
			{StepID: "A", Type: StepTypeCode, Description: "Step A", Status: StepStatusCompleted, StartedAt: now(), CompletedAt: now()},
			{StepID: "B", Type: StepTypeCode, Description: "Step B", Status: StepStatusExecuting, StartedAt: now()},
			{StepID: "C", Type: StepTypeCode, Description: "Step C", Status: StepStatusApproved},
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

	executor := NewExecutor(q, protocol.NewEmitter(), newMockStepRunner())

	loadedPlan, err := executor.LoadPlanWithStatus(ctx, plan.PlanID)
	if err != nil {
		t.Fatalf("load plan: %v", err)
	}

	// Verify plan metadata
	if loadedPlan.PlanID != plan.PlanID {
		t.Fatalf("expected plan ID %q, got %q", plan.PlanID, loadedPlan.PlanID)
	}
	if loadedPlan.Goal != plan.Goal {
		t.Fatalf("expected goal %q, got %q", plan.Goal, loadedPlan.Goal)
	}
	if loadedPlan.Status != plan.Status {
		t.Fatalf("expected status %q, got %q", plan.Status, loadedPlan.Status)
	}

	// Verify steps
	if len(loadedPlan.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(loadedPlan.Steps))
	}

	// Verify step A (completed)
	stepA := loadedPlan.Steps[0]
	if stepA.Status != StepStatusCompleted {
		t.Fatalf("expected step A status %q, got %q", StepStatusCompleted, stepA.Status)
	}
	if stepA.StartedAt == "" {
		t.Error("expected step A to have started_at")
	}
	if stepA.CompletedAt == "" {
		t.Error("expected step A to have completed_at")
	}

	// Verify step B (executing)
	stepB := loadedPlan.Steps[1]
	if stepB.Status != StepStatusExecuting {
		t.Fatalf("expected step B status %q, got %q", StepStatusExecuting, stepB.Status)
	}
	if stepB.StartedAt == "" {
		t.Error("expected step B to have started_at")
	}

	// Verify step C (approved)
	stepC := loadedPlan.Steps[2]
	if stepC.Status != StepStatusApproved {
		t.Fatalf("expected step C status %q, got %q", StepStatusApproved, stepC.Status)
	}
}

func TestLoadPlanWithStatus_PlanNotFound(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	executor := NewExecutor(q, protocol.NewEmitter(), newMockStepRunner())

	_, err := executor.LoadPlanWithStatus(ctx, "non-existent-plan")
	if err == nil {
		t.Fatal("expected error for non-existent plan")
	}
}

func TestCanResume_ExecutingPlan(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	plan := &Plan{
		PlanID:    "test-resume-executing",
		Goal:      "Test can resume",
		Status:    PlanStatusExecuting,
		CreatedAt: now(),
		UpdatedAt: now(),
		Steps: []Step{
			{StepID: "A", Type: StepTypeCode, Description: "Step A", Status: StepStatusCompleted},
			{StepID: "B", Type: StepTypeCode, Description: "Step B", Status: StepStatusExecuting},
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

	executor := NewExecutor(q, protocol.NewEmitter(), newMockStepRunner())

	canResume, err := executor.CanResume(ctx, plan.PlanID)
	if err != nil {
		t.Fatalf("can resume: %v", err)
	}

	if !canResume {
		t.Error("expected CanResume to return true for executing plan")
	}
}

func TestCanResume_NonExecutingPlans(t *testing.T) {
	nonResumableStatuses := []PlanStatus{
		PlanStatusPlanned,
		PlanStatusWaitingApproval,
		PlanStatusApproved,
		PlanStatusCompleted,
		PlanStatusRejected,
		PlanStatusFailed,
	}

	for _, status := range nonResumableStatuses {
		t.Run(string(status), func(t *testing.T) {
			db := setupTestDB(t)
			q := world.New(db)
			ctx := context.Background()

			planID := "test-resume-" + string(status)
			plan := &Plan{
				PlanID:    planID,
				Goal:      "Test cannot resume",
				Status:    status,
				CreatedAt: now(),
				UpdatedAt: now(),
				Steps:     []Step{},
			}

			content, _ := json.Marshal(plan)
			_, err := q.CreatePlan(ctx, world.CreatePlanParams{
				ID:        plan.PlanID,
				Title:     plan.Goal,
				Status:    string(status),
				Content:   string(content),
				CreatedAt: plan.CreatedAt,
				UpdatedAt: plan.UpdatedAt,
			})
			if err != nil {
				t.Fatalf("create plan: %v", err)
			}

			executor := NewExecutor(q, protocol.NewEmitter(), newMockStepRunner())

			canResume, err := executor.CanResume(ctx, plan.PlanID)
			if err != nil {
				t.Fatalf("can resume: %v", err)
			}

			if canResume {
				t.Errorf("expected CanResume to return false for %s plan", status)
			}
		})
	}
}

func TestCanResume_PlanNotFound(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	executor := NewExecutor(q, protocol.NewEmitter(), newMockStepRunner())

	_, err := executor.CanResume(ctx, "non-existent-plan")
	if err == nil {
		t.Fatal("expected error for non-existent plan")
	}
}

func TestPersistenceIntegration_ResumeInterruptedPlan(t *testing.T) {
	// This test simulates a plan that was interrupted and then resumed
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	planID := "test-interrupted"
	plan := &Plan{
		PlanID:    planID,
		Goal:      "Test interrupted plan",
		Status:    PlanStatusExecuting,
		CreatedAt: now(),
		UpdatedAt: now(),
		Steps: []Step{
			{StepID: "A", Type: StepTypeCode, Description: "Step A", Status: StepStatusCompleted, StartedAt: now(), CompletedAt: now()},
			{StepID: "B", Type: StepTypeCode, Description: "Step B", Status: StepStatusFailed, StartedAt: now(), CompletedAt: now(), Error: "interrupted"},
			{StepID: "C", Type: StepTypeCode, Description: "Step C", Status: StepStatusApproved},
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

	executor := NewExecutor(q, protocol.NewEmitter(), newMockStepRunner())

	// Check if we can resume
	canResume, err := executor.CanResume(ctx, planID)
	if err != nil {
		t.Fatalf("can resume: %v", err)
	}
	if !canResume {
		t.Fatal("expected to be able to resume interrupted plan")
	}

	// Load the plan to see its state
	loadedPlan, err := executor.LoadPlanWithStatus(ctx, planID)
	if err != nil {
		t.Fatalf("load plan: %v", err)
	}

	// Verify the interrupted state
	if loadedPlan.Status != PlanStatusExecuting {
		t.Fatalf("expected status %q, got %q", PlanStatusExecuting, loadedPlan.Status)
	}
	if loadedPlan.Steps[0].Status != StepStatusCompleted {
		t.Error("expected step A to be completed")
	}
	if loadedPlan.Steps[1].Status != StepStatusFailed {
		t.Error("expected step B to be failed")
	}
	if loadedPlan.Steps[1].Error != "interrupted" {
		t.Errorf("expected error 'interrupted', got %q", loadedPlan.Steps[1].Error)
	}
	if loadedPlan.Steps[2].Status != StepStatusApproved {
		t.Error("expected step C to be approved (not started)")
	}

	// Simulate fixing step B and resuming
	fixedStepB := Step{
		StepID:      "B",
		Type:        StepTypeCode,
		Description: "Step B",
		Status:      StepStatusApproved, // Reset to approved for retry
		StartedAt:   "",
		CompletedAt: "",
		Error:       "",
	}

	err = executor.SaveStepStatus(ctx, planID, fixedStepB)
	if err != nil {
		t.Fatalf("save step status: %v", err)
	}

	// Reload and verify fix
	reloadedPlan, err := executor.LoadPlanWithStatus(ctx, planID)
	if err != nil {
		t.Fatalf("reload plan: %v", err)
	}

	if reloadedPlan.Steps[1].Status != StepStatusApproved {
		t.Errorf("expected step B to be reset to approved, got %q", reloadedPlan.Steps[1].Status)
	}
	if reloadedPlan.Steps[1].Error != "" {
		t.Error("expected step B error to be cleared")
	}
}

func TestPersistenceIntegration_StepProgression(t *testing.T) {
	// Test simulating step-by-step progress updates
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	planID := "test-progression"
	plan := &Plan{
		PlanID:    planID,
		Goal:      "Test step progression",
		Status:    PlanStatusExecuting,
		CreatedAt: now(),
		UpdatedAt: now(),
		Steps: []Step{
			{StepID: "A", Type: StepTypeCode, Description: "Step A", Status: StepStatusApproved},
			{StepID: "B", Type: StepTypeCode, Description: "Step B", Status: StepStatusApproved},
		},
	}

	content, _ := json.Marshal(plan)
	_, err := q.CreatePlan(ctx, world.CreatePlanParams{
		ID:        plan.PlanID,
		Title:     plan.Goal,
		Status:    string(PlanStatusExecuting),
		Content:   string(content),
		CreatedAt: plan.CreatedAt,
		UpdatedAt: plan.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	executor := NewExecutor(q, protocol.NewEmitter(), newMockStepRunner())

	// Step 1: Mark A as executing
	stepAExecuting := Step{
		StepID:    "A",
		Type:      StepTypeCode,
		Status:    StepStatusExecuting,
		StartedAt: now(),
	}
	err = executor.SaveStepStatus(ctx, planID, stepAExecuting)
	if err != nil {
		t.Fatalf("save step A executing: %v", err)
	}

	loadedPlan, _ := executor.LoadPlanWithStatus(ctx, planID)
	if loadedPlan.Steps[0].Status != StepStatusExecuting {
		t.Error("step A should be executing")
	}

	// Step 2: Mark A as completed
	stepACompleted := Step{
		StepID:      "A",
		Type:        StepTypeCode,
		Status:      StepStatusCompleted,
		StartedAt:   stepAExecuting.StartedAt,
		CompletedAt: now(),
	}
	err = executor.SaveStepStatus(ctx, planID, stepACompleted)
	if err != nil {
		t.Fatalf("save step A completed: %v", err)
	}

	loadedPlan, _ = executor.LoadPlanWithStatus(ctx, planID)
	if loadedPlan.Steps[0].Status != StepStatusCompleted {
		t.Error("step A should be completed")
	}

	// Step 3: Mark B as executing
	stepBExecuting := Step{
		StepID:    "B",
		Type:      StepTypeCode,
		Status:    StepStatusExecuting,
		StartedAt: now(),
	}
	err = executor.SaveStepStatus(ctx, planID, stepBExecuting)
	if err != nil {
		t.Fatalf("save step B executing: %v", err)
	}

	loadedPlan, _ = executor.LoadPlanWithStatus(ctx, planID)
	if loadedPlan.Steps[1].Status != StepStatusExecuting {
		t.Error("step B should be executing")
	}

	// Step 4: Mark plan as completed
	err = executor.SavePlanStatus(ctx, loadedPlan, PlanStatusCompleted)
	if err != nil {
		t.Fatalf("save plan completed: %v", err)
	}

	loadedPlan, _ = executor.LoadPlanWithStatus(ctx, planID)
	if loadedPlan.Status != PlanStatusCompleted {
		t.Error("plan should be completed")
	}
}
