package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

const (
	workspaceDeliveryBackoffLinear      = "linear"
	workspaceDeliveryBackoffFixed       = "fixed"
	workspaceDeliveryBackoffExponential = "exponential"
)

type workspaceDeliveryPolicy struct {
	Channel       HarnessDeliveryCapability
	MaxAttempts   int64
	BackoffMode   string
	BackoffBaseMS int64
	BackoffMaxMS  int64
}

type DeliveryPolicyEngine struct{}

func NewDeliveryPolicyEngine() DeliveryPolicyEngine { return DeliveryPolicyEngine{} }

func (e DeliveryPolicyEngine) FinishAttempt(ctx context.Context, store *globaldb.Store, delivery globaldb.PendingDelivery, result WorkspaceDeliveryAttemptResult, now time.Time) (WorkspaceDeliveryWorkerOutcome, error) {
	if store == nil {
		return WorkspaceDeliveryWorkerOutcome{}, fmt.Errorf("globaldb store is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	switch result.Status {
	case WorkspaceDeliveryAttemptCompleted:
		completed, err := store.CompletePendingDelivery(ctx, delivery.DeliveryID)
		return WorkspaceDeliveryWorkerOutcome{DeliveryID: completed.DeliveryID, Status: WorkspaceDeliveryWorkerOutcomeCompleted}, err
	case WorkspaceDeliveryAttemptFailed:
		failed, err := store.FailPendingDelivery(ctx, delivery.DeliveryID, result.LastError)
		return WorkspaceDeliveryWorkerOutcome{DeliveryID: failed.DeliveryID, Status: WorkspaceDeliveryWorkerOutcomeFailed, LastError: failed.LastError}, err
	case WorkspaceDeliveryAttemptRetry:
		return e.finishRetry(ctx, store, delivery, result, now)
	default:
		lastError := strings.TrimSpace(result.LastError)
		if lastError == "" {
			lastError = fmt.Sprintf("unsupported delivery attempt result %q", result.Status)
		}
		failed, err := store.FailPendingDelivery(ctx, delivery.DeliveryID, lastError)
		return WorkspaceDeliveryWorkerOutcome{DeliveryID: failed.DeliveryID, Status: WorkspaceDeliveryWorkerOutcomeFailed, LastError: failed.LastError}, err
	}
}

func (e DeliveryPolicyEngine) finishRetry(ctx context.Context, store *globaldb.Store, delivery globaldb.PendingDelivery, result WorkspaceDeliveryAttemptResult, now time.Time) (WorkspaceDeliveryWorkerOutcome, error) {
	policy, err := parseWorkspaceDeliveryPolicy(delivery.DeliveryPolicyJSON)
	if err != nil {
		failed, failErr := store.FailPendingDelivery(ctx, delivery.DeliveryID, err.Error())
		return WorkspaceDeliveryWorkerOutcome{DeliveryID: failed.DeliveryID, Status: WorkspaceDeliveryWorkerOutcomeFailed, LastError: failed.LastError}, failErr
	}
	if e.retryLimitReached(policy, delivery) {
		lastError := strings.TrimSpace(result.LastError)
		if lastError == "" {
			lastError = fmt.Sprintf("delivery retry limit reached after %d attempts", delivery.Attempts)
		}
		failed, failErr := store.FailPendingDelivery(ctx, delivery.DeliveryID, lastError)
		return WorkspaceDeliveryWorkerOutcome{DeliveryID: failed.DeliveryID, Status: WorkspaceDeliveryWorkerOutcomeFailed, LastError: failed.LastError}, failErr
	}
	nextAttemptAt := result.NextAttemptAt
	if nextAttemptAt == nil || nextAttemptAt.IsZero() {
		fallback := now.Add(e.RetryDelay(delivery, policy))
		nextAttemptAt = &fallback
	}
	retry, err := store.SchedulePendingDeliveryRetry(ctx, delivery.DeliveryID, *nextAttemptAt, result.LastError)
	return WorkspaceDeliveryWorkerOutcome{DeliveryID: retry.DeliveryID, Status: WorkspaceDeliveryWorkerOutcomeRetry, LastError: retry.LastError}, err
}

func (e DeliveryPolicyEngine) retryLimitReached(policy workspaceDeliveryPolicy, delivery globaldb.PendingDelivery) bool {
	return policy.MaxAttempts > 0 && delivery.Attempts >= policy.MaxAttempts
}

func (e DeliveryPolicyEngine) RetryDelay(delivery globaldb.PendingDelivery, policy workspaceDeliveryPolicy) time.Duration {
	attempt := delivery.Attempts
	if attempt < 1 {
		attempt = 1
	}
	base := time.Duration(policy.BackoffBaseMS) * time.Millisecond
	if base <= 0 {
		base = time.Second
	}
	maxDelay := time.Duration(policy.BackoffMaxMS) * time.Millisecond
	if maxDelay <= 0 {
		maxDelay = 6 * time.Second
	}
	var delay time.Duration
	switch policy.BackoffMode {
	case workspaceDeliveryBackoffFixed:
		delay = base
	case workspaceDeliveryBackoffExponential:
		delay = base
		for i := int64(1); i < attempt; i++ {
			if delay >= maxDelay/2 {
				delay = maxDelay
				break
			}
			delay *= 2
		}
	default:
		delay = time.Duration(attempt) * base
	}
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
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
		Channel       string `json:"channel"`
		MaxAttempts   int64  `json:"max_attempts"`
		BackoffMode   string `json:"backoff_mode"`
		BackoffBaseMS int64  `json:"backoff_base_ms"`
		BackoffMaxMS  int64  `json:"backoff_max_ms"`
	}
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return workspaceDeliveryPolicy{}, fmt.Errorf("delivery policy json is invalid: %w", err)
	}
	if policy.MaxAttempts < 0 {
		return workspaceDeliveryPolicy{}, fmt.Errorf("delivery max_attempts must not be negative")
	}
	if policy.BackoffBaseMS < 0 {
		return workspaceDeliveryPolicy{}, fmt.Errorf("delivery backoff_base_ms must not be negative")
	}
	if policy.BackoffMaxMS < 0 {
		return workspaceDeliveryPolicy{}, fmt.Errorf("delivery backoff_max_ms must not be negative")
	}
	backoffMode := strings.TrimSpace(policy.BackoffMode)
	if backoffMode == "" {
		backoffMode = workspaceDeliveryBackoffLinear
	}
	switch backoffMode {
	case workspaceDeliveryBackoffLinear, workspaceDeliveryBackoffFixed, workspaceDeliveryBackoffExponential:
	default:
		return workspaceDeliveryPolicy{}, fmt.Errorf("delivery backoff_mode %q is not recognized", backoffMode)
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
		return workspaceDeliveryPolicy{Channel: HarnessDeliveryCapability(channel), MaxAttempts: policy.MaxAttempts, BackoffMode: backoffMode, BackoffBaseMS: policy.BackoffBaseMS, BackoffMaxMS: policy.BackoffMaxMS}, nil
	case HarnessDeliveryUnsupported:
		return workspaceDeliveryPolicy{}, fmt.Errorf("delivery channel %q is unsupported", channel)
	default:
		return workspaceDeliveryPolicy{}, fmt.Errorf("delivery channel %q is not recognized", channel)
	}
}
