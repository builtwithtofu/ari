package plan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/provider"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/tools"
)

// mockProvider is a test double for provider.Provider
type mockProvider struct {
	responses   map[string]provider.CompletionResponse
	callCount   int
	lastRequest *provider.CompletionRequest
}

func newMockProvider() *mockProvider {
	return &mockProvider{
		responses: make(map[string]provider.CompletionResponse),
	}
}

func (m *mockProvider) Name() string {
	return "mock"
}

func (m *mockProvider) Complete(ctx context.Context, request provider.CompletionRequest) (provider.CompletionResponse, error) {
	m.callCount++
	m.lastRequest = &request

	// Return response based on first user message content
	for _, msg := range request.Messages {
		if msg.Role == provider.MessageRoleUser {
			if resp, ok := m.responses[msg.Content]; ok {
				return resp, nil
			}
			break
		}
	}

	return provider.CompletionResponse{
		Message: provider.Message{
			Role:    provider.MessageRoleAssistant,
			Content: "mock response",
		},
		FinishReason: provider.FinishReasonStop,
	}, nil
}

func (m *mockProvider) setResponse(prompt string, response provider.CompletionResponse) {
	m.responses[prompt] = response
}

// mockTool is a test double for tools.Tool
type mockTool struct {
	name        string
	description string
	result      interface{}
	executeErr  error
	lastArgs    map[string]any
}

func newMockTool(name, description string) *mockTool {
	return &mockTool{
		name:        name,
		description: description,
	}
}

func (m *mockTool) Name() string {
	return m.name
}

func (m *mockTool) Description() string {
	return m.description
}

func (m *mockTool) Execute(ctx context.Context, args map[string]any) (interface{}, error) {
	m.lastArgs = args
	return m.result, m.executeErr
}

func (m *mockTool) setResult(result interface{}) {
	m.result = result
}

func (m *mockTool) setError(err error) {
	m.executeErr = err
}

// stepTestEmitter wraps protocol.Emitter to capture events for testing
type stepTestEmitter struct {
	emitter *protocol.Emitter
	output  *bytes.Buffer
}

func newStepTestEmitter() *stepTestEmitter {
	output := &bytes.Buffer{}
	emitter := protocol.NewEmitter()
	emitter.SetOutput(output)
	return &stepTestEmitter{
		emitter: emitter,
		output:  output,
	}
}

func (c *stepTestEmitter) getEvents() []protocol.Event {
	var events []protocol.Event
	for _, line := range bytes.Split(c.output.Bytes(), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var event protocol.Event
		if err := json.Unmarshal(line, &event); err == nil {
			events = append(events, event)
		}
	}
	return events
}

func TestStepRunner_Run_CodeStep(t *testing.T) {
	ctx := context.Background()
	prov := newMockProvider()
	registry := tools.NewToolRegistry()
	emitter := newStepTestEmitter()

	runner := NewStepRunner(prov, registry, emitter.emitter)

	step := Step{
		StepID:      "code-1",
		Type:        StepTypeCode,
		Description: "Generate a hello world function",
		Payload: map[string]any{
			"language": "go",
		},
	}

	err := runner.Run(ctx, step)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if prov.callCount != 1 {
		t.Fatalf("expected provider to be called once, got %d", prov.callCount)
	}

	if prov.lastRequest == nil {
		t.Fatal("expected last request to be set")
	}

	// Verify prompt contains step description
	prompt := prov.lastRequest.Messages[1].Content
	if !bytes.Contains([]byte(prompt), []byte("Generate a hello world function")) {
		t.Fatalf("expected prompt to contain step description, got: %s", prompt)
	}

	// Verify event was emitted
	events := emitter.getEvents()
	found := false
	for _, event := range events {
		if event.Type == "code_generated" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected code_generated event to be emitted")
	}
}

func TestStepRunner_Run_CodeStep_ProviderError(t *testing.T) {
	ctx := context.Background()
	registry := tools.NewToolRegistry()
	emitter := newStepTestEmitter()

	provWithError := &failingProvider{err: errors.New("provider failed")}

	runner := NewStepRunner(provWithError, registry, emitter.emitter)

	step := Step{
		StepID:      "code-fail",
		Type:        StepTypeCode,
		Description: "Failing step",
	}

	err := runner.Run(ctx, step)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("code generation failed")) {
		t.Fatalf("expected error to contain 'code generation failed', got: %v", err)
	}
}

type failingProvider struct {
	err error
}

func (f *failingProvider) Name() string { return "failing" }
func (f *failingProvider) Complete(ctx context.Context, request provider.CompletionRequest) (provider.CompletionResponse, error) {
	return provider.CompletionResponse{}, f.err
}

func TestStepRunner_Run_ToolCallStep(t *testing.T) {
	ctx := context.Background()
	prov := newMockProvider()
	registry := tools.NewToolRegistry()
	emitter := newStepTestEmitter()

	mockTool := newMockTool("test_tool", "A test tool")
	mockTool.setResult("tool result")
	registry.Register("test_tool", mockTool)

	runner := NewStepRunner(prov, registry, emitter.emitter)

	step := Step{
		StepID:      "tool-1",
		Type:        StepTypeToolCall,
		Description: "Call test tool",
		Payload: map[string]any{
			"tool":      "test_tool",
			"arguments": map[string]any{"arg1": "value1"},
		},
	}

	err := runner.Run(ctx, step)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if mockTool.lastArgs == nil {
		t.Fatal("expected tool to be called with args")
	}
	if mockTool.lastArgs["arg1"] != "value1" {
		t.Fatalf("expected arg1 to be 'value1', got %v", mockTool.lastArgs["arg1"])
	}

	// Verify tool call and result events were emitted
	events := emitter.getEvents()
	var foundCall, foundResult bool
	for _, event := range events {
		if event.Type == string(protocol.EventTypeToolCall) {
			foundCall = true
		}
		if event.Type == string(protocol.EventTypeToolResult) {
			foundResult = true
		}
	}
	if !foundCall {
		t.Error("expected tool_call event to be emitted")
	}
	if !foundResult {
		t.Error("expected tool_result event to be emitted")
	}
}

func TestStepRunner_Run_ToolCallStep_ToolNotFound(t *testing.T) {
	ctx := context.Background()
	prov := newMockProvider()
	registry := tools.NewToolRegistry()
	emitter := newStepTestEmitter()

	runner := NewStepRunner(prov, registry, emitter.emitter)

	step := Step{
		StepID:      "tool-2",
		Type:        StepTypeToolCall,
		Description: "Call missing tool",
		Payload: map[string]any{
			"tool": "nonexistent_tool",
		},
	}

	err := runner.Run(ctx, step)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("tool lookup failed")) {
		t.Fatalf("expected error to contain 'tool lookup failed', got: %v", err)
	}
}

func TestStepRunner_Run_ToolCallStep_ToolExecutionError(t *testing.T) {
	ctx := context.Background()
	prov := newMockProvider()
	registry := tools.NewToolRegistry()
	emitter := newStepTestEmitter()

	mockTool := newMockTool("failing_tool", "A failing tool")
	mockTool.setError(errors.New("execution failed"))
	registry.Register("failing_tool", mockTool)

	runner := NewStepRunner(prov, registry, emitter.emitter)

	step := Step{
		StepID:      "tool-3",
		Type:        StepTypeToolCall,
		Description: "Call failing tool",
		Payload: map[string]any{
			"tool": "failing_tool",
		},
	}

	err := runner.Run(ctx, step)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("tool execution failed")) {
		t.Fatalf("expected error to contain 'tool execution failed', got: %v", err)
	}

	// Verify tool result event with error was emitted
	events := emitter.getEvents()
	var foundResultWithError bool
	for _, event := range events {
		if event.Type == string(protocol.EventTypeToolResult) {
			if resultData, ok := event.Data.(map[string]interface{}); ok {
				if errStr, ok := resultData["error"].(string); ok && errStr != "" {
					foundResultWithError = true
				}
			}
		}
	}
	if !foundResultWithError {
		t.Error("expected tool_result event with error to be emitted")
	}
}

func TestStepRunner_Run_ToolCallStep_InvalidPayload(t *testing.T) {
	ctx := context.Background()
	prov := newMockProvider()
	registry := tools.NewToolRegistry()
	emitter := newStepTestEmitter()

	runner := NewStepRunner(prov, registry, emitter.emitter)

	// Test missing tool name
	step := Step{
		StepID:      "tool-4",
		Type:        StepTypeToolCall,
		Description: "Call tool without name",
		Payload:     map[string]any{},
	}

	err := runner.Run(ctx, step)
	if err == nil {
		t.Fatal("expected error for missing tool name, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("failed to parse tool call payload")) {
		t.Fatalf("expected error about parsing payload, got: %v", err)
	}
}

func TestStepRunner_Run_HumanInputStep(t *testing.T) {
	ctx := context.Background()
	prov := newMockProvider()
	registry := tools.NewToolRegistry()
	emitter := newStepTestEmitter()

	runner := NewStepRunner(prov, registry, emitter.emitter)

	step := Step{
		StepID:      "human-1",
		Type:        StepTypeHumanInput,
		Description: "Please confirm this action",
		Payload: map[string]any{
			"prompt":        "Do you want to proceed?",
			"allows_custom": true,
			"options": []interface{}{
				map[string]interface{}{
					"id":          "yes",
					"label":       "Yes",
					"description": "Proceed with the action",
				},
				map[string]interface{}{
					"id":    "no",
					"label": "No",
				},
			},
		},
	}

	err := runner.Run(ctx, step)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify question event was emitted
	events := emitter.getEvents()
	var foundQuestion bool
	var questionPrompt string
	var questionOptions int
	var allowsCustom bool
	for _, event := range events {
		if event.Type == string(protocol.EventTypeQuestion) {
			foundQuestion = true
			if qd, ok := event.Data.(map[string]interface{}); ok {
				if p, ok := qd["prompt"].(string); ok {
					questionPrompt = p
				}
				if opts, ok := qd["options"].([]interface{}); ok {
					questionOptions = len(opts)
				}
				if ac, ok := qd["allows_custom"].(bool); ok {
					allowsCustom = ac
				}
			}
			break
		}
	}
	if !foundQuestion {
		t.Fatal("expected question event to be emitted")
	}
	if questionPrompt != "Do you want to proceed?" {
		t.Fatalf("expected prompt 'Do you want to proceed?', got %q", questionPrompt)
	}
	if questionOptions != 2 {
		t.Fatalf("expected 2 options, got %d", questionOptions)
	}
	if !allowsCustom {
		t.Error("expected AllowsCustom to be true")
	}
}

func TestStepRunner_Run_HumanInputStep_DefaultPrompt(t *testing.T) {
	ctx := context.Background()
	prov := newMockProvider()
	registry := tools.NewToolRegistry()
	emitter := newStepTestEmitter()

	runner := NewStepRunner(prov, registry, emitter.emitter)

	// Test that description is used when prompt is not in payload
	step := Step{
		StepID:      "human-2",
		Type:        StepTypeHumanInput,
		Description: "Use this description as prompt",
		Payload:     map[string]any{},
	}

	err := runner.Run(ctx, step)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	events := emitter.getEvents()
	var questionPrompt string
	for _, event := range events {
		if event.Type == string(protocol.EventTypeQuestion) {
			if qd, ok := event.Data.(map[string]interface{}); ok {
				if p, ok := qd["prompt"].(string); ok {
					questionPrompt = p
				}
			}
			break
		}
	}
	if questionPrompt != "Use this description as prompt" {
		t.Fatalf("expected prompt from description, got %q", questionPrompt)
	}
}

func TestStepRunner_Run_HumanInputStep_NoEmitter(t *testing.T) {
	ctx := context.Background()
	prov := newMockProvider()
	registry := tools.NewToolRegistry()

	// Test with nil emitter
	runner := NewStepRunner(prov, registry, nil)

	step := Step{
		StepID:      "human-3",
		Type:        StepTypeHumanInput,
		Description: "Human input without emitter",
	}

	err := runner.Run(ctx, step)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestStepRunner_Run_ReasoningStep(t *testing.T) {
	ctx := context.Background()
	prov := newMockProvider()
	registry := tools.NewToolRegistry()
	emitter := newStepTestEmitter()

	prov.setResponse("Task: Analyze this problem\n\n\nPlease provide your reasoning and analysis.", provider.CompletionResponse{
		Message: provider.Message{
			Role:    provider.MessageRoleAssistant,
			Content: "Here is my reasoning: the answer is 42",
		},
		FinishReason: provider.FinishReasonStop,
	})

	runner := NewStepRunner(prov, registry, emitter.emitter)

	step := Step{
		StepID:      "reason-1",
		Type:        StepTypeReasoning,
		Description: "Analyze this problem",
		Payload: map[string]any{
			"context": "Some context for analysis",
		},
	}

	err := runner.Run(ctx, step)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if prov.callCount != 1 {
		t.Fatalf("expected provider to be called once, got %d", prov.callCount)
	}

	// Verify prompt contains context
	prompt := prov.lastRequest.Messages[1].Content
	if !bytes.Contains([]byte(prompt), []byte("Some context for analysis")) {
		t.Fatalf("expected prompt to contain payload context, got: %s", prompt)
	}

	// Verify thought event was emitted
	events := emitter.getEvents()
	var foundThought bool
	for _, event := range events {
		if event.Type == string(protocol.EventTypeThought) {
			foundThought = true
			if thought, ok := event.Data.(protocol.ThoughtEvent); ok {
				if thought.Content != "Here is my reasoning: the answer is 42" {
					t.Fatalf("expected thought content, got %q", thought.Content)
				}
			}
			break
		}
	}
	if !foundThought {
		t.Error("expected thought event to be emitted")
	}
}

func TestStepRunner_Run_ReasoningStep_ProviderError(t *testing.T) {
	ctx := context.Background()
	registry := tools.NewToolRegistry()
	emitter := newStepTestEmitter()

	provWithError := &failingProvider{err: errors.New("provider unavailable")}

	runner := NewStepRunner(provWithError, registry, emitter.emitter)

	step := Step{
		StepID:      "reason-fail",
		Type:        StepTypeReasoning,
		Description: "Failing reasoning step",
	}

	err := runner.Run(ctx, step)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("reasoning failed")) {
		t.Fatalf("expected error to contain 'reasoning failed', got: %v", err)
	}
}

func TestStepRunner_Run_UnknownStepType(t *testing.T) {
	ctx := context.Background()
	prov := newMockProvider()
	registry := tools.NewToolRegistry()
	emitter := newStepTestEmitter()

	runner := NewStepRunner(prov, registry, emitter.emitter)

	step := Step{
		StepID:      "unknown-1",
		Type:        StepType("UNKNOWN_TYPE"),
		Description: "Unknown step type",
	}

	err := runner.Run(ctx, step)
	if err == nil {
		t.Fatal("expected error for unknown step type, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("unknown step type")) {
		t.Fatalf("expected error about unknown step type, got: %v", err)
	}
}

func TestStepRunner_Run_NoEmitter(t *testing.T) {
	ctx := context.Background()
	prov := newMockProvider()
	registry := tools.NewToolRegistry()

	// Test with nil emitter - should not panic
	runner := NewStepRunner(prov, registry, nil)

	step := Step{
		StepID:      "no-emitter",
		Type:        StepTypeCode,
		Description: "Step without emitter",
	}

	err := runner.Run(ctx, step)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestStepRunner_buildCodePrompt(t *testing.T) {
	prov := newMockProvider()
	registry := tools.NewToolRegistry()
	emitter := newStepTestEmitter()

	runner := NewStepRunner(prov, registry, emitter.emitter)
	sr := runner.(*stepRunner)

	step := Step{
		Description: "Generate a function",
		Payload: map[string]any{
			"language": "go",
			"target":   "utils.go",
		},
	}

	prompt := sr.buildCodePrompt(step)

	if !bytes.Contains([]byte(prompt), []byte("Task: Generate a function")) {
		t.Errorf("expected prompt to contain task description")
	}
	if !bytes.Contains([]byte(prompt), []byte("language: go")) {
		t.Errorf("expected prompt to contain language")
	}
	if !bytes.Contains([]byte(prompt), []byte("target: utils.go")) {
		t.Errorf("expected prompt to contain target")
	}
}

func TestStepRunner_buildReasoningPrompt(t *testing.T) {
	prov := newMockProvider()
	registry := tools.NewToolRegistry()
	emitter := newStepTestEmitter()

	runner := NewStepRunner(prov, registry, emitter.emitter)
	sr := runner.(*stepRunner)

	step := Step{
		Description: "Analyze the architecture",
		Payload: map[string]any{
			"constraints": "Must be scalable",
		},
	}

	prompt := sr.buildReasoningPrompt(step)

	if !bytes.Contains([]byte(prompt), []byte("Analyze the architecture")) {
		t.Errorf("expected prompt to contain description")
	}
	if !bytes.Contains([]byte(prompt), []byte("constraints: Must be scalable")) {
		t.Errorf("expected prompt to contain constraints")
	}
}

func TestStepRunner_parseToolCallPayload(t *testing.T) {
	prov := newMockProvider()
	registry := tools.NewToolRegistry()
	emitter := newStepTestEmitter()

	runner := NewStepRunner(prov, registry, emitter.emitter)
	sr := runner.(*stepRunner)

	tests := []struct {
		name        string
		payload     map[string]any
		wantTool    string
		wantArgs    map[string]any
		wantErr     bool
		errContains string
	}{
		{
			name: "valid payload",
			payload: map[string]any{
				"tool":      "my_tool",
				"arguments": map[string]any{"key": "value"},
			},
			wantTool: "my_tool",
			wantArgs: map[string]any{"key": "value"},
			wantErr:  false,
		},
		{
			name:        "missing tool name",
			payload:     map[string]any{},
			wantErr:     true,
			errContains: "tool name is required",
		},
		{
			name: "empty tool name",
			payload: map[string]any{
				"tool": "",
			},
			wantErr:     true,
			errContains: "tool name is required",
		},
		{
			name: "missing arguments",
			payload: map[string]any{
				"tool": "my_tool",
			},
			wantTool: "my_tool",
			wantArgs: map[string]any{},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, args, err := sr.parseToolCallPayload(tt.payload)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if !bytes.Contains([]byte(err.Error()), []byte(tt.errContains)) {
					t.Errorf("expected error to contain %q, got: %v", tt.errContains, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tool != tt.wantTool {
				t.Errorf("expected tool %q, got %q", tt.wantTool, tool)
			}

			if len(args) != len(tt.wantArgs) {
				t.Errorf("expected %d args, got %d", len(tt.wantArgs), len(args))
			}
		})
	}
}

func TestStepRunner_getString(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]interface{}
		key      string
		expected string
	}{
		{
			name:     "string value",
			m:        map[string]interface{}{"key": "value"},
			key:      "key",
			expected: "value",
		},
		{
			name:     "missing key",
			m:        map[string]interface{}{},
			key:      "key",
			expected: "",
		},
		{
			name:     "non-string value",
			m:        map[string]interface{}{"key": 123},
			key:      "key",
			expected: "",
		},
		{
			name:     "nil map",
			m:        nil,
			key:      "key",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getString(tt.m, tt.key)
			if result != tt.expected {
				t.Errorf("getString() = %q, want %q", result, tt.expected)
			}
		})
	}
}
