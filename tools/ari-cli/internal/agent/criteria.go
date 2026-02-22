package agent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/plan"
)

var (
	ErrCriteriaStepIDRequired      = errors.New("step id is required")
	ErrCriteriaDescriptionRequired = errors.New("step description is required")
	ErrCriteriaUnknownStepType     = errors.New("unknown step type")
)

// Criterion represents a single acceptance criterion.
type Criterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Checkable   bool   `json:"checkable"`
	StepID      string `json:"step_id"`
}

// CriteriaGenerator generates acceptance criteria for steps.
type CriteriaGenerator struct{}

// NewCriteriaGenerator creates a new generator.
func NewCriteriaGenerator() *CriteriaGenerator {
	return &CriteriaGenerator{}
}

// GenerateCriteria creates criteria for a given step.
func (g *CriteriaGenerator) GenerateCriteria(step plan.Step) ([]Criterion, error) {
	if strings.TrimSpace(step.StepID) == "" {
		return nil, ErrCriteriaStepIDRequired
	}

	if strings.TrimSpace(step.Description) == "" {
		return nil, ErrCriteriaDescriptionRequired
	}

	description := strings.TrimSpace(step.Description)

	var criterionDescriptions []string
	switch step.Type {
	case plan.StepTypeCode:
		criterionDescriptions = []string{
			"Code compiles without errors",
			"Code follows project conventions",
			fmt.Sprintf("Implementation satisfies step objective: %s", description),
		}
	case plan.StepTypeToolCall:
		criterionDescriptions = []string{
			"Tool executes successfully",
			"Tool returns expected output",
		}
	case plan.StepTypeHumanInput:
		criterionDescriptions = []string{
			"User input received and validated",
		}
	case plan.StepTypeReasoning:
		criterionDescriptions = []string{
			"Reasoning documented clearly",
		}
	default:
		return nil, fmt.Errorf("%w: %q", ErrCriteriaUnknownStepType, step.Type)
	}

	criteria := make([]Criterion, 0, len(criterionDescriptions))
	for i, criterionDescription := range criterionDescriptions {
		criteria = append(criteria, Criterion{
			ID:          fmt.Sprintf("%s-crit-%d", step.StepID, i+1),
			Description: criterionDescription,
			Checkable:   true,
			StepID:      step.StepID,
		})
	}

	return criteria, nil
}
