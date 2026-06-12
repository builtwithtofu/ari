package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

type workspaceDeliveryExecutor interface {
	AttemptWorkspaceDelivery(context.Context, WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error)
}

type harnessWorkspaceDeliveryDispatcher struct {
	daemon *Daemon
	store  *globaldb.Store
}

type activeHarnessDeliveryTarget struct {
	workspaceID       string
	sessionID         string
	providerSessionID string
	executor          Executor
}

type workspaceDeliveryPolicy struct {
	Channel     HarnessDeliveryCapability
	MaxAttempts int64
}

func newHarnessWorkspaceDeliveryDispatcher(d *Daemon, stores ...*globaldb.Store) *harnessWorkspaceDeliveryDispatcher {
	var store *globaldb.Store
	if len(stores) > 0 {
		store = stores[0]
	}
	return &harnessWorkspaceDeliveryDispatcher{daemon: d, store: store}
}

func (d *harnessWorkspaceDeliveryDispatcher) AttemptWorkspaceDelivery(ctx context.Context, attempt WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error) {
	if ctx == nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("context is required")
	}
	if d == nil || d.daemon == nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("daemon is required")
	}
	delivery := attempt.Delivery
	if strings.TrimSpace(delivery.TargetType) != "harness_session" {
		return failedWorkspaceDeliveryAttempt("unsupported delivery target type %q", delivery.TargetType), nil
	}
	targetSessionID := strings.TrimSpace(delivery.TargetID)
	if targetSessionID == "" {
		return failedWorkspaceDeliveryAttempt("delivery target session is required"), nil
	}
	if d.workspaceIsSuspended(ctx, delivery.WorkspaceID) {
		return retryWorkspaceDeliveryAttempt("workspace %q is suspended", delivery.WorkspaceID), nil
	}
	target, ok := d.daemon.activeHarnessDeliveryTarget(targetSessionID)
	if !ok {
		var err error
		target, ok, err = d.rehydrateStickyDeliveryTarget(ctx, targetSessionID)
		if err != nil {
			return retryWorkspaceDeliveryAttempt("delivery target session %q could not be rehydrated: %v", targetSessionID, err), nil
		}
		if !ok {
			return retryWorkspaceDeliveryAttempt("delivery target session %q is not active", targetSessionID), nil
		}
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
	executor, ok := target.executor.(workspaceDeliveryExecutor)
	if !ok {
		return failedWorkspaceDeliveryAttempt("delivery target session %q does not implement workspace delivery", targetSessionID), nil
	}
	if target.providerSessionID != "" {
		delivery.TargetID = target.providerSessionID
	} else {
		delivery.TargetID = target.sessionID
	}
	attempt.Delivery = delivery
	return executor.AttemptWorkspaceDelivery(ctx, attempt)
}

func (d *harnessWorkspaceDeliveryDispatcher) workspaceIsSuspended(ctx context.Context, workspaceID string) bool {
	if d == nil || d.store == nil {
		return false
	}
	workspace, err := d.store.GetWorkspace(ctx, strings.TrimSpace(workspaceID))
	return err == nil && workspace != nil && workspace.Status == "suspended"
}

func (d *harnessWorkspaceDeliveryDispatcher) rehydrateStickyDeliveryTarget(ctx context.Context, sessionID string) (activeHarnessDeliveryTarget, bool, error) {
	if d == nil || d.store == nil || d.daemon == nil {
		return activeHarnessDeliveryTarget{}, false, nil
	}
	session, err := d.store.GetHarnessSession(ctx, sessionID)
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
		projection, err := d.daemon.authProjectionForStart(ctx, d.store, session.Harness, session.WorkspaceID, authSlotID)
		if err != nil {
			return activeHarnessDeliveryTarget{}, false, err
		}
		req.AuthProjection = projection
	}
	executor, err := d.daemon.resolveHarness(req, session.CWD)
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
	d.daemon.registerHarnessDeliveryTarget(session.WorkspaceID, session.SessionID, providerSessionID, executor)
	target, ok := d.daemon.activeHarnessDeliveryTarget(session.SessionID)
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

func workspaceDeliveryCapabilityFromPolicy(raw string) (HarnessDeliveryCapability, error) {
	policy, err := parseWorkspaceDeliveryPolicy(raw)
	if err != nil {
		return "", err
	}
	return policy.Channel, nil
}

func parseWorkspaceDeliveryPolicy(raw string) (workspaceDeliveryPolicy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "{}"
	}
	var policy struct {
		Channel     string `json:"channel"`
		MaxAttempts int64  `json:"max_attempts"`
	}
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return workspaceDeliveryPolicy{}, fmt.Errorf("delivery policy json is invalid: %w", err)
	}
	if policy.MaxAttempts < 0 {
		return workspaceDeliveryPolicy{}, fmt.Errorf("delivery max_attempts must not be negative")
	}
	channel := strings.TrimSpace(policy.Channel)
	if channel == "" {
		channel = string(HarnessDeliveryVisiblePromptTurn)
	}
	switch HarnessDeliveryCapability(channel) {
	case HarnessDeliveryVisiblePromptTurn,
		HarnessDeliveryQueuedPromptTurn,
		HarnessDeliveryNativeResume,
		HarnessDeliveryHumanNotification,
		HarnessDeliveryMCPChannel:
		return workspaceDeliveryPolicy{Channel: HarnessDeliveryCapability(channel), MaxAttempts: policy.MaxAttempts}, nil
	case HarnessDeliveryUnsupported:
		return workspaceDeliveryPolicy{}, fmt.Errorf("delivery channel %q is unsupported", channel)
	default:
		return workspaceDeliveryPolicy{}, fmt.Errorf("delivery channel %q is not recognized", channel)
	}
}

func activeHarnessDeclaresDeliveryCapability(executor Executor, capability HarnessDeliveryCapability) bool {
	describer, ok := executor.(HarnessDescriber)
	if !ok {
		return false
	}
	descriptor := describer.Descriptor()
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
