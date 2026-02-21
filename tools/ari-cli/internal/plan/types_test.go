package plan

import (
	"testing"
)

func TestPlanValidate_ValidPlan(t *testing.T) {
	plan := validPlan()

	errs := plan.Validate()
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %d", len(errs))
	}
}

func TestPlanValidate_RequiredFields(t *testing.T) {
	plan := Plan{}

	errs := plan.Validate()
	if len(errs) != 6 {
		t.Fatalf("expected 6 errors, got %d", len(errs))
	}

	assertErrEq(t, errs[0], "plan.plan_id is required")
	assertErrEq(t, errs[1], "plan.goal is required")
	assertErrEq(t, errs[2], "plan.steps is required")
	assertErrEq(t, errs[3], "plan.status is required")
	assertErrEq(t, errs[4], "plan.created_at is required")
	assertErrEq(t, errs[5], "plan.updated_at is required")
}

func TestPlanValidate_DuplicateStepIDs(t *testing.T) {
	plan := validPlan()
	plan.Steps = []Step{
		{
			StepID:      "step-1",
			Type:        StepTypeReasoning,
			Description: "Analyze",
			Status:      StepStatusCompleted,
		},
		{
			StepID:      "step-1",
			Type:        StepTypeCode,
			Description: "Implement",
			DependsOn:   []string{"step-1"},
			Status:      StepStatusPlanned,
		},
	}

	errs := plan.Validate()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}

	assertErrEq(t, errs[0], "duplicate step_id \"step-1\" at index 1 (first seen at index 0)")
}

func TestPlanValidate_DependencyMustExist(t *testing.T) {
	plan := validPlan()
	plan.Steps[1].DependsOn = []string{"missing-step"}

	errs := plan.Validate()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}

	assertErrEq(t, errs[0], "plan.steps[1] depends_on references unknown step_id \"missing-step\"")
}

func TestPlanValidate_DetectsCycle(t *testing.T) {
	plan := validPlan()
	plan.Steps = []Step{
		{
			StepID:      "step-a",
			Type:        StepTypeReasoning,
			Description: "A",
			DependsOn:   []string{"step-c"},
			Status:      StepStatusPlanned,
		},
		{
			StepID:      "step-b",
			Type:        StepTypeCode,
			Description: "B",
			DependsOn:   []string{"step-a"},
			Status:      StepStatusPlanned,
		},
		{
			StepID:      "step-c",
			Type:        StepTypeToolCall,
			Description: "C",
			DependsOn:   []string{"step-b"},
			Status:      StepStatusPlanned,
		},
	}

	errs := plan.Validate()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}

	assertErrEq(t, errs[0], "cycle detected at step_id \"step-a\"")
}

func TestPlanValidate_StepRequiredFields(t *testing.T) {
	plan := validPlan()
	plan.Steps = []Step{{}}

	errs := plan.Validate()
	if len(errs) != 4 {
		t.Fatalf("expected 4 errors, got %d", len(errs))
	}

	assertErrEq(t, errs[0], "plan.steps[0].step_id is required")
	assertErrEq(t, errs[1], "plan.steps[0].type is required")
	assertErrEq(t, errs[2], "plan.steps[0].description is required")
	assertErrEq(t, errs[3], "plan.steps[0].status is required")
}

func validPlan() Plan {
	return Plan{
		PlanID:    "plan-1",
		Goal:      "Implement feature",
		Status:    PlanStatusPlanned,
		CreatedAt: "2026-02-22T10:00:00Z",
		UpdatedAt: "2026-02-22T10:05:00Z",
		Steps: []Step{
			{
				StepID:      "step-1",
				Type:        StepTypeReasoning,
				Description: "Analyze",
				Status:      StepStatusCompleted,
			},
			{
				StepID:      "step-2",
				Type:        StepTypeCode,
				Description: "Implement",
				DependsOn:   []string{"step-1"},
				Status:      StepStatusPlanned,
			},
		},
	}
}

func assertErrEq(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %q, got nil", want)
	}
	if err.Error() != want {
		t.Fatalf("expected error %q, got %q", want, err.Error())
	}
}
