package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/plan"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/provider"
)

func TestRefineApproveSavesPlan(t *testing.T) {
	queries := setupResearchTestWorld(t)
	refiner := NewRefiner(provider.NewSimulator(provider.PlanningScenario(nil)), queries)

	currentPlan := &plan.Plan{
		PlanID: "plan-approve",
		Goal:   "Implement iterative planning",
		Status: plan.PlanStatusWaitingApproval,
		Steps: []plan.Step{
			{
				StepID:      "s1",
				Type:        plan.StepTypeReasoning,
				Description: "Draft questions",
				Status:      plan.StepStatusPlanned,
			},
		},
	}

	result, err := refiner.Refine(context.Background(), currentPlan, nil, "approve")
	if err != nil {
		t.Fatalf("Refine returned error: %v", err)
	}

	if !result.Complete {
		t.Fatal("result.Complete = false, want true")
	}
	if result.Plan.Status != plan.PlanStatusApproved {
		t.Fatalf("result.Plan.Status = %q, want %q", result.Plan.Status, plan.PlanStatusApproved)
	}

	saved, err := queries.GetPlan(context.Background(), "plan-approve")
	if err != nil {
		t.Fatalf("GetPlan returned error: %v", err)
	}
	if saved.Status != string(plan.PlanStatusApproved) {
		t.Fatalf("saved status = %q, want %q", saved.Status, plan.PlanStatusApproved)
	}

	var stored plan.Plan
	if err := json.Unmarshal([]byte(saved.Content), &stored); err != nil {
		t.Fatalf("unmarshal saved content: %v", err)
	}
	if stored.PlanID != "plan-approve" {
		t.Fatalf("stored plan id = %q, want %q", stored.PlanID, "plan-approve")
	}
}

func TestRefineResearchMoreSetsFlag(t *testing.T) {
	refiner := NewRefiner(provider.NewSimulator(provider.PlanningScenario(nil)), nil)

	result, err := refiner.Refine(context.Background(), &plan.Plan{PlanID: "plan-research", Goal: "Goal"}, nil, "research more")
	if err != nil {
		t.Fatalf("Refine returned error: %v", err)
	}

	if !result.NeedsMoreResearch {
		t.Fatal("result.NeedsMoreResearch = false, want true")
	}
	if result.Complete {
		t.Fatal("result.Complete = true, want false")
	}
}

func TestRefineUpdateAppliesAnswers(t *testing.T) {
	refiner := NewRefiner(provider.NewSimulator(provider.PlanningScenario(nil)), nil)

	currentPlan := &plan.Plan{PlanID: "plan-update", Goal: "Goal"}
	answers := []plan.Answer{
		{QuestionID: "q-1", Type: plan.ResponseTypeAnswer, Content: "Need rollback support"},
		{QuestionID: "q-2", Type: plan.ResponseTypeSkip},
	}

	result, err := refiner.Refine(context.Background(), currentPlan, answers, "update")
	if err != nil {
		t.Fatalf("Refine returned error: %v", err)
	}

	if result.Command != "update" {
		t.Fatalf("result.Command = %q, want %q", result.Command, "update")
	}

	contextValue, ok := result.Plan.Metadata["refinement_context"].(string)
	if !ok {
		t.Fatalf("refinement_context type = %T, want string", result.Plan.Metadata["refinement_context"])
	}
	if contextValue != "q-1: Need rollback support" {
		t.Fatalf("refinement_context = %q, want %q", contextValue, "q-1: Need rollback support")
	}
}

func TestRefineGapAnalysisSetsIndicator(t *testing.T) {
	refiner := NewRefiner(provider.NewSimulator(provider.PlanningScenario(nil)), nil)

	result, err := refiner.Refine(context.Background(), &plan.Plan{PlanID: "plan-gap", Goal: "Goal"}, nil, "gap analysis")
	if err != nil {
		t.Fatalf("Refine returned error: %v", err)
	}

	needsGapAnalysis, ok := result.Plan.Metadata["needs_gap_analysis"].(bool)
	if !ok {
		t.Fatalf("needs_gap_analysis type = %T, want bool", result.Plan.Metadata["needs_gap_analysis"])
	}
	if !needsGapAnalysis {
		t.Fatal("needs_gap_analysis = false, want true")
	}
	if result.Command != "gap analysis" {
		t.Fatalf("result.Command = %q, want %q", result.Command, "gap analysis")
	}
}
