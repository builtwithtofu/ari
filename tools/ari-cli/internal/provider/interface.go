package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type MessageRole string

const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleTool      MessageRole = "tool"
)

type ToolChoice string

const (
	ToolChoiceAuto     ToolChoice = "auto"
	ToolChoiceNone     ToolChoice = "none"
	ToolChoiceRequired ToolChoice = "required"
)

type FinishReason string

const (
	FinishReasonStop      FinishReason = "stop"
	FinishReasonLength    FinishReason = "length"
	FinishReasonToolCalls FinishReason = "tool_calls"
)

type Message struct {
	Role       MessageRole `json:"role"`
	Content    string      `json:"content,omitempty"`
	Name       string      `json:"name,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

type CompletionRequest struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	ToolChoice  ToolChoice       `json:"tool_choice,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Metadata    map[string]any   `json:"metadata,omitempty"`
}

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type CompletionResponse struct {
	Model        string       `json:"model"`
	Message      Message      `json:"message"`
	FinishReason FinishReason `json:"finish_reason"`
	Usage        TokenUsage   `json:"usage"`
}

type Provider interface {
	Name() string
	Complete(ctx context.Context, request CompletionRequest) (CompletionResponse, error)
}

var (
	ErrUnknownProvider        = errors.New("unknown provider")
	ErrProviderNameRequired   = errors.New("provider name is required")
	ErrProviderNil            = errors.New("provider is nil")
	ErrProviderAlreadyDefined = errors.New("provider already registered")
)

type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

func (r *Registry) Register(name string, provider Provider) error {
	canonical := canonicalProviderName(name)
	if canonical == "" {
		return ErrProviderNameRequired
	}
	if provider == nil {
		return ErrProviderNil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[canonical]; exists {
		return fmt.Errorf("%w: %q", ErrProviderAlreadyDefined, canonical)
	}

	r.providers[canonical] = provider
	return nil
}

func (r *Registry) Resolve(name string) (Provider, error) {
	canonical := canonicalProviderName(name)
	if canonical == "" {
		return nil, ErrProviderNameRequired
	}

	r.mu.RLock()
	provider, ok := r.providers[canonical]
	available := providerNamesLocked(r.providers)
	r.mu.RUnlock()

	if ok {
		return provider, nil
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, canonical)
	}

	return nil, fmt.Errorf("%w: %q (available: %s)", ErrUnknownProvider, canonical, strings.Join(available, ", "))
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return providerNamesLocked(r.providers)
}

func canonicalProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func providerNamesLocked(providers map[string]Provider) []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
