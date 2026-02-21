package plan

import (
	"fmt"
)

type StepType string

const (
	StepTypeCode       StepType = "CODE"
	StepTypeToolCall   StepType = "TOOL_CALL"
	StepTypeReasoning  StepType = "REASONING"
	StepTypeHumanInput StepType = "HUMAN_INPUT"
)

type PlanStatus string

const (
	PlanStatusPlanned         PlanStatus = "planned"
	PlanStatusWaitingApproval PlanStatus = "waiting_approval"
	PlanStatusApproved        PlanStatus = "approved"
	PlanStatusExecuting       PlanStatus = "executing"
	PlanStatusCompleted       PlanStatus = "completed"
	PlanStatusRejected        PlanStatus = "rejected"
	PlanStatusFailed          PlanStatus = "failed"
)

type StepStatus string

const (
	StepStatusPlanned            StepStatus = "planned"
	StepStatusWaitingApproval    StepStatus = "waiting_approval"
	StepStatusApproved           StepStatus = "approved"
	StepStatusRejected           StepStatus = "rejected"
	StepStatusExecuting          StepStatus = "executing"
	StepStatusCompleted          StepStatus = "completed"
	StepStatusFailed             StepStatus = "failed"
	StepStatusFailedNonResumable StepStatus = "failed_non_resumable"
)

type Plan struct {
	PlanID    string         `json:"plan_id"`
	Goal      string         `json:"goal"`
	Steps     []Step         `json:"steps"`
	Status    PlanStatus     `json:"status"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type Step struct {
	StepID      string         `json:"step_id"`
	Type        StepType       `json:"type"`
	Description string         `json:"description"`
	DependsOn   []string       `json:"depends_on"`
	Status      StepStatus     `json:"status"`
	Payload     map[string]any `json:"payload,omitempty"`
	Validation  map[string]any `json:"validation,omitempty"`
	Error       string         `json:"error,omitempty"`
	StartedAt   string         `json:"started_at,omitempty"`
	CompletedAt string         `json:"completed_at,omitempty"`
}

func (p Plan) Validate() []error {
	errList := make([]error, 0)

	if p.PlanID == "" {
		errList = append(errList, fmt.Errorf("plan.plan_id is required"))
	}
	if p.Goal == "" {
		errList = append(errList, fmt.Errorf("plan.goal is required"))
	}
	if len(p.Steps) == 0 {
		errList = append(errList, fmt.Errorf("plan.steps is required"))
	}
	if p.Status == "" {
		errList = append(errList, fmt.Errorf("plan.status is required"))
	}
	if p.CreatedAt == "" {
		errList = append(errList, fmt.Errorf("plan.created_at is required"))
	}
	if p.UpdatedAt == "" {
		errList = append(errList, fmt.Errorf("plan.updated_at is required"))
	}

	idIndex := make(map[string]int, len(p.Steps))
	for i, step := range p.Steps {
		prefix := fmt.Sprintf("plan.steps[%d]", i)

		if step.StepID == "" {
			errList = append(errList, fmt.Errorf("%s.step_id is required", prefix))
		} else {
			if firstIndex, exists := idIndex[step.StepID]; exists {
				errList = append(errList, fmt.Errorf("duplicate step_id %q at index %d (first seen at index %d)", step.StepID, i, firstIndex))
			} else {
				idIndex[step.StepID] = i
			}
		}

		if step.Type == "" {
			errList = append(errList, fmt.Errorf("%s.type is required", prefix))
		}
		if step.Description == "" {
			errList = append(errList, fmt.Errorf("%s.description is required", prefix))
		}
		if step.Status == "" {
			errList = append(errList, fmt.Errorf("%s.status is required", prefix))
		}
	}

	for i, step := range p.Steps {
		for _, depID := range step.DependsOn {
			if _, exists := idIndex[depID]; !exists {
				errList = append(errList, fmt.Errorf("plan.steps[%d] depends_on references unknown step_id %q", i, depID))
			}
		}
	}

	if cycleErr := validateNoCycles(p.Steps); cycleErr != nil {
		errList = append(errList, cycleErr)
	}

	return errList
}

func validateNoCycles(steps []Step) error {
	visited := make(map[string]bool, len(steps))
	inStack := make(map[string]bool, len(steps))

	stepByID := make(map[string]Step, len(steps))
	for _, step := range steps {
		if step.StepID == "" {
			continue
		}
		if _, exists := stepByID[step.StepID]; !exists {
			stepByID[step.StepID] = step
		}
	}

	var visit func(stepID string) error
	visit = func(stepID string) error {
		if inStack[stepID] {
			return fmt.Errorf("cycle detected at step_id %q", stepID)
		}
		if visited[stepID] {
			return nil
		}

		visited[stepID] = true
		inStack[stepID] = true

		step, ok := stepByID[stepID]
		if !ok {
			inStack[stepID] = false
			return nil
		}

		for _, depID := range step.DependsOn {
			if _, depExists := stepByID[depID]; !depExists {
				continue
			}
			if err := visit(depID); err != nil {
				return err
			}
		}

		inStack[stepID] = false
		return nil
	}

	for _, step := range steps {
		if step.StepID == "" {
			continue
		}
		if err := visit(step.StepID); err != nil {
			return err
		}
	}

	return nil
}
