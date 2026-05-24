package daemon

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

// harnessLifecycle owns daemon persistence for harness session execution.
// Sticky sessions and ephemeral calls keep their request/reply orchestration in
// their RPC handlers, but share this boundary for Ari lifecycle facts: run log,
// provider identity, final response, telemetry, and status transitions.
type harnessLifecycle struct {
	store *globaldb.Store
}

func newHarnessLifecycle(store *globaldb.Store) harnessLifecycle {
	return harnessLifecycle{store: store}
}

func (l harnessLifecycle) persistNewStickyResult(ctx context.Context, result HarnessCallResult, primaryFolder string, profile ...Profile) error {
	if err := storeHarnessRunLogMessages(ctx, l.store, result, primaryFolder, profile...); err != nil {
		return err
	}
	return l.persistResultArtifacts(ctx, result, profile...)
}

func (l harnessLifecycle) persistExistingEphemeralResult(ctx context.Context, result HarnessCallResult, profile Profile) error {
	if err := appendTimelineItemsAsRunLogMessages(ctx, l.store, result.HarnessSession.HarnessSessionID, result.Items); err != nil {
		return err
	}
	run := result.HarnessSession
	providerMetadata, err := json.Marshal(map[string]any{"session_ref": result.SessionRef, "provider_session_id": run.ProviderSessionID, "provider_run_id": run.ProviderRunID, "invocation_mode": run.InvocationMode, "usage_bucket": run.UsageBucket})
	if err != nil {
		return err
	}
	if err := l.store.UpdateHarnessSessionProvider(ctx, run.HarnessSessionID, run.ProviderSessionID, run.ProviderRunID, string(providerMetadata)); err != nil {
		return err
	}
	run = result.HarnessSession
	sample := agentSessionProcessMetricsSampler(ctx, run)
	if run.ProcessSample != nil {
		sample = *run.ProcessSample
	}
	return storeHarnessSessionTelemetry(ctx, l.store, result, sample, profile)
}

func (l harnessLifecycle) persistResultArtifacts(ctx context.Context, result HarnessCallResult, profile ...Profile) error {
	if result.FinalResponse != nil {
		if err := storeFinalResponse(ctx, l.store, result, profile...); err != nil {
			return err
		}
	}
	run := result.HarnessSession
	sample := agentSessionProcessMetricsSampler(ctx, run)
	if run.ProcessSample != nil {
		sample = *run.ProcessSample
	}
	return storeHarnessSessionTelemetry(ctx, l.store, result, sample, profile...)
}

func (l harnessLifecycle) markCompleted(ctx context.Context, sessionID string) error {
	return l.store.UpdateHarnessSessionStatus(ctx, strings.TrimSpace(sessionID), "completed")
}

func (l harnessLifecycle) markFailed(ctx context.Context, sessionID string) {
	_ = l.store.UpdateHarnessSessionStatus(ctx, strings.TrimSpace(sessionID), "failed")
}

func (l harnessLifecycle) markFailedWithFinalResponse(ctx context.Context, sessionID string, response globaldb.FinalResponse) {
	sessionID = strings.TrimSpace(sessionID)
	l.markFailed(ctx, sessionID)
	response.HarnessSessionID = sessionID
	response.Status = "failed"
	response.Text = strings.TrimSpace(response.Text)
	_ = l.store.UpsertFinalResponse(ctx, response)
}
