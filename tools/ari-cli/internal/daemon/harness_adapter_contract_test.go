package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// harnessAdapterContractCase describes the per-harness facts every officially
// supported adapter must declare and prove. Adding a harness to the registry
// means adding one row here.
type harnessAdapterContractCase struct {
	name               string
	describer          HarnessDescriber
	observation        []HarnessObservationCapability
	delivery           []HarnessDeliveryCapability
	invocationModes    []HarnessInvocationMode
	login              HarnessAuthSupport
	loginMethods       []string
	logout             HarnessAuthSupport
	namedSlotStatus    HarnessAuthSupport
	namedSlotExecution HarnessAuthSupport
	slotScope          string
	riskLabels         []string
	caveats            []string
	// startCall runs a sticky StartHarnessCallResult against injected fakes
	// so the adapter-reported session ref can be asserted.
	startCall       func(t *testing.T) HarnessCallResult
	wantPersistence HarnessSessionPersistence
	wantResumeMode  HarnessResumeMode
	wantCursorKey   string
}

func harnessAdapterContractCases() []harnessAdapterContractCase {
	contractPacket := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	return []harnessAdapterContractCase{
		{
			name:               HarnessNameClaude,
			describer:          NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo"}),
			observation:        []HarnessObservationCapability{HarnessObservationUnsupported},
			delivery:           []HarnessDeliveryCapability{HarnessDeliveryVisiblePromptTurn},
			invocationModes:    []HarnessInvocationMode{HarnessInvocationModeHeadless, HarnessInvocationModeBackground},
			login:              HarnessAuthSupportPartial,
			loginMethods:       []string{"browser", "console", "api_key"},
			logout:             HarnessAuthSupportSupported,
			namedSlotStatus:    HarnessAuthSupportPartial,
			namedSlotExecution: HarnessAuthSupportPartial,
			slotScope:          "claude_config_dir",
			riskLabels:         []string{"provider_owned", "client_side_login", "native_config_root_isolation", "keychain_slot_isolation_risk"},
			caveats:            []string{"client_side_login", "claude_named_slots_use_per_slot_config_dir", "macos_keychain_limits_named_slot_isolation"},
			startCall: func(t *testing.T) HarnessCallResult {
				t.Helper()
				runner := &fakeClaudeRunner{output: []byte(`backgrounded · 7c5dcf5d`)}
				executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: runner.Run})
				result, err := StartExecutorRunResult(context.Background(), executor, contractPacket, "", Profile{Name: "builder", Harness: HarnessNameClaude, InvocationClass: HarnessInvocationSticky})
				if err != nil {
					t.Fatalf("StartExecutorRunResult returned error: %v", err)
				}
				return result
			},
			wantPersistence: HarnessSessionPersistent,
			wantResumeMode:  HarnessResumeCLIFlag,
			wantCursorKey:   "session_id",
		},
		{
			name:               HarnessNameCodex,
			describer:          NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo"}),
			observation:        []HarnessObservationCapability{HarnessObservationEventStream},
			delivery:           []HarnessDeliveryCapability{HarnessDeliveryVisiblePromptTurn},
			invocationModes:    []HarnessInvocationMode{HarnessInvocationModeServer},
			login:              HarnessAuthSupportSupported,
			loginMethods:       []string{"browser", "device_code", "api_key"},
			logout:             HarnessAuthSupportSupported,
			namedSlotStatus:    HarnessAuthSupportSupported,
			namedSlotExecution: HarnessAuthSupportSupported,
			slotScope:          "codex_home",
			riskLabels:         []string{"provider_owned", "native_config_root_isolation"},
			caveats:            []string{"codex_named_slots_use_per_slot_codex_home"},
			startCall: func(t *testing.T) HarnessCallResult {
				t.Helper()
				transport := newFakeCodexTransport([]codexNotification{
					{Method: "item/completed", Params: json.RawMessage(`{"item":{"id":"item_1","type":"agentMessage","text":"done"}}`)},
					{Method: "turn/completed", Params: json.RawMessage(`{}`)},
				})
				executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo", StartTransport: fakeCodexStarter(transport)})
				result, err := StartExecutorRunResult(context.Background(), executor, contractPacket, "", Profile{Name: "builder", Harness: HarnessNameCodex, InvocationClass: HarnessInvocationSticky})
				if err != nil {
					t.Fatalf("StartExecutorRunResult returned error: %v", err)
				}
				return result
			},
			wantPersistence: HarnessSessionPersistent,
			wantResumeMode:  HarnessResumeJSONRPC,
			wantCursorKey:   "thread_id",
		},
		{
			name:               HarnessNameOpenCode,
			describer:          NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo"}),
			observation:        []HarnessObservationCapability{HarnessObservationUnsupported},
			delivery:           []HarnessDeliveryCapability{HarnessDeliveryVisiblePromptTurn},
			invocationModes:    []HarnessInvocationMode{HarnessInvocationModeHeadless},
			login:              HarnessAuthSupportPartial,
			loginMethods:       []string{"opencode_interactive"},
			logout:             HarnessAuthSupportSupported,
			namedSlotStatus:    HarnessAuthSupportPartial,
			namedSlotExecution: HarnessAuthSupportSupported,
			slotScope:          "ari_auth_content",
			riskLabels:         []string{"provider_owned", "provider_hint_matching", "ari_projected_auth_content", "env_projection_downgrade_risk"},
			caveats:            []string{"provider_hint_status", "provider_methods_discovery_is_optional", "named_execution_requires_ari_secret_grant"},
			startCall: func(t *testing.T) HarnessCallResult {
				t.Helper()
				runner := &fakeOpenCodeRunner{output: []byte(strings.Join([]string{
					`{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"busy"}}}`,
					`{"type":"message.part.updated","properties":{"part":{"id":"part_1","sessionID":"sess_123","messageID":"msg_123","type":"text","text":"done"}}}`,
					`{"type":"session.status","properties":{"sessionID":"sess_123","status":{"type":"idle"}}}`,
				}, "\n"))}
				executor := NewOpenCodeExecutorForTest(opencodeExecutorOptions{Executable: "opencode", Cwd: "/repo", RunCommand: runner.Run})
				result, err := StartExecutorRunResult(context.Background(), executor, contractPacket, "", Profile{Name: "builder", Harness: HarnessNameOpenCode, InvocationClass: HarnessInvocationSticky})
				if err != nil {
					t.Fatalf("StartExecutorRunResult returned error: %v", err)
				}
				return result
			},
			wantPersistence: HarnessSessionPersistent,
			wantResumeMode:  HarnessResumeHTTPAPI,
			wantCursorKey:   "session_id",
		},
	}
}

func TestHarnessAdapterDescriptorsAdvertiseSharedRuntimeContract(t *testing.T) {
	required := sharedHarnessRuntimeCapabilities()
	for _, tt := range harnessAdapterContractCases() {
		t.Run(tt.name, func(t *testing.T) {
			descriptor := tt.describer.Descriptor()
			if descriptor.Name != tt.name {
				t.Fatalf("descriptor name = %q, want %q", descriptor.Name, tt.name)
			}
			for _, capability := range required {
				if !harnessCapabilitiesContain(descriptor.Capabilities, capability) {
					t.Fatalf("%s capabilities = %#v, want %s", descriptor.Name, descriptor.Capabilities, capability)
				}
			}
			if len(descriptor.InvocationModes) == 0 {
				t.Fatalf("%s invocation modes are empty, want declared modes", descriptor.Name)
			}
			for _, mode := range tt.invocationModes {
				if !harnessInvocationModesContain(descriptor.InvocationModes, mode) {
					t.Fatalf("%s invocation modes = %#v, want %s", descriptor.Name, descriptor.InvocationModes, mode)
				}
			}
			if len(descriptor.InvocationModes) != len(tt.invocationModes) {
				t.Fatalf("%s invocation modes = %#v, want exactly %#v", descriptor.Name, descriptor.InvocationModes, tt.invocationModes)
			}
			for _, capability := range tt.observation {
				if !harnessObservationCapabilitiesContain(descriptor.ObservationCapabilities, capability) {
					t.Fatalf("%s observation capabilities = %#v, want %s", descriptor.Name, descriptor.ObservationCapabilities, capability)
				}
			}
			for _, capability := range tt.delivery {
				if !harnessDeliveryCapabilitiesContain(descriptor.DeliveryCapabilities, capability) {
					t.Fatalf("%s delivery capabilities = %#v, want %s", descriptor.Name, descriptor.DeliveryCapabilities, capability)
				}
			}
		})
	}
}

func TestProviderAuthDescriptorsMatchCurrentHarnessBehavior(t *testing.T) {
	for _, tt := range harnessAdapterContractCases() {
		t.Run(tt.name, func(t *testing.T) {
			auth := tt.describer.Descriptor().Auth
			if auth.StatusCheck != HarnessAuthSupportSupported || auth.Login != tt.login || auth.Logout != tt.logout || auth.NamedSlotStatus != tt.namedSlotStatus || auth.NamedSlotExecution != tt.namedSlotExecution || auth.SlotScope != tt.slotScope || auth.CredentialOwner != HarnessCredentialOwnerProvider {
				t.Fatalf("auth descriptor = %#v, want current %s capability facts", auth, tt.name)
			}
			if len(auth.RiskLabels) == 0 || len(auth.Caveats) == 0 {
				t.Fatalf("%s auth descriptor = %#v, want risk labels and caveats", tt.name, auth)
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

func TestHarnessAdaptersReportSessionRefFacts(t *testing.T) {
	for _, tt := range harnessAdapterContractCases() {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.startCall(t)
			ref := result.SessionRef
			if err := ref.Validate(); err != nil {
				t.Fatalf("session ref %#v failed validation: %v", ref, err)
			}
			if ref.Persistence == HarnessSessionUnknown && ref.ResumeMode == HarnessResumeUnknown {
				t.Fatalf("session ref %#v is all-unknown, want adapter-reported facts", ref)
			}
			if ref.Persistence != tt.wantPersistence || ref.ResumeMode != tt.wantResumeMode {
				t.Fatalf("session ref persistence/resume = %q/%q, want %q/%q", ref.Persistence, ref.ResumeMode, tt.wantPersistence, tt.wantResumeMode)
			}
			if tt.wantCursorKey != "" {
				var cursor map[string]string
				if err := json.Unmarshal(ref.ResumeCursor, &cursor); err != nil {
					t.Fatalf("decode resume cursor %q: %v", string(ref.ResumeCursor), err)
				}
				if strings.TrimSpace(cursor[tt.wantCursorKey]) == "" {
					t.Fatalf("resume cursor = %q, want %q key", string(ref.ResumeCursor), tt.wantCursorKey)
				}
			}
		})
	}
}

func TestStartHarnessCallRejectsUndeclaredInvocationMode(t *testing.T) {
	executor := NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo"})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	call, err := NewHarnessSessionHarnessCall(packet, nil)
	if err != nil {
		t.Fatalf("NewHarnessSessionHarnessCall returned error: %v", err)
	}
	call.Input = json.RawMessage(`{"context_packet_id":"ctx_123"}`)
	call.Options = []HarnessOption{WithInvocationMode(HarnessInvocationModeHeadless)}

	_, err = StartHarnessCallResult(context.Background(), executor, call)
	var validation *HarnessValidationError
	if err == nil || !errors.As(err, &validation) || validation.Field != "invocation_mode" {
		t.Fatalf("error = %v, want invocation_mode validation error before Start is invoked", err)
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
