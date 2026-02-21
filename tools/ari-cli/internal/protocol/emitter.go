package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

type Emitter struct {
	output io.Writer
}

func NewEmitter() *Emitter {
	return &Emitter{
		output: os.Stdout,
	}
}

func (e *Emitter) EmitEvent(event Event) error {
	event.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	data = append(data, '\n')
	if _, err := e.output.Write(data); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}

	return nil
}

func (e *Emitter) EmitSessionStart(sessionID, command string) error {
	event := Event{
		Type: string(EventTypeSessionStart),
		Data: SessionStartEvent{
			SessionID: sessionID,
			Command:   command,
		},
	}
	return e.EmitEvent(event)
}

func (e *Emitter) EmitSessionEnd(sessionID, status string) error {
	event := Event{
		Type: string(EventTypeSessionEnd),
		Data: SessionEndEvent{
			SessionID: sessionID,
			Status:    status,
		},
	}
	return e.EmitEvent(event)
}

func (e *Emitter) EmitStateChange(from, to string) error {
	event := Event{
		Type: string(EventTypeStateChange),
		Data: StateChangeEvent{
			From: from,
			To:   to,
		},
	}
	return e.EmitEvent(event)
}

func (e *Emitter) EmitThought(content string) error {
	event := Event{
		Type: string(EventTypeThought),
		Data: ThoughtEvent{
			Content: content,
		},
	}
	return e.EmitEvent(event)
}

func (e *Emitter) EmitToolCall(id, tool string, args map[string]interface{}) error {
	event := Event{
		Type: string(EventTypeToolCall),
		Data: ToolCallEvent{
			ID:   id,
			Tool: tool,
			Args: args,
		},
	}
	return e.EmitEvent(event)
}

func (e *Emitter) EmitToolResult(id string, result interface{}, resultErr error) error {
	toolResult := ToolResultEvent{
		ID:     id,
		Result: result,
	}
	if resultErr != nil {
		toolResult.Error = resultErr.Error()
	}

	event := Event{
		Type: string(EventTypeToolResult),
		Data: toolResult,
	}
	return e.EmitEvent(event)
}

func (e *Emitter) EmitQuestion(id, prompt string, options []QuestionOption, allowsCustom bool) error {
	event := Event{
		Type: string(EventTypeQuestion),
		Data: QuestionEvent{
			ID:           id,
			Prompt:       prompt,
			Options:      options,
			AllowsCustom: allowsCustom,
		},
	}
	return e.EmitEvent(event)
}

func (e *Emitter) EmitAnswer(questionID, answer, custom string) error {
	event := Event{
		Type: string(EventTypeAnswer),
		Data: AnswerEvent{
			QuestionID: questionID,
			Answer:     answer,
			Custom:     custom,
		},
	}
	return e.EmitEvent(event)
}

func (e *Emitter) EmitAnswerReceived(questionID string) error {
	_ = questionID
	event := Event{
		Type: string(EventTypeAnswerReceived),
	}
	return e.EmitEvent(event)
}

func (e *Emitter) EmitDecisionCreated(id, title string) error {
	event := Event{
		Type: string(EventTypeDecisionCreated),
		Data: DecisionCreatedEvent{
			ID:    id,
			Title: title,
		},
	}
	return e.EmitEvent(event)
}

func (e *Emitter) EmitPlanCreated(id, title string) error {
	event := Event{
		Type: string(EventTypePlanCreated),
		Data: PlanCreatedEvent{
			ID:    id,
			Title: title,
		},
	}
	return e.EmitEvent(event)
}

func (e *Emitter) EmitPlanProgress(id, completed, total int) error {
	event := Event{
		Type: string(EventTypePlanProgress),
		Data: PlanProgressEvent{
			ID:        id,
			Completed: completed,
			Total:     total,
		},
	}
	return e.EmitEvent(event)
}
