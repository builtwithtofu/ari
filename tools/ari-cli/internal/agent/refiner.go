package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/plan"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/provider"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/world"
)

const (
	refinementCommandApprove      = "approve"
	refinementCommandResearchMore = "research more"
	refinementCommandGapAnalysis  = "gap analysis"
	refinementCommandUpdate       = "update"
)

var (
	ErrRefinerPlanRequired      = errors.New("current plan is required")
	ErrRefinerUnknownCommand    = errors.New("unknown refinement command")
	ErrRefinerWorldNotAvailable = errors.New("world db is required for approval")
)

type Refiner struct {
	provider provider.Provider
	world    *world.Queries
}

func NewRefiner(provider provider.Provider, worldQueries *world.Queries) *Refiner {
	return &Refiner{provider: provider, world: worldQueries}
}

type RefinementResult struct {
	Plan              *plan.Plan
	Complete          bool
	NeedsMoreResearch bool
	Command           string
}

func (r *Refiner) Refine(ctx context.Context, currentPlan *plan.Plan, answers []plan.Answer, command string) (*RefinementResult, error) {
	if currentPlan == nil {
		return nil, ErrRefinerPlanRequired
	}

	normalizedCommand := strings.ToLower(strings.TrimSpace(command))
	if normalizedCommand == "" {
		normalizedCommand = refinementCommandUpdate
	}

	refined := applyAnswers(currentPlan, answers)

	result := &RefinementResult{
		Plan:    refined,
		Command: normalizedCommand,
	}

	switch normalizedCommand {
	case refinementCommandApprove:
		refined.Status = plan.PlanStatusApproved
		if err := r.savePlan(ctx, refined); err != nil {
			return nil, err
		}
		result.Complete = true
	case refinementCommandResearchMore:
		result.NeedsMoreResearch = true
	case refinementCommandGapAnalysis:
		setMetadataValue(refined, "needs_gap_analysis", true)
	case refinementCommandUpdate:
	default:
		return nil, fmt.Errorf("%w: %q", ErrRefinerUnknownCommand, command)
	}

	return result, nil
}

func applyAnswers(currentPlan *plan.Plan, answers []plan.Answer) *plan.Plan {
	refined := *currentPlan

	if len(currentPlan.Steps) > 0 {
		refined.Steps = append([]plan.Step(nil), currentPlan.Steps...)
	}

	if currentPlan.Metadata != nil {
		refined.Metadata = copyMetadata(currentPlan.Metadata)
	}

	noteParts := make([]string, 0, len(answers))
	for _, answer := range answers {
		if answer.Type != plan.ResponseTypeAnswer {
			continue
		}

		content := strings.TrimSpace(answer.Content)
		if content == "" {
			continue
		}

		noteParts = append(noteParts, fmt.Sprintf("%s: %s", answer.QuestionID, content))
	}

	if len(noteParts) == 0 {
		return &refined
	}

	setMetadataValue(&refined, "last_refinement_answers", answers)
	setMetadataValue(&refined, "refinement_context", strings.Join(noteParts, " | "))

	return &refined
}

func copyMetadata(metadata map[string]any) map[string]any {
	copied := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copied[key] = value
	}

	return copied
}

func setMetadataValue(p *plan.Plan, key string, value any) {
	if p.Metadata == nil {
		p.Metadata = make(map[string]any)
	}
	p.Metadata[key] = value
}

func (r *Refiner) savePlan(ctx context.Context, refined *plan.Plan) error {
	if r.world == nil {
		return ErrRefinerWorldNotAvailable
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if refined.CreatedAt == "" {
		refined.CreatedAt = now
	}
	refined.UpdatedAt = now

	content, err := json.Marshal(refined)
	if err != nil {
		return err
	}

	_, err = r.world.CreatePlan(ctx, world.CreatePlanParams{
		ID:        refined.PlanID,
		Title:     refined.Goal,
		Status:    string(refined.Status),
		Content:   string(content),
		CreatedAt: refined.CreatedAt,
		UpdatedAt: refined.UpdatedAt,
	})
	if err != nil {
		return err
	}

	return nil
}
