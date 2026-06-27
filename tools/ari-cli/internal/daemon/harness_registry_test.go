package daemon

import (
	"context"
	"strings"
	"testing"
)

type registryDescriptorHarness struct {
	adapterLifecycle[struct{}]
	descriptor HarnessAdapterDescriptor
}

func newRegistryDescriptorHarness(descriptor HarnessAdapterDescriptor) *registryDescriptorHarness {
	name := descriptor.Name
	if name == "" {
		name = "test"
	}
	return &registryDescriptorHarness{adapterLifecycle: newAdapterLifecycle[struct{}](name), descriptor: descriptor}
}

func (h *registryDescriptorHarness) Descriptor() HarnessAdapterDescriptor {
	return h.descriptor
}

func (h *registryDescriptorHarness) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	_ = ctx
	return ExecutorRun{RunID: req.RunID, SessionID: req.SessionID, Executor: h.descriptor.Name}, nil
}

func (h *registryDescriptorHarness) AttemptWorkspaceDelivery(ctx context.Context, attempt WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error) {
	_ = ctx
	_ = attempt
	return failedWorkspaceDeliveryAttempt("test harness does not support workspace delivery"), nil
}

func (h *registryDescriptorHarness) AuthStatus(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	_ = ctx
	return unsupportedHarnessAuthStatus(h.descriptor.Name, slot), nil
}

func TestHarnessRegistryRejectsInvalidRegistration(t *testing.T) {
	registry := NewHarnessRegistry()
	if err := registry.Register("", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("Register returned nil error for empty name")
	}
	if err := registry.Register("test", nil); err == nil {
		t.Fatal("Register returned nil error for nil factory")
	}
}

func TestHarnessRegistryRejectsDuplicateRegistration(t *testing.T) {
	registry := NewHarnessRegistry()
	factory := func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		return nil, nil
	}
	if err := registry.Register("test", factory); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if err := registry.Register("test", factory); err == nil {
		t.Fatal("Register returned nil error for duplicate harness")
	}
}

func TestHarnessRegistryResolveUnknownHarness(t *testing.T) {
	registry := NewHarnessRegistry()
	if factory, ok := registry.Resolve("missing"); ok || factory != nil {
		t.Fatalf("Resolve returned factory=%v ok=%v, want missing", factory, ok)
	}
}

func TestHarnessRegistryReplaceForTestAllowsInjection(t *testing.T) {
	registry := NewHarnessRegistry()
	factory := func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		return nil, nil
	}
	if err := registry.ReplaceForTest("test", factory); err != nil {
		t.Fatalf("ReplaceForTest returned error: %v", err)
	}
	if resolved, ok := registry.Resolve("test"); !ok || resolved == nil {
		t.Fatalf("Resolve returned factory=%v ok=%v, want injected factory", resolved, ok)
	}
}

func TestHarnessRegistryResolvesDescriptorFromAdapterContract(t *testing.T) {
	registry := NewHarnessRegistry()
	descriptor := HarnessAdapterDescriptor{Name: "test", Auth: HarnessAuthDescriptor{StatusCheck: HarnessAuthSupportSupported, CredentialOwner: HarnessCredentialOwnerProvider, NamedSlotExecution: HarnessAuthSupportUnsupported}}
	factory := func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newRegistryDescriptorHarness(descriptor), nil
	}
	if err := registry.Register("test", factory, descriptor); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	got, ok := registry.Descriptor("test")
	if !ok || got.Name != "test" || got.Auth.StatusCheck != HarnessAuthSupportSupported || got.Auth.CredentialOwner != HarnessCredentialOwnerProvider {
		t.Fatalf("Descriptor returned %#v ok=%v, want registered auth descriptor", got, ok)
	}
}

func TestHarnessRegistryStoresObservationDeliveryOnlyDescriptor(t *testing.T) {
	registry := NewHarnessRegistry()
	descriptor := HarnessAdapterDescriptor{
		Name:                    "streaming",
		ObservationCapabilities: []HarnessObservationCapability{HarnessObservationEventStream},
		DeliveryCapabilities:    []HarnessDeliveryCapability{HarnessDeliveryVisiblePromptTurn},
	}
	factory := func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newRegistryDescriptorHarness(descriptor), nil
	}
	if err := registry.Register("streaming", factory, descriptor); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	got, ok := registry.Descriptor("streaming")
	if !ok || got.Name != "streaming" || !harnessObservationCapabilitiesContain(got.ObservationCapabilities, HarnessObservationEventStream) || !harnessDeliveryCapabilitiesContain(got.DeliveryCapabilities, HarnessDeliveryVisiblePromptTurn) {
		t.Fatalf("Descriptor returned %#v ok=%v, want observation/delivery descriptor", got, ok)
	}
}

func TestDefaultHarnessRegistryProvidesProviderAuthDescriptors(t *testing.T) {
	registry := NewDefaultHarnessRegistry()
	for _, harness := range []string{HarnessNameClaude, HarnessNameCodex, HarnessNameOpenCode, HarnessNameGrok} {
		descriptor, ok := registry.Descriptor(harness)
		if !ok || descriptor.Auth.StatusCheck == HarnessAuthSupportUnsupported || descriptor.Auth.CredentialOwner != HarnessCredentialOwnerProvider {
			t.Fatalf("%s descriptor = %#v ok=%v, want provider auth descriptor", harness, descriptor, ok)
		}
	}
}

func TestHarnessRegistryReplaceForTestClearsDescriptorWhenOmitted(t *testing.T) {
	registry := NewHarnessRegistry()
	descriptor := HarnessAdapterDescriptor{Name: "replaceable", Auth: HarnessAuthDescriptor{StatusCheck: HarnessAuthSupportSupported}}
	factory := func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newRegistryDescriptorHarness(descriptor), nil
	}
	if err := registry.Register("replaceable", factory, descriptor); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if _, ok := registry.Descriptor("replaceable"); !ok {
		t.Fatal("Descriptor missing before replace")
	}
	if err := registry.ReplaceForTest("replaceable", factory); err != nil {
		t.Fatalf("ReplaceForTest returned error: %v", err)
	}
	if got, ok := registry.Descriptor("replaceable"); ok {
		t.Fatalf("Descriptor after replace = %#v, want cleared", got)
	}
}

func TestAdapterLifecycleStopFailsWhenAdapterDoesNotImplementStop(t *testing.T) {
	lifecycle := newAdapterLifecycle[struct{}]("test-harness")
	if err := lifecycle.Stop(context.Background(), "run-1"); err == nil || !strings.Contains(err.Error(), "does not implement stop") {
		t.Fatalf("Stop error = %v, want explicit unsupported stop", err)
	}
}

func TestHarnessExecutorsUseExplicitExecutableOverrides(t *testing.T) {
	t.Setenv(EnvCodexExecutable, "/opt/agents/codex")
	t.Setenv(EnvClaudeExecutable, "/opt/agents/claude")
	t.Setenv(EnvOpenCodeExecutable, "/opt/agents/opencode")

	if got := NewCodexExecutor("/repo").options.Executable; got != "/opt/agents/codex" {
		t.Fatalf("Codex executable = %q, want override", got)
	}
	if got := NewClaudeExecutor("/repo").options.Executable; got != "/opt/agents/claude" {
		t.Fatalf("Claude executable = %q, want override", got)
	}
	if got := NewOpenCodeExecutor("/repo").options.Executable; got != "/opt/agents/opencode" {
		t.Fatalf("OpenCode executable = %q, want override", got)
	}
}

func TestHarnessExecutorsDefaultToSupportedCommandNames(t *testing.T) {
	if got := NewCodexExecutor("/repo").options.Executable; got != "codex" {
		t.Fatalf("Codex executable = %q, want codex", got)
	}
	if got := NewClaudeExecutor("/repo").options.Executable; got != "claude" {
		t.Fatalf("Claude executable = %q, want claude", got)
	}
	if got := NewOpenCodeExecutor("/repo").options.Executable; got != "opencode" {
		t.Fatalf("OpenCode executable = %q, want opencode", got)
	}
}
