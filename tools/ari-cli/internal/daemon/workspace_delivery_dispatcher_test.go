package daemon

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

func TestHarnessWorkspaceDeliveryDispatcherRoutesActiveHarnessSession(t *testing.T) {
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	executor := &recordingHarnessDeliveryExecutor{result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}, items: []TimelineItem{{ID: "sticky-delivery:completed", Kind: "lifecycle", Status: "completed"}}}
	unregister := d.registerActiveHarnessRun("ws-delivery", "ari-session", "provider-session", executor, func() {})
	t.Cleanup(unregister)
	dispatcher := newHarnessWorkspaceDeliveryDispatcher(d)
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)

	result, err := dispatcher.AttemptWorkspaceDelivery(context.Background(), WorkspaceDeliveryAttempt{Delivery: globaldb.PendingDelivery{DeliveryID: "pd-active", WorkspaceID: "ws-delivery", SubscriptionID: "sub-active", TargetType: "harness_session", TargetID: "ari-session", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn"}`, EventIDs: []string{"we-active"}, Status: "attempted", Attempts: 1}, Now: now})
	if err != nil {
		t.Fatalf("AttemptWorkspaceDelivery returned error: %v", err)
	}
	if result.Status != WorkspaceDeliveryAttemptCompleted {
		t.Fatalf("AttemptWorkspaceDelivery status = %s, want completed", result.Status)
	}

	attempts := executor.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("executor attempts = %#v, want one attempt", attempts)
	}
	got := attempts[0]
	if got.Now != now {
		t.Fatalf("executor attempt Now = %v, want %v", got.Now, now)
	}
	if got.Delivery.TargetID != "provider-session" {
		t.Fatalf("executor delivery target = %q, want provider session id", got.Delivery.TargetID)
	}
	if got.Delivery.DeliveryID != "pd-active" || got.Delivery.WorkspaceID != "ws-delivery" || got.Delivery.SubscriptionID != "sub-active" || got.Delivery.EventIDs[0] != "we-active" {
		t.Fatalf("executor delivery = %#v, want original Ari delivery fields with provider target", got.Delivery)
	}
}

func TestStartHarnessSessionRegistersStickyDeliveryTarget(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.AddFolder(ctx, "ws-1", t.TempDir(), "git", true); err != nil {
		t.Fatalf("AddFolder returned error: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	executor := &recordingHarnessDeliveryExecutor{result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}, items: []TimelineItem{{ID: "sticky-delivery:completed", Kind: "lifecycle", Status: "completed"}}}
	if err := d.harnessRegistry.Register("sticky-delivery", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		return executor, nil
	}); err != nil {
		t.Fatalf("Register sticky-delivery returned error: %v", err)
	}

	started, err := d.startHarnessSession(ctx, store, HarnessSessionStartRequest{Executor: "sticky-delivery", Packet: ContextPacket{ID: "ctx-sticky-delivery", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:sticky"}}, Profile{Name: "sticky-delivery", Harness: "sticky-delivery", InvocationClass: HarnessInvocationSticky})
	if err != nil {
		t.Fatalf("startHarnessSession returned error: %v", err)
	}
	dispatcher := newHarnessWorkspaceDeliveryDispatcher(d)
	result, err := dispatcher.AttemptWorkspaceDelivery(ctx, WorkspaceDeliveryAttempt{Delivery: globaldb.PendingDelivery{DeliveryID: "pd-sticky", WorkspaceID: "ws-1", SubscriptionID: "sub-sticky", TargetType: "harness_session", TargetID: started.Run.SessionID, DeliveryPolicyJSON: `{"channel":"visible_prompt_turn"}`, EventIDs: []string{"we-sticky"}, Status: "attempted", Attempts: 1}, Now: time.Date(2026, 6, 11, 18, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("AttemptWorkspaceDelivery returned error: %v", err)
	}
	if result.Status != WorkspaceDeliveryAttemptCompleted {
		t.Fatalf("AttemptWorkspaceDelivery status = %s error=%q, want completed", result.Status, result.LastError)
	}
	d.stopActiveHarnessesForWorkspace(ctx, store, "ws-1")
	if executor.StopCount() != 0 {
		t.Fatalf("executor stop count = %d, want completed delivery target not stopped on suspend", executor.StopCount())
	}
}

func TestHarnessWorkspaceDeliveryDispatcherRehydratesPersistedStickyTarget(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-delivery", "ws-delivery", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-delivery", Name: "agent-1", Harness: "sticky-delivery"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig returned error: %v", err)
	}
	if err := store.CreateHarnessSession(ctx, globaldb.HarnessSession{SessionID: "ari-session", WorkspaceID: "ws-delivery", AgentID: "agent-1", Harness: "sticky-delivery", Status: "completed", Usage: globaldb.HarnessSessionUsageSticky, ProviderSessionID: "provider-session", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateHarnessSession returned error: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	executor := &recordingHarnessDeliveryExecutor{result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}}
	d.setHarnessFactoryForTest("sticky-delivery", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		if req.SessionID != "ari-session" || req.WorkspaceID != "ws-delivery" {
			t.Fatalf("rehydrate request = %#v, want persisted session identity", req)
		}
		return executor, nil
	})
	dispatcher := newHarnessWorkspaceDeliveryDispatcher(d, store)

	result, err := dispatcher.AttemptWorkspaceDelivery(ctx, WorkspaceDeliveryAttempt{Delivery: globaldb.PendingDelivery{DeliveryID: "pd-rehydrate", WorkspaceID: "ws-delivery", SubscriptionID: "sub-rehydrate", TargetType: "harness_session", TargetID: "ari-session", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn"}`, EventIDs: []string{"we-rehydrate"}, Status: "attempted", Attempts: 1}, Now: time.Date(2026, 6, 11, 21, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("AttemptWorkspaceDelivery returned error: %v", err)
	}
	if result.Status != WorkspaceDeliveryAttemptCompleted {
		t.Fatalf("AttemptWorkspaceDelivery status = %s error=%q, want completed", result.Status, result.LastError)
	}
	attempts := executor.Attempts()
	if len(attempts) != 1 || attempts[0].Delivery.TargetID != "provider-session" {
		t.Fatalf("executor attempts = %#v, want rehydrated provider target", attempts)
	}
}

func TestHarnessWorkspaceDeliveryDispatcherRehydratesAuthProjection(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-delivery", "ws-delivery", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-delivery", Name: "agent-1", Harness: HarnessNameOpenCode}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig returned error: %v", err)
	}
	backend := globaldb.NewMemorySecretBackend()
	seedOpenCodeProjectionSecret(t, ctx, store, backend, "opencode-work", "ws-delivery", []byte(`{"provider":"anthropic","apiKey":"ari-secret"}`))
	if err := store.CreateHarnessSession(ctx, globaldb.HarnessSession{SessionID: "ari-session", WorkspaceID: "ws-delivery", AgentID: "agent-1", Harness: HarnessNameOpenCode, Status: "completed", Usage: globaldb.HarnessSessionUsageSticky, ProviderSessionID: "provider-session", CWD: t.TempDir(), ProviderMetadataJSON: `{"auth_slot_id":"opencode-work"}`}); err != nil {
		t.Fatalf("CreateHarnessSession returned error: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.secretBackend = backend
	d.setHarnessFactoryForTest(HarnessNameOpenCode, func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (HarnessAdapter, error) {
		if req.AuthProjection.Kind != HarnessAuthProjectionAuthContent || req.AuthProjection.Env["OPENCODE_AUTH_CONTENT"] == "" {
			t.Fatalf("rehydrate auth projection = %#v, want durable named-slot projection", req.AuthProjection)
		}
		return &recordingHarnessDeliveryExecutor{result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}}, nil
	})
	dispatcher := newHarnessWorkspaceDeliveryDispatcher(d, store)

	result, err := dispatcher.AttemptWorkspaceDelivery(ctx, WorkspaceDeliveryAttempt{Delivery: globaldb.PendingDelivery{DeliveryID: "pd-rehydrate", WorkspaceID: "ws-delivery", SubscriptionID: "sub-rehydrate", TargetType: "harness_session", TargetID: "ari-session", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn"}`, EventIDs: []string{"we-rehydrate"}, Status: "attempted", Attempts: 1}, Now: time.Date(2026, 6, 11, 21, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("AttemptWorkspaceDelivery returned error: %v", err)
	}
	if result.Status != WorkspaceDeliveryAttemptCompleted {
		t.Fatalf("AttemptWorkspaceDelivery result = %#v, want completed", result)
	}
}

func TestHarnessWorkspaceDeliveryDispatcherRetriesWhileWorkspaceSuspended(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	if err := store.CreateWorkspace(ctx, "ws-suspended", "ws-suspended", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	if err := store.UpdateWorkspaceStatus(ctx, "ws-suspended", "suspended"); err != nil {
		t.Fatalf("UpdateWorkspaceStatus returned error: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	executor := &recordingHarnessDeliveryExecutor{result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}}
	unregister := d.registerHarnessDeliveryTarget("ws-suspended", "ari-session", "provider-session", executor)
	t.Cleanup(unregister)
	dispatcher := newHarnessWorkspaceDeliveryDispatcher(d, store)

	result, err := dispatcher.AttemptWorkspaceDelivery(ctx, WorkspaceDeliveryAttempt{Delivery: globaldb.PendingDelivery{DeliveryID: "pd-suspended", WorkspaceID: "ws-suspended", SubscriptionID: "sub-suspended", TargetType: "harness_session", TargetID: "ari-session", DeliveryPolicyJSON: `{"channel":"visible_prompt_turn"}`, EventIDs: []string{"we-suspended"}, Status: "attempted", Attempts: 1}, Now: time.Date(2026, 6, 11, 21, 35, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("AttemptWorkspaceDelivery returned error: %v", err)
	}
	if result.Status != WorkspaceDeliveryAttemptRetry || result.LastError == "" {
		t.Fatalf("AttemptWorkspaceDelivery result = %#v, want retry while suspended", result)
	}
	if attempts := executor.Attempts(); len(attempts) != 0 {
		t.Fatalf("executor attempts = %#v, want no delivery while suspended", attempts)
	}
}

type recordingHarnessDeliveryExecutor struct {
	mu       sync.Mutex
	result   WorkspaceDeliveryAttemptResult
	items    []TimelineItem
	attempts []WorkspaceDeliveryAttempt
	stops    int
}

func (e *recordingHarnessDeliveryExecutor) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{Name: "delivery-test", Capabilities: []HarnessCapability{HarnessCapabilityHarnessSessionFromContext, HarnessCapabilityContextPacket, HarnessCapabilityTimelineItems}, DeliveryCapabilities: []HarnessDeliveryCapability{HarnessDeliveryVisiblePromptTurn}}
}

func (e *recordingHarnessDeliveryExecutor) Start(_ context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	return ExecutorRun{SessionID: req.SessionID, Executor: "delivery-test", ProviderSessionID: req.SessionID, CapabilityNames: []string{string(HarnessCapabilityTimelineItems)}}, nil
}

func (e *recordingHarnessDeliveryExecutor) Items(context.Context, string) ([]TimelineItem, error) {
	return append([]TimelineItem(nil), e.items...), nil
}

func (e *recordingHarnessDeliveryExecutor) Stop(context.Context, string) error {
	e.mu.Lock()
	e.stops++
	e.mu.Unlock()
	return nil
}

func (e *recordingHarnessDeliveryExecutor) AttemptWorkspaceDelivery(_ context.Context, attempt WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error) {
	e.mu.Lock()
	e.attempts = append(e.attempts, attempt)
	e.mu.Unlock()
	return e.result, nil
}

func (e *recordingHarnessDeliveryExecutor) AuthStatus(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	_ = ctx
	return unsupportedHarnessAuthStatus("delivery-test", slot), nil
}

func (e *recordingHarnessDeliveryExecutor) AuthStart(ctx context.Context, slot HarnessAuthSlot, method string) (HarnessAuthStatus, error) {
	_ = ctx
	_ = method
	return unsupportedHarnessAuthStatus("delivery-test", slot), &HarnessUnavailableError{Harness: "delivery-test", Reason: "auth_start_unsupported", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
}

func (e *recordingHarnessDeliveryExecutor) AuthCancel(ctx context.Context, slot HarnessAuthSlot, flowID string) (HarnessAuthStatus, error) {
	_ = ctx
	_ = flowID
	return unsupportedHarnessAuthStatus("delivery-test", slot), &HarnessUnavailableError{Harness: "delivery-test", Reason: "auth_cancel_unsupported", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
}

func (e *recordingHarnessDeliveryExecutor) AuthLogout(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	_ = ctx
	return unsupportedHarnessAuthStatus("delivery-test", slot), &HarnessUnavailableError{Harness: "delivery-test", Reason: "auth_logout_unsupported", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
}

func (e *recordingHarnessDeliveryExecutor) AuthProviderMethods(ctx context.Context) (HarnessAuthProviderMethodsResponse, error) {
	_ = ctx
	return HarnessAuthProviderMethodsResponse{Status: "unsupported"}, nil
}

func (e *recordingHarnessDeliveryExecutor) StopCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stops
}

func (e *recordingHarnessDeliveryExecutor) Attempts() []WorkspaceDeliveryAttempt {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]WorkspaceDeliveryAttempt(nil), e.attempts...)
}
