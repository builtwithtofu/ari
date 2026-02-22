package provider

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	simulatorName              = "simulator"
	simulatorBaseDelay         = 50 * time.Millisecond
	simulatorDelayPer100Tokens = 50 * time.Millisecond
)

type Scenario struct {
	Name     string
	Response CompletionResponse
	Delay    time.Duration
}

type Simulator struct {
	scenario Scenario
}

func NewSimulator(scenario Scenario) *Simulator {
	return &Simulator{scenario: scenario}
}

func (s *Simulator) Name() string {
	if strings.TrimSpace(s.scenario.Name) != "" {
		return s.scenario.Name
	}
	return simulatorName
}

func (s *Simulator) Complete(ctx context.Context, _ CompletionRequest) (CompletionResponse, error) {
	delay := s.scenario.Delay
	if delay <= 0 {
		delay = defaultDelay(s.scenario.Response)
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return CompletionResponse{}, ctx.Err()
	case <-timer.C:
		return cloneResponse(s.scenario.Response), nil
	}
}

func defaultDelay(response CompletionResponse) time.Duration {
	tokens := response.Usage.CompletionTokens
	if tokens <= 0 {
		tokens = estimateCompletionTokens(response)
	}

	batches := int(math.Ceil(float64(tokens) / 100.0))
	return simulatorBaseDelay + (time.Duration(batches) * simulatorDelayPer100Tokens)
}

func estimateCompletionTokens(response CompletionResponse) int {
	content := strings.TrimSpace(response.Message.Content)
	tokens := len(strings.Fields(content))

	if len(response.Message.ToolCalls) > 0 {
		for _, tc := range response.Message.ToolCalls {
			argBytes, _ := json.Marshal(tc.Arguments)
			tokens += len(strings.Fields(tc.Name))
			tokens += len(strings.Fields(string(argBytes)))
		}
	}

	if tokens == 0 {
		return 1
	}

	return tokens
}

func cloneResponse(response CompletionResponse) CompletionResponse {
	cloned := response
	cloned.Message = response.Message
	if len(response.Message.ToolCalls) == 0 {
		return cloned
	}

	clonedCalls := make([]ToolCall, len(response.Message.ToolCalls))
	for i, call := range response.Message.ToolCalls {
		clonedCalls[i] = ToolCall{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: cloneArguments(call.Arguments),
		}
	}
	cloned.Message.ToolCalls = clonedCalls
	return cloned
}

func cloneArguments(arguments map[string]any) map[string]any {
	if arguments == nil {
		return nil
	}

	cloned := make(map[string]any, len(arguments))
	for key, value := range arguments {
		cloned[key] = value
	}
	return cloned
}

func SimpleResponse(content string) Scenario {
	completionTokens := len(strings.Fields(strings.TrimSpace(content)))
	if completionTokens == 0 {
		completionTokens = 1
	}

	return Scenario{
		Name: simulatorName,
		Response: CompletionResponse{
			Model: "simulator-v0",
			Message: Message{
				Role:    MessageRoleAssistant,
				Content: content,
			},
			FinishReason: FinishReasonStop,
			Usage: TokenUsage{
				CompletionTokens: completionTokens,
				TotalTokens:      completionTokens,
			},
		},
	}
}

func ToolUseResponse(toolName string, arguments map[string]any) Scenario {
	if strings.TrimSpace(toolName) == "" {
		toolName = "tool"
	}

	return Scenario{
		Name: simulatorName,
		Response: CompletionResponse{
			Model: "simulator-v0",
			Message: Message{
				Role: MessageRoleAssistant,
				ToolCalls: []ToolCall{
					{
						ID:        "call_1",
						Name:      toolName,
						Arguments: cloneArguments(arguments),
					},
				},
			},
			FinishReason: FinishReasonToolCalls,
			Usage: TokenUsage{
				CompletionTokens: estimateCompletionTokens(CompletionResponse{Message: Message{ToolCalls: []ToolCall{{Name: toolName, Arguments: arguments}}}}),
			},
		},
	}
}

func PlanningResponse(question string) Scenario {
	if strings.TrimSpace(question) == "" {
		question = "Can you clarify the expected behavior?"
	}

	content := "Before implementation, I need one clarification: " + question
	return SimpleResponse(content)
}

// SimpleResponseScenario returns a scenario with simple text response.
func SimpleResponseScenario(response string) Scenario {
	return SimpleResponse(response)
}

// ToolUseScenario returns a scenario where LLM wants to call a tool.
func ToolUseScenario(toolName string, toolArgs map[string]any) Scenario {
	if strings.TrimSpace(toolName) == "" {
		toolName = "tool"
	}

	message := "I should call " + toolName + " to gather the required information."
	toolCalls := []ToolCall{{
		ID:        "call_1",
		Name:      toolName,
		Arguments: cloneArguments(toolArgs),
	}}

	response := CompletionResponse{
		Model: "simulator-v0",
		Message: Message{
			Role:      MessageRoleAssistant,
			Content:   message,
			ToolCalls: toolCalls,
		},
		FinishReason: FinishReasonToolCalls,
		Usage: TokenUsage{
			CompletionTokens: estimateCompletionTokens(CompletionResponse{Message: Message{Content: message, ToolCalls: toolCalls}}),
		},
	}
	response.Usage.TotalTokens = response.Usage.CompletionTokens

	return Scenario{
		Name:     simulatorName,
		Response: response,
	}
}

// PlanningScenario returns a scenario for planning phase with questions.
func PlanningScenario(questions []string) Scenario {
	trimmed := make([]string, 0, len(questions))
	for _, question := range questions {
		q := strings.TrimSpace(question)
		if q == "" {
			continue
		}
		trimmed = append(trimmed, q)
	}

	if len(trimmed) == 0 {
		trimmed = []string{
			"What constraints should I optimize for?",
			"What does success look like for this change?",
		}
	}

	content := "Before implementation, I need clarifications:\n"
	for i, question := range trimmed {
		content += "- Q" + strconv.Itoa(i+1) + ": " + question + "\n"
	}
	content = strings.TrimRight(content, "\n")

	return SimpleResponse(content)
}

// ErrorScenario returns a scenario that simulates an error.
func ErrorScenario(errorMessage string) Scenario {
	message := strings.TrimSpace(errorMessage)
	if message == "" {
		message = "provider error: simulated failure"
	}

	content := "I cannot complete this request because an error occurred: " + message
	return SimpleResponse(content)
}
