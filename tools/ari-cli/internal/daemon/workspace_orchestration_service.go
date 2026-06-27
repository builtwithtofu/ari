package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

const (
	defaultWorkspaceOrchestrationWorkLimit = 100
	workspaceOrchestrationErrorBackoff     = time.Second

	workspaceDeliveryBackoffLinear      = "linear"
	workspaceDeliveryBackoffFixed       = "fixed"
	workspaceDeliveryBackoffExponential = "exponential"
)

type WorkspaceDeliveryAttemptStatus string

const (
	WorkspaceDeliveryAttemptCompleted WorkspaceDeliveryAttemptStatus = "completed"
	WorkspaceDeliveryAttemptFailed    WorkspaceDeliveryAttemptStatus = "failed"
	WorkspaceDeliveryAttemptRetry     WorkspaceDeliveryAttemptStatus = "retry"
)

type WorkspaceDeliveryOutcomeStatus string

const (
	WorkspaceDeliveryOutcomeCompleted WorkspaceDeliveryOutcomeStatus = "completed"
	WorkspaceDeliveryOutcomeFailed    WorkspaceDeliveryOutcomeStatus = "failed"
	WorkspaceDeliveryOutcomeRetry     WorkspaceDeliveryOutcomeStatus = "retry"
	WorkspaceDeliveryOutcomeSkipped   WorkspaceDeliveryOutcomeStatus = "skipped"
)

type WorkspaceDeliveryAttempt struct {
	Delivery globaldb.PendingDelivery
	Now      time.Time
}

type WorkspaceDeliveryAttemptResult struct {
	Status        WorkspaceDeliveryAttemptStatus
	LastError     string
	NextAttemptAt *time.Time
}

type WorkspaceDeliveryOutcome struct {
	DeliveryID string
	Status     WorkspaceDeliveryOutcomeStatus
	LastError  string
}

type WorkspaceDeliveryDispatcher interface {
	AttemptWorkspaceDelivery(context.Context, WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error)
}

type workspaceDeliveryPolicy struct {
	Channel       HarnessDeliveryCapability
	MaxAttempts   int64
	BackoffMode   string
	BackoffBaseMS int64
	BackoffMaxMS  int64
}

type workspaceOrchestrationService struct {
	store          *globaldb.Store
	dispatcher     WorkspaceDeliveryDispatcher
	workLimit      int
	now            func() time.Time
	errorBackoff   time.Duration
	wakeSubscriber func() (<-chan struct{}, func())
}

func (d *Daemon) startWorkspaceOrchestrationService(store *globaldb.Store) {
	if d == nil || store == nil {
		return
	}
	if d.startWorkspaceOrchestrationServiceForTest != nil {
		d.startWorkspaceOrchestrationServiceForTest(store)
		return
	}
	service := newWorkspaceOrchestrationService(store, newHarnessWorkspaceDeliveryDispatcher(d, store))
	d.startHarnessLifecycleWork(func(ctx context.Context) {
		_ = service.run(ctx)
	})
}

func newWorkspaceOrchestrationService(store *globaldb.Store, dispatcher WorkspaceDeliveryDispatcher) *workspaceOrchestrationService {
	return &workspaceOrchestrationService{store: store, dispatcher: dispatcher, workLimit: defaultWorkspaceOrchestrationWorkLimit, now: func() time.Time { return time.Now().UTC() }, errorBackoff: workspaceOrchestrationErrorBackoff}
}

func (s *workspaceOrchestrationService) run(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if s == nil || s.store == nil {
		return fmt.Errorf("workspace orchestration store is required")
	}
	if s.dispatcher == nil {
		return fmt.Errorf("workspace delivery dispatcher is required")
	}
	wake, unsubscribe := s.subscribeWake()
	defer unsubscribe()
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		now := s.currentTime()
		next, ok, err := s.store.NextWorkspaceOrchestrationDueAt(ctx, now)
		if err != nil {
			if err := waitForWorkspaceOrchestrationWake(ctx, wake, s.errorDelay()); err != nil {
				return nil
			}
			continue
		}
		if !ok {
			select {
			case <-ctx.Done():
				return nil
			case <-wake:
			}
			continue
		}
		wait := next.Sub(s.currentTime())
		if wait <= 0 {
			if err := s.runDueOnce(ctx, now); err != nil {
				if err := waitForWorkspaceOrchestrationWake(ctx, wake, s.errorDelay()); err != nil {
					return nil
				}
			}
			continue
		}
		if err := waitForWorkspaceOrchestrationWake(ctx, wake, wait); err != nil {
			return nil
		}
	}
}

func (s *workspaceOrchestrationService) runDueOnce(ctx context.Context, now time.Time) error {
	if now.IsZero() {
		now = s.currentTime()
	}
	limit := s.workLimit
	if limit <= 0 {
		limit = defaultWorkspaceOrchestrationWorkLimit
	}
	var errs []string
	if _, err := s.fireDueWorkspaceTimers(ctx, now, limit); err != nil {
		errs = append(errs, err.Error())
	}
	if _, err := s.attemptDueDeliveries(ctx, now, limit); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("run workspace orchestration due work: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *workspaceOrchestrationService) fireDueWorkspaceTimers(ctx context.Context, now time.Time, limit int) ([]globaldb.WorkspaceTimer, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("workspace orchestration store is required")
	}
	if now.IsZero() {
		now = s.currentTime()
	}
	if limit <= 0 {
		limit = defaultWorkspaceOrchestrationWorkLimit
	}
	due, err := s.store.ListDueWorkspaceTimers(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	fired := make([]globaldb.WorkspaceTimer, 0, len(due))
	var fireErrs []string
	for _, timer := range due {
		updated, err := s.store.FireWorkspaceTimer(ctx, timer.TimerID)
		if errors.Is(err, globaldb.ErrNotFound) {
			continue
		}
		if err != nil {
			fireErrs = append(fireErrs, err.Error())
			continue
		}
		fired = append(fired, updated)
	}
	if len(fireErrs) > 0 {
		return fired, fmt.Errorf("fire due workspace timers: %s", strings.Join(fireErrs, "; "))
	}
	return fired, nil
}

func (s *workspaceOrchestrationService) attemptDueDeliveries(ctx context.Context, now time.Time, limit int) ([]WorkspaceDeliveryOutcome, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("workspace orchestration store is required")
	}
	if s.dispatcher == nil {
		return nil, fmt.Errorf("workspace delivery dispatcher is required")
	}
	if now.IsZero() {
		now = s.currentTime()
	}
	if _, err := s.store.RequeueStalePendingDeliveryAttempts(ctx, now); err != nil {
		return nil, err
	}
	if _, err := s.store.FailExpiredPendingDeliveries(ctx, now); err != nil {
		return nil, err
	}
	if _, err := s.store.FailTimedOutSubscriptionDeliveries(ctx, now); err != nil {
		return nil, err
	}
	due, err := s.store.ListDuePendingDeliveries(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	outcomes := make([]WorkspaceDeliveryOutcome, 0, len(due))
	for _, pending := range due {
		claimed, err := s.store.ClaimDuePendingDeliveryAttempt(ctx, pending.DeliveryID, now)
		if err != nil {
			if errors.Is(err, globaldb.ErrNotFound) {
				outcomes = append(outcomes, WorkspaceDeliveryOutcome{DeliveryID: pending.DeliveryID, Status: WorkspaceDeliveryOutcomeSkipped})
				continue
			}
			return outcomes, err
		}

		result, dispatchErr := s.dispatcher.AttemptWorkspaceDelivery(ctx, WorkspaceDeliveryAttempt{Delivery: claimed, Now: now})
		if dispatchErr != nil {
			result = WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: dispatchErr.Error()}
		}
		outcome, err := s.finishDeliveryAttempt(ctx, claimed, result, now)
		outcomes = append(outcomes, outcome)
		if err != nil {
			return outcomes, err
		}
	}
	return outcomes, nil
}

func (s *workspaceOrchestrationService) finishDeliveryAttempt(ctx context.Context, delivery globaldb.PendingDelivery, result WorkspaceDeliveryAttemptResult, now time.Time) (WorkspaceDeliveryOutcome, error) {
	if s == nil || s.store == nil {
		return WorkspaceDeliveryOutcome{}, fmt.Errorf("workspace orchestration store is required")
	}
	if now.IsZero() {
		now = s.currentTime()
	}
	switch result.Status {
	case WorkspaceDeliveryAttemptCompleted:
		completed, err := s.store.CompletePendingDelivery(ctx, delivery.DeliveryID)
		if errors.Is(err, globaldb.ErrNotFound) {
			return WorkspaceDeliveryOutcome{DeliveryID: delivery.DeliveryID, Status: WorkspaceDeliveryOutcomeSkipped}, nil
		}
		return WorkspaceDeliveryOutcome{DeliveryID: completed.DeliveryID, Status: WorkspaceDeliveryOutcomeCompleted}, err
	case WorkspaceDeliveryAttemptFailed:
		failed, err := s.store.FailPendingDelivery(ctx, delivery.DeliveryID, result.LastError)
		if errors.Is(err, globaldb.ErrNotFound) {
			return WorkspaceDeliveryOutcome{DeliveryID: delivery.DeliveryID, Status: WorkspaceDeliveryOutcomeSkipped}, nil
		}
		return WorkspaceDeliveryOutcome{DeliveryID: failed.DeliveryID, Status: WorkspaceDeliveryOutcomeFailed, LastError: failed.LastError}, err
	case WorkspaceDeliveryAttemptRetry:
		return s.scheduleDeliveryRetry(ctx, delivery, result, now)
	default:
		lastError := strings.TrimSpace(result.LastError)
		if lastError == "" {
			lastError = fmt.Sprintf("unsupported delivery attempt result %q", result.Status)
		}
		failed, err := s.store.FailPendingDelivery(ctx, delivery.DeliveryID, lastError)
		if errors.Is(err, globaldb.ErrNotFound) {
			return WorkspaceDeliveryOutcome{DeliveryID: delivery.DeliveryID, Status: WorkspaceDeliveryOutcomeSkipped}, nil
		}
		return WorkspaceDeliveryOutcome{DeliveryID: failed.DeliveryID, Status: WorkspaceDeliveryOutcomeFailed, LastError: failed.LastError}, err
	}
}

func (s *workspaceOrchestrationService) scheduleDeliveryRetry(ctx context.Context, delivery globaldb.PendingDelivery, result WorkspaceDeliveryAttemptResult, now time.Time) (WorkspaceDeliveryOutcome, error) {
	policy, err := parseWorkspaceDeliveryPolicy(delivery.DeliveryPolicyJSON)
	if err != nil {
		failed, failErr := s.store.FailPendingDelivery(ctx, delivery.DeliveryID, err.Error())
		if errors.Is(failErr, globaldb.ErrNotFound) {
			return WorkspaceDeliveryOutcome{DeliveryID: delivery.DeliveryID, Status: WorkspaceDeliveryOutcomeSkipped}, nil
		}
		return WorkspaceDeliveryOutcome{DeliveryID: failed.DeliveryID, Status: WorkspaceDeliveryOutcomeFailed, LastError: failed.LastError}, failErr
	}
	if workspaceDeliveryRetryLimitReached(policy, delivery) {
		lastError := strings.TrimSpace(result.LastError)
		if lastError == "" {
			lastError = fmt.Sprintf("delivery retry limit reached after %d attempts", delivery.Attempts)
		}
		failed, failErr := s.store.FailPendingDelivery(ctx, delivery.DeliveryID, lastError)
		if errors.Is(failErr, globaldb.ErrNotFound) {
			return WorkspaceDeliveryOutcome{DeliveryID: delivery.DeliveryID, Status: WorkspaceDeliveryOutcomeSkipped}, nil
		}
		return WorkspaceDeliveryOutcome{DeliveryID: failed.DeliveryID, Status: WorkspaceDeliveryOutcomeFailed, LastError: failed.LastError}, failErr
	}
	nextAttemptAt := result.NextAttemptAt
	if nextAttemptAt == nil || nextAttemptAt.IsZero() {
		fallback := now.Add(workspaceDeliveryRetryDelay(delivery, policy))
		nextAttemptAt = &fallback
	}
	retry, err := s.store.SchedulePendingDeliveryRetry(ctx, delivery.DeliveryID, *nextAttemptAt, result.LastError)
	if errors.Is(err, globaldb.ErrNotFound) {
		return WorkspaceDeliveryOutcome{DeliveryID: delivery.DeliveryID, Status: WorkspaceDeliveryOutcomeSkipped}, nil
	}
	return WorkspaceDeliveryOutcome{DeliveryID: retry.DeliveryID, Status: WorkspaceDeliveryOutcomeRetry, LastError: retry.LastError}, err
}

func (s *workspaceOrchestrationService) subscribeWake() (<-chan struct{}, func()) {
	if s.wakeSubscriber != nil {
		return s.wakeSubscriber()
	}
	return s.store.SubscribeOrchestrationWake()
}

func (s *workspaceOrchestrationService) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func (s *workspaceOrchestrationService) errorDelay() time.Duration {
	if s != nil && s.errorBackoff > 0 {
		return s.errorBackoff
	}
	return workspaceOrchestrationErrorBackoff
}

func workspaceDeliveryRetryLimitReached(policy workspaceDeliveryPolicy, delivery globaldb.PendingDelivery) bool {
	return policy.MaxAttempts > 0 && delivery.Attempts >= policy.MaxAttempts
}

func workspaceDeliveryRetryDelay(delivery globaldb.PendingDelivery, policy workspaceDeliveryPolicy) time.Duration {
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

func waitForWorkspaceOrchestrationWake(ctx context.Context, wake <-chan struct{}, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
			return nil
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wake:
		return nil
	case <-timer.C:
		return nil
	}
}
