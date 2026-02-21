package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Request struct {
	SessionID string         `json:"session_id"`
	State     State          `json:"state"`
	Input     string         `json:"input,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type Response struct {
	State    State          `json:"state"`
	Output   string         `json:"output,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type Agent interface {
	Name() string
	Run(ctx context.Context, request Request) (Response, error)
}

var (
	ErrUnknownAgent        = errors.New("unknown agent")
	ErrAgentNameRequired   = errors.New("agent name is required")
	ErrAgentNil            = errors.New("agent is nil")
	ErrAgentAlreadyDefined = errors.New("agent already registered")
)

type Registry struct {
	mu     sync.RWMutex
	agents map[string]Agent
}

func NewRegistry() *Registry {
	return &Registry{agents: make(map[string]Agent)}
}

func (r *Registry) Register(name string, agent Agent) error {
	canonical := canonicalAgentName(name)
	if canonical == "" {
		return ErrAgentNameRequired
	}
	if agent == nil {
		return ErrAgentNil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.agents[canonical]; exists {
		return fmt.Errorf("%w: %q", ErrAgentAlreadyDefined, canonical)
	}

	r.agents[canonical] = agent
	return nil
}

func (r *Registry) Resolve(name string) (Agent, error) {
	canonical := canonicalAgentName(name)
	if canonical == "" {
		return nil, ErrAgentNameRequired
	}

	r.mu.RLock()
	agent, ok := r.agents[canonical]
	available := agentNamesLocked(r.agents)
	r.mu.RUnlock()

	if ok {
		return agent, nil
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrUnknownAgent, canonical)
	}

	return nil, fmt.Errorf("%w: %q (available: %s)", ErrUnknownAgent, canonical, strings.Join(available, ", "))
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return agentNamesLocked(r.agents)
}

func canonicalAgentName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func agentNamesLocked(agents map[string]Agent) []string {
	names := make([]string, 0, len(agents))
	for name := range agents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
