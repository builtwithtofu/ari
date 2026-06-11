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
	executor := &recordingHarnessDeliveryExecutor{result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}}
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
	executor := &recordingHarnessDeliveryExecutor{result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}}
	if err := d.harnessRegistry.Register("sticky-delivery", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
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
}

type recordingHarnessDeliveryExecutor struct {
	mu       sync.Mutex
	result   WorkspaceDeliveryAttemptResult
	attempts []WorkspaceDeliveryAttempt
}

func (e *recordingHarnessDeliveryExecutor) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{Name: "delivery-test", Capabilities: []HarnessCapability{HarnessCapabilityHarnessSessionFromContext, HarnessCapabilityContextPacket, HarnessCapabilityTimelineItems}, DeliveryCapabilities: []HarnessDeliveryCapability{HarnessDeliveryVisiblePromptTurn}}
}

func (e *recordingHarnessDeliveryExecutor) Start(_ context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	return ExecutorRun{SessionID: req.SessionID, Executor: "delivery-test", ProviderSessionID: req.SessionID, CapabilityNames: []string{string(HarnessCapabilityTimelineItems)}}, nil
}

func (e *recordingHarnessDeliveryExecutor) Items(context.Context, string) ([]TimelineItem, error) {
	return nil, nil
}

func (e *recordingHarnessDeliveryExecutor) Stop(context.Context, string) error {
	return nil
}

func (e *recordingHarnessDeliveryExecutor) AttemptWorkspaceDelivery(_ context.Context, attempt WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error) {
	e.mu.Lock()
	e.attempts = append(e.attempts, attempt)
	e.mu.Unlock()
	return e.result, nil
}

func (e *recordingHarnessDeliveryExecutor) Attempts() []WorkspaceDeliveryAttempt {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]WorkspaceDeliveryAttempt(nil), e.attempts...)
}
