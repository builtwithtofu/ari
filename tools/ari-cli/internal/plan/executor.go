package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/world"
)

// RetryConfig configures retry behavior
type RetryConfig struct {
	MaxRetries int           // Maximum retry attempts (default: 3)
	BaseDelay  time.Duration // Initial delay (default: 1s)
	MaxDelay   time.Duration // Maximum delay (default: 30s)
	Multiplier float64       // Exponential multiplier (default: 2.0)
}

// DefaultRetryConfig returns the default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		BaseDelay:  1 * time.Second,
		MaxDelay:   30 * time.Second,
		Multiplier: 2.0,
	}
}

// StepError represents a step execution error
type StepError struct {
	StepID    string
	Cause     error
	Retryable bool
}

func (e *StepError) Unwrap() error {
	return e.Cause
}

func (e *StepError) Error() string {
	if e.Retryable {
		return fmt.Sprintf("step %q failed (retryable): %v", e.StepID, e.Cause)
	}
	return fmt.Sprintf("step %q failed (non-retryable): %v", e.StepID, e.Cause)
}

// RetryableError is an error type that can be marked as retryable
type RetryableError struct {
	Wrapped error
}
func (e *RetryableError) Error() string {
	return e.Wrapped.Error()
}
func (e *RetryableError) Unwrap() error {
	return e.Wrapped
}

// MarkRetryable wraps an error to indicate it is retryable
func MarkRetryable(err error) error {
	if err == nil {
		return nil
	}
	return &RetryableError{Wrapped: err}
}

// IsRetryableError determines if an error should be retried
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for context cancellation - not retryable
	if errors.Is(err, context.Canceled) {
		return false
	}

	// Check for context deadline exceeded - retryable (temporary timeout)
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Check if error explicitly implements retryable interface
	var retryable interface{ IsRetryable() bool }
	if errors.As(err, &retryable) {
		return retryable.IsRetryable()
	}

	// Check if error is wrapped with RetryableError
	var re *RetryableError
	if errors.As(err, &re) {
		return true
	}

	// Check for network-related errors (simplified check)
	errStr := err.Error()
	networkKeywords := []string{
		"timeout",
		"temporary",
		"unavailable",
		"connection refused",
		"no such host",
		"rate limit",
		"too many requests",
	}

	for _, keyword := range networkKeywords {
		if containsSubstring(errStr, keyword) {
			return true
		}
	}

	// Non-retryable by default
	return false
}

// containsSubstring is a simple case-insensitive substring check
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && containsLower(s, substr)
}

func containsLower(s, substr string) bool {
	sLower := toLower(s)
	subLower := toLower(substr)
	for i := 0; i <= len(sLower)-len(subLower); i++ {
		if sLower[i:i+len(subLower)] == subLower {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c = c + ('a' - 'A')
		}
		result[i] = c
	}
	return string(result)
}

// calculateBackoff calculates the delay for a given attempt using exponential backoff
func calculateBackoff(config RetryConfig, attempt int) time.Duration {
	if attempt < 0 {
		return config.BaseDelay
	}

	// Calculate: baseDelay * multiplier^attempt
	delay := float64(config.BaseDelay)
	for i := 0; i < attempt; i++ {
		delay *= config.Multiplier
		if delay > float64(config.MaxDelay) {
			return config.MaxDelay
		}
	}

	d := time.Duration(delay)
	if d > config.MaxDelay {
		return config.MaxDelay
	}
	return d
}

// StepRunner executes a single step
type StepRunner interface {
	Run(ctx context.Context, step Step) error
}

// Executor runs plans in DAG order
type Executor struct {
	world   *world.Queries
	emitter *protocol.Emitter
	runner  StepRunner
}

// ExecutionResult tracks execution outcome
type ExecutionResult struct {
	PlanID      string
	Success     bool
	StepsRun    int
	StepsFailed int
	Error       error
}

// NewExecutor creates executor with dependencies
func NewExecutor(world *world.Queries, emitter *protocol.Emitter, runner StepRunner) *Executor {
	return &Executor{
		world:   world,
		emitter: emitter,
		runner:  runner,
	}
}

// executeStepWithRetry runs a step with retry logic
func (e *Executor) executeStepWithRetry(ctx context.Context, step Step, config RetryConfig) error {
	var lastErr error

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// Try executing the step
		if err := e.runner.Run(ctx, step); err != nil {
			lastErr = err

			// Check if error is retryable
			if !IsRetryableError(err) {
				return &StepError{
					StepID:    step.StepID,
					Cause:     err,
					Retryable: false,
				}
			}

			// Check if we've exhausted retries
			if attempt >= config.MaxRetries {
				return &StepError{
					StepID:    step.StepID,
					Cause:     err,
					Retryable: true,
				}
			}

			// Calculate backoff delay
			delay := calculateBackoff(config, attempt)

			// Wait before retrying
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return &StepError{
					StepID:    step.StepID,
					Cause:     ctx.Err(),
					Retryable: false,
				}
			case <-timer.C:
				// Continue to next retry
			}
		} else {
			// Success!
			return nil
		}
	}

	return &StepError{
		StepID:    step.StepID,
		Cause:     lastErr,
		Retryable: true,
	}
}

// Run executes a plan in topological order
func (e *Executor) Run(ctx context.Context, plan *Plan) (*ExecutionResult, error) {
	return e.RunWithConfig(ctx, plan, DefaultRetryConfig())
}

// RunWithConfig executes a plan with custom retry configuration
func (e *Executor) RunWithConfig(ctx context.Context, plan *Plan, retryConfig RetryConfig) (*ExecutionResult, error) {
	result := &ExecutionResult{
		PlanID: plan.PlanID,
	}

	// Sort steps using topological sort
	orderedSteps, err := TopologicalSort(plan.Steps)
	if err != nil {
		result.Error = fmt.Errorf("topological sort failed: %w", err)
		return result, result.Error
	}

	// Update plan status to executing
	if err := e.updatePlanStatus(ctx, plan, PlanStatusExecuting); err != nil {
		result.Error = fmt.Errorf("failed to update plan status: %w", err)
		return result, result.Error
	}

	// Execute steps in order
	for i, step := range orderedSteps {
		// Skip steps that are already completed
		if step.Status == StepStatusCompleted {
			continue
		}

		// Emit step started event
		if e.emitter != nil {
			_ = e.emitStepStatusEvent(plan.PlanID, step.StepID, string(StepStatusExecuting), i+1, len(orderedSteps))
		}

		// Update step status to executing
		if err := e.updateStepStatus(ctx, plan, step.StepID, StepStatusExecuting); err != nil {
			result.Error = fmt.Errorf("failed to update step %q status to executing: %w", step.StepID, err)
			result.StepsFailed++
			_ = e.updatePlanStatus(ctx, plan, PlanStatusFailed)
			return result, result.Error
		}

		// Execute the step with retry logic
		stepErr := e.executeStepWithRetry(ctx, step, retryConfig)
		result.StepsRun++

		if stepErr != nil {
			stepErrTyped, ok := stepErr.(*StepError)
			if !ok {
				// Wrap non-StepError in StepError
				stepErrTyped = &StepError{
					StepID:    step.StepID,
					Cause:     stepErr,
					Retryable: IsRetryableError(stepErr),
				}
			}

			// Update step status to failed
			if err := e.updateStepStatusWithError(ctx, plan, step.StepID, stepErrTyped.Cause); err != nil {
				result.Error = fmt.Errorf("failed to update step %q status to failed: %w", step.StepID, err)
				return result, result.Error
			}
			result.StepsFailed++
			result.Error = stepErrTyped
			_ = e.updatePlanStatus(ctx, plan, PlanStatusFailed)
			return result, result.Error
		}

		// Update step status to completed
		if err := e.updateStepStatus(ctx, plan, step.StepID, StepStatusCompleted); err != nil {
			result.Error = fmt.Errorf("failed to update step %q status to completed: %w", step.StepID, err)
			result.StepsFailed++
			_ = e.updatePlanStatus(ctx, plan, PlanStatusFailed)
			return result, result.Error
		}

		// Emit step completed event
		if e.emitter != nil {
			_ = e.emitStepStatusEvent(plan.PlanID, step.StepID, string(StepStatusCompleted), i+1, len(orderedSteps))
		}
	}

	// Update plan status to completed
	if err := e.updatePlanStatus(ctx, plan, PlanStatusCompleted); err != nil {
		result.Error = fmt.Errorf("failed to update plan status to completed: %w", err)
		return result, result.Error
	}

	result.Success = true
	return result, nil
}

// updatePlanStatus updates the plan status in the database
func (e *Executor) updatePlanStatus(ctx context.Context, plan *Plan, status PlanStatus) error {
	plan.Status = status
	plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	content, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}

	_, err = e.world.UpdatePlan(ctx, world.UpdatePlanParams{
		ID:        plan.PlanID,
		Title:     plan.Goal,
		Status:    string(status),
		Content:   string(content),
		UpdatedAt: plan.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("update plan in db: %w", err)
	}

	return nil
}

// updateStepStatus updates a step's status in the database
func (e *Executor) updateStepStatus(ctx context.Context, plan *Plan, stepID string, status StepStatus) error {
	for i := range plan.Steps {
		if plan.Steps[i].StepID == stepID {
			plan.Steps[i].Status = status
			if status == StepStatusExecuting {
				plan.Steps[i].StartedAt = time.Now().UTC().Format(time.RFC3339)
			} else if status == StepStatusCompleted {
				plan.Steps[i].CompletedAt = time.Now().UTC().Format(time.RFC3339)
			}
			break
		}
	}

	return e.updatePlanStatus(ctx, plan, plan.Status)
}

// updateStepStatusWithError updates a step's status to failed with error info
func (e *Executor) updateStepStatusWithError(ctx context.Context, plan *Plan, stepID string, stepErr error) error {
	for i := range plan.Steps {
		if plan.Steps[i].StepID == stepID {
			plan.Steps[i].Status = StepStatusFailed
			plan.Steps[i].Error = stepErr.Error()
			plan.Steps[i].CompletedAt = time.Now().UTC().Format(time.RFC3339)
			break
		}
	}

	return e.updatePlanStatus(ctx, plan, plan.Status)
}

// emitStepStatusEvent emits a step status change event
func (e *Executor) emitStepStatusEvent(planID, stepID, status string, current, total int) error {
	event := protocol.Event{
		Type: "step_status_changed",
		Data: map[string]interface{}{
			"plan_id": planID,
			"step_id": stepID,
			"status":  status,
			"current": current,
			"total":   total,
		},
	}
	return e.emitter.EmitEvent(event)
}

// SaveStepStatus persists step status to world DB
func (e *Executor) SaveStepStatus(ctx context.Context, planID string, step Step) error {
	plan, err := e.loadPlanFromDB(ctx, planID)
	if err != nil {
		return fmt.Errorf("load plan from db: %w", err)
	}

	for i := range plan.Steps {
		if plan.Steps[i].StepID == step.StepID {
			plan.Steps[i] = step
			break
		}
	}

	return e.updatePlanStatus(ctx, plan, plan.Status)
}

// SavePlanStatus persists plan status to world DB
func (e *Executor) SavePlanStatus(ctx context.Context, plan *Plan, status PlanStatus) error {
	return e.updatePlanStatus(ctx, plan, status)
}

// LoadPlanWithStatus loads plan and step statuses from DB
func (e *Executor) LoadPlanWithStatus(ctx context.Context, planID string) (*Plan, error) {
	return e.loadPlanFromDB(ctx, planID)
}

// CanResume checks if a plan can be resumed after interruption
func (e *Executor) CanResume(ctx context.Context, planID string) (bool, error) {
	plan, err := e.loadPlanFromDB(ctx, planID)
	if err != nil {
		return false, fmt.Errorf("load plan from db: %w", err)
	}

	// Can resume if plan status is "executing" (interrupted)
	return plan.Status == PlanStatusExecuting, nil
}

// loadPlanFromDB retrieves a plan from the database and unmarshals it
func (e *Executor) loadPlanFromDB(ctx context.Context, planID string) (*Plan, error) {
	dbPlan, err := e.world.GetPlan(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("get plan from db: %w", err)
	}

	var plan Plan
	if err := json.Unmarshal([]byte(dbPlan.Content), &plan); err != nil {
		return nil, fmt.Errorf("unmarshal plan content: %w", err)
	}

	// Ensure PlanID is set (might not be in content)
	plan.PlanID = dbPlan.ID
	plan.Status = PlanStatus(dbPlan.Status)

	return &plan, nil
}
