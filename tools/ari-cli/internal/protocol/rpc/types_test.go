package rpc

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRequestEnvelopeMarshal(t *testing.T) {
	payload := RequestEnvelope[BuildRequest]{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "ari/build",
		Params: BuildRequest{
			PlanID: "plan-123",
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request envelope: %v", err)
	}

	got := string(data)
	want := `{"jsonrpc":"2.0","id":1,"method":"ari/build","params":{"plan_id":"plan-123"}}`
	if got != want {
		t.Fatalf("unexpected JSON\nwant: %s\ngot:  %s", want, got)
	}
}

func TestResponseEnvelopeMarshal(t *testing.T) {
	payload := ResponseEnvelope[BuildResponse]{
		JSONRPC: "2.0",
		ID:      "req-1",
		Result: BuildResponse{
			SessionID: "sess-1",
			Status:    "started",
			Plan: Plan{
				ID:   "plan-123",
				Goal: "ship feature",
				Steps: []Step{
					{ID: "step-1", Description: "implement", Status: "pending"},
				},
			},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal response envelope: %v", err)
	}

	got := string(data)
	want := `{"jsonrpc":"2.0","id":"req-1","result":{"session_id":"sess-1","status":"started","plan":{"id":"plan-123","goal":"ship feature","steps":[{"id":"step-1","description":"implement","status":"pending"}]}}}`
	if got != want {
		t.Fatalf("unexpected JSON\nwant: %s\ngot:  %s", want, got)
	}
}

func TestEventsMarshal(t *testing.T) {
	ts := time.Date(2026, 2, 22, 21, 45, 0, 0, time.UTC)

	stepStatus := StepStatusEvent{
		Type:      "step_status",
		SessionID: "sess-1",
		StepID:    "step-1",
		Status:    "running",
		Timestamp: ts,
	}

	stepData, err := json.Marshal(stepStatus)
	if err != nil {
		t.Fatalf("marshal step_status event: %v", err)
	}

	stepGot := string(stepData)
	stepWant := `{"type":"step_status","session_id":"sess-1","step_id":"step-1","status":"running","timestamp":"2026-02-22T21:45:00Z"}`
	if stepGot != stepWant {
		t.Fatalf("unexpected JSON\nwant: %s\ngot:  %s", stepWant, stepGot)
	}

	toolCall := ToolCallEvent{
		Type:      "tool_call",
		SessionID: "sess-1",
		Tool:      "read",
		Input:     json.RawMessage(`{"path":"README.md"}`),
		Timestamp: ts,
	}

	toolCallData, err := json.Marshal(toolCall)
	if err != nil {
		t.Fatalf("marshal tool_call event: %v", err)
	}

	toolCallGot := string(toolCallData)
	toolCallWant := `{"type":"tool_call","session_id":"sess-1","tool":"read","input":{"path":"README.md"},"timestamp":"2026-02-22T21:45:00Z"}`
	if toolCallGot != toolCallWant {
		t.Fatalf("unexpected JSON\nwant: %s\ngot:  %s", toolCallWant, toolCallGot)
	}
}

func TestMethodRegistryRegisterWithGenericMethodDefinition(t *testing.T) {
	r := NewMethodRegistry()

	err := r.Register(MethodDefinition(Method[PlanRequest, PlanResponse]{
		Name:        "ari/plan",
		Description: "Create a plan",
	}))
	if err != nil {
		t.Fatalf("register method: %v", err)
	}

	method, ok := r.Get("ari/plan")
	if !ok {
		t.Fatalf("expected method to be registered")
	}

	if method.RequestType != nil && method.RequestType.Name() != "PlanRequest" {
		t.Fatalf("unexpected request type: %v", method.RequestType)
	}

	if method.ResponseType != nil && method.ResponseType.Name() != "PlanResponse" {
		t.Fatalf("unexpected response type: %v", method.ResponseType)
	}
}
