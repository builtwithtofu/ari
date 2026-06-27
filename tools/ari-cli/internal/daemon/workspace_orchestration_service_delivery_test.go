package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

func TestWorkspaceOrchestrationServiceProcessesDeliveryAttemptOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		result     func(time.Time) WorkspaceDeliveryAttemptResult
		assertions func(*testing.T, *workspaceDeliveryServiceScenario, []WorkspaceDeliveryOutcome)
	}{
		{
			name: "completed attempt completes delivery and marks source event delivered",
			result: func(time.Time) WorkspaceDeliveryAttemptResult {
				return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}
			},
			assertions: func(t *testing.T, scenario *workspaceDeliveryServiceScenario, outcomes []WorkspaceDeliveryOutcome) {
				requireDeliveryOutcome(t, outcomes, scenario.delivery.DeliveryID, WorkspaceDeliveryOutcomeCompleted)
				requireDeliveryCompleted(t, scenario)
				requireSourceEventDelivered(t, scenario)
			},
		},
		{
			name: "terminal failure leaves source event readable",
			result: func(time.Time) WorkspaceDeliveryAttemptResult {
				return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptFailed, LastError: "adapter rejected visible turn"}
			},
			assertions: func(t *testing.T, scenario *workspaceDeliveryServiceScenario, outcomes []WorkspaceDeliveryOutcome) {
				requireDeliveryOutcome(t, outcomes, scenario.delivery.DeliveryID, WorkspaceDeliveryOutcomeFailed)
				requireDeliveryFailed(t, scenario, "adapter rejected visible turn")
				requireSourceEventReadable(t, scenario)
			},
		},
		{
			name: "retryable failure schedules a later attempt and leaves source event readable",
			result: func(now time.Time) WorkspaceDeliveryAttemptResult {
				nextAttempt := now.Add(5 * time.Second)
				return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: "adapter offline", NextAttemptAt: &nextAttempt}
			},
			assertions: func(t *testing.T, scenario *workspaceDeliveryServiceScenario, outcomes []WorkspaceDeliveryOutcome) {
				requireDeliveryOutcome(t, outcomes, scenario.delivery.DeliveryID, WorkspaceDeliveryOutcomeRetry)
				requireDeliveryPendingRetry(t, scenario, "adapter offline", scenario.now.Add(5*time.Second))
				requireSourceEventReadable(t, scenario)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scenario := newWorkspaceDeliveryServiceScenario(t, tc.name)
			dispatcher := &recordingWorkspaceDeliveryDispatcher{result: tc.result(scenario.now)}
			outcomes := runWorkspaceOrchestrationOnce(t, scenario, dispatcher, scenario.now)

			requireSingleClaimedAttempt(t, dispatcher, scenario.delivery.DeliveryID)
			tc.assertions(t, scenario, outcomes)
		})
	}
}

func TestWorkspaceOrchestrationServiceRunDueAttemptsDueDeliveries(t *testing.T) {
	scenario := newWorkspaceDeliveryServiceScenario(t, "run-due")
	dispatcher := &recordingWorkspaceDeliveryDispatcher{result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}}
	service := newWorkspaceOrchestrationService(scenario.store, dispatcher)

	if err := service.runDueOnce(scenario.ctx, scenario.now); err != nil {
		t.Fatalf("runDueOnce returned error: %v", err)
	}

	requireSingleClaimedAttempt(t, dispatcher, scenario.delivery.DeliveryID)
	requireDeliveryCompleted(t, scenario)
}

func TestWorkspaceOrchestrationServiceFailsRetryAfterMaxAttempts(t *testing.T) {
	scenario := newWorkspaceDeliveryServiceScenario(t, "max-attempts")
	dispatcher := &recordingWorkspaceDeliveryDispatcher{result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: "adapter still offline"}}

	firstRetry := runWorkspaceOrchestrationOnce(t, scenario, dispatcher, scenario.now)
	requireDeliveryOutcome(t, firstRetry, scenario.delivery.DeliveryID, WorkspaceDeliveryOutcomeRetry)
	firstNextAttempt := requireScheduledRetry(t, scenario, scenario.now)
	secondRetry := runWorkspaceOrchestrationOnce(t, scenario, dispatcher, firstNextAttempt)
	requireDeliveryOutcome(t, secondRetry, scenario.delivery.DeliveryID, WorkspaceDeliveryOutcomeRetry)
	secondNextAttempt := requireScheduledRetry(t, scenario, firstNextAttempt)
	outcomes := runWorkspaceOrchestrationOnce(t, scenario, dispatcher, secondNextAttempt)

	requireDeliveryOutcome(t, outcomes, scenario.delivery.DeliveryID, WorkspaceDeliveryOutcomeFailed)
	requireDispatcherAttemptCount(t, dispatcher, 3)
	requireDeliveryFailed(t, scenario, "adapter still offline")
	requireSourceEventReadable(t, scenario)
}

func TestWorkspaceOrchestrationServiceFailsTimedOutSubscriptionDeliveriesWithoutDispatch(t *testing.T) {
	base := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	timeoutAt := base.Add(time.Minute)
	scenario := newWorkspaceDeliveryServiceScenario(t, "timeout", withScenarioBase(base), withScenarioTimeout(timeoutAt))
	dispatcher := &recordingWorkspaceDeliveryDispatcher{result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}}

	outcomes := runWorkspaceOrchestrationOnce(t, scenario, dispatcher, timeoutAt.Add(time.Second))

	if len(outcomes) != 0 {
		t.Fatalf("outcomes = %#v, want timeout cleanup before dispatch", outcomes)
	}
	requireDispatcherAttemptCount(t, dispatcher, 0)
	requireDeliveryFailed(t, scenario, "event subscription timed out")
}

func TestWorkspaceOrchestrationServiceSkipsDeliveryCanceledDuringDispatch(t *testing.T) {
	scenario := newWorkspaceDeliveryServiceScenario(t, "cancel-race")
	dispatcher := &recordingWorkspaceDeliveryDispatcher{
		result: WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted},
		onAttempt: func(attempt WorkspaceDeliveryAttempt) {
			if _, err := scenario.store.CancelEventSubscription(scenario.ctx, attempt.Delivery.SubscriptionID); err != nil {
				t.Fatalf("CancelEventSubscription returned error: %v", err)
			}
		},
	}

	outcomes := runWorkspaceOrchestrationOnce(t, scenario, dispatcher, scenario.now)

	requireDeliveryOutcome(t, outcomes, scenario.delivery.DeliveryID, WorkspaceDeliveryOutcomeSkipped)
	requireDeliveryFailed(t, scenario, "event subscription canceled")
}

type workspaceDeliveryServiceScenario struct {
	ctx          context.Context
	store        *globaldb.Store
	base         time.Time
	now          time.Time
	workspaceID  string
	subscription globaldb.EventSubscription
	sourceEvent  globaldb.WorkspaceEvent
	delivery     globaldb.PendingDelivery
}

type workspaceDeliveryScenarioOptions struct {
	base      time.Time
	timeoutAt *time.Time
}

type workspaceDeliveryScenarioOption func(*workspaceDeliveryScenarioOptions)

func withScenarioBase(base time.Time) workspaceDeliveryScenarioOption {
	return func(opts *workspaceDeliveryScenarioOptions) {
		opts.base = base
	}
}

func withScenarioTimeout(timeoutAt time.Time) workspaceDeliveryScenarioOption {
	return func(opts *workspaceDeliveryScenarioOptions) {
		opts.timeoutAt = &timeoutAt
	}
}

func newWorkspaceDeliveryServiceScenario(t *testing.T, name string, options ...workspaceDeliveryScenarioOption) *workspaceDeliveryServiceScenario {
	t.Helper()
	opts := workspaceDeliveryScenarioOptions{base: time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)}
	for _, option := range options {
		option(&opts)
	}
	suffix := stableRuntimeAgentIDSegment(name)
	ctx := context.Background()
	store := newCommandMethodTestStore(t)
	workspaceID := "ws-delivery-" + suffix
	subscriptionID := "sub-delivery-" + suffix
	ownerSessionID := "owner-" + suffix
	if err := store.CreateWorkspace(ctx, workspaceID, workspaceID, t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}
	subscription, err := store.CreateEventSubscription(ctx, globaldb.EventSubscription{
		SubscriptionID:     subscriptionID,
		WorkspaceID:        workspaceID,
		OwnerSessionID:     ownerSessionID,
		FilterJSON:         mustJSONForWorkspaceDeliveryTest(t, globaldb.EventSubscriptionFilter{EventTypes: []string{globaldb.WorkspaceEventWorkerCompleted}}),
		DeliveryTargetType: globaldb.WorkspaceEventSubjectHarnessSession,
		DeliveryTargetID:   ownerSessionID,
		DeliveryPolicyJSON: mustJSONForWorkspaceDeliveryTest(t, map[string]any{"channel": string(HarnessDeliveryVisiblePromptTurn), "max_attempts": 3}),
		TimeoutAt:          opts.timeoutAt,
		CreatedAt:          opts.base,
		UpdatedAt:          opts.base,
	})
	if err != nil {
		t.Fatalf("CreateEventSubscription returned error: %v", err)
	}
	sourceEventParams := globaldb.NewFanoutWorkerWorkspaceEvent(globaldb.FanoutWorkerWorkspaceEventParams{
		WorkspaceID:           workspaceID,
		EventType:             globaldb.WorkspaceEventWorkerCompleted,
		WorkerSessionID:       "worker-" + suffix,
		ProducerID:            "worker-" + suffix,
		FanoutGroupID:         "fg-" + suffix,
		FanoutMemberID:        "fm-" + suffix,
		SourceSessionID:       ownerSessionID,
		TargetProfileID:       "agent-" + suffix,
		RequestAgentMessageID: "request-" + suffix,
		FinalResponseID:       "fr-" + suffix,
	})
	sourceEventParams.EventID = "we-worker-" + suffix
	sourceEventParams.CreatedAt = opts.base.Add(time.Second)
	sourceEvent, err := store.AppendWorkspaceEvent(ctx, sourceEventParams)
	if err != nil {
		t.Fatalf("AppendWorkspaceEvent returned error: %v", err)
	}
	dueLookupAt := opts.base.Add(time.Minute)
	if opts.timeoutAt != nil {
		dueLookupAt = opts.base.Add(30 * time.Second)
	}
	delivery := requireOnlyDueDelivery(t, store, workspaceID, sourceEvent.EventID, dueLookupAt)
	return &workspaceDeliveryServiceScenario{ctx: ctx, store: store, base: opts.base, now: opts.base.Add(time.Minute), workspaceID: workspaceID, subscription: subscription, sourceEvent: sourceEvent, delivery: delivery}
}

func runWorkspaceOrchestrationOnce(t *testing.T, scenario *workspaceDeliveryServiceScenario, dispatcher *recordingWorkspaceDeliveryDispatcher, now time.Time) []WorkspaceDeliveryOutcome {
	t.Helper()
	service := newWorkspaceOrchestrationService(scenario.store, dispatcher)
	outcomes, err := service.attemptDueDeliveries(scenario.ctx, now, 10)
	if err != nil {
		t.Fatalf("attemptDueDeliveries returned error: %v", err)
	}
	return outcomes
}

func requireOnlyDueDelivery(t *testing.T, store *globaldb.Store, workspaceID, sourceEventID string, dueAt time.Time) globaldb.PendingDelivery {
	t.Helper()
	due, err := store.ListDuePendingDeliveries(context.Background(), dueAt, 10)
	if err != nil {
		t.Fatalf("ListDuePendingDeliveries returned error: %v", err)
	}
	if len(due) != 1 || due[0].WorkspaceID != workspaceID || len(due[0].EventIDs) != 1 || due[0].EventIDs[0] != sourceEventID {
		t.Fatalf("due deliveries = %#v, want one delivery for %s", due, sourceEventID)
	}
	return due[0]
}

func requireSingleClaimedAttempt(t *testing.T, dispatcher *recordingWorkspaceDeliveryDispatcher, deliveryID string) {
	t.Helper()
	attempts := dispatcher.Attempts()
	if len(attempts) != 1 {
		t.Fatalf("dispatcher attempts = %#v, want one attempt", attempts)
	}
	attempt := attempts[0]
	if attempt.Delivery.DeliveryID != deliveryID || attempt.Delivery.Attempts != 1 || !deliveryWasClaimed(attempt.Delivery) {
		t.Fatalf("dispatcher attempt = %#v, want claimed delivery %s", attempt, deliveryID)
	}
}

func requireDispatcherAttemptCount(t *testing.T, dispatcher *recordingWorkspaceDeliveryDispatcher, count int) {
	t.Helper()
	if attempts := dispatcher.Attempts(); len(attempts) != count {
		t.Fatalf("dispatcher attempts = %#v, want %d attempts", attempts, count)
	}
}

func requireDeliveryOutcome(t *testing.T, outcomes []WorkspaceDeliveryOutcome, deliveryID string, status WorkspaceDeliveryOutcomeStatus) {
	t.Helper()
	if len(outcomes) != 1 || outcomes[0].DeliveryID != deliveryID || outcomes[0].Status != status {
		t.Fatalf("outcomes = %#v, want one %s outcome for %s", outcomes, status, deliveryID)
	}
}

func requireDeliveryCompleted(t *testing.T, scenario *workspaceDeliveryServiceScenario) {
	t.Helper()
	delivery := currentDelivery(t, scenario)
	if !deliveryCompleted(delivery) || delivery.Attempts == 0 {
		t.Fatalf("delivery = %#v, want completed attempted delivery", delivery)
	}
}

func requireDeliveryFailed(t *testing.T, scenario *workspaceDeliveryServiceScenario, lastError string) {
	t.Helper()
	delivery := currentDelivery(t, scenario)
	if !deliveryFailed(delivery) || delivery.LastError != lastError || delivery.TerminalAt == nil {
		t.Fatalf("delivery = %#v, want failed delivery with error %q", delivery, lastError)
	}
}

func requireDeliveryPendingRetry(t *testing.T, scenario *workspaceDeliveryServiceScenario, lastError string, nextAttempt time.Time) {
	t.Helper()
	delivery := currentDelivery(t, scenario)
	if !deliveryPending(delivery) || delivery.LastError != lastError || !sameOptionalTime(delivery.NextAttemptAt, &nextAttempt) {
		t.Fatalf("delivery = %#v, want pending retry at %s with error %q", delivery, nextAttempt, lastError)
	}
}

func requireScheduledRetry(t *testing.T, scenario *workspaceDeliveryServiceScenario, after time.Time) time.Time {
	t.Helper()
	delivery := currentDelivery(t, scenario)
	if !deliveryPending(delivery) || delivery.NextAttemptAt == nil || !delivery.NextAttemptAt.After(after) {
		t.Fatalf("delivery = %#v, want pending retry after %s", delivery, after)
	}
	return *delivery.NextAttemptAt
}

func currentDelivery(t *testing.T, scenario *workspaceDeliveryServiceScenario) globaldb.PendingDelivery {
	t.Helper()
	delivery, err := scenario.store.GetPendingDelivery(scenario.ctx, scenario.delivery.DeliveryID)
	if err != nil {
		t.Fatalf("GetPendingDelivery returned error: %v", err)
	}
	return delivery
}

func requireSourceEventDelivered(t *testing.T, scenario *workspaceDeliveryServiceScenario) {
	t.Helper()
	if eventIsReadable(t, scenario) {
		t.Fatalf("source event %s is still readable after completed delivery", scenario.sourceEvent.EventID)
	}
}

func requireSourceEventReadable(t *testing.T, scenario *workspaceDeliveryServiceScenario) {
	t.Helper()
	if !eventIsReadable(t, scenario) {
		t.Fatalf("source event %s is not readable", scenario.sourceEvent.EventID)
	}
}

func eventIsReadable(t *testing.T, scenario *workspaceDeliveryServiceScenario) bool {
	t.Helper()
	result, err := scenario.store.ReadEventSubscription(scenario.ctx, globaldb.EventSubscriptionReadRequest{SubscriptionID: scenario.subscription.SubscriptionID, Limit: 10})
	if err != nil {
		t.Fatalf("ReadEventSubscription returned error: %v", err)
	}
	for _, event := range result.Events {
		if event.EventID == scenario.sourceEvent.EventID {
			return true
		}
	}
	return false
}

func deliveryWasClaimed(delivery globaldb.PendingDelivery) bool {
	return delivery.Attempts > 0 && delivery.NextAttemptAt == nil && delivery.TerminalAt == nil
}

func deliveryPending(delivery globaldb.PendingDelivery) bool {
	return delivery.TerminalAt == nil
}

func deliveryCompleted(delivery globaldb.PendingDelivery) bool {
	return delivery.TerminalAt != nil && delivery.LastError == ""
}

func deliveryFailed(delivery globaldb.PendingDelivery) bool {
	return delivery.TerminalAt != nil && delivery.LastError != ""
}

type recordingWorkspaceDeliveryDispatcher struct {
	mu        sync.Mutex
	result    WorkspaceDeliveryAttemptResult
	attempts  []WorkspaceDeliveryAttempt
	onAttempt func(WorkspaceDeliveryAttempt)
}

func (d *recordingWorkspaceDeliveryDispatcher) AttemptWorkspaceDelivery(ctx context.Context, attempt WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error) {
	if ctx == nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("context is required")
	}
	d.mu.Lock()
	d.attempts = append(d.attempts, attempt)
	d.mu.Unlock()
	if d.onAttempt != nil {
		d.onAttempt(attempt)
	}
	return d.result, nil
}

func (d *recordingWorkspaceDeliveryDispatcher) Attempts() []WorkspaceDeliveryAttempt {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]WorkspaceDeliveryAttempt(nil), d.attempts...)
}

func mustJSONForWorkspaceDeliveryTest(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON fixture: %v", err)
	}
	return string(data)
}

func sameOptionalTime(got, want *time.Time) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return got.Equal(*want)
}
