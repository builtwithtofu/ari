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
	expectedObservation := map[string][]HarnessObservationCapability{
		HarnessNameClaude:   {HarnessObservationUnsupported},
		HarnessNameCodex:    {HarnessObservationEventStream},
		HarnessNameOpenCode: {HarnessObservationUnsupported},
	}
	expectedDelivery := map[string][]HarnessDeliveryCapability{
		HarnessNameClaude: {HarnessDeliveryVisiblePromptTurn},
		HarnessNameCodex:  {HarnessDeliveryVisiblePromptTurn},
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
		if descriptor.Auth.StatusCheck == "" || descriptor.Auth.CredentialOwner != HarnessCredentialOwnerProvider {
			t.Fatalf("%s auth descriptor = %#v, want provider-owned auth capability metadata", descriptor.Name, descriptor.Auth)
		}
		if descriptor.Name != HarnessNameCodex && descriptor.Name != HarnessNameOpenCode && descriptor.Name != HarnessNameClaude && descriptor.Auth.NamedSlotExecution != HarnessAuthSupportUnsupported {
			t.Fatalf("%s named slot execution = %q, want current unsupported capability", descriptor.Name, descriptor.Auth.NamedSlotExecution)
		}
		if descriptor.Name == HarnessNameClaude && descriptor.Auth.NamedSlotExecution != HarnessAuthSupportPartial {
			t.Fatalf("%s named slot execution = %q, want partial CLAUDE_CONFIG_DIR capability", descriptor.Name, descriptor.Auth.NamedSlotExecution)
		}
		if (descriptor.Name == HarnessNameCodex || descriptor.Name == HarnessNameOpenCode) && descriptor.Auth.NamedSlotExecution != HarnessAuthSupportSupported {
			t.Fatalf("%s named slot execution = %q, want supported named-slot capability", descriptor.Name, descriptor.Auth.NamedSlotExecution)
		}
		if len(descriptor.Auth.RiskLabels) == 0 || len(descriptor.Auth.Caveats) == 0 {
			t.Fatalf("%s auth descriptor = %#v, want risk labels and caveats", descriptor.Name, descriptor.Auth)
		}
		for _, capability := range required {
			if !harnessCapabilitiesContain(descriptor.Capabilities, capability) {
				t.Fatalf("%s capabilities = %#v, want %s", descriptor.Name, descriptor.Capabilities, capability)
			}
		}
		for _, capability := range expectedObservation[descriptor.Name] {
			if !harnessObservationCapabilitiesContain(descriptor.ObservationCapabilities, capability) {
				t.Fatalf("%s observation capabilities = %#v, want %s", descriptor.Name, descriptor.ObservationCapabilities, capability)
			}
		}
		for _, capability := range expectedDelivery[descriptor.Name] {
			if !harnessDeliveryCapabilitiesContain(descriptor.DeliveryCapabilities, capability) {
				t.Fatalf("%s delivery capabilities = %#v, want %s", descriptor.Name, descriptor.DeliveryCapabilities, capability)
			}
		}
	}
}

func TestProviderAuthDescriptorsMatchCurrentHarnessBehavior(t *testing.T) {
	tests := []struct {
		name               string
		descriptor         HarnessAuthDescriptor
		login              HarnessAuthSupport
		loginMethods       []string
		logout             HarnessAuthSupport
		namedSlotStatus    HarnessAuthSupport
		namedSlotExecution HarnessAuthSupport
		slotScope          string
		riskLabels         []string
		caveats            []string
	}{
		{name: HarnessNameClaude, descriptor: NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo"}).Descriptor().Auth, login: HarnessAuthSupportPartial, loginMethods: []string{"browser", "console", "api_key"}, logout: HarnessAuthSupportSupported, namedSlotStatus: HarnessAuthSupportPartial, namedSlotExecution: HarnessAuthSupportPartial, slotScope: "claude_config_dir", riskLabels: []string{"provider_owned", "client_side_login", "native_config_root_isolation", "keychain_slot_isolation_risk"}, caveats: []string{"client_side_login", "claude_named_slots_use_per_slot_config_dir", "macos_keychain_limits_named_slot_isolation"}},
		{name: HarnessNameCodex, descriptor: NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo"}).Descriptor().Auth, login: HarnessAuthSupportSupported, loginMethods: []string{"browser", "device_code", "api_key"}, logout: HarnessAuthSupportSupported, namedSlotStatus: HarnessAuthSupportSupported, namedSlotExecution: HarnessAuthSupportSupported, slotScope: "codex_home", riskLabels: []string{"provider_owned", "native_config_root_isolation"}, caveats: []string{"codex_named_slots_use_per_slot_codex_home"}},
		{name: HarnessNameOpenCode, descriptor: NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo"}).Descriptor().Auth, login: HarnessAuthSupportPartial, loginMethods: []string{"opencode_interactive"}, logout: HarnessAuthSupportSupported, namedSlotStatus: HarnessAuthSupportPartial, namedSlotExecution: HarnessAuthSupportSupported, slotScope: "ari_auth_content", riskLabels: []string{"provider_owned", "provider_hint_matching", "ari_projected_auth_content", "env_projection_downgrade_risk"}, caveats: []string{"provider_hint_status", "provider_methods_discovery_is_optional", "named_execution_requires_ari_secret_grant"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := tt.descriptor
			if auth.StatusCheck != HarnessAuthSupportSupported || auth.Login != tt.login || auth.Logout != tt.logout || auth.NamedSlotStatus != tt.namedSlotStatus || auth.NamedSlotExecution != tt.namedSlotExecution || auth.SlotScope != tt.slotScope || auth.CredentialOwner != HarnessCredentialOwnerProvider {
				t.Fatalf("auth descriptor = %#v, want current %s capability facts", auth, tt.name)
			}
			for _, method := range tt.loginMethods {
				if !stringsContain(auth.LoginMethods, method) {
					t.Fatalf("%s login methods = %#v, want %q", tt.name, auth.LoginMethods, method)
				}
			}
			for _, label := range tt.riskLabels {
				if !stringsContain(auth.RiskLabels, label) {
					t.Fatalf("%s risks = %#v, want %q", tt.name, auth.RiskLabels, label)
				}
			}
			for _, caveat := range tt.caveats {
				if !stringsContain(auth.Caveats, caveat) {
					t.Fatalf("%s caveats = %#v, want %q", tt.name, auth.Caveats, caveat)
				}
			}
		})
	}
}

func stringsContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func harnessObservationCapabilitiesContain(values []HarnessObservationCapability, want HarnessObservationCapability) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func harnessDeliveryCapabilitiesContain(values []HarnessDeliveryCapability, want HarnessDeliveryCapability) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
