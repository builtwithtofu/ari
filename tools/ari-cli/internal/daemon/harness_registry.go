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
	if err := registry.RegisterWithDescriptor(HarnessNameCodex, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = sink
		return NewCodexExecutor(primaryFolder), nil
	}, NewCodexExecutor("").Descriptor()); err != nil {
		panic(fmt.Sprintf("register default Codex harness: %v", err))
	}
	if err := registry.RegisterWithDescriptor(HarnessNameClaude, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = sink
		return NewClaudeExecutor(primaryFolder), nil
	}, NewClaudeExecutor("").Descriptor()); err != nil {
		panic(fmt.Sprintf("register default Claude harness: %v", err))
	}
	if err := registry.RegisterWithDescriptor(HarnessNameOpenCode, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = sink
		executor := NewOpenCodeExecutor(primaryFolder)
		executor.options.AuthProjection = req.AuthProjection
		return executor, nil
	}, NewOpenCodeExecutor("").Descriptor()); err != nil {
		panic(fmt.Sprintf("register default OpenCode harness: %v", err))
	}
	if err := registry.RegisterWithDescriptor(HarnessNamePi, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = sink
		executor := NewPiExecutor(primaryFolder)
		executor.options.AuthProjection = req.AuthProjection
		return executor, nil
	}, NewPiExecutor("").Descriptor()); err != nil {
		panic(fmt.Sprintf("register default pi harness: %v", err))
	}
	if err := registry.RegisterWithDescriptor(HarnessNameGrok, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = sink
		return NewGrokExecutor(primaryFolder), nil
	}, NewGrokExecutor("").Descriptor()); err != nil {
		panic(fmt.Sprintf("register default grok harness: %v", err))
	}
	if err := registry.Register(HarnessNamePTY, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		return NewPTYExecutorWithSink(req.Command, req.Args, primaryFolder, sink), nil
	}); err != nil {
		panic(fmt.Sprintf("register default PTY harness: %v", err))
	}
	return registry
}

func (r *HarnessRegistry) Register(name string, factory HarnessFactory) error {
	return r.RegisterWithDescriptor(name, factory, HarnessAdapterDescriptor{})
}

func (r *HarnessRegistry) RegisterWithDescriptor(name string, factory HarnessFactory, descriptor HarnessAdapterDescriptor) error {
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
	if harnessAdapterDescriptorHasContract(descriptor) {
		descriptor.Name = name
		r.descriptors[name] = descriptor
	}
	return nil
}

func (r *HarnessRegistry) ReplaceForTest(name string, factory HarnessFactory) error {
	return r.ReplaceForTestWithDescriptor(name, factory, HarnessAdapterDescriptor{})
}

func (r *HarnessRegistry) ReplaceForTestWithDescriptor(name string, factory HarnessFactory, descriptor HarnessAdapterDescriptor) error {
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
	if harnessAdapterDescriptorHasContract(descriptor) {
		descriptor.Name = name
		r.descriptors[name] = descriptor
	}
	return nil
}

func harnessAdapterDescriptorHasContract(descriptor HarnessAdapterDescriptor) bool {
	return descriptor.Name != "" ||
		descriptor.DisplayName != "" ||
		len(descriptor.Capabilities) > 0 ||
		len(descriptor.ObservationCapabilities) > 0 ||
		len(descriptor.DeliveryCapabilities) > 0 ||
		len(descriptor.InvocationModes) > 0 ||
		descriptor.Auth.StatusCheck != ""
}

func (r *HarnessRegistry) Resolve(name string) (HarnessFactory, bool) {
	if r == nil || r.factories == nil {
		return nil, false
	}
	factory := r.factories[strings.TrimSpace(name)]
	return factory, factory != nil
}

func (r *HarnessRegistry) ResolveDescriptor(name string) (HarnessAdapterDescriptor, bool) {
	if r == nil || r.descriptors == nil {
		return HarnessAdapterDescriptor{}, false
	}
	descriptor, ok := r.descriptors[strings.TrimSpace(name)]
	return descriptor, ok
}
