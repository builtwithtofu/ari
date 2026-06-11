package daemon

import "testing"

func TestHarnessRegistryRejectsInvalidRegistration(t *testing.T) {
	registry := NewHarnessRegistry()
	if err := registry.Register("", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
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
	factory := func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
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
	factory := func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		return nil, nil
	}
	if err := registry.ReplaceForTest("test", factory); err != nil {
		t.Fatalf("ReplaceForTest returned error: %v", err)
	}
	if resolved, ok := registry.Resolve("test"); !ok || resolved == nil {
		t.Fatalf("Resolve returned factory=%v ok=%v, want injected factory", resolved, ok)
	}
}

func TestHarnessRegistryResolvesDescriptorWithoutConstructingExecutor(t *testing.T) {
	registry := NewHarnessRegistry()
	factory := func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		return nil, nil
	}
	descriptor := HarnessAdapterDescriptor{Name: "test", Auth: HarnessAuthDescriptor{StatusCheck: HarnessAuthSupportSupported, CredentialOwner: HarnessCredentialOwnerProvider, NamedSlotExecution: HarnessAuthSupportUnsupported}}
	if err := registry.RegisterWithDescriptor("test", factory, descriptor); err != nil {
		t.Fatalf("RegisterWithDescriptor returned error: %v", err)
	}

	got, ok := registry.ResolveDescriptor("test")
	if !ok || got.Name != "test" || got.Auth.StatusCheck != HarnessAuthSupportSupported || got.Auth.CredentialOwner != HarnessCredentialOwnerProvider {
		t.Fatalf("ResolveDescriptor returned %#v ok=%v, want registered auth descriptor", got, ok)
	}
}

func TestHarnessRegistryStoresObservationDeliveryOnlyDescriptor(t *testing.T) {
	registry := NewHarnessRegistry()
	factory := func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		return nil, nil
	}
	descriptor := HarnessAdapterDescriptor{
		ObservationCapabilities: []HarnessObservationCapability{HarnessObservationEventStream},
		DeliveryCapabilities:    []HarnessDeliveryCapability{HarnessDeliveryVisiblePromptTurn},
	}
	if err := registry.RegisterWithDescriptor("streaming", factory, descriptor); err != nil {
		t.Fatalf("RegisterWithDescriptor returned error: %v", err)
	}

	got, ok := registry.ResolveDescriptor("streaming")
	if !ok || got.Name != "streaming" || !harnessObservationCapabilitiesContain(got.ObservationCapabilities, HarnessObservationEventStream) || !harnessDeliveryCapabilitiesContain(got.DeliveryCapabilities, HarnessDeliveryVisiblePromptTurn) {
		t.Fatalf("ResolveDescriptor returned %#v ok=%v, want observation/delivery descriptor", got, ok)
	}
}

func TestDefaultHarnessRegistryProvidesProviderAuthDescriptors(t *testing.T) {
	registry := NewDefaultHarnessRegistry()
	for _, harness := range []string{HarnessNameClaude, HarnessNameCodex, HarnessNameOpenCode} {
		descriptor, ok := registry.ResolveDescriptor(harness)
		if !ok || descriptor.Auth.StatusCheck != HarnessAuthSupportSupported || descriptor.Auth.CredentialOwner != HarnessCredentialOwnerProvider {
			t.Fatalf("%s descriptor = %#v ok=%v, want provider auth descriptor", harness, descriptor, ok)
		}
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
