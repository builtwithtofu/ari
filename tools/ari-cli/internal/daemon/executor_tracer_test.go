package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"

	_ "modernc.org/sqlite"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func mustNewAriULID(t *testing.T) string {
	t.Helper()
	id, err := newAriULID()
	if err != nil {
		t.Fatalf("newAriULID returned error: %v", err)
	}
	return id
}

func TestStartExecutorRunProjectsPacketIntoAgentSessionAndTimeline(t *testing.T) {
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	executor := newFakeHarness("fake", []TimelineItem{{Kind: "agent_text", Text: "done"}})

	run, items, err := StartExecutorRun(context.Background(), executor, packet)
	if err != nil {
		t.Fatalf("StartExecutorRun returned error: %v", err)
	}
	if run.WorkspaceID != "ws-1" || run.TaskID != "task-1" || run.ContextPacketID != "ctx_123" {
		t.Fatalf("agent run ids = %#v, want workspace/task/context packet ids", run)
	}
	if run.Executor != "fake" || run.Status != "completed" {
		t.Fatalf("agent run executor/status = %q/%q, want fake/completed", run.Executor, run.Status)
	}
	if run.ProviderSessionID == "" {
		t.Fatal("provider run id is empty")
	}
	if run.AgentSessionID == run.ProviderSessionID || !isULID(run.AgentSessionID) {
		t.Fatalf("agent run id = %q, provider run id = %q, want distinct Ari ULID run id", run.AgentSessionID, run.ProviderSessionID)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if executor.lastContextPacket == "" || !strings.Contains(executor.lastContextPacket, "ctx_123") {
		t.Fatalf("executor context packet = %q, want serialized packet", executor.lastContextPacket)
	}
	if items[0].SessionID != run.AgentSessionID || items[0].Kind != "agent_text" || items[0].SourceKind != "executor" {
		t.Fatalf("timeline item = %#v, want executor agent_text linked to run", items[0])
	}
	if items[0].SourceID != run.AgentSessionID {
		t.Fatalf("timeline source id = %q, want Ari run id %q", items[0].SourceID, run.AgentSessionID)
	}
}

func TestCreateStoredAgentProfileReturnsPersistedProfileIDAfterUpdate(t *testing.T) {
	store := newDaemonMigratedGlobalDBStore(t)
	ctx := context.Background()

	first, err := createStoredAgentProfile(ctx, store, AgentProfileCreateRequest{WorkspaceID: "ws-1", Name: "executor", Harness: HarnessNameCodex})
	if err != nil {
		t.Fatalf("createStoredAgentProfile first returned error: %v", err)
	}
	second, err := createStoredAgentProfile(ctx, store, AgentProfileCreateRequest{WorkspaceID: "ws-1", Name: "executor", Harness: HarnessNameClaude})
	if err != nil {
		t.Fatalf("createStoredAgentProfile update returned error: %v", err)
	}
	if second.ProfileID != first.ProfileID || second.Harness != HarnessNameClaude {
		t.Fatalf("updated profile = %#v, want existing persisted id %q and updated harness", second, first.ProfileID)
	}
}

func newDaemonMigratedGlobalDBStore(t *testing.T) *globaldb.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ari.db")
	if err := applyMigrationSQLFiles(dbPath); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		t.Fatalf("set busy timeout: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := globaldb.NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore returned error: %v", err)
	}
	return store
}

func TestStartExecutorRunRejectsMissingRequiredCapabilityBeforeStart(t *testing.T) {
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	executor := &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityAgentSessionFromContext}}
	call, err := NewAgentSessionHarnessCall(packet, []HarnessCapability{HarnessCapabilityMeasuredTokenTelemetry})
	if err != nil {
		t.Fatalf("NewAgentSessionHarnessCall returned error: %v", err)
	}

	_, _, err = StartHarnessCall(context.Background(), executor, call)
	if err == nil {
		t.Fatal("StartHarnessCall returned nil error for missing required capability")
	}
	unsupported, ok := err.(*UnsupportedHarnessCapabilitiesError)
	if !ok {
		t.Fatalf("error = %T %[1]v, want UnsupportedHarnessCapabilitiesError", err)
	}
	if got := strings.Join(harnessCapabilitiesToStrings(unsupported.Capabilities), ","); got != string(HarnessCapabilityMeasuredTokenTelemetry) {
		t.Fatalf("unsupported capabilities = %q, want measured token telemetry", got)
	}
	if executor.started {
		t.Fatal("executor Start was called before required capability check")
	}
}

func TestStartExecutorRunRejectsMissingTimelineCapabilityBeforeStart(t *testing.T) {
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	executor := &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityAgentSessionFromContext}}

	_, _, err := StartExecutorRun(context.Background(), executor, packet)
	if err == nil {
		t.Fatal("StartExecutorRun returned nil error for missing timeline capability")
	}
	unsupported, ok := err.(*UnsupportedHarnessCapabilitiesError)
	if !ok {
		t.Fatalf("error = %T %[1]v, want UnsupportedHarnessCapabilitiesError", err)
	}
	if got := strings.Join(harnessCapabilitiesToStrings(unsupported.Capabilities), ","); got != string(HarnessCapabilityTimelineItems) {
		t.Fatalf("unsupported capabilities = %q, want timeline items", got)
	}
	if executor.started {
		t.Fatal("executor Start was called before timeline capability check")
	}
}

func TestStartExecutorRunReturnsULIDEntropyError(t *testing.T) {
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	executor := newFakeHarness("fake", []TimelineItem{{Kind: "agent_text", Text: "done"}})
	restore := replaceAriRandomReaderForTest(failingReader{})
	t.Cleanup(restore)

	_, _, err := StartExecutorRun(context.Background(), executor, packet)
	if err == nil || !strings.Contains(err.Error(), "generate Ari ULID") {
		t.Fatalf("StartExecutorRun error = %v, want Ari ULID entropy error", err)
	}
}

func TestStartHarnessCallRejectsUnsupportedCapabilityBeforeStart(t *testing.T) {
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	executor := &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityAgentSessionFromContext}}
	call, err := NewAgentSessionHarnessCall(packet, nil)
	if err != nil {
		t.Fatalf("NewAgentSessionHarnessCall returned error: %v", err)
	}
	call.Capability = HarnessCapabilityFinalResponse
	call.Required = nil

	_, _, err = StartHarnessCall(context.Background(), executor, call)
	if err == nil {
		t.Fatal("StartHarnessCall returned nil error for unsupported requested capability")
	}
	unsupported, ok := err.(*UnsupportedHarnessCapabilitiesError)
	if !ok {
		t.Fatalf("error = %T %[1]v, want UnsupportedHarnessCapabilitiesError", err)
	}
	if got := strings.Join(harnessCapabilitiesToStrings(unsupported.Capabilities), ","); got != string(HarnessCapabilityFinalResponse) {
		t.Fatalf("unsupported capabilities = %q, want final response", got)
	}
	if executor.started {
		t.Fatal("executor Start was called for unsupported requested capability")
	}
}

func TestStartHarnessCallRejectsMismatchedSchemaBeforeStart(t *testing.T) {
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	executor := &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityAgentSessionFromContext}}
	call, err := NewAgentSessionHarnessCall(packet, nil)
	if err != nil {
		t.Fatalf("NewAgentSessionHarnessCall returned error: %v", err)
	}
	call.InputSchemaVersion = "agent.run.from_context.v999"

	_, _, err = StartHarnessCall(context.Background(), executor, call)
	if err == nil {
		t.Fatal("StartHarnessCall returned nil error for mismatched schema")
	}
	if executor.started {
		t.Fatal("executor Start was called for mismatched schema")
	}
}

func TestStartHarnessCallRejectsMissingInputBeforeStart(t *testing.T) {
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	executor := &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityAgentSessionFromContext}}
	call, err := NewAgentSessionHarnessCall(packet, nil)
	if err != nil {
		t.Fatalf("NewAgentSessionHarnessCall returned error: %v", err)
	}

	_, _, err = StartHarnessCall(context.Background(), executor, call)
	if err == nil {
		t.Fatal("StartHarnessCall returned nil error for missing input")
	}
	if executor.started {
		t.Fatal("executor Start was called for missing input")
	}
}

func TestStartHarnessCallRejectsInvalidJSONInputBeforeStart(t *testing.T) {
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	executor := &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityAgentSessionFromContext}}
	call, err := NewAgentSessionHarnessCall(packet, nil)
	if err != nil {
		t.Fatalf("NewAgentSessionHarnessCall returned error: %v", err)
	}
	call.Input = []byte("{")

	_, _, err = StartHarnessCall(context.Background(), executor, call)
	if err == nil {
		t.Fatal("StartHarnessCall returned nil error for invalid input")
	}
	if executor.started {
		t.Fatal("executor Start was called for invalid input")
	}
}

func TestHarnessSessionRefValidationRejectsInvalidEnums(t *testing.T) {
	ref := HarnessSessionRef{
		AriSessionID:           mustNewAriULID(t),
		ProviderCanUseClientID: HarnessTriState("sometimes"),
		Persistence:            HarnessSessionUnknown,
		ResumeMode:             HarnessResumeNone,
	}
	if err := ref.Validate(); err == nil {
		t.Fatal("Validate returned nil error for invalid provider client id state")
	}
}

func TestHarnessSessionRefValidationRejectsOutOfRangeULID(t *testing.T) {
	ref := HarnessSessionRef{AriSessionID: "ZZZZZZZZZZZZZZZZZZZZZZZZZZ", ProviderCanUseClientID: HarnessUnknown, Persistence: HarnessSessionUnknown, ResumeMode: HarnessResumeNone}
	if err := ref.Validate(); err == nil {
		t.Fatal("Validate returned nil error for out-of-range ULID")
	}
}

func TestStartHarnessCallResultReturnsUnknownTelemetrySeed(t *testing.T) {
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	executor := newFakeHarness("fake", []TimelineItem{{Kind: "agent_text", Text: "done"}})
	call, err := NewAgentSessionHarnessCall(packet, nil)
	if err != nil {
		t.Fatalf("NewAgentSessionHarnessCall returned error: %v", err)
	}
	call.Input = []byte(renderContextPacket(packet))

	result, err := StartHarnessCallResult(context.Background(), executor, call)
	if err != nil {
		t.Fatalf("StartHarnessCallResult returned error: %v", err)
	}
	if result.Status != HarnessCallCompleted || result.AgentSession.AgentSessionID == "" {
		t.Fatalf("harness result = %#v, want completed run", result)
	}
	if result.FinalResponse != nil {
		t.Fatalf("final response = %#v, want nil seed for fake executor", result.FinalResponse)
	}
	if result.Telemetry.Model != "unknown" || result.Telemetry.InputTokens != nil || result.Telemetry.OutputTokens != nil || result.Telemetry.MeasuredTokenTelemetry {
		t.Fatalf("telemetry = %#v, want explicit unknown token telemetry", result.Telemetry)
	}
	if got := strings.Join(result.AgentSession.Capabilities, ","); got != "agent.run.from_context,context_packet,timeline_items" {
		t.Fatalf("capabilities = %q, want normalized descriptor capabilities", got)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != string(HarnessEventAgentText) || !strings.Contains(string(result.Events[0].Payload), "done") {
		t.Fatalf("events = %#v, want normalized agent text payload containing timeline text", result.Events)
	}
	if result.SessionRef.AriSessionID != result.AgentSession.AgentSessionID {
		t.Fatalf("session ref = %#v, want Ari run id %q", result.SessionRef, result.AgentSession.AgentSessionID)
	}
	if err := result.SessionRef.Validate(); err != nil {
		t.Fatalf("session ref = %#v, validate error = %v", result.SessionRef, err)
	}
}

func TestStartHarnessCallResultBuildsCanonicalEventsAndTelemetry(t *testing.T) {
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	executor := newFinalResponseHarness("fake", []TimelineItem{
		{ID: "ti_start", Kind: string(HarnessEventLifecycle), Status: HarnessLifecycleTurnStarted, Sequence: 1},
		{ID: "ti_text", Kind: string(HarnessEventAgentText), Text: "working", Metadata: map[string]any{"final": false}, Sequence: 2},
		{ID: "ti_usage", Kind: string(HarnessEventUsage), Metadata: map[string]any{"input_tokens": int64(0), "output_tokens": int64(5), "estimated_cost": int64(2), "cost_estimated": true}, Sequence: 3},
		{ID: "ti_error", Kind: string(HarnessEventError), Status: "failed", Text: "redacted failure", Metadata: map[string]any{"code": "provider_error", "retryable": false}, Sequence: 4},
		{ID: "ti_final", Kind: string(HarnessEventAgentText), Text: "final answer", Metadata: map[string]any{"final": true}, Sequence: 5},
	})
	call, err := NewAgentSessionHarnessCall(packet, nil)
	if err != nil {
		t.Fatalf("NewAgentSessionHarnessCall returned error: %v", err)
	}
	call.Input = []byte(renderContextPacket(packet))
	call.Model = "model-1"

	result, err := StartHarnessCallResult(context.Background(), executor, call)
	if err != nil {
		t.Fatalf("StartHarnessCallResult returned error: %v", err)
	}
	gotKinds := make([]string, 0, len(result.Events))
	for _, event := range result.Events {
		gotKinds = append(gotKinds, event.Kind)
		if strings.Contains(string(event.Payload), "threadId") || strings.Contains(string(event.Payload), "turnId") || strings.Contains(string(event.Payload), "access_token") {
			t.Fatalf("event payload leaked provider-specific or token-like fields: %s", event.Payload)
		}
	}
	if strings.Join(gotKinds, ",") != "lifecycle,agent_text,usage,error,agent_text" {
		t.Fatalf("event kinds = %q, want canonical Ari-owned event kinds", strings.Join(gotKinds, ","))
	}
	if result.Events[2].Sequence != 3 || !strings.Contains(string(result.Events[2].Payload), `"input_tokens":{"known":true,"value":0}`) || !strings.Contains(string(result.Events[2].Payload), `"estimated":true`) || !strings.Contains(string(result.Events[2].Payload), `"estimated_cost":{"estimated":true,"known":true,"value":2}`) {
		t.Fatalf("usage event = %#v payload=%s, want known zero distinct from unknown and estimated cost marker", result.Events[2], result.Events[2].Payload)
	}
	if result.FinalResponse == nil || result.FinalResponse.Text != "final answer" || result.FinalResponse.EvidenceEventID != result.Events[4].EventID {
		t.Fatalf("final response = %#v, want provenance linked to final event", result.FinalResponse)
	}
	if result.Telemetry.InputTokens == nil || *result.Telemetry.InputTokens != 0 || result.Telemetry.OutputTokens == nil || *result.Telemetry.OutputTokens != 5 || !result.Telemetry.MeasuredTokenTelemetry {
		t.Fatalf("telemetry = %#v, want known zero input tokens and measured output tokens", result.Telemetry)
	}
}

func TestHarnessAuthStatusRedactsProviderOwnedRemediation(t *testing.T) {
	status := NewHarnessAuthRequired("codex", "slot-work", HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, FlowID: "auth_123", Method: "device_code", VerificationURL: "https://example.test/device", UserCode: "ABCD-EFGH", SecretOwnedBy: "codex"})
	if status.Status != HarnessAuthRequired || status.AriSecretStorage != HarnessAriSecretStorageNone || status.Remediation == nil || status.Remediation.SecretOwnedBy != "codex" {
		t.Fatalf("status = %#v, want provider-owned auth-required remediation without Ari secret storage", status)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal auth status: %v", err)
	}
	if strings.Contains(string(encoded), "access_token") || strings.Contains(string(encoded), "refresh_token") || strings.Contains(string(encoded), "api_key") {
		t.Fatalf("auth status leaked token-like field: %s", encoded)
	}
}

func TestHarnessAuthStatusOmitsRemediationWhenAuthenticated(t *testing.T) {
	status := HarnessAuthStatus{Harness: "codex", AuthSlotID: "slot-work", Status: HarnessAuthAuthenticated, AriSecretStorage: HarnessAriSecretStorageNone}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal auth status: %v", err)
	}
	if strings.Contains(string(encoded), "remediation") {
		t.Fatalf("authenticated auth status included remediation: %s", encoded)
	}
}

func TestHarnessAuthSlotSelectionFailsClosed(t *testing.T) {
	slots := []HarnessAuthSlot{{AuthSlotID: "codex-work", Harness: "codex", Label: "Work", CredentialOwner: HarnessCredentialOwnerProvider, Status: HarnessAuthAuthenticated}, {AuthSlotID: "codex-personal", Harness: "codex", Label: "Personal", CredentialOwner: HarnessCredentialOwnerProvider, Status: HarnessAuthRequired}}
	selected, status, err := ResolveHarnessAuthSlot(HarnessAuthSelection{RequestSlotID: "codex-personal", Harness: "codex"}, slots)
	if err == nil || selected.AuthSlotID != "codex-personal" || status.Status != HarnessAuthRequired {
		t.Fatalf("selected=%#v status=%#v err=%v, want selected unavailable slot to fail closed", selected, status, err)
	}
	if status.AuthSlotID != "codex-personal" || status.Remediation == nil || status.Remediation.SecretOwnedBy != "codex" {
		t.Fatalf("status = %#v, want remediation for selected slot only", status)
	}
}

func TestHarnessAuthPoolFailoverSelectsSecondSlotOnlyForSafePreStartUnavailable(t *testing.T) {
	slots := []HarnessAuthSlot{{AuthSlotID: "codex-work", Harness: "codex", Label: "Work", CredentialOwner: HarnessCredentialOwnerProvider, Status: HarnessAuthNotInstalled}, {AuthSlotID: "codex-personal", Harness: "codex", Label: "Personal", CredentialOwner: HarnessCredentialOwnerProvider, Status: HarnessAuthAuthenticated}}
	selected, status, err := ResolveHarnessAuthSlot(HarnessAuthSelection{ProfilePool: HarnessAuthPool{SlotIDs: []string{"codex-work", "codex-personal"}, Strategy: HarnessAuthPoolFailover}, Harness: "codex"}, slots)
	if err != nil {
		t.Fatalf("ResolveHarnessAuthSlot returned error: %v", err)
	}
	if selected.AuthSlotID != "codex-personal" || status.Status != HarnessAuthAuthenticated {
		t.Fatalf("selected=%#v status=%#v, want failover to authenticated second slot", selected, status)
	}
}

func TestHarnessAuthPoolFailoverSkipsMissingSlot(t *testing.T) {
	slots := []HarnessAuthSlot{{AuthSlotID: "codex-personal", Harness: "codex", Label: "Personal", CredentialOwner: HarnessCredentialOwnerProvider, Status: HarnessAuthAuthenticated}}
	selected, status, err := ResolveHarnessAuthSlot(HarnessAuthSelection{ProfilePool: HarnessAuthPool{SlotIDs: []string{"codex-work", "codex-personal"}, Strategy: HarnessAuthPoolFailover}, Harness: "codex"}, slots)
	if err != nil {
		t.Fatalf("ResolveHarnessAuthSlot returned error: %v", err)
	}
	if selected.AuthSlotID != "codex-personal" || status.Status != HarnessAuthAuthenticated {
		t.Fatalf("selected=%#v status=%#v, want failover past missing slot", selected, status)
	}
}

func TestHarnessAuthPoolDoesNotFailoverForInteractiveAuthRequired(t *testing.T) {
	slots := []HarnessAuthSlot{{AuthSlotID: "codex-work", Harness: "codex", Label: "Work", CredentialOwner: HarnessCredentialOwnerProvider, Status: HarnessAuthRequired}, {AuthSlotID: "codex-personal", Harness: "codex", Label: "Personal", CredentialOwner: HarnessCredentialOwnerProvider, Status: HarnessAuthAuthenticated}}
	selected, status, err := ResolveHarnessAuthSlot(HarnessAuthSelection{ProfilePool: HarnessAuthPool{SlotIDs: []string{"codex-work", "codex-personal"}, Strategy: HarnessAuthPoolFailover}, Harness: "codex"}, slots)
	if err == nil || selected.AuthSlotID != "codex-work" || status.Status != HarnessAuthRequired {
		t.Fatalf("selected=%#v status=%#v err=%v, want first auth_required slot to fail closed", selected, status, err)
	}
}

func TestAuthStatusReportsMissingHarnessAsNotInstalled(t *testing.T) {
	registry := NewHarnessRegistry()
	if err := registry.Register("missing-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &missingAuthHarness{}, nil
	}); err != nil {
		t.Fatalf("register harness: %v", err)
	}
	daemon := &Daemon{harnessRegistry: registry}
	store := newCommandMethodTestStore(t)
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "missing-default", Harness: "missing-harness", Label: "Missing", Status: "unknown"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	resp, err := daemon.harnessAuthStatus(context.Background(), store, HarnessAuthStatusRequest{})
	if err != nil {
		t.Fatalf("harnessAuthStatus returned error: %v", err)
	}
	var missingStatus HarnessAuthStatus
	for _, status := range resp.Statuses {
		if status.AuthSlotID == "missing-default" {
			missingStatus = status
		}
	}
	if missingStatus.Status != HarnessAuthNotInstalled {
		t.Fatalf("statuses = %#v, want not_installed", resp.Statuses)
	}
	stored, err := store.GetAuthSlot(context.Background(), "missing-default")
	if err != nil {
		t.Fatalf("GetAuthSlot returned error: %v", err)
	}
	if stored.Status != string(HarnessAuthNotInstalled) {
		t.Fatalf("stored status = %q, want not_installed", stored.Status)
	}
}

func TestAuthStatusDoesNotPersistSelectionUnsupportedAsNotInstalled(t *testing.T) {
	registry := NewHarnessRegistry()
	if err := registry.Register("codex", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: "/repo"}), nil
	}); err != nil {
		t.Fatalf("register harness: %v", err)
	}
	daemon := &Daemon{harnessRegistry: registry}
	store := newCommandMethodTestStore(t)
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "codex-work", Harness: "codex", Label: "Work", Status: "unknown"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	_, err := daemon.harnessAuthStatus(context.Background(), store, HarnessAuthStatusRequest{})
	if err == nil {
		t.Fatal("harnessAuthStatus returned nil error, want slot selection unsupported")
	}
	stored, getErr := store.GetAuthSlot(context.Background(), "codex-work")
	if getErr != nil {
		t.Fatalf("GetAuthSlot returned error: %v", getErr)
	}
	if stored.Status == string(HarnessAuthNotInstalled) {
		t.Fatalf("stored status = %q, want unsupported selection not persisted as not_installed", stored.Status)
	}
}

func TestAuthStatusRejectsUnknownRequestedSlot(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	err := callMethodError(registry, "auth.status", HarnessAuthStatusRequest{Slots: []HarnessAuthSlot{{AuthSlotID: "synthetic", Harness: "codex"}}})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "unknown_auth_slot" {
		t.Fatalf("error data = %#v, want unknown_auth_slot", data)
	}
}

func TestAuthStatusPersistsProviderReadinessForStoredSlot(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &capturingHarness{name: "test-harness", authStatuses: map[string]HarnessAuthState{"slot-work": HarnessAuthAuthenticated}}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "slot-work", Harness: "test-harness", Label: "Work", Status: "auth_in_progress"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	resp := callMethod[HarnessAuthStatusResponse](t, registry, "auth.status", HarnessAuthStatusRequest{Slots: []HarnessAuthSlot{{AuthSlotID: "slot-work"}}})
	if len(resp.Statuses) != 1 || resp.Statuses[0].Status != HarnessAuthAuthenticated {
		t.Fatalf("statuses = %#v, want authenticated", resp.Statuses)
	}
	stored, err := store.GetAuthSlot(context.Background(), "slot-work")
	if err != nil {
		t.Fatalf("GetAuthSlot returned error: %v", err)
	}
	if stored.Status != string(HarnessAuthAuthenticated) {
		t.Fatalf("stored status = %q, want authenticated", stored.Status)
	}
}

func TestAuthStartUsesStoredSlotAndPersistsInProgress(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	var capturedSlot HarnessAuthSlot
	var capturedMethod string
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &capturingHarness{name: "test-harness", authStart: func(ctx context.Context, slot HarnessAuthSlot, method string) (HarnessAuthStatus, error) {
			_ = ctx
			capturedSlot = slot
			capturedMethod = method
			return HarnessAuthStatus{Harness: slot.Harness, AuthSlotID: slot.AuthSlotID, Status: HarnessAuthInProgress, Remediation: &HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: method, VerificationURL: "https://provider.example/device", UserCode: "ABCD-EFGH", SecretOwnedBy: slot.Harness}, AriSecretStorage: HarnessAriSecretStorageNone}, nil
		}}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "slot-work", Harness: "test-harness", Label: "Work", ProviderLabel: "Provider Account", Status: "auth_required"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	resp := callMethod[HarnessAuthStartResponse](t, registry, "auth.start", HarnessAuthStartRequest{AuthSlotID: "slot-work", Method: "device_code"})
	if capturedSlot.AuthSlotID != "slot-work" || capturedSlot.ProviderLabel != "Provider Account" || capturedMethod != "device_code" {
		t.Fatalf("captured slot = %#v method = %q, want stored slot and requested method", capturedSlot, capturedMethod)
	}
	if resp.Status.Status != HarnessAuthInProgress || resp.Status.Remediation == nil || resp.Status.Remediation.UserCode != "ABCD-EFGH" || resp.Status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want non-secret auth flow response", resp.Status)
	}
	stored, err := store.GetAuthSlot(context.Background(), "slot-work")
	if err != nil {
		t.Fatalf("GetAuthSlot returned error: %v", err)
	}
	if stored.Status != string(HarnessAuthInProgress) {
		t.Fatalf("stored status = %q, want auth_in_progress", stored.Status)
	}
}

func TestAuthStartRejectsUnknownSlotBeforeHarnessStart(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "auth.start", HarnessAuthStartRequest{AuthSlotID: "missing-slot", Method: "device_code"})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "unknown_auth_slot" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown_auth_slot before start", data)
	}
}

func TestAuthCancelUsesStoredSlotAndPersistsCancelled(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	var capturedSlot HarnessAuthSlot
	var capturedFlowID string
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &capturingHarness{name: "test-harness", authCancel: func(ctx context.Context, slot HarnessAuthSlot, flowID string) (HarnessAuthStatus, error) {
			_ = ctx
			capturedSlot = slot
			capturedFlowID = flowID
			return HarnessAuthStatus{Harness: slot.Harness, AuthSlotID: slot.AuthSlotID, Status: HarnessAuthCancelled, AriSecretStorage: HarnessAriSecretStorageNone}, nil
		}}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "slot-work", Harness: "test-harness", Label: "Work", ProviderLabel: "Provider Account", Status: "auth_in_progress"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	resp := callMethod[HarnessAuthCancelResponse](t, registry, "auth.cancel", HarnessAuthCancelRequest{AuthSlotID: "slot-work", FlowID: "flow-123"})
	if capturedSlot.AuthSlotID != "slot-work" || capturedSlot.ProviderLabel != "Provider Account" || capturedFlowID != "flow-123" {
		t.Fatalf("captured slot = %#v flow = %q, want stored slot and requested flow", capturedSlot, capturedFlowID)
	}
	if resp.Status.Status != HarnessAuthCancelled || resp.Status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want cancelled non-secret status", resp.Status)
	}
	stored, err := store.GetAuthSlot(context.Background(), "slot-work")
	if err != nil {
		t.Fatalf("GetAuthSlot returned error: %v", err)
	}
	if stored.Status != string(HarnessAuthCancelled) {
		t.Fatalf("stored status = %q, want cancelled", stored.Status)
	}
}

func TestAuthCancelRejectsUnknownSlotBeforeHarnessCancel(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "auth.cancel", HarnessAuthCancelRequest{AuthSlotID: "missing-slot", FlowID: "flow-123"})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "unknown_auth_slot" || data["cancel_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown_auth_slot before cancel", data)
	}
}

func TestAuthLogoutUsesStoredSlotAndPersistsAuthRequired(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	var capturedSlot HarnessAuthSlot
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &capturingHarness{name: "test-harness", authLogout: func(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
			_ = ctx
			capturedSlot = slot
			return HarnessAuthStatus{Harness: slot.Harness, AuthSlotID: slot.AuthSlotID, Status: HarnessAuthRequired, AriSecretStorage: HarnessAriSecretStorageNone}, nil
		}}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "slot-work", Harness: "test-harness", Label: "Work", ProviderLabel: "Provider Account", Status: "authenticated"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	resp := callMethod[HarnessAuthLogoutResponse](t, registry, "auth.logout", HarnessAuthLogoutRequest{AuthSlotID: "slot-work"})
	if capturedSlot.AuthSlotID != "slot-work" || capturedSlot.ProviderLabel != "Provider Account" {
		t.Fatalf("captured slot = %#v, want stored slot metadata", capturedSlot)
	}
	if resp.Status.Status != HarnessAuthRequired || resp.Status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want non-secret auth_required logout status", resp.Status)
	}
	stored, err := store.GetAuthSlot(context.Background(), "slot-work")
	if err != nil {
		t.Fatalf("GetAuthSlot returned error: %v", err)
	}
	if stored.Status != string(HarnessAuthRequired) {
		t.Fatalf("stored status = %q, want auth_required", stored.Status)
	}
}

func TestAuthLogoutRejectsUnknownSlotBeforeHarnessLogout(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "auth.logout", HarnessAuthLogoutRequest{AuthSlotID: "missing-slot"})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "unknown_auth_slot" || data["logout_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown_auth_slot before logout", data)
	}
}

func TestAuthLogoutUnsupportedHarnessReturnsProviderOwnedRemediation(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("test-harness", nil), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "slot-work", Harness: "test-harness", Label: "Work", Status: "authenticated"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	err := callMethodError(registry, "auth.logout", HarnessAuthLogoutRequest{AuthSlotID: "slot-work"})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "auth_logout_unsupported" || data["logout_invoked"] != false || data["ari_secret_storage"] != string(HarnessAriSecretStorageNone) {
		t.Fatalf("error data = %#v, want unsupported logout without Ari secret storage", data)
	}
}

func TestAuthLogoutReportsPartialSuccessWhenStatusPersistenceFails(t *testing.T) {
	store := newCommandMethodTestStore(t)
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	ctx, cancel := context.WithCancel(context.Background())
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &capturingHarness{name: "test-harness", authLogout: func(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
			_ = ctx
			cancel()
			return HarnessAuthStatus{Harness: slot.Harness, AuthSlotID: slot.AuthSlotID, Status: HarnessAuthRequired, AriSecretStorage: HarnessAriSecretStorageNone}, nil
		}}, nil
	})
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "slot-work", Harness: "test-harness", Label: "Work", Status: "authenticated"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	_, err := d.harnessAuthLogout(ctx, store, HarnessAuthLogoutRequest{AuthSlotID: "slot-work"})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "auth_logout_status_persist_failed" || data["logout_invoked"] != true || data["status"] != string(HarnessAuthRequired) || data["ari_secret_storage"] != string(HarnessAriSecretStorageNone) {
		t.Fatalf("error data = %#v, want provider logout partial success with failed Ari status persistence", data)
	}
}

func TestStartHarnessCallResultExtractsFinalResponseFromCapableHarness(t *testing.T) {
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	executor := newFinalResponseHarness("fake", []TimelineItem{{ID: "ti_full_transcript", Kind: "agent_text", Text: "Detailed internal transcript"}, {ID: "ti_final", Kind: "agent_text", Text: "Concise answer"}})
	call, err := NewAgentSessionHarnessCall(packet, nil)
	if err != nil {
		t.Fatalf("NewAgentSessionHarnessCall returned error: %v", err)
	}
	call.Input = []byte(renderContextPacket(packet))

	result, err := StartHarnessCallResult(context.Background(), executor, call)
	if err != nil {
		t.Fatalf("StartHarnessCallResult returned error: %v", err)
	}
	if result.FinalResponse == nil || result.FinalResponse.Status != "completed" || result.FinalResponse.Text != "Concise answer" {
		t.Fatalf("final response = %#v, want deterministic final agent text", result.FinalResponse)
	}
}

func TestStartExecutorRunRejectsMissingPacketIdentity(t *testing.T) {
	executor := newFakeHarness("fake", nil)
	_, _, err := StartExecutorRun(context.Background(), executor, ContextPacket{WorkspaceID: "ws-1", TaskID: "task-1"})
	if err == nil {
		t.Fatal("StartExecutorRun returned nil error for missing context packet id")
	}
}

type spyExecutor struct {
	capabilities []HarnessCapability
	started      bool
}

type missingAuthHarness struct{ spyExecutor }

func (missingAuthHarness) AuthStatus(context.Context, HarnessAuthSlot) (HarnessAuthStatus, error) {
	return HarnessAuthStatus{}, &HarnessUnavailableError{Harness: "missing-harness", Reason: "missing_executable", Executable: "missing-harness", Probe: "missing-harness --version", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
}

func (e *spyExecutor) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{Name: "spy", Capabilities: append([]HarnessCapability(nil), e.capabilities...)}
}

func (e *spyExecutor) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	_ = ctx
	_ = req
	e.started = true
	return ExecutorRun{SessionID: "spy-run", Executor: "spy", ProviderSessionID: "spy-run", CapabilityNames: []string{"agent.run.from_context"}}, nil
}

func (e *spyExecutor) Items(ctx context.Context, sessionID string) ([]TimelineItem, error) {
	_ = ctx
	_ = sessionID
	return nil, nil
}

func (e *spyExecutor) Stop(ctx context.Context, sessionID string) error {
	_ = ctx
	_ = sessionID
	return nil
}

type fakeHarness struct {
	mu                sync.Mutex
	name              string
	template          []TimelineItem
	runs              map[string][]TimelineItem
	lastContextPacket string
	finalResponse     bool
}

func newFakeHarness(name string, items []TimelineItem) *fakeHarness {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "fake"
	}
	return &fakeHarness{name: name, template: append([]TimelineItem(nil), items...), runs: map[string][]TimelineItem{}}
}

func newFinalResponseHarness(name string, items []TimelineItem) *fakeHarness {
	harness := newFakeHarness(name, items)
	harness.finalResponse = true
	return harness
}

func (e *fakeHarness) Descriptor() HarnessAdapterDescriptor {
	capabilities := []HarnessCapability{HarnessCapabilityAgentSessionFromContext, HarnessCapabilityContextPacket, HarnessCapabilityTimelineItems}
	if e.finalResponse {
		capabilities = append(capabilities, HarnessCapabilityFinalResponse)
	}
	return HarnessAdapterDescriptor{Name: e.name, Capabilities: capabilities}
}

func (e *fakeHarness) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	if ctx == nil {
		return ExecutorRun{}, fmt.Errorf("context is required")
	}
	sessionID := fmt.Sprintf("%s-run-%d", e.name, time.Now().UnixNano())
	e.lastContextPacket = req.ContextPacket
	items := append([]TimelineItem(nil), e.template...)
	for i := range items {
		items[i].SessionID = sessionID
		items[i].WorkspaceID = req.WorkspaceID
		items[i].SourceKind = "executor"
		if strings.TrimSpace(items[i].SourceID) == "" {
			items[i].SourceID = sessionID
		}
		if strings.TrimSpace(items[i].ID) == "" {
			items[i].ID = fmt.Sprintf("%s:item-%d", sessionID, i+1)
		}
		if items[i].Sequence == 0 {
			items[i].Sequence = i + 1
		}
		if strings.TrimSpace(items[i].Status) == "" {
			items[i].Status = "completed"
		}
	}
	e.mu.Lock()
	e.runs[sessionID] = items
	e.mu.Unlock()
	return ExecutorRun{SessionID: sessionID, Executor: e.name, ProviderSessionID: sessionID, CapabilityNames: []string{string(HarnessCapabilityTimelineItems)}}, nil
}

func (e *fakeHarness) Items(ctx context.Context, sessionID string) ([]TimelineItem, error) {
	_ = ctx
	e.mu.Lock()
	items, ok := e.runs[strings.TrimSpace(sessionID)]
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("run %q not found", sessionID)
	}
	return append([]TimelineItem(nil), items...), nil
}

func (e *fakeHarness) Stop(ctx context.Context, sessionID string) error {
	_ = ctx
	_ = sessionID
	return nil
}

type capturingHarness struct {
	name         string
	captured     *ExecutorStartRequest
	authStatuses map[string]HarnessAuthState
	authStart    func(context.Context, HarnessAuthSlot, string) (HarnessAuthStatus, error)
	authCancel   func(context.Context, HarnessAuthSlot, string) (HarnessAuthStatus, error)
	authLogout   func(context.Context, HarnessAuthSlot) (HarnessAuthStatus, error)
}

func (e *capturingHarness) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{Name: e.name, Capabilities: []HarnessCapability{HarnessCapabilityAgentSessionFromContext, HarnessCapabilityContextPacket, HarnessCapabilityTimelineItems}}
}

func (e *capturingHarness) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	_ = ctx
	*e.captured = req
	sessionID := fmt.Sprintf("%s-run-%d", e.name, time.Now().UnixNano())
	return ExecutorRun{SessionID: sessionID, Executor: e.name, ProviderSessionID: sessionID}, nil
}

func (e *capturingHarness) Items(ctx context.Context, sessionID string) ([]TimelineItem, error) {
	_ = ctx
	return []TimelineItem{{ID: sessionID + ":item-1", SessionID: sessionID, Kind: "agent_text", Status: "completed", Sequence: 1}}, nil
}

func (e *capturingHarness) AuthStatus(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	_ = ctx
	status := HarnessAuthAuthenticated
	if configured, ok := e.authStatuses[strings.TrimSpace(slot.AuthSlotID)]; ok {
		status = configured
	}
	return HarnessAuthStatus{Harness: e.name, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: status, AriSecretStorage: HarnessAriSecretStorageNone}, nil
}

func (e *capturingHarness) AuthStart(ctx context.Context, slot HarnessAuthSlot, method string) (HarnessAuthStatus, error) {
	if e.authStart != nil {
		return e.authStart(ctx, slot, method)
	}
	return HarnessAuthStatus{}, fmt.Errorf("auth start not configured")
}

func (e *capturingHarness) AuthCancel(ctx context.Context, slot HarnessAuthSlot, flowID string) (HarnessAuthStatus, error) {
	if e.authCancel != nil {
		return e.authCancel(ctx, slot, flowID)
	}
	return HarnessAuthStatus{}, fmt.Errorf("auth cancel not configured")
}

func (e *capturingHarness) AuthLogout(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	if e.authLogout != nil {
		return e.authLogout(ctx, slot)
	}
	return HarnessAuthStatus{}, fmt.Errorf("auth logout not configured")
}

func (e *capturingHarness) Stop(ctx context.Context, sessionID string) error {
	_ = ctx
	_ = sessionID
	return nil
}

func callMethodError(registry *rpc.MethodRegistry, methodName string, params any) error {
	spec, ok := registry.Get(methodName)
	if !ok {
		return fmt.Errorf("method %s not registered", methodName)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	_, err = spec.Call(context.Background(), raw)
	return err
}

func requireHandlerErrorData(t *testing.T, err error) map[string]any {
	t.Helper()
	if err == nil {
		t.Fatal("error is nil, want handler error")
	}
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok {
		t.Fatalf("error = %T %[1]v, want HandlerError", err)
	}
	data, ok := handlerErr.Data.(map[string]any)
	if !ok {
		t.Fatalf("handler error data = %T %#v, want map", handlerErr.Data, handlerErr.Data)
	}
	return data
}

func TestAgentSessionMethodRejectsFakeExecutor(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	primaryFolder := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "ws-1", primaryFolder)
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "codex-personal", Harness: "test-harness", Label: "Personal", ProviderLabel: "ChatGPT Plus", Status: "authenticated"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	err := callMethodError(registry, "agent.run", AgentSessionStartRequest{
		Executor: "fake",
		Packet:   packet,
	})
	if err == nil || !strings.Contains(err.Error(), "harness is not available") {
		t.Fatalf("agent.run fake error = %v, want unavailable executor", err)
	}
}

func TestAgentSessionMethodUsesInjectedHarnessFactory(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return newFakeHarness(req.Executor, []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	primaryFolder := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "ws-1", primaryFolder)
	for _, slot := range []globaldb.AuthSlot{
		{AuthSlotID: "slot-one", Harness: "test-harness", Label: "One", Status: "not_installed"},
		{AuthSlotID: "slot-two", Harness: "test-harness", Label: "Two", Status: "authenticated"},
	} {
		if err := store.UpsertAuthSlot(context.Background(), slot); err != nil {
			t.Fatalf("UpsertAuthSlot(%s) returned error: %v", slot.AuthSlotID, err)
		}
	}

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentSessionStartResponse](t, registry, "agent.run", AgentSessionStartRequest{Executor: "test-harness", Packet: packet})
	if resp.Run.Executor != "test-harness" || resp.Run.ContextPacketID != "ctx_123" {
		t.Fatalf("agent run = %#v, want injected harness run linked to context packet", resp.Run)
	}
}

func TestAgentSessionMethodPersistsRepeatedNoProfileRunsAndRunLogScope(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return newFakeHarness(req.Executor, []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	first := callMethod[AgentSessionStartResponse](t, registry, "agent.run", AgentSessionStartRequest{Executor: "test-harness", Packet: ContextPacket{ID: "ctx_1", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:1"}})
	second := callMethod[AgentSessionStartResponse](t, registry, "agent.run", AgentSessionStartRequest{Executor: "test-harness", Packet: ContextPacket{ID: "ctx_2", WorkspaceID: "ws-1", TaskID: "task-2", PacketHash: "sha256:2"}})

	runs, err := store.ListAgentSessions(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("ListAgentSessions returned error: %v", err)
	}
	if len(runs) != 2 || runs[0].AgentID != runs[1].AgentID {
		t.Fatalf("runs = %#v, want repeated no-profile runs to share persisted harness runtime agent config", runs)
	}
	messages, err := store.TailRunLogMessages(context.Background(), second.Run.AgentSessionID, 1)
	if err != nil {
		t.Fatalf("TailRunLogMessages returned error: %v", err)
	}
	if len(messages) != 1 || messages[0].WorkspaceID != "ws-1" || messages[0].AgentID != runs[0].AgentID || messages[0].SessionID != second.Run.AgentSessionID {
		t.Fatalf("messages = %#v first=%#v second=%#v, want run-log rows scoped to workspace and runtime agent", messages, first.Run, second.Run)
	}
}

func TestAgentProfileRunUsesProfileHarness(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return newFakeHarness(req.Executor, []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	d.setAgentProfileForTest(AgentProfile{Name: "executor", Harness: "test-harness", Model: "test-model", Prompt: "test-prompt", InvocationClass: HarnessInvocationAgent})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "codex-personal", Harness: "test-harness", Label: "Personal", ProviderLabel: "ChatGPT Plus", Status: "authenticated"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{Profile: "executor", Packet: packet})
	if resp.Profile != "executor" || resp.Harness != "test-harness" || resp.Run.Executor != "test-harness" {
		t.Fatalf("profile run response = %#v, want executor routed to test-harness", resp)
	}
	if resp.Run.ContextPacketID != "ctx_123" || len(resp.Items) != 1 || resp.Items[0].SessionID != resp.Run.AgentSessionID {
		t.Fatalf("profile run items = %#v run = %#v, want linked context/timeline", resp.Items, resp.Run)
	}
}

func TestAgentProfileRunUsesStoredProfile(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return newFakeHarness(req.Executor, []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "ap_executor", Name: "executor", Harness: "test-harness", Model: "stored-model", Prompt: "stored-prompt", InvocationClass: string(HarnessInvocationAgent)}); err != nil {
		t.Fatalf("UpsertAgentProfile returned error: %v", err)
	}

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{Profile: "executor", Packet: packet})
	if resp.Profile != "executor" || resp.Harness != "test-harness" || resp.Run.Executor != "test-harness" {
		t.Fatalf("profile run response = %#v, want stored profile routed to test-harness", resp)
	}
}

func TestAgentProfileRunPersistsFinalResponseArtifact(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFinalResponseHarness("test-harness", []TimelineItem{{ID: "ti_transcript", Kind: "agent_text", Text: "Internal transcript text", Metadata: map[string]any{"provider_message_id": "msg-raw-1", "provider_item_id": "raw-1", "provider_turn_id": "turn-1", "provider_response_id": "resp-1", "provider_call_id": "call-1", "provider_channel": "analysis", "tool_name": "web.search"}}, {ID: "ti_final", Kind: "agent_text", Text: "Excerptable answer"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	primaryFolder := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "ws-1", primaryFolder)
	if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "ap_executor", Name: "executor", Harness: "test-harness", InvocationClass: string(HarnessInvocationAgent)}); err != nil {
		t.Fatalf("UpsertAgentProfile returned error: %v", err)
	}

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	runResp := callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{Profile: "executor", Packet: packet})
	finalResp := callMethod[FinalResponseResponse](t, registry, "final_response.get", FinalResponseGetRequest{SessionID: runResp.Run.AgentSessionID})
	if finalResp.ProfileID != "ap_executor" || finalResp.ContextPacketID != "ctx_123" || finalResp.Text != "Excerptable answer" {
		t.Fatalf("final response = %#v, want stored excerptable artifact", finalResp)
	}
	if strings.Contains(finalResp.Text, "Internal transcript") {
		t.Fatalf("final response text = %q, must not include transcript text", finalResp.Text)
	}
	if len(finalResp.EvidenceLinks) < 2 || finalResp.EvidenceLinks[0].Kind != "context_packet" {
		t.Fatalf("evidence links = %#v, want context/run provenance", finalResp.EvidenceLinks)
	}
	runs, err := store.ListAgentSessions(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("ListAgentSessions returned error: %v", err)
	}
	if len(runs) != 1 || runs[0].SessionID != runResp.Run.AgentSessionID || runs[0].WorkspaceID != "ws-1" || runs[0].Harness != "test-harness" || runs[0].Status != "completed" {
		t.Fatalf("agent runs = %#v, want normalized persisted harness run", runs)
	}
	if runs[0].ProviderSessionID != runResp.Run.ProviderSessionID || runs[0].ProviderRunID != runResp.Run.ProviderRunID || runs[0].CWD != primaryFolder || !strings.Contains(runs[0].FolderScopeJSON, primaryFolder) || !strings.Contains(runs[0].ProviderMetadataJSON, `"resume_mode":"none"`) || !strings.Contains(runs[0].ProviderMetadataJSON, `"provider_session_id"`) || !strings.Contains(runs[0].ContextPayloadIDsJSON, "ctx_123") {
		t.Fatalf("agent run metadata = %#v, want provider session and context/session metadata", runs[0])
	}
	messages, err := store.TailRunLogMessages(context.Background(), runResp.Run.AgentSessionID, 2)
	if err != nil {
		t.Fatalf("TailRunLogMessages returned error: %v", err)
	}
	if len(messages) != 2 || messages[0].Role != "assistant" || messages[0].Parts[0].Text != "Internal transcript text" || messages[1].Parts[0].Text != "Excerptable answer" {
		t.Fatalf("run messages = %#v, want normalized harness transcript messages", messages)
	}
	if messages[0].ProviderKind != "agent_text" || messages[0].ProviderMessageID != "msg-raw-1" || messages[0].ProviderItemID != "raw-1" || messages[0].ProviderTurnID != "turn-1" || messages[0].ProviderResponseID != "resp-1" || messages[0].ProviderCallID != "call-1" || messages[0].ProviderChannel != "analysis" || messages[0].Parts[0].ToolName != "web.search" || messages[0].Parts[0].ToolCallID != "call-1" || !strings.Contains(messages[0].RawMetadataJSON, "raw-1") {
		t.Fatalf("run message metadata = %#v, want provider kind and raw metadata", messages[0])
	}
}

func TestAgentProfileRunPreservesProviderMessageFacetsAcrossHarnesses(t *testing.T) {
	for _, harness := range []string{HarnessNameClaude, HarnessNameCodex, HarnessNameOpenCode, HarnessNamePTY} {
		t.Run(harness, func(t *testing.T) {
			store := newCommandMethodTestStore(t)
			registry := rpc.NewMethodRegistry()
			d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
			d.setHarnessFactoryForTest(harness, func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
				_ = req
				_ = primaryFolder
				_ = sink
				return newFakeHarness(harness, []TimelineItem{{ID: harness + "-item", Kind: "agent_text", Text: harness + " output", Metadata: map[string]any{"message_id": harness + "-message", "item_id": harness + "-item", "turn_id": harness + "-turn", "response_id": harness + "-response", "tool_call_id": harness + "-call", "channel": "commentary", "name": harness + ".tool", "raw_event": harness}}}), nil
			})
			if err := d.registerMethods(registry, store); err != nil {
				t.Fatalf("registerMethods returned error: %v", err)
			}
			primaryFolder := t.TempDir()
			seedSessionWithPrimaryFolder(t, store, "ws-1", primaryFolder)
			if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "ap_" + harness, Name: harness + "-agent", Harness: harness, InvocationClass: string(HarnessInvocationAgent)}); err != nil {
				t.Fatalf("UpsertAgentProfile returned error: %v", err)
			}

			runResp := callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{Profile: harness + "-agent", Packet: ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}})
			messages, err := store.TailRunLogMessages(context.Background(), runResp.Run.AgentSessionID, 1)
			if err != nil {
				t.Fatalf("TailRunLogMessages returned error: %v", err)
			}
			if len(messages) != 1 || messages[0].ProviderMessageID != harness+"-message" || messages[0].ProviderItemID != harness+"-item" || messages[0].ProviderTurnID != harness+"-turn" || messages[0].ProviderResponseID != harness+"-response" || messages[0].ProviderCallID != harness+"-call" || messages[0].ProviderChannel != "commentary" || messages[0].Parts[0].ToolName != harness+".tool" || !strings.Contains(messages[0].RawMetadataJSON, "raw_event") {
				t.Fatalf("message = %#v, want normalized provider facets and raw event metadata", messages)
			}
		})
	}
}

func TestAgentProfileRunNormalizesRealCodexItemFacets(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest(HarnessNameCodex, func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		transport := newFakeCodexTransport([]codexNotification{
			{Method: "thread/started", Params: mustRawJSON(t, `{"thread":{"id":"thr_123"}}`)},
			{Method: "turn/started", Params: mustRawJSON(t, `{"threadId":"thr_123","turn":{"id":"turn_456"}}`)},
			{Method: "item/completed", Params: mustRawJSON(t, `{"threadId":"thr_123","turnId":"turn_456","item":{"id":"item_1","type":"agent_message","text":"hello world"}}`)},
			{Method: "turn/completed", Params: mustRawJSON(t, `{"threadId":"thr_123","turn":{"id":"turn_456","status":"completed"}}`)},
		})
		return NewCodexExecutorForTest(codexExecutorOptions{Executable: "codex", Cwd: primaryFolder, StartTransport: fakeCodexStarter(transport)}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "ap_codex", Name: "codex-agent", Harness: HarnessNameCodex, InvocationClass: string(HarnessInvocationAgent)}); err != nil {
		t.Fatalf("UpsertAgentProfile returned error: %v", err)
	}

	runResp := callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{Profile: "codex-agent", Packet: ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}})
	messages, err := store.ListRunLogMessages(context.Background(), runResp.Run.AgentSessionID, 0, 10)
	if err != nil {
		t.Fatalf("TailRunLogMessages returned error: %v", err)
	}
	var matched *globaldb.RunLogMessage
	for i := range messages {
		if messages[i].ProviderItemID == "item_1" {
			matched = &messages[i]
		}
	}
	if matched == nil || matched.ProviderTurnID != "turn_456" || matched.ProviderKind != "agent_message" || !strings.Contains(matched.RawMetadataJSON, "item_1") {
		t.Fatalf("message = %#v, want real Codex item id/type/turn normalized and raw metadata preserved", messages)
	}
}

func TestAgentProfileRunUsesWorkspaceScopedRuntimeAgentForGlobalProfile(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("test-harness", []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	seedSessionWithPrimaryFolder(t, store, "ws-2", t.TempDir())
	if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "ap_global_executor", Name: "executor", Harness: "test-harness", InvocationClass: string(HarnessInvocationAgent)}); err != nil {
		t.Fatalf("UpsertAgentProfile returned error: %v", err)
	}

	first := callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{Profile: "executor", Packet: ContextPacket{ID: "ctx_1", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:1"}})
	second := callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{Profile: "executor", Packet: ContextPacket{ID: "ctx_2", WorkspaceID: "ws-2", TaskID: "task-2", PacketHash: "sha256:2"}})
	runsOne, err := store.ListAgentSessions(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("ListAgentSessions ws-1 returned error: %v", err)
	}
	runsTwo, err := store.ListAgentSessions(context.Background(), "ws-2")
	if err != nil {
		t.Fatalf("ListAgentSessions ws-2 returned error: %v", err)
	}
	if len(runsOne) != 1 || len(runsTwo) != 1 || runsOne[0].SessionID != first.Run.AgentSessionID || runsTwo[0].SessionID != second.Run.AgentSessionID || runsOne[0].AgentID == runsTwo[0].AgentID || runsOne[0].WorkspaceID != "ws-1" || runsTwo[0].WorkspaceID != "ws-2" {
		t.Fatalf("runs = %#v / %#v, want workspace-scoped runtime agents for excerptd global profile", runsOne, runsTwo)
	}
}

func TestAgentProfileRunPersistsMeasuredTelemetryAndProcessSample(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("test-harness", []TimelineItem{{Kind: "telemetry", Metadata: map[string]any{"input_tokens": "12", "output_tokens": "5"}}}), nil
	})
	originalSampler := agentSessionProcessMetricsSampler
	pid := int64(12345)
	agentSessionProcessMetricsSampler = func(context.Context, AgentSession) ProcessMetricsSample {
		return ProcessMetricsSample{OwnedByAri: true, PID: ProcessMetricValue{Known: true, Value: &pid, Confidence: "sampled"}, CPUTimeMS: unknownProcessMetric("unsupported"), MemoryRSSBytesPeak: unknownProcessMetric("unsupported"), ChildProcessesPeak: unknownProcessMetric("unsupported"), OrphanState: "not_orphaned", ExitCode: unknownProcessMetric("unknown")}
	}
	t.Cleanup(func() { agentSessionProcessMetricsSampler = originalSampler })
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "ap_executor", Name: "executor", Harness: "test-harness", Model: "model-1", InvocationClass: string(HarnessInvocationAgent)}); err != nil {
		t.Fatalf("UpsertAgentProfile returned error: %v", err)
	}

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	_ = callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{Profile: "executor", Packet: packet})
	rollups, err := store.RollupAgentSessionTelemetry(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("RollupAgentSessionTelemetry returned error: %v", err)
	}
	if len(rollups) != 1 || rollups[0].Group.ProfileID != "ap_executor" || !rollups[0].InputTokens.Known || *rollups[0].InputTokens.Value != 12 || !rollups[0].OutputTokens.Known || *rollups[0].OutputTokens.Value != 5 {
		t.Fatalf("telemetry rollups = %#v, want measured token rollup", rollups)
	}
}

func TestAgentProfileCreateAndGetPersistProfile(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	created := callMethod[AgentProfileResponse](t, registry, "profile.create", AgentProfileCreateRequest{Name: "executor", Harness: "codex", Model: "gpt-5.1-codex", Prompt: "Do work", InvocationClass: HarnessInvocationAgent, Defaults: map[string]any{"effort": "high"}})
	if created.ProfileID == "" || created.Name != "executor" || created.Harness != "codex" || created.Defaults["effort"] != "high" {
		t.Fatalf("created profile = %#v, want durable profile response", created)
	}
	got := callMethod[AgentProfileResponse](t, registry, "profile.get", AgentProfileGetRequest{Name: "executor"})
	if got.ProfileID != created.ProfileID || got.Model != "gpt-5.1-codex" || got.Prompt != "Do work" || got.InvocationClass != HarnessInvocationAgent {
		t.Fatalf("got profile = %#v, want created profile %#v", got, created)
	}
	listed := callMethod[AgentProfileListResponse](t, registry, "profile.list", AgentProfileListRequest{})
	if len(listed.Profiles) != 1 || listed.Profiles[0].ProfileID != created.ProfileID {
		t.Fatalf("listed profiles = %#v, want created profile", listed.Profiles)
	}
}

func TestAgentProfileCreateRejectsMissingName(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "profile.create", AgentProfileCreateRequest{Harness: "codex"})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "missing_profile_name" {
		t.Fatalf("error data = %#v, want missing profile name", data)
	}
}

func TestAuthSlotListReturnsMetadataWithoutSources(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "codex-personal", Harness: "codex", Label: "Personal", ProviderLabel: "ChatGPT Plus", Status: "authenticated"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	resp := callMethod[AuthSlotListResponse](t, registry, "auth.slot.list", AuthSlotListRequest{Harness: "codex"})
	var personal AuthSlotResponse
	for _, slot := range resp.Slots {
		if slot.AuthSlotID == "codex-personal" {
			personal = slot
		}
	}
	if personal.AuthSlotID != "codex-personal" || personal.ProviderLabel != "ChatGPT Plus" || personal.CredentialOwner != "provider" {
		t.Fatalf("slots = %#v, want redacted metadata for codex-personal", resp.Slots)
	}
}

func TestAuthStatusUsesStoredSlotsWhenNoSlotsRequested(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return &capturingHarness{name: req.Executor, authStatuses: map[string]HarnessAuthState{"slot-two": HarnessAuthAuthenticated}}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "slot-two", Harness: "test-harness", Label: "Second", Status: "unknown"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	resp := callMethod[HarnessAuthStatusResponse](t, registry, "auth.status", HarnessAuthStatusRequest{})
	var slotStatus HarnessAuthStatus
	for _, status := range resp.Statuses {
		if status.AuthSlotID == "slot-two" {
			slotStatus = status
		}
	}
	if slotStatus.Status != HarnessAuthAuthenticated {
		t.Fatalf("statuses = %#v, want stored slot status", resp.Statuses)
	}
}

func TestDefaultHelperEnsureAndGetUseWorkspaceScopedHelper(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	seedSessionWithPrimaryFolder(t, store, "ws-2", t.TempDir())

	created := callMethod[AgentProfileResponse](t, registry, "profile.helper.ensure", DefaultHelperEnsureRequest{WorkspaceID: "ws-1", Harness: "codex", Prompt: "Help here"})
	if created.Name != "helper" || created.WorkspaceID != "ws-1" || created.Harness != "codex" || created.Prompt != "Help here" {
		t.Fatalf("created helper = %#v", created)
	}
	got := callMethod[AgentProfileResponse](t, registry, "profile.helper.get", DefaultHelperGetRequest{WorkspaceID: "ws-1"})
	if got.ProfileID != created.ProfileID {
		t.Fatalf("got helper = %#v, want %#v", got, created)
	}

	err := callMethodError(registry, "profile.helper.get", DefaultHelperGetRequest{WorkspaceID: "ws-2"})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "helper_setup_required" {
		t.Fatalf("error data = %#v, want helper_setup_required", data)
	}
}

func TestDefaultHelperEnsureRejectsUnknownWorkspace(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "profile.helper.ensure", DefaultHelperEnsureRequest{WorkspaceID: "missing", Harness: "codex"})
	if err == nil {
		t.Fatal("profile.helper.ensure returned nil error for unknown workspace")
	}
}

func TestAgentProfileResponsesDoNotExposeRoleClassification(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	created := callMethod[AgentProfileResponse](t, registry, "profile.create", AgentProfileCreateRequest{Name: "helper", Harness: "codex"})
	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, ok := fields["role"]; ok {
		t.Fatalf("profile response exposed role: %s", encoded)
	}
	if _, ok := fields["kind"]; ok {
		t.Fatalf("profile response exposed kind: %s", encoded)
	}
}

func TestAgentProfileRunUsesInlineProfileDefinition(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return newFakeHarness(req.Executor, []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	for _, slot := range []globaldb.AuthSlot{
		{AuthSlotID: "slot-one", Harness: "test-harness", Label: "One", Status: "not_installed"},
		{AuthSlotID: "slot-two", Harness: "test-harness", Label: "Two", Status: "authenticated"},
	} {
		if err := store.UpsertAuthSlot(context.Background(), slot); err != nil {
			t.Fatalf("UpsertAuthSlot(%s) returned error: %v", slot.AuthSlotID, err)
		}
	}

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{ProfileDefinition: &AgentProfile{Name: "dynamic", Harness: "test-harness"}, Packet: packet})
	if resp.Profile != "dynamic" || resp.Harness != "test-harness" || resp.Run.Executor != "test-harness" {
		t.Fatalf("profile run response = %#v, want inline dynamic profile routed to test-harness", resp)
	}
}

func TestAgentProfileRunUsesDefaultsOnlyHarness(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return newFakeHarness(req.Executor, []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{Defaults: AgentSessionDefaults{Harness: "test-harness", Model: "default-model", Prompt: "default-prompt"}, Packet: packet})
	if resp.Profile != "" || resp.Harness != "test-harness" || resp.Run.Executor != "test-harness" {
		t.Fatalf("profile run response = %#v, want defaults-only harness route", resp)
	}
}

func TestAgentProfileRunDefaultsFillPartialProfile(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return newFakeHarness(req.Executor, []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{ProfileDefinition: &AgentProfile{Name: "dynamic"}, Defaults: AgentSessionDefaults{Harness: "test-harness"}, Packet: packet})
	if resp.Profile != "dynamic" || resp.Harness != "test-harness" || resp.Run.Executor != "test-harness" {
		t.Fatalf("profile run response = %#v, want defaults filling partial profile", resp)
	}
}

func TestAgentProfileRunPassesProfileMetadataToHarness(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	var captured ExecutorStartRequest
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return &capturingHarness{name: req.Executor, captured: &captured}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	_ = callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{ProfileDefinition: &AgentProfile{Name: "dynamic", Harness: "test-harness", Model: "explicit-model"}, Defaults: AgentSessionDefaults{Model: "default-model", Prompt: "default-prompt"}, Packet: packet})
	if captured.SourceProfileID != "dynamic" || captured.Model != "explicit-model" || captured.Prompt != "default-prompt" || captured.InvocationClass != HarnessInvocationAgent {
		t.Fatalf("captured request = %#v, want profile/default metadata at harness boundary", captured)
	}
}

func TestAgentProfileRunPassesSelectedAuthSlotToHarnessAndRun(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	var captured ExecutorStartRequest
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return &capturingHarness{name: req.Executor, captured: &captured}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "codex-personal", Harness: "test-harness", Label: "Personal", ProviderLabel: "ChatGPT Plus", Status: "authenticated"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{ProfileDefinition: &AgentProfile{Name: "dynamic", Harness: "test-harness", AuthSlotID: "codex-personal"}, Packet: packet})
	if captured.AuthSlotID != "codex-personal" || resp.Run.AuthSlotID != "codex-personal" {
		t.Fatalf("captured request = %#v run = %#v, want selected auth slot recorded", captured, resp.Run)
	}
}

func TestAgentProfileRunUsesDefaultAuthSlotWhenProfileOmitsAuth(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	var captured ExecutorStartRequest
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return &capturingHarness{name: req.Executor, captured: &captured}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "slot-default", Harness: "test-harness", Label: "Default", Status: "authenticated"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{ProfileDefinition: &AgentProfile{Name: "dynamic", Harness: "test-harness"}, Defaults: AgentSessionDefaults{AuthSlotID: "slot-default"}, Packet: packet})
	if captured.AuthSlotID != "slot-default" || resp.Run.AuthSlotID != "slot-default" {
		t.Fatalf("captured request = %#v run = %#v, want default auth slot recorded", captured, resp.Run)
	}
}

func TestAgentProfileRunAuthPoolRecordsSafeFailoverSlot(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	var captured ExecutorStartRequest
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return &capturingHarness{name: req.Executor, captured: &captured, authStatuses: map[string]HarnessAuthState{"slot-one": HarnessAuthNotInstalled, "slot-two": HarnessAuthAuthenticated}}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	for _, slot := range []globaldb.AuthSlot{
		{AuthSlotID: "slot-one", Harness: "test-harness", Label: "One", Status: "not_installed"},
		{AuthSlotID: "slot-two", Harness: "test-harness", Label: "Two", Status: "authenticated"},
	} {
		if err := store.UpsertAuthSlot(context.Background(), slot); err != nil {
			t.Fatalf("UpsertAuthSlot(%s) returned error: %v", slot.AuthSlotID, err)
		}
	}

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{ProfileDefinition: &AgentProfile{Name: "dynamic", Harness: "test-harness", AuthPool: HarnessAuthPool{SlotIDs: []string{"slot-one", "slot-two"}, Strategy: HarnessAuthPoolFailover}}, Packet: packet})
	if captured.AuthSlotID != "slot-two" || resp.Run.AuthSlotID != "slot-two" {
		t.Fatalf("captured request = %#v run = %#v, want safe failover slot recorded", captured, resp.Run)
	}
}

func TestAgentProfileRunAuthPoolSkipsMissingDBSlot(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	var captured ExecutorStartRequest
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return &capturingHarness{name: req.Executor, captured: &captured, authStatuses: map[string]HarnessAuthState{"slot-two": HarnessAuthAuthenticated}}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.UpsertAuthSlot(context.Background(), globaldb.AuthSlot{AuthSlotID: "slot-two", Harness: "test-harness", Label: "Two", Status: "authenticated"}); err != nil {
		t.Fatalf("UpsertAuthSlot returned error: %v", err)
	}

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentProfileRunResponse](t, registry, "profile.run", AgentProfileRunRequest{ProfileDefinition: &AgentProfile{Name: "dynamic", Harness: "test-harness", AuthPool: HarnessAuthPool{SlotIDs: []string{"slot-one", "slot-two"}, Strategy: HarnessAuthPoolFailover}}, Packet: packet})
	if captured.AuthSlotID != "slot-two" || resp.Run.AuthSlotID != "slot-two" {
		t.Fatalf("captured request = %#v run = %#v, want failover past missing DB slot", captured, resp.Run)
	}
}

func TestAgentProfileRunRejectsUnstoredAuthSlot(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &capturingHarness{name: "test-harness"}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	err := callMethodError(registry, "profile.run", AgentProfileRunRequest{ProfileDefinition: &AgentProfile{Name: "dynamic", Harness: "test-harness", AuthSlotID: "missing-slot"}, Packet: ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "unknown_auth_slot" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown_auth_slot before start", data)
	}
}

func TestAgentProfileRunRejectsMissingHarness(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	err := callMethodError(registry, "profile.run", AgentProfileRunRequest{ProfileDefinition: &AgentProfile{Name: "dynamic"}, Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws-1", TaskID: "task"}})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "missing_harness" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want missing harness data", data)
	}
}

func TestAgentProfileRunRejectsAmbiguousProfileInput(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setAgentProfileForTest(AgentProfile{Name: "stored", Harness: "test-harness"})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "profile.run", AgentProfileRunRequest{Profile: "stored", ProfileDefinition: &AgentProfile{Name: "other"}, Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws", TaskID: "task"}})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "ambiguous_profile" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want ambiguous profile data", data)
	}
}

func TestAgentProfileRunRejectsSameNameAmbiguousProfileInput(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setAgentProfileForTest(AgentProfile{Name: "stored", Harness: "test-harness"})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "profile.run", AgentProfileRunRequest{Profile: "stored", ProfileDefinition: &AgentProfile{Name: "stored", Harness: "other"}, Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws", TaskID: "task"}})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "ambiguous_profile" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want same-name ambiguous profile data", data)
	}
}

func TestAgentProfileRunReturnsUnknownProfileData(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "profile.run", AgentProfileRunRequest{Profile: "missing", Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws", TaskID: "task"}})
	data := requireHandlerErrorData(t, err)
	if data["profile"] != "missing" || data["reason"] != "unknown_profile" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown profile data", data)
	}
}

func TestAgentProfileRunReturnsUnknownHarnessData(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setAgentProfileForTest(AgentProfile{Name: "executor", Harness: "missing-harness", InvocationClass: HarnessInvocationAgent})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	err := callMethodError(registry, "profile.run", AgentProfileRunRequest{Profile: "executor", Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws-1", TaskID: "task"}})
	data := requireHandlerErrorData(t, err)
	if data["harness"] != "missing-harness" || data["reason"] != "unknown_harness" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown harness data", data)
	}
}

func TestAgentProfileRunReturnsUnavailableHarnessData(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("missing-binary", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return nil, &HarnessUnavailableError{Harness: "missing-binary", Reason: "missing_executable", Executable: "missing-binary", Probe: "missing-binary --version", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
	})
	d.setAgentProfileForTest(AgentProfile{Name: "executor", Harness: "missing-binary", InvocationClass: HarnessInvocationAgent})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	err := callMethodError(registry, "profile.run", AgentProfileRunRequest{Profile: "executor", Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws-1", TaskID: "task"}})
	data := requireHandlerErrorData(t, err)
	if data["harness"] != "missing-binary" || data["reason"] != "missing_executable" || data["executable"] != "missing-binary" || data["probe"] != "missing-binary --version" || data["required_capability"] != string(HarnessCapabilityAgentSessionFromContext) || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unavailable harness data", data)
	}
}

func TestAgentProfileRunHasNoDefaultProfiles(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	err := callMethodError(registry, "profile.run", AgentProfileRunRequest{Profile: "plan", Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws-1", TaskID: "task"}})
	data := requireHandlerErrorData(t, err)
	if data["profile"] != "plan" || data["reason"] != "unknown_profile" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want no default profile data", data)
	}
}

func TestAgentSessionReturnsUnsupportedCapabilitiesData(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("limited", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityContextPacket}}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	err := callMethodError(registry, "agent.run", AgentSessionStartRequest{Executor: "limited", Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws-1", TaskID: "task"}})
	data := requireHandlerErrorData(t, err)
	capabilities, ok := data["unsupported_capabilities"].([]string)
	wantCapabilities := string(HarnessCapabilityAgentSessionFromContext) + "," + string(HarnessCapabilityTimelineItems)
	if !ok || strings.Join(capabilities, ",") != wantCapabilities || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unsupported capabilities data", data)
	}
}

func TestAgentSessionReturnsInvalidParamsForMissingPacketID(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return newFakeHarness(req.Executor, []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	err := callMethodError(registry, "agent.run", AgentSessionStartRequest{Executor: "test-harness", Packet: ContextPacket{WorkspaceID: "ws-1", TaskID: "task"}})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams HandlerError", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["field"] != "packet.id" || data["reason"] != "invalid_harness_call" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want packet id validation data", data)
	}
}

func TestAgentSessionReturnsInvalidParamsForMissingPTYCommand(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	err := callMethodError(registry, "agent.run", AgentSessionStartRequest{Executor: HarnessNamePTY, Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws-1", TaskID: "task"}})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams HandlerError", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["field"] != "command" || data["reason"] != "invalid_harness_call" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want command validation data", data)
	}
}

func TestAgentProfileRunReturnsUnsupportedCapabilitiesData(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("limited", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityContextPacket}}, nil
	})
	d.setAgentProfileForTest(AgentProfile{Name: "executor", Harness: "limited", InvocationClass: HarnessInvocationAgent})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	err := callMethodError(registry, "profile.run", AgentProfileRunRequest{Profile: "executor", Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws-1", TaskID: "task"}})
	data := requireHandlerErrorData(t, err)
	capabilities, ok := data["unsupported_capabilities"].([]string)
	wantCapabilities := string(HarnessCapabilityAgentSessionFromContext) + "," + string(HarnessCapabilityTimelineItems)
	if !ok || strings.Join(capabilities, ",") != wantCapabilities || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want profile unsupported capabilities data", data)
	}
}

func TestAgentSessionMethodStartsPTYExecutorFromContextPacket(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	start := time.Now()
	resp := callMethod[AgentSessionStartResponse](t, registry, "agent.run", AgentSessionStartRequest{
		Executor: "pty",
		Packet:   packet,
		Command:  "/bin/sh",
		Args:     []string{"-c", "sleep 0.2; printf done"},
	})
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("agent.run pty took %s, want prompt return", time.Since(start))
	}
	if resp.Run.Executor != "pty" || resp.Run.ContextPacketID != "ctx_123" {
		t.Fatalf("agent run = %#v, want pty run linked to context packet", resp.Run)
	}
	if len(resp.Items) != 1 || resp.Items[0].Kind != "lifecycle" || resp.Items[0].Status != "running" {
		t.Fatalf("items = %#v, want one running lifecycle item", resp.Items)
	}
	deadline := time.Now().Add(boundedTestTimeout(t, 5*time.Second))
	for time.Now().Before(deadline) {
		timeline := callMethod[WorkspaceTimelineResponse](t, registry, "workspace.timeline", WorkspaceTimelineRequest{WorkspaceID: "ws-1"})
		for _, item := range timeline.Items {
			if item.SessionID == resp.Run.AgentSessionID && item.Kind == "run_log_message" && item.Text == "done" {
				if item.ID != resp.Run.AgentSessionID+":output" {
					t.Fatalf("pty output timeline item id = %q, want Ari session scoped output id", item.ID)
				}
				activity := callMethod[WorkspaceActivityResponse](t, registry, "workspace.activity", WorkspaceActivityRequest{WorkspaceID: "ws-1"})
				if len(activity.Agents) != 1 || activity.Agents[0].Status != "completed" {
					t.Fatalf("activity agents = %#v, want completed pty run after output", activity.Agents)
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("workspace.timeline did not persist pty output for run %s", resp.Run.AgentSessionID)
}

func TestRecordExecutorRunPreservesBufferedSinkItems(t *testing.T) {
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.appendExecutorItems("run-1", []TimelineItem{{ID: "run-1:output", WorkspaceID: "ws-1", SessionID: "run-1", SourceKind: "executor", SourceID: "run-1", Kind: "terminal_output", Status: "completed", Text: "done"}})

	d.recordExecutorRun(AgentSession{AgentSessionID: "run-1", WorkspaceID: "ws-1", Status: "running", Executor: "pty"}, []TimelineItem{{ID: "run-1:lifecycle", WorkspaceID: "ws-1", SessionID: "run-1", SourceKind: "executor", SourceID: "run-1", Kind: "lifecycle", Status: "running", Text: "pty"}})

	items := d.executorTimelineItems("ws-1")
	if len(items) != 2 {
		t.Fatalf("executor items len = %d, want buffered output plus initial lifecycle: %#v", len(items), items)
	}
	if items[0].ID != "run-1:lifecycle" || items[1].ID != "run-1:output" {
		t.Fatalf("executor items = %#v, want lifecycle then buffered output", items)
	}
	activity := AgentActivity{Status: d.executorRuns["run-1"].Status}
	if activity.Status != "completed" {
		t.Fatalf("executor run status = %q, want completed from buffered sink item", activity.Status)
	}
}

func TestExecutorRunStatusFailureTakesPrecedence(t *testing.T) {
	status := executorRunStatusFromItems([]TimelineItem{
		{ID: "run-1:output", Status: "completed"},
		{ID: "run-1:failure", Status: "failed"},
	})
	if status != "failed" {
		t.Fatalf("executor status = %q, want failed", status)
	}
}

func TestAgentSessionMethodMarksPTYFailureFromExitCode(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentSessionStartResponse](t, registry, "agent.run", AgentSessionStartRequest{
		Executor: "pty",
		Packet:   packet,
		Command:  "/bin/sh",
		Args:     []string{"-c", "printf failed; exit 7"},
	})
	deadline := time.Now().Add(boundedTestTimeout(t, 5*time.Second))
	for time.Now().Before(deadline) {
		activity := callMethod[WorkspaceActivityResponse](t, registry, "workspace.activity", WorkspaceActivityRequest{WorkspaceID: "ws-1"})
		if len(activity.Agents) == 1 && activity.Agents[0].ID == resp.Run.AgentSessionID && activity.Agents[0].Status == "failed" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("workspace.activity did not mark failed pty run %s as failed", resp.Run.AgentSessionID)
}
