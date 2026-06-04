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

type recordingHarnessDeliveryExecutor struct {
	mu       sync.Mutex
	result   WorkspaceDeliveryAttemptResult
	attempts []WorkspaceDeliveryAttempt
}

func (e *recordingHarnessDeliveryExecutor) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{Name: "delivery-test", DeliveryCapabilities: []HarnessDeliveryCapability{HarnessDeliveryVisiblePromptTurn}}
}

func (e *recordingHarnessDeliveryExecutor) Start(context.Context, ExecutorStartRequest) (ExecutorRun, error) {
	return ExecutorRun{}, nil
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
