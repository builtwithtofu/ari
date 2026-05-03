package daemon

import (
	"fmt"
	"strings"
)

type HarnessRegistry struct {
	factories map[string]HarnessFactory
}

func NewHarnessRegistry() *HarnessRegistry {
	return &HarnessRegistry{factories: make(map[string]HarnessFactory)}
}

func NewDefaultHarnessRegistry() *HarnessRegistry {
	registry := NewHarnessRegistry()
	if err := registry.Register(HarnessNameCodex, func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = sink
		return NewCodexExecutor(primaryFolder), nil
	}); err != nil {
		panic(fmt.Sprintf("register default Codex harness: %v", err))
	}
	if err := registry.Register(HarnessNameClaude, func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = sink
		return NewClaudeExecutor(primaryFolder), nil
	}); err != nil {
		panic(fmt.Sprintf("register default Claude harness: %v", err))
	}
	if err := registry.Register(HarnessNameOpenCode, func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = sink
		return NewOpenCodeExecutor(primaryFolder), nil
	}); err != nil {
		panic(fmt.Sprintf("register default OpenCode harness: %v", err))
	}
	if err := registry.Register(HarnessNamePTY, func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		return NewPTYExecutorWithSink(req.Command, req.Args, primaryFolder, sink), nil
	}); err != nil {
		panic(fmt.Sprintf("register default PTY harness: %v", err))
	}
	return registry
}

func (r *HarnessRegistry) Register(name string, factory HarnessFactory) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("harness name is required")
	}
	if factory == nil {
		return fmt.Errorf("harness factory is required")
	}
	if r == nil {
		return fmt.Errorf("harness registry is required")
	}
	if r.factories == nil {
		r.factories = make(map[string]HarnessFactory)
	}
	if r.factories[name] != nil {
		return fmt.Errorf("harness %q is already registered", name)
	}
	r.factories[name] = factory
	return nil
}

func (r *HarnessRegistry) ReplaceForTest(name string, factory HarnessFactory) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("harness name is required")
	}
	if factory == nil {
		return fmt.Errorf("harness factory is required")
	}
	if r == nil {
		return fmt.Errorf("harness registry is required")
	}
	if r.factories == nil {
		r.factories = make(map[string]HarnessFactory)
	}
	r.factories[name] = factory
	return nil
}

func (r *HarnessRegistry) Resolve(name string) (HarnessFactory, bool) {
	if r == nil || r.factories == nil {
		return nil, false
	}
	factory := r.factories[strings.TrimSpace(name)]
	return factory, factory != nil
}
