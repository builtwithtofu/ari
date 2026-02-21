// Package protocol defines the event types for LSP-style communication.
package protocol

// Event represents a protocol event that can be emitted to clients.
type Event struct {
	Type      string      `json:"type"`
	Timestamp string      `json:"timestamp"`
	Data      interface{} `json:"data,omitempty"`
}

// EventType represents the type of protocol event.
type EventType string

const (
	// Lifecycle events
	EventTypeSessionStart EventType = "session_start"
	EventTypeSessionEnd   EventType = "session_end"

	// State events
	EventTypeStateChange EventType = "state_change"

	// Thought events
	EventTypeThought EventType = "thought"

	// Tool events
	EventTypeToolCall   EventType = "tool_call"
	EventTypeToolResult EventType = "tool_result"

	// Human-in-the-loop events
	EventTypeQuestion       EventType = "question"
	EventTypeAnswer         EventType = "answer"
	EventTypeAnswerReceived EventType = "answer_received"

	// World events
	EventTypeDecisionCreated EventType = "decision_created"
	EventTypePlanCreated     EventType = "plan_created"
	EventTypePlanProgress    EventType = "plan_progress"
)

// SessionStartEvent represents the start of a session.
type SessionStartEvent struct {
	SessionID string `json:"session_id"`
	Command   string `json:"command"`
}

// SessionEndEvent represents the end of a session.
type SessionEndEvent struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"` // success, error, cancelled
}

// StateChangeEvent represents a state change in the agent.
type StateChangeEvent struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ThoughtEvent represents a thought from the agent.
type ThoughtEvent struct {
	Content string `json:"content"`
}

// ToolCallEvent represents a tool call being made.
type ToolCallEvent struct {
	ID   string                 `json:"id"`
	Tool string                 `json:"tool"`
	Args map[string]interface{} `json:"args"`
}

// ToolResultEvent represents the result of a tool call.
type ToolResultEvent struct {
	ID     string      `json:"id"`
	Result interface{} `json:"result"`
	Error  string      `json:"error,omitempty"`
}

// QuestionEvent represents a question for human-in-the-loop.
type QuestionEvent struct {
	ID           string           `json:"id"`
	Prompt       string           `json:"prompt"`
	Options      []QuestionOption `json:"options,omitempty"`
	AllowsCustom bool             `json:"allows_custom"`
}

// QuestionOption represents an option for a question.
type QuestionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// AnswerEvent represents an answer to a question.
type AnswerEvent struct {
	QuestionID string `json:"question_id"`
	Answer     string `json:"answer"`
	Custom     string `json:"custom,omitempty"`
}

// DecisionCreatedEvent represents a decision being created.
type DecisionCreatedEvent struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// PlanCreatedEvent represents a plan being created.
type PlanCreatedEvent struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// PlanProgressEvent represents progress on a plan.
type PlanProgressEvent struct {
	ID        int `json:"id"`
	Completed int `json:"completed"`
	Total     int `json:"total"`
}
