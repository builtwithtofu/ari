package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSimulatorImplementsProvider(t *testing.T) {
	var p Provider = NewSimulator(SimpleResponse("hello"))
	if p.Name() == "" {
		t.Fatal("simulator provider name is empty")
	}
}

func TestSimulatorCompleteReturnsScenarioResponse(t *testing.T) {
	scenario := SimpleResponse("fixed output")
	sim := NewSimulator(scenario)

	resp, err := sim.Complete(context.Background(), CompletionRequest{})
	if err != nil {
		t.Fatalf("complete returned error: %v", err)
	}

	if resp.Message.Content != "fixed output" {
		t.Fatalf("response content = %q, want %q", resp.Message.Content, "fixed output")
	}
	if resp.FinishReason != FinishReasonStop {
		t.Fatalf("finish reason = %q, want %q", resp.FinishReason, FinishReasonStop)
	}
}

func TestSimulatorCompleteRespectsConfiguredDelay(t *testing.T) {
	scenario := SimpleResponse("hi")
	scenario.Delay = 120 * time.Millisecond

	sim := NewSimulator(scenario)
	start := time.Now()
	_, err := sim.Complete(context.Background(), CompletionRequest{})
	if err != nil {
		t.Fatalf("complete returned error: %v", err)
	}

	elapsed := time.Since(start)
	if elapsed < 100*time.Millisecond {
		t.Fatalf("elapsed delay = %v, want at least %v", elapsed, 100*time.Millisecond)
	}
}

func TestSimulatorCompleteUsesDeterministicDefaultDelay(t *testing.T) {
	response := CompletionResponse{
		Message: Message{Role: MessageRoleAssistant, Content: "one two three"},
		Usage:   TokenUsage{CompletionTokens: 200},
	}
	sim := NewSimulator(Scenario{Name: "simulator", Response: response})

	startA := time.Now()
	_, err := sim.Complete(context.Background(), CompletionRequest{})
	if err != nil {
		t.Fatalf("first complete returned error: %v", err)
	}
	elapsedA := time.Since(startA)

	startB := time.Now()
	_, err = sim.Complete(context.Background(), CompletionRequest{})
	if err != nil {
		t.Fatalf("second complete returned error: %v", err)
	}
	elapsedB := time.Since(startB)

	if elapsedA < 130*time.Millisecond || elapsedA > 260*time.Millisecond {
		t.Fatalf("first elapsed = %v, want around 150ms", elapsedA)
	}
	if elapsedB < 130*time.Millisecond || elapsedB > 260*time.Millisecond {
		t.Fatalf("second elapsed = %v, want around 150ms", elapsedB)
	}
}

func TestSimulatorCompleteHonorsContextCancelation(t *testing.T) {
	scenario := SimpleResponse("long")
	scenario.Delay = 500 * time.Millisecond
	sim := NewSimulator(scenario)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := sim.Complete(ctx, CompletionRequest{})
	if err == nil {
		t.Fatal("complete returned nil error with canceled context")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("complete error = %v, want deadline exceeded", err)
	}
}

func TestToolUseResponseScenario(t *testing.T) {
	scenario := ToolUseResponse("search_docs", map[string]any{"query": "provider"})
	sim := NewSimulator(scenario)

	resp, err := sim.Complete(context.Background(), CompletionRequest{})
	if err != nil {
		t.Fatalf("complete returned error: %v", err)
	}

	if resp.FinishReason != FinishReasonToolCalls {
		t.Fatalf("finish reason = %q, want %q", resp.FinishReason, FinishReasonToolCalls)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(resp.Message.ToolCalls))
	}
	if resp.Message.ToolCalls[0].Name != "search_docs" {
		t.Fatalf("tool name = %q, want %q", resp.Message.ToolCalls[0].Name, "search_docs")
	}
}

func TestPlanningResponseScenario(t *testing.T) {
	scenario := PlanningResponse("what constraints matter?")
	sim := NewSimulator(scenario)

	resp, err := sim.Complete(context.Background(), CompletionRequest{})
	if err != nil {
		t.Fatalf("complete returned error: %v", err)
	}

	if resp.FinishReason != FinishReasonStop {
		t.Fatalf("finish reason = %q, want %q", resp.FinishReason, FinishReasonStop)
	}
	if resp.Message.Content == "" {
		t.Fatal("planning response content is empty")
	}
}
