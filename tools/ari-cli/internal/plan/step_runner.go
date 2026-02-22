package plan

import (
	"context"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/provider"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/tools"
)

// stepRunner executes individual steps
type stepRunner struct {
	provider provider.Provider
	registry *tools.ToolRegistry
	emitter  *protocol.Emitter
}

// NewStepRunner creates a runner with dependencies
func NewStepRunner(provider provider.Provider, registry *tools.ToolRegistry, emitter *protocol.Emitter) StepRunner {
	return &stepRunner{
		provider: provider,
		registry: registry,
		emitter:  emitter,
	}
}

// Run executes a single step based on its type
func (r *stepRunner) Run(ctx context.Context, step Step) error {
	switch step.Type {
	case StepTypeCode:
		return r.runCodeStep(ctx, step)
	case StepTypeToolCall:
		return r.runToolCallStep(ctx, step)
	case StepTypeHumanInput:
		return r.runHumanInputStep(ctx, step)
	case StepTypeReasoning:
		return r.runReasoningStep(ctx, step)
	default:
		return fmt.Errorf("unknown step type: %s", step.Type)
	}
}

// runCodeStep executes a code generation step
func (r *stepRunner) runCodeStep(ctx context.Context, step Step) error {
	prompt := r.buildCodePrompt(step)

	request := provider.CompletionRequest{
		Messages: []provider.Message{
			{Role: provider.MessageRoleSystem, Content: "You are a helpful coding assistant. Generate code based on the user's request."},
			{Role: provider.MessageRoleUser, Content: prompt},
		},
	}

	response, err := r.provider.Complete(ctx, request)
	if err != nil {
		return fmt.Errorf("code generation failed: %w", err)
	}

	if r.emitter != nil {
		_ = r.emitter.EmitEvent(protocol.Event{
			Type: "code_generated",
			Data: map[string]interface{}{
				"step_id":     step.StepID,
				"description": step.Description,
				"content":     response.Message.Content,
			},
		})
	}

	return nil
}

// runToolCallStep executes a tool call step
func (r *stepRunner) runToolCallStep(ctx context.Context, step Step) error {
	toolName, args, err := r.parseToolCallPayload(step.Payload)
	if err != nil {
		return fmt.Errorf("failed to parse tool call payload: %w", err)
	}

	tool, err := r.registry.Get(toolName)
	if err != nil {
		return fmt.Errorf("tool lookup failed: %w", err)
	}

	if r.emitter != nil {
		_ = r.emitter.EmitToolCall(step.StepID, toolName, args)
	}

	result, toolErr := tool.Execute(ctx, args)

	if r.emitter != nil {
		_ = r.emitter.EmitToolResult(step.StepID, result, toolErr)
	}

	if toolErr != nil {
		return fmt.Errorf("tool execution failed: %w", toolErr)
	}

	return nil
}

// runHumanInputStep handles human-in-the-loop input request
func (r *stepRunner) runHumanInputStep(ctx context.Context, step Step) error {
	prompt := step.Description
	if payloadPrompt, ok := step.Payload["prompt"].(string); ok && payloadPrompt != "" {
		prompt = payloadPrompt
	}

	if r.emitter != nil {
		options := []protocol.QuestionOption{}
		if opts, ok := step.Payload["options"].([]interface{}); ok {
			for _, opt := range opts {
				if optMap, ok := opt.(map[string]interface{}); ok {
					option := protocol.QuestionOption{
						ID:          getString(optMap, "id"),
						Label:       getString(optMap, "label"),
						Description: getString(optMap, "description"),
					}
					options = append(options, option)
				}
			}
		}

		allowsCustom := false
		if ac, ok := step.Payload["allows_custom"].(bool); ok {
			allowsCustom = ac
		}

		_ = r.emitter.EmitQuestion(step.StepID, prompt, options, allowsCustom)
	}

	// v0: Return placeholder - actual UI handling is done by CLI
	return nil
}

// runReasoningStep executes a reasoning/analysis step
func (r *stepRunner) runReasoningStep(ctx context.Context, step Step) error {
	prompt := r.buildReasoningPrompt(step)

	request := provider.CompletionRequest{
		Messages: []provider.Message{
			{Role: provider.MessageRoleSystem, Content: "You are a helpful analytical assistant. Think through the problem and provide reasoning."},
			{Role: provider.MessageRoleUser, Content: prompt},
		},
	}

	response, err := r.provider.Complete(ctx, request)
	if err != nil {
		return fmt.Errorf("reasoning failed: %w", err)
	}

	if r.emitter != nil {
		_ = r.emitter.EmitThought(response.Message.Content)
	}

	return nil
}

// buildCodePrompt creates a prompt for code generation
func (r *stepRunner) buildCodePrompt(step Step) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Task: %s\n\n", step.Description))

	if len(step.Payload) > 0 {
		b.WriteString("Additional context:\n")
		for key, value := range step.Payload {
			b.WriteString(fmt.Sprintf("- %s: %v\n", key, value))
		}
	}

	b.WriteString("\nPlease generate the code to accomplish this task.")
	return b.String()
}

// buildReasoningPrompt creates a prompt for reasoning/analysis
func (r *stepRunner) buildReasoningPrompt(step Step) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Analyze the following task:\n\n%s\n\n", step.Description))

	if len(step.Payload) > 0 {
		b.WriteString("Context:\n")
		for key, value := range step.Payload {
			b.WriteString(fmt.Sprintf("- %s: %v\n", key, value))
		}
	}

	b.WriteString("\nPlease provide your reasoning and analysis.")
	return b.String()
}

// parseToolCallPayload extracts tool name and arguments from step payload
func (r *stepRunner) parseToolCallPayload(payload map[string]any) (string, map[string]any, error) {
	toolName, ok := payload["tool"].(string)
	if !ok || toolName == "" {
		return "", nil, fmt.Errorf("tool name is required in payload")
	}

	args := make(map[string]any)
	if argsRaw, ok := payload["arguments"].(map[string]any); ok {
		args = argsRaw
	}

	return toolName, args, nil
}

// getString safely extracts a string value from a map
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
