package provider

import (
	"strings"
	"testing"
)

func TestSimpleResponseScenario(t *testing.T) {
	scenario := SimpleResponseScenario("Hello!")

	if scenario.Name != simulatorName {
		t.Fatalf("scenario name = %q, want %q", scenario.Name, simulatorName)
	}
	if scenario.Delay != 0 {
		t.Fatalf("scenario delay = %v, want 0", scenario.Delay)
	}
	if scenario.Response.Message.Role != MessageRoleAssistant {
		t.Fatalf("message role = %q, want %q", scenario.Response.Message.Role, MessageRoleAssistant)
	}
	if scenario.Response.Message.Content != "Hello!" {
		t.Fatalf("message content = %q, want %q", scenario.Response.Message.Content, "Hello!")
	}
	if scenario.Response.FinishReason != FinishReasonStop {
		t.Fatalf("finish reason = %q, want %q", scenario.Response.FinishReason, FinishReasonStop)
	}
}

func TestToolUseScenario(t *testing.T) {
	args := map[string]any{"query": "provider", "limit": 3}
	scenario := ToolUseScenario("search_docs", args)

	if scenario.Response.FinishReason != FinishReasonToolCalls {
		t.Fatalf("finish reason = %q, want %q", scenario.Response.FinishReason, FinishReasonToolCalls)
	}
	if len(scenario.Response.Message.ToolCalls) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(scenario.Response.Message.ToolCalls))
	}

	call := scenario.Response.Message.ToolCalls[0]
	if call.Name != "search_docs" {
		t.Fatalf("tool name = %q, want %q", call.Name, "search_docs")
	}
	if call.ID != "call_1" {
		t.Fatalf("tool call id = %q, want %q", call.ID, "call_1")
	}
	if got, ok := call.Arguments["query"]; !ok || got != "provider" {
		t.Fatalf("tool args query = %v, want %q", got, "provider")
	}
	if got, ok := call.Arguments["limit"]; !ok || got != 3 {
		t.Fatalf("tool args limit = %v, want %d", got, 3)
	}
	if strings.TrimSpace(scenario.Response.Message.Content) == "" {
		t.Fatal("tool-use message content is empty")
	}

	args["query"] = "mutated"
	if call.Arguments["query"] != "provider" {
		t.Fatalf("tool args mutated through input map: got %v", call.Arguments["query"])
	}
}

func TestPlanningScenarioIncludesQuestions(t *testing.T) {
	questions := []string{"What constraints matter?", "What does success look like?"}
	scenario := PlanningScenario(questions)

	if scenario.Response.FinishReason != FinishReasonStop {
		t.Fatalf("finish reason = %q, want %q", scenario.Response.FinishReason, FinishReasonStop)
	}

	content := scenario.Response.Message.Content
	for _, question := range questions {
		if !strings.Contains(content, question) {
			t.Fatalf("planning content %q does not include question %q", content, question)
		}
	}
}

func TestErrorScenario(t *testing.T) {
	scenario := ErrorScenario("rate limit exceeded")

	if scenario.Response.FinishReason != FinishReasonStop {
		t.Fatalf("finish reason = %q, want %q", scenario.Response.FinishReason, FinishReasonStop)
	}
	if !strings.Contains(scenario.Response.Message.Content, "rate limit exceeded") {
		t.Fatalf("error content %q does not include provided message", scenario.Response.Message.Content)
	}
}
