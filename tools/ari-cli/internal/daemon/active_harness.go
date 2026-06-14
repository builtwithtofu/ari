package daemon

import (
	"context"
	"strings"
	"sync"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

type activeHarnessRun struct {
	workspaceID       string
	sessionID         string
	providerSessionID string
	executor          Executor
	cancel            context.CancelFunc
	stopOnSuspend     bool
	once              sync.Once
}

type trackedHarnessExecutor struct {
	Executor
	daemon      *Daemon
	store       *globaldb.Store
	workspaceID string
	sessionID   string
	cancel      context.CancelFunc
	unregister  func()
}

func (e *trackedHarnessExecutor) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	run, err := e.Executor.Start(ctx, req)
	if err != nil {
		return run, err
	}
	providerSessionID := strings.TrimSpace(run.SessionID)
	if providerSessionID == "" {
		providerSessionID = strings.TrimSpace(run.ProviderSessionID)
	}
	if providerSessionID == "" {
		providerSessionID = strings.TrimSpace(run.RunID)
	}
	if providerSessionID == "" {
		providerSessionID = strings.TrimSpace(run.ProviderRunID)
	}
	e.unregister = e.daemon.registerActiveHarnessRun(e.workspaceID, e.sessionID, providerSessionID, e.Executor, e.cancel)
	return run, nil
}

func (e *trackedHarnessExecutor) Descriptor() HarnessAdapterDescriptor {
	if describer, ok := e.Executor.(HarnessDescriber); ok {
		return describer.Descriptor()
	}
	return HarnessAdapterDescriptor{}
}

func (e *trackedHarnessExecutor) Items(ctx context.Context, sessionID string) ([]TimelineItem, error) {
	defer func() {
		if e.unregister != nil {
			e.unregister()
		}
	}()
	return e.Executor.Items(ctx, sessionID)
}

func (d *Daemon) registerActiveHarnessRun(workspaceID, sessionID, providerSessionID string, executor Executor, cancel context.CancelFunc) func() {
	return d.registerHarnessRun(workspaceID, sessionID, providerSessionID, executor, cancel, true)
}

func (d *Daemon) registerHarnessDeliveryTarget(workspaceID, sessionID, providerSessionID string, executor Executor) func() {
	return d.registerHarnessRun(workspaceID, sessionID, providerSessionID, executor, nil, false)
}

func (d *Daemon) registerHarnessRun(workspaceID, sessionID, providerSessionID string, executor Executor, cancel context.CancelFunc, stopOnSuspend bool) func() {
	if d == nil || executor == nil {
		return func() {}
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return func() {}
	}
	run := &activeHarnessRun{workspaceID: strings.TrimSpace(workspaceID), sessionID: sessionID, providerSessionID: strings.TrimSpace(providerSessionID), executor: executor, cancel: cancel, stopOnSuspend: stopOnSuspend}
	d.activeHarnessMu.Lock()
	d.activeHarnesses[sessionID] = run
	d.activeHarnessMu.Unlock()
	return func() {
		d.activeHarnessMu.Lock()
		if d.activeHarnesses[sessionID] == run {
			delete(d.activeHarnesses, sessionID)
		}
		d.activeHarnessMu.Unlock()
	}
}

func (d *Daemon) stopActiveHarnessesForWorkspace(ctx context.Context, store *globaldb.Store, workspaceID string) {
	workspaceID = strings.TrimSpace(workspaceID)
	if d == nil || workspaceID == "" {
		return
	}
	d.activeHarnessMu.Lock()
	runs := make([]*activeHarnessRun, 0, len(d.activeHarnesses))
	activeSessionIDs := make(map[string]struct{}, len(d.activeHarnesses))
	for _, run := range d.activeHarnesses {
		if run.workspaceID == workspaceID {
			if run.stopOnSuspend {
				runs = append(runs, run)
				activeSessionIDs[run.sessionID] = struct{}{}
			}
		}
	}
	d.activeHarnessMu.Unlock()
	for _, run := range runs {
		run.stop(ctx, store)
	}
	d.stopPersistedRunningHarnessSessions(ctx, store, workspaceID, activeSessionIDs)
}

func (d *Daemon) stopPersistedRunningHarnessSessions(ctx context.Context, store *globaldb.Store, workspaceID string, skip map[string]struct{}) {
	if d == nil || store == nil {
		return
	}
	sessions, err := store.ListHarnessSessions(ctx, workspaceID)
	if err != nil {
		return
	}
	for _, session := range sessions {
		sessionID := strings.TrimSpace(session.SessionID)
		if session.Status != "running" || sessionID == "" {
			continue
		}
		if _, ok := skip[sessionID]; ok {
			continue
		}
		executor, err := d.resolveHarness(ctx, store, HarnessSessionStartRequest{Executor: session.Harness}, session.CWD)
		if err == nil && executor != nil {
			providerSessionID := strings.TrimSpace(session.ProviderSessionID)
			if providerSessionID == "" {
				providerSessionID = sessionID
			}
			_ = executor.Stop(context.Background(), providerSessionID)
		}
		_ = newHarnessLifecycle(store).markStopped(ctx, sessionID)
		markFanoutMemberForWorkerSession(ctx, store, sessionID, "stopped", "", "")
	}
}

func (r *activeHarnessRun) stop(ctx context.Context, store *globaldb.Store) {
	if r == nil {
		return
	}
	r.once.Do(func() {
		if store != nil {
			_ = newHarnessLifecycle(store).markStopped(ctx, r.sessionID)
			markFanoutMemberForWorkerSession(ctx, store, r.sessionID, "stopped", "", "")
		}
		if r.cancel != nil {
			r.cancel()
		}
		providerSessionID := strings.TrimSpace(r.providerSessionID)
		if providerSessionID == "" {
			providerSessionID = r.sessionID
		}
		if r.executor != nil {
			_ = r.executor.Stop(context.Background(), providerSessionID)
		}
	})
}
