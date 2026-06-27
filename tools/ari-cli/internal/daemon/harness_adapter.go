package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// HarnessAdapter is the complete in-process contract for an official Ari
// harness integration. The registry returns this type, so lifecycle,
// descriptor, auth, and workspace-delivery support are compile-time adapter
// responsibilities rather than daemon-side runtime type assertions.
type HarnessAdapter interface {
	Executor
	HarnessDescriber
	AttemptWorkspaceDelivery(context.Context, WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error)
	AuthStatus(context.Context, HarnessAuthSlot) (HarnessAuthStatus, error)
	AuthStart(context.Context, HarnessAuthSlot, string) (HarnessAuthStatus, error)
	AuthCancel(context.Context, HarnessAuthSlot, string) (HarnessAuthStatus, error)
	AuthLogout(context.Context, HarnessAuthSlot) (HarnessAuthStatus, error)
	AuthProviderMethods(context.Context) (HarnessAuthProviderMethodsResponse, error)
}

type adapterLifecycle[O any] struct {
	harness         string
	mu              sync.Mutex
	runs            map[string][]TimelineItem
	deliveryOptions map[string]O
}

func newAdapterLifecycle[O any](harness string) adapterLifecycle[O] {
	return adapterLifecycle[O]{harness: strings.TrimSpace(harness), runs: map[string][]TimelineItem{}, deliveryOptions: map[string]O{}}
}

func (l *adapterLifecycle[O]) storeRun(runID string, items []TimelineItem, options O) {
	if l.runs == nil {
		l.runs = map[string][]TimelineItem{}
	}
	if l.deliveryOptions == nil {
		l.deliveryOptions = map[string]O{}
	}
	runID = strings.TrimSpace(runID)
	l.mu.Lock()
	l.runs[runID] = append([]TimelineItem(nil), items...)
	l.deliveryOptions[runID] = options
	l.mu.Unlock()
}

func (l *adapterLifecycle[O]) appendRunItems(runID string, items ...TimelineItem) {
	if l.runs == nil {
		l.runs = map[string][]TimelineItem{}
	}
	runID = strings.TrimSpace(runID)
	l.mu.Lock()
	l.runs[runID] = append(l.runs[runID], items...)
	l.mu.Unlock()
}

func (l *adapterLifecycle[O]) items(runID string) ([]TimelineItem, error) {
	runID = strings.TrimSpace(runID)
	l.mu.Lock()
	items, ok := l.runs[runID]
	l.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("run %q not found", runID)
	}
	return append([]TimelineItem(nil), items...), nil
}

func (l *adapterLifecycle[O]) Items(ctx context.Context, runID string) ([]TimelineItem, error) {
	_ = ctx
	if l == nil {
		return nil, fmt.Errorf("executor is required")
	}
	return l.items(runID)
}

func (l *adapterLifecycle[O]) Stop(ctx context.Context, runID string) error {
	_ = ctx
	_ = runID
	return nil
}

func (l *adapterLifecycle[O]) deliveryOption(runID string) (O, bool) {
	runID = strings.TrimSpace(runID)
	l.mu.Lock()
	options, ok := l.deliveryOptions[runID]
	l.mu.Unlock()
	return options, ok
}

func (l *adapterLifecycle[O]) AuthStart(ctx context.Context, slot HarnessAuthSlot, method string) (HarnessAuthStatus, error) {
	_ = ctx
	_ = method
	return unsupportedHarnessAuthStatus(l.harness, slot), &HarnessUnavailableError{Harness: l.harness, Reason: "auth_start_unsupported", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
}

func (l *adapterLifecycle[O]) AuthCancel(ctx context.Context, slot HarnessAuthSlot, flowID string) (HarnessAuthStatus, error) {
	_ = ctx
	_ = flowID
	return unsupportedHarnessAuthStatus(l.harness, slot), &HarnessUnavailableError{Harness: l.harness, Reason: "auth_cancel_unsupported", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
}

func (l *adapterLifecycle[O]) AuthLogout(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	_ = ctx
	return unsupportedHarnessAuthStatus(l.harness, slot), &HarnessUnavailableError{Harness: l.harness, Reason: "auth_logout_unsupported", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
}

func (l *adapterLifecycle[O]) AuthProviderMethods(ctx context.Context) (HarnessAuthProviderMethodsResponse, error) {
	_ = ctx
	return HarnessAuthProviderMethodsResponse{Status: "unsupported"}, nil
}

func unsupportedHarnessAuthStatus(harness string, slot HarnessAuthSlot) HarnessAuthStatus {
	return HarnessAuthStatus{Harness: strings.TrimSpace(harness), AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthUnknown, AriSecretStorage: HarnessAriSecretStorageNone}
}
