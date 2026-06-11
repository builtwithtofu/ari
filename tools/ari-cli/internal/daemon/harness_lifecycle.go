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
	if err := appendHarnessRuntimeWorkspaceEvents(ctx, l.store, result.HarnessSession, result.Events); err != nil {
		return err
	}
	if err := l.persistResultArtifacts(ctx, result, profile...); err != nil {
		return err
	}
	// A sticky session whose turn results persisted without failing or being
	// stopped is idle: alive and available for the next input. This covers
	// both "completed" (final-response harnesses) and "running" (managed
	// sessions that stay alive between turns). Ephemeral calls never take
	// this path — they terminate.
	switch result.HarnessSession.Status {
	case "completed", "running":
		return appendSessionIdleWorkspaceEvent(ctx, l.store, result.HarnessSession)
	}
	return nil
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
	if err := appendHarnessRuntimeWorkspaceEvents(ctx, l.store, run, result.Events); err != nil {
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
	finalResponseID := ""
	if result.FinalResponse != nil {
		storedID, err := storeFinalResponse(ctx, l.store, result, profile...)
		if err != nil {
			return err
		}
		finalResponseID = storedID
	}
	run := result.HarnessSession
	if err := appendSessionTerminalEvents(ctx, l.store, run, run.Status, finalResponseID); err != nil {
		return err
	}
	sample := agentSessionProcessMetricsSampler(ctx, run)
	if run.ProcessSample != nil {
		sample = *run.ProcessSample
	}
	return storeHarnessSessionTelemetry(ctx, l.store, result, sample, profile...)
}

func (l harnessLifecycle) markFailed(ctx context.Context, sessionID string) error {
	return l.markTerminal(ctx, sessionID, "failed", "")
}

func (l harnessLifecycle) markStopped(ctx context.Context, sessionID string) error {
	return l.markTerminal(ctx, sessionID, "stopped", "")
}

// markCompletedWithFinalResponse persists the final response before the
// terminal status/events so the session.completed workspace event always
// carries a resolvable final_response payload ref.
func (l harnessLifecycle) markCompletedWithFinalResponse(ctx context.Context, sessionID string, response globaldb.FinalResponse) error {
	sessionID = strings.TrimSpace(sessionID)
	response.HarnessSessionID = sessionID
	response.Status = "completed"
	response.Text = strings.TrimSpace(response.Text)
	if err := l.store.UpsertFinalResponse(ctx, response); err != nil {
		return err
	}
	return l.markTerminal(ctx, sessionID, "completed", response.FinalResponseID)
}

func (l harnessLifecycle) markFailedWithFinalResponse(ctx context.Context, sessionID string, response globaldb.FinalResponse) {
	sessionID = strings.TrimSpace(sessionID)
	response.HarnessSessionID = sessionID
	response.Status = "failed"
	response.Text = strings.TrimSpace(response.Text)
	_ = l.store.UpsertFinalResponse(ctx, response)
	_ = l.markTerminal(ctx, sessionID, "failed", response.FinalResponseID)
}

func (l harnessLifecycle) markTerminal(ctx context.Context, sessionID, status, finalResponseID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if err := l.store.UpdateHarnessSessionStatus(ctx, sessionID, status); err != nil {
		return err
	}
	stored, err := l.store.GetHarnessSession(ctx, sessionID)
	if err != nil {
		return err
	}
	run := HarnessSession{HarnessSessionID: stored.SessionID, WorkspaceID: stored.WorkspaceID, Executor: stored.Harness, Status: status}
	return appendSessionTerminalEvents(ctx, l.store, run, status, finalResponseID)
}

// appendSessionTerminalEvents is the single emission path for terminal
// harness session facts: one workspace event in event history, the source of
// truth for projections and subscriptions.
func appendSessionTerminalEvents(ctx context.Context, store *globaldb.Store, run HarnessSession, status, finalResponseID string) error {
	var workspaceEventType string
	attentionRequired := false
	switch status {
	case "completed":
		workspaceEventType = workspaceEventSessionCompleted
	case "failed":
		workspaceEventType = workspaceEventSessionFailed
		attentionRequired = true
	case "stopped":
		workspaceEventType = workspaceEventSessionStopped
	default:
		return nil
	}
	return appendHarnessSessionWorkspaceEvent(ctx, store, run, workspaceEventType, status, finalResponseID, attentionRequired)
}
