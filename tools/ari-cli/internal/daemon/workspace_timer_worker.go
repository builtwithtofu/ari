package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

const (
	defaultWorkspaceTimerWorkerInterval = time.Second
	defaultWorkspaceTimerWorkerLimit    = 100
)

// startWorkspaceTimerWorker makes durable timers daemon-owned: due timers
// fire and append timer.fired workspace events without any client calling
// workspace.timers.fire_due.
func (d *Daemon) startWorkspaceTimerWorker(store *globaldb.Store) {
	if d == nil || store == nil {
		return
	}
	d.startHarnessLifecycleWork(func(ctx context.Context) {
		ticker := time.NewTicker(defaultWorkspaceTimerWorkerInterval)
		defer ticker.Stop()
		_ = runWorkspaceTimerWorkerLoop(ctx, store, ticker.C, defaultWorkspaceTimerWorkerLimit)
	})
}

func runWorkspaceTimerWorkerLoop(ctx context.Context, store *globaldb.Store, ticks <-chan time.Time, limit int) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}
	if ticks == nil {
		return fmt.Errorf("workspace timer worker ticks are required")
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case now, ok := <-ticks:
			if !ok {
				return nil
			}
			// Timers are durable rows; a transient store error leaves them
			// due, so the next tick retries the same work.
			_, _ = store.FireDueWorkspaceTimers(ctx, now.UTC(), limit)
		}
	}
}
