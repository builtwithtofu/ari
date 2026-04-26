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
	for _, harness := range []struct {
		name       string
		executable string
		probe      string
	}{
		{name: HarnessNameCodex, executable: "codex", probe: "codex --version"},
		{name: HarnessNameClaude, executable: "claude", probe: "claude --version"},
		{name: HarnessNameOpenCode, executable: "opencode", probe: "opencode --version"},
	} {
		name := harness.name
		executable := harness.executable
		probe := harness.probe
		if err := registry.Register(name, func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
			_ = req
			_ = primaryFolder
			_ = sink
			return nil, &HarnessUnavailableError{Harness: name, Reason: "missing_executable", Executable: executable, Probe: probe, RequiredCapability: HarnessCapabilityAgentRunFromContext, StartInvoked: false}
		}); err != nil {
			panic(fmt.Sprintf("register default %s harness placeholder: %v", name, err))
		}
	}
	if err := registry.Register(HarnessNamePTY, func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
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
