package daemon

import (
	"fmt"
	"strings"
)

type HarnessRegistry struct {
	factories   map[string]HarnessFactory
	descriptors map[string]HarnessAdapterDescriptor
}

func NewHarnessRegistry() *HarnessRegistry {
	return &HarnessRegistry{factories: make(map[string]HarnessFactory), descriptors: make(map[string]HarnessAdapterDescriptor)}
}

func NewDefaultHarnessRegistry() *HarnessRegistry {
	registry := NewHarnessRegistry()
	if err := registry.Register(HarnessNameCodex, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = sink
		return NewCodexExecutor(primaryFolder), nil
	}, NewCodexExecutor("").Descriptor()); err != nil {
		panic(fmt.Sprintf("register default Codex harness: %v", err))
	}
	if err := registry.Register(HarnessNameClaude, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = sink
		return NewClaudeExecutor(primaryFolder), nil
	}, NewClaudeExecutor("").Descriptor()); err != nil {
		panic(fmt.Sprintf("register default Claude harness: %v", err))
	}
	if err := registry.Register(HarnessNameOpenCode, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = sink
		executor := NewOpenCodeExecutor(primaryFolder)
		executor.options.AuthProjection = req.AuthProjection
		return executor, nil
	}, NewOpenCodeExecutor("").Descriptor()); err != nil {
		panic(fmt.Sprintf("register default OpenCode harness: %v", err))
	}
	if err := registry.Register(HarnessNamePi, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = sink
		executor := NewPiExecutor(primaryFolder)
		executor.options.AuthProjection = req.AuthProjection
		return executor, nil
	}, NewPiExecutor("").Descriptor()); err != nil {
		panic(fmt.Sprintf("register default pi harness: %v", err))
	}
	if err := registry.Register(HarnessNameGrok, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = sink
		return NewGrokExecutor(primaryFolder), nil
	}, NewGrokExecutor("").Descriptor()); err != nil {
		panic(fmt.Sprintf("register default grok harness: %v", err))
	}
	if err := registry.Register(HarnessNamePTY, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		return NewPTYExecutorWithSink(req.Command, req.Args, primaryFolder, sink), nil
	}, NewPTYExecutor("", nil, "").Descriptor()); err != nil {
		panic(fmt.Sprintf("register default PTY harness: %v", err))
	}
	return registry
}

func (r *HarnessRegistry) Register(name string, factory HarnessFactory, descriptors ...HarnessAdapterDescriptor) error {
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
	if r.descriptors == nil {
		r.descriptors = make(map[string]HarnessAdapterDescriptor)
	}
	if r.factories[name] != nil {
		return fmt.Errorf("harness %q is already registered", name)
	}
	r.factories[name] = factory
	if len(descriptors) > 0 {
		r.descriptors[name] = normalizeHarnessDescriptor(name, descriptors[0])
	}
	return nil
}

func (r *HarnessRegistry) ReplaceForTest(name string, factory HarnessFactory, descriptors ...HarnessAdapterDescriptor) error {
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
	if r.descriptors == nil {
		r.descriptors = make(map[string]HarnessAdapterDescriptor)
	}
	r.factories[name] = factory
	if len(descriptors) > 0 {
		r.descriptors[name] = normalizeHarnessDescriptor(name, descriptors[0])
	}
	return nil
}

func (r *HarnessRegistry) Resolve(name string) (HarnessFactory, bool) {
	if r == nil || r.factories == nil {
		return nil, false
	}
	factory := r.factories[strings.TrimSpace(name)]
	return factory, factory != nil
}

func (r *HarnessRegistry) Descriptor(name string) (HarnessAdapterDescriptor, bool) {
	if r == nil || r.descriptors == nil {
		return HarnessAdapterDescriptor{}, false
	}
	descriptor, ok := r.descriptors[strings.TrimSpace(name)]
	return descriptor, ok
}

func normalizeHarnessDescriptor(name string, descriptor HarnessAdapterDescriptor) HarnessAdapterDescriptor {
	if strings.TrimSpace(descriptor.Name) == "" {
		descriptor.Name = strings.TrimSpace(name)
	}
	return descriptor
}
