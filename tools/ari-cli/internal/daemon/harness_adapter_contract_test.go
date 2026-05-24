package daemon

import "testing"

func TestHarnessAdapterDescriptorsAdvertiseSharedRuntimeContract(t *testing.T) {
	required := []HarnessCapability{
		HarnessCapabilityHarnessSessionFromContext,
		HarnessCapabilityContextPacket,
		HarnessCapabilityTimelineItems,
		HarnessCapabilityFinalResponse,
		HarnessCapabilityMeasuredTokenTelemetry,
	}
	adapters := []HarnessDescriber{
		NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo"}),
		NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo"}),
		NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo"}),
	}

	for _, adapter := range adapters {
		descriptor := adapter.Descriptor()
		if descriptor.Name == "" {
			t.Fatalf("descriptor = %#v, want harness name", descriptor)
		}
		for _, capability := range required {
			if !harnessCapabilitiesContain(descriptor.Capabilities, capability) {
				t.Fatalf("%s capabilities = %#v, want %s", descriptor.Name, descriptor.Capabilities, capability)
			}
		}
	}
}
