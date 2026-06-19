package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

const (
	defaultWorkspaceOrchestrationWorkLimit = 100
	workspaceOrchestrationErrorBackoff     = time.Second
)

type workspaceOrchestrationRuntime struct {
	store          *globaldb.Store
	dispatcher     WorkspaceDeliveryDispatcher
	workLimit      int
	now            func() time.Time
	errorBackoff   time.Duration
	wakeSubscriber func() (<-chan struct{}, func())
}

func (d *Daemon) startWorkspaceOrchestrationRuntime(store *globaldb.Store) {
	if d == nil || store == nil {
		return
	}
	if d.startWorkspaceOrchestrationRuntimeForTest != nil {
		d.startWorkspaceOrchestrationRuntimeForTest(store)
		return
	}
	runtime := newWorkspaceOrchestrationRuntime(store, newHarnessWorkspaceDeliveryDispatcher(d, store))
	d.startHarnessLifecycleWork(func(ctx context.Context) {
		_ = runtime.run(ctx)
	})
}

func newWorkspaceOrchestrationRuntime(store *globaldb.Store, dispatcher WorkspaceDeliveryDispatcher) *workspaceOrchestrationRuntime {
	return &workspaceOrchestrationRuntime{store: store, dispatcher: dispatcher, workLimit: defaultWorkspaceOrchestrationWorkLimit, now: func() time.Time { return time.Now().UTC() }, errorBackoff: workspaceOrchestrationErrorBackoff}
}

func (r *workspaceOrchestrationRuntime) run(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if r == nil || r.store == nil {
		return fmt.Errorf("workspace orchestration store is required")
	}
	if r.dispatcher == nil {
		return fmt.Errorf("workspace delivery dispatcher is required")
	}
	wake, unsubscribe := r.subscribeWake()
	defer unsubscribe()
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		now := r.currentTime()
		next, ok, err := r.store.NextWorkspaceOrchestrationDueAt(ctx, now)
		if err != nil {
			if err := waitForWorkspaceOrchestrationWake(ctx, wake, r.errorDelay()); err != nil {
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
		wait := next.Sub(r.currentTime())
		if wait <= 0 {
			if err := r.runDueOnce(ctx, now); err != nil {
				if err := waitForWorkspaceOrchestrationWake(ctx, wake, r.errorDelay()); err != nil {
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

func (r *workspaceOrchestrationRuntime) runDueOnce(ctx context.Context, now time.Time) error {
	if now.IsZero() {
		now = r.currentTime()
	}
	limit := r.workLimit
	if limit <= 0 {
		limit = defaultWorkspaceOrchestrationWorkLimit
	}
	var errs []string
	if _, err := r.store.FireDueWorkspaceTimers(ctx, now, limit); err != nil {
		errs = append(errs, err.Error())
	}
	if _, err := runWorkspaceDeliveryWorkerOnce(ctx, r.store, r.dispatcher, now, limit); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("run workspace orchestration due work: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (r *workspaceOrchestrationRuntime) subscribeWake() (<-chan struct{}, func()) {
	if r.wakeSubscriber != nil {
		return r.wakeSubscriber()
	}
	return r.store.SubscribeOrchestrationWake()
}

func (r *workspaceOrchestrationRuntime) currentTime() time.Time {
	if r != nil && r.now != nil {
		return r.now().UTC()
	}
	return time.Now().UTC()
}

func (r *workspaceOrchestrationRuntime) errorDelay() time.Duration {
	if r != nil && r.errorBackoff > 0 {
		return r.errorBackoff
	}
	return workspaceOrchestrationErrorBackoff
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
