package daemon

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
)

var ariRandomReader io.Reader = rand.Reader

type HarnessCall struct {
	CallID              string                 `json:"call_id"`
	WorkspaceID         string                 `json:"workspace_id"`
	TaskID              string                 `json:"task_id"`
	SourceProfileID     string                 `json:"source_profile_id,omitempty"`
	Model               string                 `json:"model,omitempty"`
	Prompt              string                 `json:"prompt,omitempty"`
	AuthSlotID          string                 `json:"auth_slot_id,omitempty"`
	InvocationClass     HarnessInvocationClass `json:"invocation_class"`
	Capability          HarnessCapability      `json:"capability"`
	ContextPacketID     string                 `json:"context_packet_id"`
	InputSchemaVersion  string                 `json:"input_schema_version"`
	Input               json.RawMessage        `json:"input,omitempty"`
	ResultSchemaVersion string                 `json:"result_schema_version"`
	Required            []HarnessCapability    `json:"required,omitempty"`
	Timeout             time.Duration          `json:"-"`
}

type HarnessCapability string

type HarnessInvocationClass string

const (
	HarnessInvocationAgent     HarnessInvocationClass = "agent"
	HarnessInvocationTemporary HarnessInvocationClass = "temporary"
)

const (
	HarnessCapabilityAgentRunFromContext    HarnessCapability = "agent.run.from_context"
	HarnessCapabilityContextPacket          HarnessCapability = "context_packet"
	HarnessCapabilityTimelineItems          HarnessCapability = "timeline_items"
	HarnessCapabilityFinalResponse          HarnessCapability = "final_response"
	HarnessCapabilityMeasuredTokenTelemetry HarnessCapability = "measured_token_telemetry"
	HarnessInputSchemaAgentRunFromContextV1                   = "agent.run.from_context.v1"
	HarnessResultSchemaV1                                     = "harness.call.result.v1"
)

type HarnessCallStatus string

const (
	HarnessCallCompleted   HarnessCallStatus = "completed"
	HarnessCallFailed      HarnessCallStatus = "failed"
	HarnessCallUnsupported HarnessCallStatus = "unsupported"
)

type HarnessCallResult struct {
	CallID        string                    `json:"call_id"`
	Status        HarnessCallStatus         `json:"status"`
	Unsupported   []HarnessCapability       `json:"unsupported,omitempty"`
	AgentRun      AgentRun                  `json:"agent_run"`
	SessionRef    HarnessSessionRef         `json:"session_ref"`
	Items         []TimelineItem            `json:"items,omitempty"`
	Events        []HarnessRuntimeEvent     `json:"events,omitempty"`
	FinalResponse *HarnessFinalResponseSeed `json:"final_response,omitempty"`
	Telemetry     HarnessTelemetrySeed      `json:"telemetry"`
}

type HarnessRuntimeEvent struct {
	EventID      string          `json:"event_id"`
	RunID        string          `json:"run_id"`
	SessionID    string          `json:"session_id"`
	Kind         string          `json:"kind"`
	Sequence     int             `json:"sequence"`
	CreatedAt    time.Time       `json:"created_at"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	ProviderKind string          `json:"provider_kind,omitempty"`
}

// Canonical Ari-owned runtime event kinds. Adapters map provider protocols into
// these kinds so public callers do not depend on provider event names.
const (
	HarnessEventLifecycle  HarnessRuntimeEventKind = "lifecycle"
	HarnessEventAgentText  HarnessRuntimeEventKind = "agent_text"
	HarnessEventTool       HarnessRuntimeEventKind = "tool"
	HarnessEventFileChange HarnessRuntimeEventKind = "file_change"
	HarnessEventApproval   HarnessRuntimeEventKind = "approval"
	HarnessEventError      HarnessRuntimeEventKind = "error"
	HarnessEventUsage      HarnessRuntimeEventKind = "usage"
	HarnessEventDebug      HarnessRuntimeEventKind = "debug"

	HarnessLifecycleRunStarted  = "run_started"
	HarnessLifecycleTurnStarted = "turn_started"
	HarnessLifecycleCompleted   = "completed"
	HarnessLifecycleFailed      = "failed"
)

type HarnessRuntimeEventKind string

type HarnessFinalResponseSeed struct {
	Status          string `json:"status"`
	Text            string `json:"text,omitempty"`
	EvidenceEventID string `json:"evidence_event_id,omitempty"`
}

type HarnessTelemetrySeed struct {
	Model                  string `json:"model"`
	InputTokens            *int64 `json:"input_tokens"`
	OutputTokens           *int64 `json:"output_tokens"`
	MeasuredTokenTelemetry bool   `json:"measured_token_telemetry"`
}

type (
	HarnessAuthState           string
	HarnessCredentialOwner     string
	HarnessAriSecretStorage    string
	HarnessAuthRemediationKind string
)

const (
	HarnessAuthAuthenticated HarnessAuthState = "authenticated"
	HarnessAuthRequired      HarnessAuthState = "auth_required"
	HarnessAuthInProgress    HarnessAuthState = "auth_in_progress"
	HarnessAuthFailed        HarnessAuthState = "auth_failed"
	HarnessAuthCancelled     HarnessAuthState = "cancelled"
	HarnessAuthUnknown       HarnessAuthState = "unknown"
	HarnessAuthNotInstalled  HarnessAuthState = "not_installed"

	HarnessCredentialOwnerProvider HarnessCredentialOwner = "provider"

	HarnessAriSecretStorageNone HarnessAriSecretStorage = "none"

	HarnessAuthRemediationProviderAuthFlow HarnessAuthRemediationKind = "provider_auth_flow"
)

type HarnessAuthRemediation struct {
	Kind            HarnessAuthRemediationKind `json:"kind"`
	FlowID          string                     `json:"flow_id,omitempty"`
	Method          string                     `json:"method,omitempty"`
	VerificationURL string                     `json:"verification_url,omitempty"`
	UserCode        string                     `json:"user_code,omitempty"`
	SecretOwnedBy   string                     `json:"secret_owned_by"`
}

type HarnessAuthStatus struct {
	Harness          string                  `json:"harness"`
	Name             string                  `json:"name,omitempty"`
	AuthSlotID       string                  `json:"auth_slot_id,omitempty"`
	Status           HarnessAuthState        `json:"status"`
	Remediation      *HarnessAuthRemediation `json:"remediation,omitempty"`
	AriSecretStorage HarnessAriSecretStorage `json:"ari_secret_storage"`
}

type HarnessAuthSlot struct {
	AuthSlotID      string                 `json:"auth_slot_id"`
	Harness         string                 `json:"harness"`
	Label           string                 `json:"label"`
	ProviderLabel   string                 `json:"provider_label,omitempty"`
	CredentialOwner HarnessCredentialOwner `json:"credential_owner"`
	Status          HarnessAuthState       `json:"status"`
}

type HarnessAuthPoolStrategy string

const HarnessAuthPoolFailover HarnessAuthPoolStrategy = "failover"

type HarnessAuthPool struct {
	SlotIDs  []string                `json:"slot_ids"`
	Strategy HarnessAuthPoolStrategy `json:"strategy"`
}

type HarnessAuthSelection struct {
	RequestSlotID   string
	ProfileSlotID   string
	ProfilePool     HarnessAuthPool
	WorkspaceSlotID string
	WorkspacePool   HarnessAuthPool
	Harness         string
}

func NewHarnessAuthRequired(harness, slotID string, remediation HarnessAuthRemediation) HarnessAuthStatus {
	if strings.TrimSpace(remediation.SecretOwnedBy) == "" {
		remediation.SecretOwnedBy = strings.TrimSpace(harness)
	}
	return HarnessAuthStatus{Harness: strings.TrimSpace(harness), AuthSlotID: strings.TrimSpace(slotID), Status: HarnessAuthRequired, Remediation: &remediation, AriSecretStorage: HarnessAriSecretStorageNone}
}

func ResolveHarnessAuthSlot(selection HarnessAuthSelection, slots []HarnessAuthSlot) (HarnessAuthSlot, HarnessAuthStatus, error) {
	target := strings.TrimSpace(selection.RequestSlotID)
	if target == "" {
		target = strings.TrimSpace(selection.ProfileSlotID)
	}
	if target == "" {
		target = strings.TrimSpace(selection.WorkspaceSlotID)
	}
	harness := strings.TrimSpace(selection.Harness)
	if target == "" {
		if selected, status, ok, err := resolveHarnessAuthPool(selection.ProfilePool, harness, slots); ok || err != nil {
			return selected, status, err
		}
		if selected, status, ok, err := resolveHarnessAuthPool(selection.WorkspacePool, harness, slots); ok || err != nil {
			return selected, status, err
		}
	}
	for _, slot := range slots {
		if target != "" && strings.TrimSpace(slot.AuthSlotID) != target {
			continue
		}
		if target == "" && harness != "" && strings.TrimSpace(slot.Harness) != harness {
			continue
		}
		if slot.Status == HarnessAuthAuthenticated {
			return slot, HarnessAuthStatus{Harness: slot.Harness, AuthSlotID: slot.AuthSlotID, Status: slot.Status, AriSecretStorage: HarnessAriSecretStorageNone}, nil
		}
		status := NewHarnessAuthRequired(slot.Harness, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, SecretOwnedBy: slot.Harness})
		status.Status = slot.Status
		return slot, status, fmt.Errorf("auth slot %s is not ready", slot.AuthSlotID)
	}
	return HarnessAuthSlot{}, HarnessAuthStatus{Harness: harness, Status: HarnessAuthRequired, AriSecretStorage: HarnessAriSecretStorageNone}, fmt.Errorf("auth slot is not available")
}

func resolveHarnessAuthPool(pool HarnessAuthPool, harness string, slots []HarnessAuthSlot) (HarnessAuthSlot, HarnessAuthStatus, bool, error) {
	if pool.Strategy != HarnessAuthPoolFailover || len(pool.SlotIDs) == 0 {
		return HarnessAuthSlot{}, HarnessAuthStatus{}, false, nil
	}
	var firstUnavailableSlot HarnessAuthSlot
	var firstUnavailableStatus HarnessAuthStatus
	for i, slotID := range pool.SlotIDs {
		slotID = strings.TrimSpace(slotID)
		if slotID == "" {
			continue
		}
		slot, ok := findHarnessAuthSlot(slots, slotID, harness)
		if !ok {
			status := HarnessAuthStatus{Harness: harness, AuthSlotID: slotID, Status: HarnessAuthRequired, AriSecretStorage: HarnessAriSecretStorageNone}
			if strings.TrimSpace(firstUnavailableSlot.AuthSlotID) == "" {
				firstUnavailableSlot = HarnessAuthSlot{AuthSlotID: slotID, Harness: harness, CredentialOwner: HarnessCredentialOwnerProvider, Status: HarnessAuthRequired}
				firstUnavailableStatus = status
			}
			continue
		}
		if slot.Status == HarnessAuthAuthenticated {
			return slot, HarnessAuthStatus{Harness: slot.Harness, AuthSlotID: slot.AuthSlotID, Status: slot.Status, AriSecretStorage: HarnessAriSecretStorageNone}, true, nil
		}
		status := NewHarnessAuthRequired(slot.Harness, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, SecretOwnedBy: slot.Harness})
		status.Status = slot.Status
		if i == 0 {
			firstUnavailableSlot = slot
			firstUnavailableStatus = status
		}
		if !harnessAuthStateAllowsPreStartFailover(slot.Status) {
			return slot, status, true, fmt.Errorf("auth slot %s is not ready", slot.AuthSlotID)
		}
	}
	if strings.TrimSpace(firstUnavailableSlot.AuthSlotID) != "" {
		return firstUnavailableSlot, firstUnavailableStatus, true, fmt.Errorf("auth pool has no ready slots")
	}
	return HarnessAuthSlot{}, HarnessAuthStatus{Harness: harness, Status: HarnessAuthRequired, AriSecretStorage: HarnessAriSecretStorageNone}, true, fmt.Errorf("auth pool has no slots")
}

func findHarnessAuthSlot(slots []HarnessAuthSlot, slotID, harness string) (HarnessAuthSlot, bool) {
	for _, slot := range slots {
		if strings.TrimSpace(slot.AuthSlotID) != slotID {
			continue
		}
		if harness != "" && strings.TrimSpace(slot.Harness) != harness {
			continue
		}
		return slot, true
	}
	return HarnessAuthSlot{}, false
}

func harnessAuthStateAllowsPreStartFailover(status HarnessAuthState) bool {
	switch status {
	case HarnessAuthNotInstalled:
		return true
	default:
		return false
	}
}

func authSlotIsDefaultForHarness(harness, slotID string) bool {
	harness = strings.TrimSpace(harness)
	slotID = strings.TrimSpace(slotID)
	return slotID == "" || slotID == harness+"-default"
}

type HarnessAdapterDescriptor struct {
	Name         string
	Capabilities []HarnessCapability
}

type HarnessDescriber interface {
	Descriptor() HarnessAdapterDescriptor
}

type HarnessSessionRef struct {
	AriSessionID           string                    `json:"ari_session_id"`
	ProviderSessionID      string                    `json:"provider_session_id,omitempty"`
	ProviderThreadID       string                    `json:"provider_thread_id,omitempty"`
	ResumeCursor           json.RawMessage           `json:"resume_cursor,omitempty"`
	ProviderCanUseClientID HarnessTriState           `json:"provider_can_use_client_id"`
	Persistence            HarnessSessionPersistence `json:"persistence"`
	ResumeMode             HarnessResumeMode         `json:"resume_mode"`
}

type HarnessTriState string

type HarnessSessionPersistence string

type HarnessResumeMode string

const (
	HarnessUnknown HarnessTriState = "unknown"
	HarnessTrue    HarnessTriState = "true"
	HarnessFalse   HarnessTriState = "false"

	HarnessSessionPersistent HarnessSessionPersistence = "persistent"
	HarnessSessionEphemeral  HarnessSessionPersistence = "ephemeral"
	HarnessSessionUnknown    HarnessSessionPersistence = "unknown"

	HarnessResumeJSONRPC HarnessResumeMode = "json_rpc"
	HarnessResumeCLIFlag HarnessResumeMode = "cli_flag"
	HarnessResumeHTTPAPI HarnessResumeMode = "http_api"
	HarnessResumeNone    HarnessResumeMode = "none"
	HarnessResumeUnknown HarnessResumeMode = "unknown"
)

func (r HarnessSessionRef) Validate() error {
	if !isULID(r.AriSessionID) {
		return fmt.Errorf("ari session id must be a ULID")
	}
	if !validHarnessTriState(r.ProviderCanUseClientID) {
		return fmt.Errorf("provider client id support state %q is invalid", r.ProviderCanUseClientID)
	}
	if !validHarnessSessionPersistence(r.Persistence) {
		return fmt.Errorf("session persistence %q is invalid", r.Persistence)
	}
	if !validHarnessResumeMode(r.ResumeMode) {
		return fmt.Errorf("resume mode %q is invalid", r.ResumeMode)
	}
	return nil
}

func validHarnessTriState(value HarnessTriState) bool {
	switch value {
	case HarnessUnknown, HarnessTrue, HarnessFalse:
		return true
	default:
		return false
	}
}

func validHarnessSessionPersistence(value HarnessSessionPersistence) bool {
	switch value {
	case HarnessSessionPersistent, HarnessSessionEphemeral, HarnessSessionUnknown:
		return true
	default:
		return false
	}
}

func validHarnessResumeMode(value HarnessResumeMode) bool {
	switch value {
	case HarnessResumeJSONRPC, HarnessResumeCLIFlag, HarnessResumeHTTPAPI, HarnessResumeNone, HarnessResumeUnknown:
		return true
	default:
		return false
	}
}

type UnsupportedHarnessCapabilitiesError struct {
	Capabilities []HarnessCapability
}

func (e *UnsupportedHarnessCapabilitiesError) Error() string {
	if e == nil || len(e.Capabilities) == 0 {
		return "unsupported harness capabilities"
	}
	return "unsupported harness capabilities: " + strings.Join(harnessCapabilitiesToStrings(e.Capabilities), ", ")
}

type HarnessValidationError struct {
	Message string
	Field   string
}

func (e *HarnessValidationError) Error() string {
	if e == nil || strings.TrimSpace(e.Message) == "" {
		return "invalid harness call"
	}
	return e.Message
}

func (e *HarnessValidationError) Data() map[string]any {
	data := map[string]any{"reason": "invalid_harness_call", "start_invoked": false}
	if e != nil && strings.TrimSpace(e.Field) != "" {
		data["field"] = strings.TrimSpace(e.Field)
	}
	return data
}

type HarnessUnavailableError struct {
	Harness            string
	Reason             string
	Executable         string
	Probe              string
	RequiredCapability HarnessCapability
	StartInvoked       bool
}

func (e *HarnessUnavailableError) Error() string {
	if e == nil {
		return "harness is unavailable"
	}
	reason := strings.TrimSpace(e.Reason)
	if reason == "" {
		reason = "unavailable"
	}
	if strings.TrimSpace(e.Harness) == "" {
		return "harness is unavailable: " + reason
	}
	return fmt.Sprintf("harness %s is unavailable: %s", e.Harness, reason)
}

func (e *HarnessUnavailableError) Data() map[string]any {
	data := map[string]any{"harness": strings.TrimSpace(e.Harness), "reason": strings.TrimSpace(e.Reason), "executable": strings.TrimSpace(e.Executable), "probe": strings.TrimSpace(e.Probe), "start_invoked": e.StartInvoked}
	if e.RequiredCapability != "" {
		data["required_capability"] = string(e.RequiredCapability)
	}
	return data
}

func NewAgentRunHarnessCall(packet ContextPacket, required []HarnessCapability) (HarnessCall, error) {
	callID, err := newAriULID()
	if err != nil {
		return HarnessCall{}, err
	}
	if len(required) == 0 {
		required = []HarnessCapability{HarnessCapabilityAgentRunFromContext, HarnessCapabilityTimelineItems}
	}
	return HarnessCall{
		CallID:              callID,
		WorkspaceID:         packet.WorkspaceID,
		TaskID:              packet.TaskID,
		InvocationClass:     HarnessInvocationAgent,
		Capability:          HarnessCapabilityAgentRunFromContext,
		ContextPacketID:     packet.ID,
		InputSchemaVersion:  HarnessInputSchemaAgentRunFromContextV1,
		ResultSchemaVersion: HarnessResultSchemaV1,
		Required:            append([]HarnessCapability(nil), required...),
	}, nil
}

func StartHarnessCall(ctx context.Context, executor Executor, call HarnessCall) (AgentRun, []TimelineItem, error) {
	result, err := StartHarnessCallResult(ctx, executor, call)
	if err != nil {
		return AgentRun{}, nil, err
	}
	return result.AgentRun, result.Items, nil
}

func StartHarnessCallResult(ctx context.Context, executor Executor, call HarnessCall) (HarnessCallResult, error) {
	startedAt := time.Now().UTC()
	if ctx == nil {
		return HarnessCallResult{}, fmt.Errorf("context is required")
	}
	if executor == nil {
		return HarnessCallResult{}, fmt.Errorf("executor is required")
	}
	describer, ok := executor.(HarnessDescriber)
	if !ok {
		return HarnessCallResult{}, fmt.Errorf("executor descriptor is required")
	}
	descriptor := describer.Descriptor()
	required := uniqueHarnessCapabilities(append([]HarnessCapability{call.Capability}, call.Required...))
	if missing := missingHarnessCapabilities(required, descriptor.Capabilities); len(missing) > 0 {
		return HarnessCallResult{}, &UnsupportedHarnessCapabilitiesError{Capabilities: missing}
	}
	if err := validateHarnessCallEnvelope(call); err != nil {
		return HarnessCallResult{}, err
	}
	run, items, err := startHarnessCallAfterCapabilityCheck(ctx, executor, call, descriptor)
	if err != nil {
		return HarnessCallResult{}, err
	}
	finishedAt := time.Now().UTC()
	run.StartedAt = startedAt.Format(time.RFC3339Nano)
	run.FinishedAt = finishedAt.Format(time.RFC3339Nano)
	return HarnessCallResult{
		CallID:   call.CallID,
		Status:   harnessCallStatusFromAgentRun(run),
		AgentRun: run,
		SessionRef: HarnessSessionRef{
			AriSessionID:           run.AgentRunID,
			ProviderSessionID:      run.ProviderRunID,
			ProviderCanUseClientID: HarnessUnknown,
			Persistence:            HarnessSessionUnknown,
			ResumeMode:             HarnessResumeNone,
		},
		Items:         items,
		Events:        harnessRuntimeEventsFromItems(run, items),
		FinalResponse: harnessFinalResponseFromItems(run, descriptor, items),
		Telemetry:     harnessTelemetryFromItems(call, items),
	}, nil
}

func harnessTelemetryFromItems(call HarnessCall, items []TimelineItem) HarnessTelemetrySeed {
	seed := HarnessTelemetrySeed{Model: strings.TrimSpace(call.Model), InputTokens: nil, OutputTokens: nil, MeasuredTokenTelemetry: false}
	if seed.Model == "" {
		seed.Model = "unknown"
	}
	for _, item := range items {
		if strings.TrimSpace(item.Kind) != string(HarnessEventUsage) && strings.TrimSpace(item.Kind) != "telemetry" {
			continue
		}
		if value, ok := metadataInt64(item.Metadata, "input_tokens"); ok {
			seed.InputTokens = &value
			seed.MeasuredTokenTelemetry = true
		}
		if value, ok := metadataInt64(item.Metadata, "output_tokens"); ok {
			seed.OutputTokens = &value
			seed.MeasuredTokenTelemetry = true
		}
	}
	return seed
}

func metadataInt64(metadata map[string]any, key string) (int64, bool) {
	if metadata == nil {
		return 0, false
	}
	switch value := metadata[key].(type) {
	case int64:
		return value, true
	case int:
		return int64(value), true
	case float64:
		if value < 0 || value != float64(int64(value)) {
			return 0, false
		}
		return int64(value), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return parsed, err == nil && parsed >= 0
	default:
		return 0, false
	}
}

func harnessFinalResponseFromItems(run AgentRun, descriptor HarnessAdapterDescriptor, items []TimelineItem) *HarnessFinalResponseSeed {
	if !harnessCapabilitiesContain(descriptor.Capabilities, HarnessCapabilityFinalResponse) {
		return nil
	}
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if strings.TrimSpace(item.Kind) != string(HarnessEventAgentText) {
			continue
		}
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		status := strings.TrimSpace(item.Status)
		if status == "" {
			status = "completed"
		}
		return &HarnessFinalResponseSeed{Status: status, Text: text, EvidenceEventID: fmt.Sprintf("%s:event-%d", run.AgentRunID, i+1)}
	}
	return nil
}

func harnessCapabilitiesContain(capabilities []HarnessCapability, target HarnessCapability) bool {
	for _, capability := range capabilities {
		if capability == target {
			return true
		}
	}
	return false
}

func validateHarnessCallEnvelope(call HarnessCall) error {
	if call.Capability != HarnessCapabilityAgentRunFromContext {
		return nil
	}
	if call.InputSchemaVersion != HarnessInputSchemaAgentRunFromContextV1 {
		return &HarnessValidationError{Message: fmt.Sprintf("input schema version %q is not supported for %s", call.InputSchemaVersion, call.Capability), Field: "input_schema_version"}
	}
	if call.ResultSchemaVersion != HarnessResultSchemaV1 {
		return &HarnessValidationError{Message: fmt.Sprintf("result schema version %q is not supported for %s", call.ResultSchemaVersion, call.Capability), Field: "result_schema_version"}
	}
	if len(call.Input) == 0 {
		return &HarnessValidationError{Message: fmt.Sprintf("input is required for %s", call.Capability), Field: "input"}
	}
	if !json.Valid(call.Input) {
		return &HarnessValidationError{Message: fmt.Sprintf("input must be valid JSON for %s", call.Capability), Field: "input"}
	}
	return nil
}

func harnessCallStatusFromAgentRun(run AgentRun) HarnessCallStatus {
	if strings.TrimSpace(run.Status) == "failed" {
		return HarnessCallFailed
	}
	return HarnessCallCompleted
}

func harnessRuntimeEventsFromItems(run AgentRun, items []TimelineItem) []HarnessRuntimeEvent {
	events := make([]HarnessRuntimeEvent, 0, len(items))
	for i, item := range items {
		kind := normalizedHarnessEventKind(item.Kind)
		payload, err := normalizedHarnessEventPayload(kind, item)
		if err != nil {
			panic(fmt.Sprintf("encode harness runtime event payload: %v", err))
		}
		events = append(events, HarnessRuntimeEvent{
			EventID:      fmt.Sprintf("%s:event-%d", run.AgentRunID, i+1),
			RunID:        run.AgentRunID,
			SessionID:    run.AgentRunID,
			Kind:         string(kind),
			Sequence:     item.Sequence,
			CreatedAt:    time.Now().UTC(),
			Payload:      payload,
			ProviderKind: item.Kind,
		})
	}
	return events
}

func normalizedHarnessEventKind(kind string) HarnessRuntimeEventKind {
	switch strings.TrimSpace(kind) {
	case string(HarnessEventLifecycle):
		return HarnessEventLifecycle
	case string(HarnessEventAgentText), "terminal_output":
		return HarnessEventAgentText
	case string(HarnessEventTool):
		return HarnessEventTool
	case string(HarnessEventFileChange):
		return HarnessEventFileChange
	case string(HarnessEventApproval):
		return HarnessEventApproval
	case string(HarnessEventError):
		return HarnessEventError
	case string(HarnessEventUsage), "telemetry":
		return HarnessEventUsage
	case string(HarnessEventDebug):
		return HarnessEventDebug
	default:
		return HarnessEventAgentText
	}
}

func normalizedHarnessEventPayload(kind HarnessRuntimeEventKind, item TimelineItem) (json.RawMessage, error) {
	payload := map[string]any{}
	switch kind {
	case HarnessEventLifecycle:
		payload["status"] = trimOrDefault(item.Status, HarnessLifecycleRunStarted)
		if reason := stringMetadata(item.Metadata, "reason"); reason != "" {
			payload["reason"] = reason
		}
	case HarnessEventAgentText:
		payload["text"] = strings.TrimSpace(item.Text)
		payload["final"] = boolMetadata(item.Metadata, "final")
	case HarnessEventUsage:
		payload["input_tokens"] = knownIntPayload(item.Metadata, "input_tokens")
		payload["output_tokens"] = knownIntPayload(item.Metadata, "output_tokens")
		cost := knownIntPayload(item.Metadata, "estimated_cost")
		cost["estimated"] = boolMetadata(item.Metadata, "cost_estimated")
		payload["estimated_cost"] = cost
	case HarnessEventError:
		payload["code"] = trimOrDefault(stringMetadata(item.Metadata, "code"), "provider_error")
		payload["message"] = strings.TrimSpace(item.Text)
		payload["retryable"] = boolMetadata(item.Metadata, "retryable")
	default:
		payload["status"] = strings.TrimSpace(item.Status)
		payload["text"] = strings.TrimSpace(item.Text)
	}
	encoded, err := json.Marshal(payload)
	return encoded, err
}

func knownIntPayload(metadata map[string]any, key string) map[string]any {
	if value, ok := metadataInt64(metadata, key); ok {
		return map[string]any{"known": true, "value": value}
	}
	return map[string]any{"known": false}
}

func boolMetadata(metadata map[string]any, key string) bool {
	value, ok := metadata[key]
	if !ok {
		return false
	}
	parsed, ok := value.(bool)
	return ok && parsed
}

func stringMetadata(metadata map[string]any, key string) string {
	value, ok := metadata[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func trimOrDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func startHarnessCallAfterCapabilityCheck(ctx context.Context, executor Executor, call HarnessCall, descriptor HarnessAdapterDescriptor) (AgentRun, []TimelineItem, error) {
	ariRunID, err := newAriULID()
	if err != nil {
		return AgentRun{}, nil, err
	}
	providerRun, err := executor.Start(ctx, ExecutorStartRequest{WorkspaceID: call.WorkspaceID, RunID: ariRunID, ContextPacket: string(call.Input), SourceProfileID: call.SourceProfileID, Model: call.Model, Prompt: call.Prompt, AuthSlotID: call.AuthSlotID, InvocationClass: call.InvocationClass})
	if err != nil {
		return AgentRun{}, nil, err
	}
	items, err := executor.Items(ctx, providerRun.RunID)
	if err != nil {
		return AgentRun{}, nil, err
	}
	agentRun := AgentRun{
		AgentRunID:      ariRunID,
		WorkspaceID:     call.WorkspaceID,
		TaskID:          call.TaskID,
		Executor:        providerRun.Executor,
		ProviderRunID:   providerRun.ProviderRunID,
		AuthSlotID:      call.AuthSlotID,
		Status:          executorRunStatusFromItems(items),
		ContextPacketID: call.ContextPacketID,
		StartedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		PID:             providerRun.PID,
		ExitCode:        providerRun.ExitCode,
		ProcessSample:   providerRun.ProcessSample,
		Capabilities:    harnessCapabilitiesToStrings(descriptor.Capabilities),
	}
	for i := range items {
		items[i].RunID = agentRun.AgentRunID
		items[i].WorkspaceID = agentRun.WorkspaceID
		if strings.TrimSpace(items[i].SourceID) == "" || items[i].SourceID == providerRun.RunID || items[i].SourceID == providerRun.ProviderRunID {
			items[i].SourceID = agentRun.AgentRunID
		}
	}
	return agentRun, items, nil
}

func missingHarnessCapabilities(required, available []HarnessCapability) []HarnessCapability {
	availableSet := make(map[HarnessCapability]bool, len(available))
	for _, capability := range available {
		availableSet[capability] = true
	}
	missing := make([]HarnessCapability, 0)
	for _, capability := range required {
		if !availableSet[capability] {
			missing = append(missing, capability)
		}
	}
	return missing
}

func uniqueHarnessCapabilities(capabilities []HarnessCapability) []HarnessCapability {
	seen := make(map[HarnessCapability]bool, len(capabilities))
	out := make([]HarnessCapability, 0, len(capabilities))
	for _, capability := range capabilities {
		if seen[capability] {
			continue
		}
		seen[capability] = true
		out = append(out, capability)
	}
	return out
}

func harnessCapabilitiesToStrings(capabilities []HarnessCapability) []string {
	out := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		out = append(out, string(capability))
	}
	return out
}

const ulidEncoding = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func newAriULID() (string, error) {
	var data [16]byte
	millis := uint64(time.Now().UTC().UnixMilli())
	data[0] = byte(millis >> 40)
	data[1] = byte(millis >> 32)
	data[2] = byte(millis >> 24)
	data[3] = byte(millis >> 16)
	data[4] = byte(millis >> 8)
	data[5] = byte(millis)
	if _, err := io.ReadFull(ariRandomReader, data[6:]); err != nil {
		return "", fmt.Errorf("generate Ari ULID: %w", err)
	}
	return encodeULID(data), nil
}

func replaceAriRandomReaderForTest(reader io.Reader) func() {
	previous := ariRandomReader
	ariRandomReader = reader
	return func() { ariRandomReader = previous }
}

func encodeULID(data [16]byte) string {
	out := make([]byte, 26)
	out[0] = ulidEncoding[(data[0]&224)>>5]
	out[1] = ulidEncoding[data[0]&31]
	out[2] = ulidEncoding[(data[1]&248)>>3]
	out[3] = ulidEncoding[((data[1]&7)<<2)|((data[2]&192)>>6)]
	out[4] = ulidEncoding[(data[2]&62)>>1]
	out[5] = ulidEncoding[((data[2]&1)<<4)|((data[3]&240)>>4)]
	out[6] = ulidEncoding[((data[3]&15)<<1)|((data[4]&128)>>7)]
	out[7] = ulidEncoding[(data[4]&124)>>2]
	out[8] = ulidEncoding[((data[4]&3)<<3)|((data[5]&224)>>5)]
	out[9] = ulidEncoding[data[5]&31]
	out[10] = ulidEncoding[(data[6]&248)>>3]
	out[11] = ulidEncoding[((data[6]&7)<<2)|((data[7]&192)>>6)]
	out[12] = ulidEncoding[(data[7]&62)>>1]
	out[13] = ulidEncoding[((data[7]&1)<<4)|((data[8]&240)>>4)]
	out[14] = ulidEncoding[((data[8]&15)<<1)|((data[9]&128)>>7)]
	out[15] = ulidEncoding[(data[9]&124)>>2]
	out[16] = ulidEncoding[((data[9]&3)<<3)|((data[10]&224)>>5)]
	out[17] = ulidEncoding[data[10]&31]
	out[18] = ulidEncoding[(data[11]&248)>>3]
	out[19] = ulidEncoding[((data[11]&7)<<2)|((data[12]&192)>>6)]
	out[20] = ulidEncoding[(data[12]&62)>>1]
	out[21] = ulidEncoding[((data[12]&1)<<4)|((data[13]&240)>>4)]
	out[22] = ulidEncoding[((data[13]&15)<<1)|((data[14]&128)>>7)]
	out[23] = ulidEncoding[(data[14]&124)>>2]
	out[24] = ulidEncoding[((data[14]&3)<<3)|((data[15]&224)>>5)]
	out[25] = ulidEncoding[data[15]&31]
	return string(out)
}

func isULID(value string) bool {
	if len(value) != 26 {
		return false
	}
	if !slices.Contains([]rune{'0', '1', '2', '3', '4', '5', '6', '7'}, rune(value[0])) {
		return false
	}
	for _, ch := range value {
		if !strings.ContainsRune(ulidEncoding, ch) {
			return false
		}
	}
	return true
}
