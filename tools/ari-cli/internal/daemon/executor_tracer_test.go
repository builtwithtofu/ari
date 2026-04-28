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
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/testutil"

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

func TestStartExecutorRunProjectsPacketIntoAgentRunAndTimeline(t *testing.T) {
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
	if run.ProviderRunID == "" {
		t.Fatal("provider run id is empty")
	}
	if run.AgentRunID == run.ProviderRunID || !isULID(run.AgentRunID) {
		t.Fatalf("agent run id = %q, provider run id = %q, want distinct Ari ULID run id", run.AgentRunID, run.ProviderRunID)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if executor.lastContextPacket == "" || !strings.Contains(executor.lastContextPacket, "ctx_123") {
		t.Fatalf("executor context packet = %q, want serialized packet", executor.lastContextPacket)
	}
	if items[0].RunID != run.AgentRunID || items[0].Kind != "agent_text" || items[0].SourceKind != "executor" {
		t.Fatalf("timeline item = %#v, want executor agent_text linked to run", items[0])
	}
	if items[0].SourceID != run.AgentRunID {
		t.Fatalf("timeline source id = %q, want Ari run id %q", items[0].SourceID, run.AgentRunID)
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
	migrationsDir := filepath.Join("..", "..", "migrations")
	if err := testutil.ApplySQLMigrations(dbPath, migrationsDir); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	store, err := globaldb.NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore returned error: %v", err)
	}
	return store
}

func TestStartExecutorRunRejectsMissingRequiredCapabilityBeforeStart(t *testing.T) {
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	executor := &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityAgentRunFromContext}}
	call, err := NewAgentRunHarnessCall(packet, []HarnessCapability{HarnessCapabilityMeasuredTokenTelemetry})
	if err != nil {
		t.Fatalf("NewAgentRunHarnessCall returned error: %v", err)
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
	executor := &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityAgentRunFromContext}}

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
	executor := &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityAgentRunFromContext}}
	call, err := NewAgentRunHarnessCall(packet, nil)
	if err != nil {
		t.Fatalf("NewAgentRunHarnessCall returned error: %v", err)
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
	executor := &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityAgentRunFromContext}}
	call, err := NewAgentRunHarnessCall(packet, nil)
	if err != nil {
		t.Fatalf("NewAgentRunHarnessCall returned error: %v", err)
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
	executor := &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityAgentRunFromContext}}
	call, err := NewAgentRunHarnessCall(packet, nil)
	if err != nil {
		t.Fatalf("NewAgentRunHarnessCall returned error: %v", err)
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
	executor := &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityAgentRunFromContext}}
	call, err := NewAgentRunHarnessCall(packet, nil)
	if err != nil {
		t.Fatalf("NewAgentRunHarnessCall returned error: %v", err)
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
	call, err := NewAgentRunHarnessCall(packet, nil)
	if err != nil {
		t.Fatalf("NewAgentRunHarnessCall returned error: %v", err)
	}
	call.Input = []byte(renderContextPacket(packet))

	result, err := StartHarnessCallResult(context.Background(), executor, call)
	if err != nil {
		t.Fatalf("StartHarnessCallResult returned error: %v", err)
	}
	if result.Status != HarnessCallCompleted || result.AgentRun.AgentRunID == "" {
		t.Fatalf("harness result = %#v, want completed run", result)
	}
	if result.FinalResponse != nil {
		t.Fatalf("final response = %#v, want nil seed for fake executor", result.FinalResponse)
	}
	if result.Telemetry.Model != "unknown" || result.Telemetry.InputTokens != nil || result.Telemetry.OutputTokens != nil || result.Telemetry.MeasuredTokenTelemetry {
		t.Fatalf("telemetry = %#v, want explicit unknown token telemetry", result.Telemetry)
	}
	if got := strings.Join(result.AgentRun.Capabilities, ","); got != "agent.run.from_context,context_packet,timeline_items" {
		t.Fatalf("capabilities = %q, want normalized descriptor capabilities", got)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != "output.delta" || !strings.Contains(string(result.Events[0].Payload), "done") {
		t.Fatalf("events = %#v, want output payload containing timeline text", result.Events)
	}
	if result.SessionRef.AriSessionID != result.AgentRun.AgentRunID {
		t.Fatalf("session ref = %#v, want Ari run id %q", result.SessionRef, result.AgentRun.AgentRunID)
	}
	if err := result.SessionRef.Validate(); err != nil {
		t.Fatalf("session ref = %#v, validate error = %v", result.SessionRef, err)
	}
}

func TestStartHarnessCallResultExtractsFinalResponseFromCapableHarness(t *testing.T) {
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	executor := newFinalResponseHarness("fake", []TimelineItem{{ID: "ti_full_transcript", Kind: "agent_text", Text: "Detailed internal transcript"}, {ID: "ti_final", Kind: "agent_text", Text: "Concise answer"}})
	call, err := NewAgentRunHarnessCall(packet, nil)
	if err != nil {
		t.Fatalf("NewAgentRunHarnessCall returned error: %v", err)
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

func (e *spyExecutor) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{Name: "spy", Capabilities: append([]HarnessCapability(nil), e.capabilities...)}
}

func (e *spyExecutor) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	_ = ctx
	_ = req
	e.started = true
	return ExecutorRun{RunID: "spy-run", Executor: "spy", ProviderRunID: "spy-run", CapabilityNames: []string{"agent.run.from_context"}}, nil
}

func (e *spyExecutor) Items(ctx context.Context, runID string) ([]TimelineItem, error) {
	_ = ctx
	_ = runID
	return nil, nil
}

func (e *spyExecutor) Stop(ctx context.Context, runID string) error {
	_ = ctx
	_ = runID
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
	capabilities := []HarnessCapability{HarnessCapabilityAgentRunFromContext, HarnessCapabilityContextPacket, HarnessCapabilityTimelineItems}
	if e.finalResponse {
		capabilities = append(capabilities, HarnessCapabilityFinalResponse)
	}
	return HarnessAdapterDescriptor{Name: e.name, Capabilities: capabilities}
}

func (e *fakeHarness) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	if ctx == nil {
		return ExecutorRun{}, fmt.Errorf("context is required")
	}
	runID := fmt.Sprintf("%s-run-%d", e.name, time.Now().UnixNano())
	e.lastContextPacket = req.ContextPacket
	items := append([]TimelineItem(nil), e.template...)
	for i := range items {
		items[i].RunID = runID
		items[i].WorkspaceID = req.WorkspaceID
		items[i].SourceKind = "executor"
		if strings.TrimSpace(items[i].SourceID) == "" {
			items[i].SourceID = runID
		}
		if strings.TrimSpace(items[i].ID) == "" {
			items[i].ID = fmt.Sprintf("%s:item-%d", runID, i+1)
		}
		if items[i].Sequence == 0 {
			items[i].Sequence = i + 1
		}
		if strings.TrimSpace(items[i].Status) == "" {
			items[i].Status = "completed"
		}
	}
	e.mu.Lock()
	e.runs[runID] = items
	e.mu.Unlock()
	return ExecutorRun{RunID: runID, Executor: e.name, ProviderRunID: runID, CapabilityNames: []string{string(HarnessCapabilityTimelineItems)}}, nil
}

func (e *fakeHarness) Items(ctx context.Context, runID string) ([]TimelineItem, error) {
	_ = ctx
	e.mu.Lock()
	items, ok := e.runs[strings.TrimSpace(runID)]
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("run %q not found", runID)
	}
	return append([]TimelineItem(nil), items...), nil
}

func (e *fakeHarness) Stop(ctx context.Context, runID string) error {
	_ = ctx
	_ = runID
	return nil
}

type capturingHarness struct {
	name     string
	captured *ExecutorStartRequest
}

func (e *capturingHarness) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{Name: e.name, Capabilities: []HarnessCapability{HarnessCapabilityAgentRunFromContext, HarnessCapabilityContextPacket, HarnessCapabilityTimelineItems}}
}

func (e *capturingHarness) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	_ = ctx
	*e.captured = req
	runID := fmt.Sprintf("%s-run-%d", e.name, time.Now().UnixNano())
	return ExecutorRun{RunID: runID, Executor: e.name, ProviderRunID: runID}, nil
}

func (e *capturingHarness) Items(ctx context.Context, runID string) ([]TimelineItem, error) {
	_ = ctx
	return []TimelineItem{{ID: runID + ":item-1", RunID: runID, Kind: "agent_text", Status: "completed", Sequence: 1}}, nil
}

func (e *capturingHarness) Stop(ctx context.Context, runID string) error {
	_ = ctx
	_ = runID
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

func TestAgentRunMethodRejectsFakeExecutor(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	err := callMethodError(registry, "agent.run", AgentRunStartRequest{
		Executor: "fake",
		Packet:   packet,
	})
	if err == nil || !strings.Contains(err.Error(), "harness is not available") {
		t.Fatalf("agent.run fake error = %v, want unavailable executor", err)
	}
}

func TestAgentRunMethodUsesInjectedHarnessFactory(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return newFakeHarness(req.Executor, []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentRunStartResponse](t, registry, "agent.run", AgentRunStartRequest{Executor: "test-harness", Packet: packet})
	if resp.Run.Executor != "test-harness" || resp.Run.ContextPacketID != "ctx_123" {
		t.Fatalf("agent run = %#v, want injected harness run linked to context packet", resp.Run)
	}
}

func TestAgentProfileRunUsesProfileHarness(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return newFakeHarness(req.Executor, []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	d.setAgentProfileForTest(AgentProfile{Name: "executor", Harness: "test-harness", Model: "test-model", Prompt: "test-prompt", InvocationClass: HarnessInvocationAgent})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentProfileRunResponse](t, registry, "agent.profile.run", AgentProfileRunRequest{Profile: "executor", Packet: packet})
	if resp.Profile != "executor" || resp.Harness != "test-harness" || resp.Run.Executor != "test-harness" {
		t.Fatalf("profile run response = %#v, want executor routed to test-harness", resp)
	}
	if resp.Run.ContextPacketID != "ctx_123" || len(resp.Items) != 1 || resp.Items[0].RunID != resp.Run.AgentRunID {
		t.Fatalf("profile run items = %#v run = %#v, want linked context/timeline", resp.Items, resp.Run)
	}
}

func TestAgentProfileRunUsesStoredProfile(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
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
	resp := callMethod[AgentProfileRunResponse](t, registry, "agent.profile.run", AgentProfileRunRequest{Profile: "executor", Packet: packet})
	if resp.Profile != "executor" || resp.Harness != "test-harness" || resp.Run.Executor != "test-harness" {
		t.Fatalf("profile run response = %#v, want stored profile routed to test-harness", resp)
	}
}

func TestAgentProfileRunPersistsFinalResponseArtifact(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFinalResponseHarness("test-harness", []TimelineItem{{ID: "ti_transcript", Kind: "agent_text", Text: "Internal transcript text"}, {ID: "ti_final", Kind: "agent_text", Text: "Shareable answer"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "ap_executor", Name: "executor", Harness: "test-harness", InvocationClass: string(HarnessInvocationAgent)}); err != nil {
		t.Fatalf("UpsertAgentProfile returned error: %v", err)
	}

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	runResp := callMethod[AgentProfileRunResponse](t, registry, "agent.profile.run", AgentProfileRunRequest{Profile: "executor", Packet: packet})
	finalResp := callMethod[FinalResponseResponse](t, registry, "final_response.get", FinalResponseGetRequest{RunID: runResp.Run.AgentRunID})
	if finalResp.ProfileID != "ap_executor" || finalResp.ContextPacketID != "ctx_123" || finalResp.Text != "Shareable answer" {
		t.Fatalf("final response = %#v, want stored shareable artifact", finalResp)
	}
	if strings.Contains(finalResp.Text, "Internal transcript") {
		t.Fatalf("final response text = %q, must not include transcript text", finalResp.Text)
	}
	if len(finalResp.EvidenceLinks) < 2 || finalResp.EvidenceLinks[0].Kind != "context_packet" {
		t.Fatalf("evidence links = %#v, want context/run provenance", finalResp.EvidenceLinks)
	}
}

func TestAgentProfileRunPersistsMeasuredTelemetryAndProcessSample(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("test-harness", []TimelineItem{{Kind: "telemetry", Metadata: map[string]any{"input_tokens": "12", "output_tokens": "5"}}}), nil
	})
	originalSampler := agentRunProcessMetricsSampler
	pid := int64(12345)
	agentRunProcessMetricsSampler = func(context.Context, AgentRun) ProcessMetricsSample {
		return ProcessMetricsSample{OwnedByAri: true, PID: ProcessMetricValue{Known: true, Value: &pid, Confidence: "sampled"}, CPUTimeMS: unknownProcessMetric("unsupported"), MemoryRSSBytesPeak: unknownProcessMetric("unsupported"), ChildProcessesPeak: unknownProcessMetric("unsupported"), OrphanState: "not_orphaned", ExitCode: unknownProcessMetric("unknown")}
	}
	t.Cleanup(func() { agentRunProcessMetricsSampler = originalSampler })
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	if err := store.UpsertAgentProfile(context.Background(), globaldb.AgentProfile{ProfileID: "ap_executor", Name: "executor", Harness: "test-harness", Model: "model-1", InvocationClass: string(HarnessInvocationAgent)}); err != nil {
		t.Fatalf("UpsertAgentProfile returned error: %v", err)
	}

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	_ = callMethod[AgentProfileRunResponse](t, registry, "agent.profile.run", AgentProfileRunRequest{Profile: "executor", Packet: packet})
	rollups, err := store.RollupAgentRunTelemetry(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("RollupAgentRunTelemetry returned error: %v", err)
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

	created := callMethod[AgentProfileResponse](t, registry, "agent.profile.create", AgentProfileCreateRequest{Name: "executor", Harness: "codex", Model: "gpt-5.1-codex", Prompt: "Do work", InvocationClass: HarnessInvocationAgent, Defaults: map[string]any{"effort": "high"}})
	if created.ProfileID == "" || created.Name != "executor" || created.Harness != "codex" || created.Defaults["effort"] != "high" {
		t.Fatalf("created profile = %#v, want durable profile response", created)
	}
	got := callMethod[AgentProfileResponse](t, registry, "agent.profile.get", AgentProfileGetRequest{Name: "executor"})
	if got.ProfileID != created.ProfileID || got.Model != "gpt-5.1-codex" || got.Prompt != "Do work" || got.InvocationClass != HarnessInvocationAgent {
		t.Fatalf("got profile = %#v, want created profile %#v", got, created)
	}
	listed := callMethod[AgentProfileListResponse](t, registry, "agent.profile.list", AgentProfileListRequest{})
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

	err := callMethodError(registry, "agent.profile.create", AgentProfileCreateRequest{Harness: "codex"})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "missing_profile_name" {
		t.Fatalf("error data = %#v, want missing profile name", data)
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

	created := callMethod[AgentProfileResponse](t, registry, "agent.profile.helper.ensure", DefaultHelperEnsureRequest{WorkspaceID: "ws-1", Harness: "codex", Prompt: "Help here"})
	if created.Name != "helper" || created.WorkspaceID != "ws-1" || created.Harness != "codex" || created.Prompt != "Help here" {
		t.Fatalf("created helper = %#v", created)
	}
	got := callMethod[AgentProfileResponse](t, registry, "agent.profile.helper.get", DefaultHelperGetRequest{WorkspaceID: "ws-1"})
	if got.ProfileID != created.ProfileID {
		t.Fatalf("got helper = %#v, want %#v", got, created)
	}

	err := callMethodError(registry, "agent.profile.helper.get", DefaultHelperGetRequest{WorkspaceID: "ws-2"})
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

	err := callMethodError(registry, "agent.profile.helper.ensure", DefaultHelperEnsureRequest{WorkspaceID: "missing", Harness: "codex"})
	if err == nil {
		t.Fatal("agent.profile.helper.ensure returned nil error for unknown workspace")
	}
}

func TestAgentProfileResponsesDoNotExposeRoleClassification(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	created := callMethod[AgentProfileResponse](t, registry, "agent.profile.create", AgentProfileCreateRequest{Name: "helper", Harness: "codex"})
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
	d.setHarnessFactoryForTest("test-harness", func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return newFakeHarness(req.Executor, []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentProfileRunResponse](t, registry, "agent.profile.run", AgentProfileRunRequest{ProfileDefinition: &AgentProfile{Name: "dynamic", Harness: "test-harness"}, Packet: packet})
	if resp.Profile != "dynamic" || resp.Harness != "test-harness" || resp.Run.Executor != "test-harness" {
		t.Fatalf("profile run response = %#v, want inline dynamic profile routed to test-harness", resp)
	}
}

func TestAgentProfileRunUsesDefaultsOnlyHarness(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return newFakeHarness(req.Executor, []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentProfileRunResponse](t, registry, "agent.profile.run", AgentProfileRunRequest{Defaults: AgentRunDefaults{Harness: "test-harness", Model: "default-model", Prompt: "default-prompt"}, Packet: packet})
	if resp.Profile != "" || resp.Harness != "test-harness" || resp.Run.Executor != "test-harness" {
		t.Fatalf("profile run response = %#v, want defaults-only harness route", resp)
	}
}

func TestAgentProfileRunDefaultsFillPartialProfile(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return newFakeHarness(req.Executor, []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentProfileRunResponse](t, registry, "agent.profile.run", AgentProfileRunRequest{ProfileDefinition: &AgentProfile{Name: "dynamic"}, Defaults: AgentRunDefaults{Harness: "test-harness"}, Packet: packet})
	if resp.Profile != "dynamic" || resp.Harness != "test-harness" || resp.Run.Executor != "test-harness" {
		t.Fatalf("profile run response = %#v, want defaults filling partial profile", resp)
	}
}

func TestAgentProfileRunPassesProfileMetadataToHarness(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	var captured ExecutorStartRequest
	d.setHarnessFactoryForTest("test-harness", func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return &capturingHarness{name: req.Executor, captured: &captured}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	_ = callMethod[AgentProfileRunResponse](t, registry, "agent.profile.run", AgentProfileRunRequest{ProfileDefinition: &AgentProfile{Name: "dynamic", Harness: "test-harness", Model: "explicit-model"}, Defaults: AgentRunDefaults{Model: "default-model", Prompt: "default-prompt"}, Packet: packet})
	if captured.SourceProfileID != "dynamic" || captured.Model != "explicit-model" || captured.Prompt != "default-prompt" || captured.InvocationClass != HarnessInvocationAgent {
		t.Fatalf("captured request = %#v, want profile/default metadata at harness boundary", captured)
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

	err := callMethodError(registry, "agent.profile.run", AgentProfileRunRequest{ProfileDefinition: &AgentProfile{Name: "dynamic"}, Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws-1", TaskID: "task"}})
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

	err := callMethodError(registry, "agent.profile.run", AgentProfileRunRequest{Profile: "stored", ProfileDefinition: &AgentProfile{Name: "other"}, Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws", TaskID: "task"}})
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

	err := callMethodError(registry, "agent.profile.run", AgentProfileRunRequest{Profile: "stored", ProfileDefinition: &AgentProfile{Name: "stored", Harness: "other"}, Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws", TaskID: "task"}})
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

	err := callMethodError(registry, "agent.profile.run", AgentProfileRunRequest{Profile: "missing", Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws", TaskID: "task"}})
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

	err := callMethodError(registry, "agent.profile.run", AgentProfileRunRequest{Profile: "executor", Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws-1", TaskID: "task"}})
	data := requireHandlerErrorData(t, err)
	if data["harness"] != "missing-harness" || data["reason"] != "unknown_harness" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown harness data", data)
	}
}

func TestAgentProfileRunReturnsUnavailableHarnessData(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("missing-binary", func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return nil, &HarnessUnavailableError{Harness: "missing-binary", Reason: "missing_executable", Executable: "missing-binary", Probe: "missing-binary --version", RequiredCapability: HarnessCapabilityAgentRunFromContext, StartInvoked: false}
	})
	d.setAgentProfileForTest(AgentProfile{Name: "executor", Harness: "missing-binary", InvocationClass: HarnessInvocationAgent})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	err := callMethodError(registry, "agent.profile.run", AgentProfileRunRequest{Profile: "executor", Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws-1", TaskID: "task"}})
	data := requireHandlerErrorData(t, err)
	if data["harness"] != "missing-binary" || data["reason"] != "missing_executable" || data["executable"] != "missing-binary" || data["probe"] != "missing-binary --version" || data["required_capability"] != string(HarnessCapabilityAgentRunFromContext) || data["start_invoked"] != false {
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

	err := callMethodError(registry, "agent.profile.run", AgentProfileRunRequest{Profile: "plan", Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws-1", TaskID: "task"}})
	data := requireHandlerErrorData(t, err)
	if data["profile"] != "plan" || data["reason"] != "unknown_profile" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want no default profile data", data)
	}
}

func TestAgentRunReturnsUnsupportedCapabilitiesData(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("limited", func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &spyExecutor{capabilities: []HarnessCapability{HarnessCapabilityContextPacket}}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	err := callMethodError(registry, "agent.run", AgentRunStartRequest{Executor: "limited", Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws-1", TaskID: "task"}})
	data := requireHandlerErrorData(t, err)
	capabilities, ok := data["unsupported_capabilities"].([]string)
	wantCapabilities := string(HarnessCapabilityAgentRunFromContext) + "," + string(HarnessCapabilityTimelineItems)
	if !ok || strings.Join(capabilities, ",") != wantCapabilities || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unsupported capabilities data", data)
	}
}

func TestAgentRunReturnsInvalidParamsForMissingPacketID(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = primaryFolder
		_ = sink
		return newFakeHarness(req.Executor, []TimelineItem{{Kind: "agent_text", Text: "done"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	err := callMethodError(registry, "agent.run", AgentRunStartRequest{Executor: "test-harness", Packet: ContextPacket{WorkspaceID: "ws-1", TaskID: "task"}})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams HandlerError", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["field"] != "packet.id" || data["reason"] != "invalid_harness_call" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want packet id validation data", data)
	}
}

func TestAgentRunReturnsInvalidParamsForMissingPTYCommand(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	err := callMethodError(registry, "agent.run", AgentRunStartRequest{Executor: HarnessNamePTY, Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws-1", TaskID: "task"}})
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
	d.setHarnessFactoryForTest("limited", func(req AgentRunStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
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

	err := callMethodError(registry, "agent.profile.run", AgentProfileRunRequest{Profile: "executor", Packet: ContextPacket{ID: "ctx", WorkspaceID: "ws-1", TaskID: "task"}})
	data := requireHandlerErrorData(t, err)
	capabilities, ok := data["unsupported_capabilities"].([]string)
	wantCapabilities := string(HarnessCapabilityAgentRunFromContext) + "," + string(HarnessCapabilityTimelineItems)
	if !ok || strings.Join(capabilities, ",") != wantCapabilities || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want profile unsupported capabilities data", data)
	}
}

func TestAgentRunMethodStartsPTYExecutorFromContextPacket(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	start := time.Now()
	resp := callMethod[AgentRunStartResponse](t, registry, "agent.run", AgentRunStartRequest{
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
			if item.RunID == resp.Run.AgentRunID && item.Kind == "terminal_output" && item.Text == "done" {
				activity := callMethod[WorkspaceActivityResponse](t, registry, "workspace.activity", WorkspaceActivityRequest{WorkspaceID: "ws-1"})
				if len(activity.Agents) != 1 || activity.Agents[0].Status != "completed" {
					t.Fatalf("activity agents = %#v, want completed pty run after output", activity.Agents)
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("workspace.timeline did not persist pty output for run %s", resp.Run.AgentRunID)
}

func TestRecordExecutorRunPreservesBufferedSinkItems(t *testing.T) {
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.appendExecutorItems("run-1", []TimelineItem{{ID: "run-1:output", WorkspaceID: "ws-1", RunID: "run-1", SourceKind: "executor", SourceID: "run-1", Kind: "terminal_output", Status: "completed", Text: "done"}})

	d.recordExecutorRun(AgentRun{AgentRunID: "run-1", WorkspaceID: "ws-1", Status: "running", Executor: "pty"}, []TimelineItem{{ID: "run-1:lifecycle", WorkspaceID: "ws-1", RunID: "run-1", SourceKind: "executor", SourceID: "run-1", Kind: "lifecycle", Status: "running", Text: "pty"}})

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

func TestAgentRunMethodMarksPTYFailureFromExitCode(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())

	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}
	resp := callMethod[AgentRunStartResponse](t, registry, "agent.run", AgentRunStartRequest{
		Executor: "pty",
		Packet:   packet,
		Command:  "/bin/sh",
		Args:     []string{"-c", "printf failed; exit 7"},
	})
	deadline := time.Now().Add(boundedTestTimeout(t, 5*time.Second))
	for time.Now().Before(deadline) {
		activity := callMethod[WorkspaceActivityResponse](t, registry, "workspace.activity", WorkspaceActivityRequest{WorkspaceID: "ws-1"})
		if len(activity.Agents) == 1 && activity.Agents[0].ID == resp.Run.AgentRunID && activity.Agents[0].Status == "failed" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("workspace.activity did not mark failed pty run %s as failed", resp.Run.AgentRunID)
}
