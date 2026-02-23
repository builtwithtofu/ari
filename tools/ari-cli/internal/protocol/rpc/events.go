package rpc

import (
	"encoding/json"
	"time"
)

type StepStatusEvent struct {
	Type      string    `json:"type"`
	SessionID string    `json:"session_id"`
	StepID    string    `json:"step_id"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type ToolCallEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id"`
	Tool      string          `json:"tool"`
	Input     json.RawMessage `json:"input"`
	Timestamp time.Time       `json:"timestamp"`
}

type ToolResultEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id"`
	Tool      string          `json:"tool"`
	Result    json.RawMessage `json:"result"`
	Error     string          `json:"error,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

type QuestionEvent struct {
	Type      string    `json:"type"`
	SessionID string    `json:"session_id"`
	Question  string    `json:"question"`
	Options   []string  `json:"options,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type SessionStartEvent struct {
	Type      string    `json:"type"`
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
}

type SessionEndEvent struct {
	Type      string    `json:"type"`
	SessionID string    `json:"session_id"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}
