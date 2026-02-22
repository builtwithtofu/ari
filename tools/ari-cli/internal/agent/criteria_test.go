package agent

import (
	"errors"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/plan"
)

func TestGenerateCriteriaByStepType(t *testing.T) {
	generator := NewCriteriaGenerator()

	tests := []struct {
		name         string
		step         plan.Step
		wantCriteria []Criterion
	}{
		{
			name: "code step",
			step: plan.Step{
				StepID:      "step-1",
				Type:        plan.StepTypeCode,
				Description: "Implement auth middleware",
			},
			wantCriteria: []Criterion{
				{ID: "step-1-crit-1", Description: "Code compiles without errors", Checkable: true, StepID: "step-1"},
				{ID: "step-1-crit-2", Description: "Code follows project conventions", Checkable: true, StepID: "step-1"},
				{ID: "step-1-crit-3", Description: "Implementation satisfies step objective: Implement auth middleware", Checkable: true, StepID: "step-1"},
			},
		},
		{
			name: "tool call step",
			step: plan.Step{
				StepID:      "step-2",
				Type:        plan.StepTypeToolCall,
				Description: "Run formatter",
			},
			wantCriteria: []Criterion{
				{ID: "step-2-crit-1", Description: "Tool executes successfully", Checkable: true, StepID: "step-2"},
				{ID: "step-2-crit-2", Description: "Tool returns expected output", Checkable: true, StepID: "step-2"},
			},
		},
		{
			name: "human input step",
			step: plan.Step{
				StepID:      "step-3",
				Type:        plan.StepTypeHumanInput,
				Description: "Confirm deployment window",
			},
			wantCriteria: []Criterion{
				{ID: "step-3-crit-1", Description: "User input received and validated", Checkable: true, StepID: "step-3"},
			},
		},
		{
			name: "reasoning step",
			step: plan.Step{
				StepID:      "step-4",
				Type:        plan.StepTypeReasoning,
				Description: "Choose migration strategy",
			},
			wantCriteria: []Criterion{
				{ID: "step-4-crit-1", Description: "Reasoning documented clearly", Checkable: true, StepID: "step-4"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			criteria, err := generator.GenerateCriteria(tt.step)
			if err != nil {
				t.Fatalf("GenerateCriteria returned error: %v", err)
			}

			if len(criteria) != len(tt.wantCriteria) {
				t.Fatalf("criteria length = %d, want %d", len(criteria), len(tt.wantCriteria))
			}

			for i := range tt.wantCriteria {
				if criteria[i] != tt.wantCriteria[i] {
					t.Fatalf("criteria[%d] = %#v, want %#v", i, criteria[i], tt.wantCriteria[i])
				}
				if !criteria[i].Checkable {
					t.Fatalf("criteria[%d].Checkable = false, want true", i)
				}
			}
		})
	}
}

func TestGenerateCriteriaErrors(t *testing.T) {
	generator := NewCriteriaGenerator()

	tests := []struct {
		name    string
		step    plan.Step
		wantErr error
	}{
		{
			name: "missing step id",
			step: plan.Step{
				Type:        plan.StepTypeCode,
				Description: "Implement parser",
			},
			wantErr: ErrCriteriaStepIDRequired,
		},
		{
			name: "missing description",
			step: plan.Step{
				StepID: "step-1",
				Type:   plan.StepTypeCode,
			},
			wantErr: ErrCriteriaDescriptionRequired,
		},
		{
			name: "unknown step type",
			step: plan.Step{
				StepID:      "step-2",
				Type:        plan.StepType("UNKNOWN"),
				Description: "unknown",
			},
			wantErr: ErrCriteriaUnknownStepType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			criteria, err := generator.GenerateCriteria(tt.step)
			if err == nil {
				t.Fatal("GenerateCriteria returned nil error")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want wrapped %v", err, tt.wantErr)
			}
			if criteria != nil {
				t.Fatalf("criteria = %#v, want nil", criteria)
			}
		})
	}
}
