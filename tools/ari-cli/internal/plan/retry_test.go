package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/world"
	_ "modernc.org/sqlite"
)

type mockRetryableRunner struct {
	executedSteps []string
	failuresLeft  map[string]int
	nonRetryable  map[string]bool
	failSequence  map[string][]error
	callCount     map[string]int
}

func newMockRetryableRunner() *mockRetryableRunner {
	return &mockRetryableRunner{
		executedSteps: []string{},
		failuresLeft:  make(map[string]int),
		nonRetryable:  make(map[string]bool),
		failSequence:  make(map[string][]error),
		callCount:     make(map[string]int),
	}
}

func (m *mockRetryableRunner) Run(ctx context.Context, step Step) error {
	m.callCount[step.StepID]++
	m.executedSteps = append(m.executedSteps, step.StepID)

	if seq, ok := m.failSequence[step.StepID]; ok && len(seq) > 0 {
		err := seq[0]
		m.failSequence[step.StepID] = seq[1:]
		if err != nil {
			return err
		}
		return nil
	}

	if failures, ok := m.failuresLeft[step.StepID]; ok && failures > 0 {
		m.failuresLeft[step.StepID] = failures - 1
		if m.nonRetryable[step.StepID] {
			return errors.New("non-retryable error: invalid configuration")
		}
		return MarkRetryable(errors.New("retryable error: network timeout"))
	}

	return nil
}

func (m *mockRetryableRunner) setFailSequence(stepID string, errs []error) {
	m.failSequence[stepID] = errs
}

func (m *mockRetryableRunner) setFailures(stepID string, count int) {
	m.failuresLeft[stepID] = count
}

func (m *mockRetryableRunner) setNonRetryableFailure(stepID string) {
	m.failuresLeft[stepID] = 1
	m.nonRetryable[stepID] = true
}

func TestDefaultRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxRetries != 3 {
		t.Errorf("expected MaxRetries to be 3, got %d", config.MaxRetries)
	}
	if config.BaseDelay != 1*time.Second {
		t.Errorf("expected BaseDelay to be 1s, got %v", config.BaseDelay)
	}
	if config.MaxDelay != 30*time.Second {
		t.Errorf("expected MaxDelay to be 30s, got %v", config.MaxDelay)
	}
	if config.Multiplier != 2.0 {
		t.Errorf("expected Multiplier to be 2.0, got %f", config.Multiplier)
	}
}

func TestCalculateBackoff(t *testing.T) {
	config := RetryConfig{
		BaseDelay:  1 * time.Second,
		MaxDelay:   10 * time.Second,
		Multiplier: 2.0,
	}

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 1 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 10 * time.Second},
		{5, 10 * time.Second},
		{-1, 1 * time.Second},
	}

	for _, tc := range tests {
		result := calculateBackoff(config, tc.attempt)
		if result != tc.expected {
			t.Errorf("attempt %d: expected %v, got %v", tc.attempt, tc.expected, result)
		}
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "context canceled",
			err:      context.Canceled,
			expected: false,
		},
		{
			name:     "context deadline exceeded",
			err:      context.DeadlineExceeded,
			expected: true,
		},
		{
			name:     "timeout error",
			err:      errors.New("connection timeout"),
			expected: true,
		},
		{
			name:     "temporary error",
			err:      errors.New("temporary failure"),
			expected: true,
		},
		{
			name:     "unavailable error",
			err:      errors.New("service unavailable"),
			expected: true,
		},
		{
			name:     "connection refused",
			err:      errors.New("connection refused"),
			expected: true,
		},
		{
			name:     "rate limit error",
			err:      errors.New("rate limit exceeded"),
			expected: true,
		},
		{
			name:     "too many requests",
			err:      errors.New("429 too many requests"),
			expected: true,
		},
		{
			name:     "generic error",
			err:      errors.New("something went wrong"),
			expected: false,
		},
		{
			name:     "marked retryable",
			err:      MarkRetryable(errors.New("custom error")),
			expected: true,
		},
		{
			name:     "wrapped retryable",
			err:      fmt.Errorf("wrapped: %w", MarkRetryable(errors.New("inner"))),
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsRetryableError(tc.err)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestMarkRetryable(t *testing.T) {
	if MarkRetryable(nil) != nil {
		t.Error("expected nil for nil input")
	}

	inner := errors.New("inner error")
	marked := MarkRetryable(inner)
	if !IsRetryableError(marked) {
		t.Error("expected marked error to be retryable")
	}

	var re *RetryableError
	if !errors.As(marked, &re) {
		t.Error("expected error to be a RetryableError")
	}
	if re.Wrapped != inner {
		t.Error("expected wrapped error to be inner error")
	}
}

func TestStepError(t *testing.T) {
	err := &StepError{
		StepID:    "step-1",
		Cause:     errors.New("something failed"),
		Retryable: true,
	}

	expected := `step "step-1" failed (retryable): something failed`
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}

	err.Retryable = false
	expected = `step "step-1" failed (non-retryable): something failed`
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}

	unwrapped := err.Unwrap()
	if unwrapped == nil || unwrapped.Error() != "something failed" {
		t.Error("expected unwrapped error")
	}
}

func TestExecuteStepWithRetry_Success(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	plan := &Plan{
		PlanID:    "test-retry-success",
		Goal:      "Test retry success",
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

	runner := newMockRetryableRunner()
	runner.setFailures("A", 0)

	emitter := protocol.NewEmitter()
	executor := NewExecutor(q, emitter, runner)

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
	if runner.callCount["A"] != 1 {
		t.Fatalf("expected 1 call to step A, got %d", runner.callCount["A"])
	}
}

func TestExecuteStepWithRetry_SuccessAfterRetry(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	plan := &Plan{
		PlanID:    "test-retry-success-after",
		Goal:      "Test retry success after failures",
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

	runner := newMockRetryableRunner()
	runner.setFailures("A", 2)

	emitter := protocol.NewEmitter()
	executor := NewExecutor(q, emitter, runner)

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
	if runner.callCount["A"] != 3 {
		t.Fatalf("expected 3 calls to step A, got %d", runner.callCount["A"])
	}
}

func TestExecuteStepWithRetry_MaxRetriesExceeded(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	plan := &Plan{
		PlanID:    "test-retry-max",
		Goal:      "Test max retries exceeded",
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

	runner := newMockRetryableRunner()
	runner.setFailures("A", 5)

	emitter := protocol.NewEmitter()
	executor := NewExecutor(q, emitter, runner)

	result, err := executor.Run(ctx, plan)

	if err == nil {
		t.Fatal("expected error")
	}
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.StepsRun != 1 {
		t.Fatalf("expected 1 step run, got %d", result.StepsRun)
	}
	if result.StepsFailed != 1 {
		t.Fatalf("expected 1 step failed, got %d", result.StepsFailed)
	}
	if runner.callCount["A"] != 4 {
		t.Fatalf("expected 4 calls to step A, got %d", runner.callCount["A"])
	}

	stepErr, ok := err.(*StepError)
	if !ok {
		t.Fatalf("expected *StepError, got %T", err)
	}
	if !stepErr.Retryable {
		t.Error("expected error to be retryable")
	}
}

func TestExecuteStepWithRetry_NonRetryableError(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	plan := &Plan{
		PlanID:    "test-non-retryable",
		Goal:      "Test non-retryable error",
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

	runner := newMockRetryableRunner()
	runner.setNonRetryableFailure("A")

	emitter := protocol.NewEmitter()
	executor := NewExecutor(q, emitter, runner)

	result, err := executor.Run(ctx, plan)

	if err == nil {
		t.Fatal("expected error")
	}
	if result.Success {
		t.Fatal("expected failure")
	}
	if runner.callCount["A"] != 1 {
		t.Fatalf("expected 1 call to step A, got %d", runner.callCount["A"])
	}

	stepErr, ok := err.(*StepError)
	if !ok {
		t.Fatalf("expected *StepError, got %T", err)
	}
	if stepErr.Retryable {
		t.Error("expected error to be non-retryable")
	}
}

func TestExecuteStepWithRetry_ContextCancellation(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx, cancel := context.WithCancel(context.Background())

	plan := &Plan{
		PlanID:    "test-context-cancel",
		Goal:      "Test context cancellation",
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

	runner := newMockRetryableRunner()
	runner.setFailures("A", 10)

	emitter := protocol.NewEmitter()
	executor := NewExecutor(q, emitter, runner)

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	config := RetryConfig{
		MaxRetries: 10,
		BaseDelay:  500 * time.Millisecond,
		MaxDelay:   1 * time.Second,
		Multiplier: 2.0,
	}

	result, err := executor.RunWithConfig(ctx, plan, config)

	if err == nil {
		t.Fatal("expected error")
	}
	if result.Success {
		t.Fatal("expected failure")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestExecuteStepWithRetry_BackoffTiming(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	plan := &Plan{
		PlanID:    "test-backoff",
		Goal:      "Test backoff timing",
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

	runner := newMockRetryableRunner()
	runner.setFailures("A", 2)

	emitter := protocol.NewEmitter()
	executor := NewExecutor(q, emitter, runner)

	config := RetryConfig{
		MaxRetries: 3,
		BaseDelay:  50 * time.Millisecond,
		MaxDelay:   200 * time.Millisecond,
		Multiplier: 2.0,
	}

	start := time.Now()
	result, err := executor.RunWithConfig(ctx, plan, config)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}

	minExpected := 100 * time.Millisecond
	maxExpected := 300 * time.Millisecond
	if elapsed < minExpected {
		t.Errorf("expected at least %v elapsed, got %v", minExpected, elapsed)
	}
	if elapsed > maxExpected {
		t.Errorf("expected at most %v elapsed, got %v", maxExpected, elapsed)
	}
}

func TestExecuteStepWithRetry_MultipleSteps(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	plan := &Plan{
		PlanID:    "test-multi-retry",
		Goal:      "Test multiple steps with retry",
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

	runner := newMockRetryableRunner()
	runner.setFailures("A", 1)
	runner.setFailures("C", 2)

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
	if runner.callCount["A"] != 2 {
		t.Errorf("expected 2 calls to A, got %d", runner.callCount["A"])
	}
	if runner.callCount["B"] != 1 {
		t.Errorf("expected 1 call to B, got %d", runner.callCount["B"])
	}
	if runner.callCount["C"] != 3 {
		t.Errorf("expected 3 calls to C, got %d", runner.callCount["C"])
	}
}

func TestExecuteStepWithRetry_StepSkipOnNonCriticalError(t *testing.T) {
	db := setupTestDB(t)
	q := world.New(db)
	ctx := context.Background()

	plan := &Plan{
		PlanID:    "test-skip-capability",
		Goal:      "Test skip capability",
		Status:    PlanStatusApproved,
		CreatedAt: now(),
		UpdatedAt: now(),
		Steps: []Step{
			{StepID: "A", Type: StepTypeCode, Description: "Step A", Status: StepStatusApproved},
			{StepID: "B", Type: StepTypeCode, Description: "Step B", Status: StepStatusApproved, DependsOn: []string{"A"}},
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

	runner := newMockRetryableRunner()
	runner.setNonRetryableFailure("A")

	emitter := protocol.NewEmitter()
	executor := NewExecutor(q, emitter, runner)

	result, err := executor.Run(ctx, plan)

	if err == nil {
		t.Fatal("expected error")
	}
	if result.Success {
		t.Fatal("expected failure")
	}
	if result.StepsRun != 1 {
		t.Fatalf("expected 1 step run, got %d", result.StepsRun)
	}
	if result.StepsFailed != 1 {
		t.Fatalf("expected 1 step failed, got %d", result.StepsFailed)
	}

	if !strings.Contains(err.Error(), "step \"A\"") {
		t.Errorf("expected error to contain step name, got %v", err)
	}
}

func TestContainsSubstring(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"Hello World", "world", true},
		{"Hello World", "WORLD", true},
		{"Hello World", "foo", false},
		{"", "", true},
		{"a", "ab", false},
		{"timeout error occurred", "timeout", true},
		{"Connection Refused", "connection", true},
	}

	for _, tc := range tests {
		result := containsSubstring(tc.s, tc.substr)
		if result != tc.expected {
			t.Errorf("containsSubstring(%q, %q) = %v, expected %v", tc.s, tc.substr, result, tc.expected)
		}
	}
}

func TestToLower(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello", "hello"},
		{"HELLO", "hello"},
		{"hello", "hello"},
		{"HeLLo WoRLd", "hello world"},
		{"", ""},
		{"123ABC", "123abc"},
	}

	for _, tc := range tests {
		result := toLower(tc.input)
		if result != tc.expected {
			t.Errorf("toLower(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestRetryableError(t *testing.T) {
	inner := errors.New("inner error")
	re := &RetryableError{Wrapped: inner}

	if re.Error() != "inner error" {
		t.Errorf("expected 'inner error', got %q", re.Error())
	}

	if re.Unwrap() != inner {
		t.Error("expected Unwrap to return inner error")
	}
}
