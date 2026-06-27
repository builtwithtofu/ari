package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

type DeliveryTargetResolver interface {
	ResolveDeliveryTarget(context.Context, string) (activeHarnessDeliveryTarget, bool, error)
}

type harnessWorkspaceDeliveryDispatcher struct {
	resolver DeliveryTargetResolver
	store    *globaldb.Store
}

type activeHarnessDeliveryTarget struct {
	workspaceID       string
	sessionID         string
	providerSessionID string
	executor          HarnessAdapter
}

type daemonDeliveryTargetResolver struct {
	daemon *Daemon
	store  *globaldb.Store
}

func newHarnessWorkspaceDeliveryDispatcher(d *Daemon, stores ...*globaldb.Store) *harnessWorkspaceDeliveryDispatcher {
	var store *globaldb.Store
	if len(stores) > 0 {
		store = stores[0]
	}
	return &harnessWorkspaceDeliveryDispatcher{resolver: daemonDeliveryTargetResolver{daemon: d, store: store}, store: store}
}

func (d *harnessWorkspaceDeliveryDispatcher) AttemptWorkspaceDelivery(ctx context.Context, attempt WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error) {
	if ctx == nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("context is required")
	}
	if d == nil || d.resolver == nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("delivery target resolver is required")
	}
	delivery := attempt.Delivery
	if strings.TrimSpace(delivery.TargetType) != globaldb.WorkspaceEventSubjectHarnessSession {
		return failedWorkspaceDeliveryAttempt("unsupported delivery target type %q", delivery.TargetType), nil
	}
	targetSessionID := strings.TrimSpace(delivery.TargetID)
	if targetSessionID == "" {
		return failedWorkspaceDeliveryAttempt("delivery target session is required"), nil
	}
	if d.workspaceIsSuspended(ctx, delivery.WorkspaceID) {
		return retryWorkspaceDeliveryAttempt("workspace %q is suspended", delivery.WorkspaceID), nil
	}
	if result, ok := d.deliveryTargetWorkspaceMismatch(ctx, delivery.WorkspaceID, targetSessionID); ok {
		return result, nil
	}
	target, ok, err := d.resolver.ResolveDeliveryTarget(ctx, targetSessionID)
	if !ok {
		if err != nil {
			return retryWorkspaceDeliveryAttempt("delivery target session %q could not be rehydrated: %v", targetSessionID, err), nil
		}
		return retryWorkspaceDeliveryAttempt("delivery target session %q is not active", targetSessionID), nil
	}
	if target.workspaceID != "" && strings.TrimSpace(delivery.WorkspaceID) != target.workspaceID {
		return failedWorkspaceDeliveryAttempt("delivery target session %q belongs to workspace %q, not %q", targetSessionID, target.workspaceID, delivery.WorkspaceID), nil
	}
	capability, err := workspaceDeliveryCapabilityFromPolicy(delivery.DeliveryPolicyJSON)
	if err != nil {
		return failedWorkspaceDeliveryAttempt("%s", err.Error()), nil
	}
	if !activeHarnessDeclaresDeliveryCapability(target.executor, capability) {
		return failedWorkspaceDeliveryAttempt("delivery target session %q does not declare %s delivery support", targetSessionID, capability), nil
	}
	if target.providerSessionID != "" {
		delivery.TargetID = target.providerSessionID
	} else {
		delivery.TargetID = target.sessionID
	}
	attempt.Delivery = delivery
	return target.executor.AttemptWorkspaceDelivery(ctx, attempt)
}

func (d *harnessWorkspaceDeliveryDispatcher) workspaceIsSuspended(ctx context.Context, workspaceID string) bool {
	if d == nil || d.store == nil {
		return false
	}
	workspace, err := d.store.GetWorkspace(ctx, strings.TrimSpace(workspaceID))
	return err == nil && workspace != nil && workspace.Status == "suspended"
}

func (d *harnessWorkspaceDeliveryDispatcher) deliveryTargetWorkspaceMismatch(ctx context.Context, workspaceID, sessionID string) (WorkspaceDeliveryAttemptResult, bool) {
	if d == nil || d.store == nil {
		return WorkspaceDeliveryAttemptResult{}, false
	}
	session, err := d.store.GetHarnessSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return WorkspaceDeliveryAttemptResult{}, false
		}
		return retryWorkspaceDeliveryAttempt("delivery target session %q could not be loaded: %v", sessionID, err), true
	}
	if strings.TrimSpace(session.WorkspaceID) != strings.TrimSpace(workspaceID) {
		return failedWorkspaceDeliveryAttempt("delivery target session %q belongs to workspace %q, not %q", sessionID, session.WorkspaceID, workspaceID), true
	}
	return WorkspaceDeliveryAttemptResult{}, false
}

func (r daemonDeliveryTargetResolver) ResolveDeliveryTarget(ctx context.Context, sessionID string) (activeHarnessDeliveryTarget, bool, error) {
	if r.daemon == nil {
		return activeHarnessDeliveryTarget{}, false, nil
	}
	if target, ok := r.daemon.activeHarnessDeliveryTarget(sessionID); ok {
		return target, true, nil
	}
	return r.rehydrateStickyDeliveryTarget(ctx, sessionID)
}

func (r daemonDeliveryTargetResolver) rehydrateStickyDeliveryTarget(ctx context.Context, sessionID string) (activeHarnessDeliveryTarget, bool, error) {
	if r.daemon == nil || r.store == nil {
		return activeHarnessDeliveryTarget{}, false, nil
	}
	session, err := r.store.GetHarnessSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return activeHarnessDeliveryTarget{}, false, nil
		}
		return activeHarnessDeliveryTarget{}, false, err
	}
	if session.Usage != globaldb.HarnessSessionUsageSticky || !stickySessionCanReceiveDelivery(session.Status) {
		return activeHarnessDeliveryTarget{}, false, nil
	}
	req := HarnessSessionStartRequest{Executor: session.Harness, SessionID: session.SessionID, WorkspaceID: session.WorkspaceID}
	if authSlotID := harnessSessionAuthSlotID(session); authSlotID != "" {
		projection, err := r.daemon.authProjectionForStart(ctx, r.store, session.Harness, session.WorkspaceID, authSlotID)
		if err != nil {
			return activeHarnessDeliveryTarget{}, false, err
		}
		req.AuthProjection = projection
	}
	executor, err := r.daemon.resolveHarness(ctx, r.store, req, session.CWD)
	if err != nil {
		return activeHarnessDeliveryTarget{}, false, err
	}
	providerSessionID := strings.TrimSpace(session.ProviderSessionID)
	if providerSessionID == "" {
		providerSessionID = strings.TrimSpace(session.ProviderThreadID)
	}
	if providerSessionID == "" {
		providerSessionID = strings.TrimSpace(session.SessionID)
	}
	r.daemon.registerHarnessDeliveryTarget(session.WorkspaceID, session.SessionID, providerSessionID, executor)
	target, ok := r.daemon.activeHarnessDeliveryTarget(session.SessionID)
	return target, ok, nil
}

func harnessSessionAuthSlotID(session globaldb.HarnessSession) string {
	var metadata struct {
		AuthSlotID string `json:"auth_slot_id"`
	}
	if err := json.Unmarshal([]byte(defaultString(strings.TrimSpace(session.ProviderMetadataJSON), "{}")), &metadata); err != nil {
		return ""
	}
	return strings.TrimSpace(metadata.AuthSlotID)
}

func stickySessionCanReceiveDelivery(status string) bool {
	switch strings.TrimSpace(status) {
	case "running", "waiting", "completed":
		return true
	default:
		return false
	}
}

func (d *Daemon) activeHarnessDeliveryTarget(sessionID string) (activeHarnessDeliveryTarget, bool) {
	if d == nil {
		return activeHarnessDeliveryTarget{}, false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return activeHarnessDeliveryTarget{}, false
	}
	d.activeHarnessMu.Lock()
	defer d.activeHarnessMu.Unlock()
	run := d.activeHarnesses[sessionID]
	if run == nil || run.executor == nil {
		return activeHarnessDeliveryTarget{}, false
	}
	providerSessionID := strings.TrimSpace(run.providerSessionID)
	if providerSessionID == "" {
		providerSessionID = strings.TrimSpace(run.sessionID)
	}
	return activeHarnessDeliveryTarget{workspaceID: strings.TrimSpace(run.workspaceID), sessionID: strings.TrimSpace(run.sessionID), providerSessionID: providerSessionID, executor: run.executor}, true
}

func activeHarnessDeclaresDeliveryCapability(executor HarnessAdapter, capability HarnessDeliveryCapability) bool {
	descriptor := executor.Descriptor()
	for _, available := range descriptor.DeliveryCapabilities {
		if available == capability {
			return true
		}
	}
	return false
}

func failedWorkspaceDeliveryAttempt(format string, args ...any) WorkspaceDeliveryAttemptResult {
	return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptFailed, LastError: fmt.Sprintf(format, args...)}
}

func retryWorkspaceDeliveryAttempt(format string, args ...any) WorkspaceDeliveryAttemptResult {
	return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: fmt.Sprintf(format, args...)}
}
