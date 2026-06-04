package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type workspaceDeliveryExecutor interface {
	AttemptWorkspaceDelivery(context.Context, WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error)
}

type harnessWorkspaceDeliveryDispatcher struct {
	daemon *Daemon
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

func newHarnessWorkspaceDeliveryDispatcher(d *Daemon) *harnessWorkspaceDeliveryDispatcher {
	return &harnessWorkspaceDeliveryDispatcher{daemon: d}
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
	target, ok := d.daemon.activeHarnessDeliveryTarget(targetSessionID)
	if !ok {
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
