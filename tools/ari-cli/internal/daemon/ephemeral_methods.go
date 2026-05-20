package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func (d *Daemon) callEphemeral(ctx context.Context, store *globaldb.Store, req EphemeralCallRequest) (EphemeralCallResponse, error) {
	request, err := newEphemeralCall(req)
	if err != nil {
		return EphemeralCallResponse{}, err
	}
	setup, err := createEphemeralSession(ctx, store, request, req.ContextExcerptIDs)
	if err != nil {
		return EphemeralCallResponse{}, err
	}
	requestDM, err := createEphemeralRequestMessage(ctx, store, setup, request, req.ContextExcerptIDs)
	if err != nil {
		markEphemeralSessionFailed(ctx, store, setup.SessionID)
		return EphemeralCallResponse{}, err
	}
	result, err := d.runEphemeralHarness(ctx, store, setup, request)
	if err != nil {
		markEphemeralSessionFailed(ctx, store, setup.SessionID)
		return EphemeralCallResponse{}, err
	}
	response, markFailed, err := completeEphemeralCall(ctx, store, setup, request, requestDM, result, req.ReplyAgentMessageID)
	if err != nil {
		if markFailed {
			markEphemeralSessionFailed(ctx, store, setup.SessionID)
		}
		return EphemeralCallResponse{}, err
	}
	return response, nil
}

type ephemeralCall struct {
	CallID                string
	SourceSessionID       string
	TargetAgentID         string
	Body                  string
	SessionID             string
	TaskID                string
	ContextPacketID       string
	RequestAgentMessageID string
}

type ephemeralCallSetup struct {
	SourceRun     globaldb.HarnessSession
	TargetAgent   globaldb.HarnessSessionConfig
	TargetProfile Profile
	SessionID     string
}

type ephemeralHarnessResult struct {
	Items          []TimelineItem
	InvocationMode string
}

func newEphemeralCall(req EphemeralCallRequest) (ephemeralCall, error) {
	callID := strings.TrimSpace(req.CallID)
	sourceSessionID := strings.TrimSpace(req.SourceSessionID)
	targetAgentID := strings.TrimSpace(req.TargetAgentID)
	body := strings.TrimSpace(req.Body)
	if callID == "" || sourceSessionID == "" || targetAgentID == "" || body == "" {
		missingField := ""
		switch {
		case callID == "":
			missingField = "call_id"
		case sourceSessionID == "":
			missingField = "source_session_id"
		case targetAgentID == "":
			missingField = "target_agent_id"
		case body == "":
			missingField = "body"
		}
		return ephemeralCall{}, rpc.NewHandlerError(rpc.InvalidParams, "call_id, source_session_id, target_agent_id, and body are required", map[string]any{"reason": "missing_required_fields", "missing_field": missingField, "start_invoked": false})
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = callID + "-run"
	}
	return ephemeralCall{CallID: callID, SourceSessionID: sourceSessionID, TargetAgentID: targetAgentID, Body: body, SessionID: sessionID, TaskID: callID, ContextPacketID: callID + "-context", RequestAgentMessageID: callID + "-request"}, nil
}

func createEphemeralSession(ctx context.Context, store *globaldb.Store, request ephemeralCall, contextExcerptIDs []string) (ephemeralCallSetup, error) {
	sourceRun, err := store.GetHarnessSession(ctx, request.SourceSessionID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return ephemeralCallSetup{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_source_session", "source_session_id": request.SourceSessionID, "start_invoked": false})
		}
		return ephemeralCallSetup{}, err
	}
	targetAgent, err := store.GetHarnessSessionConfig(ctx, request.TargetAgentID)
	targetProfile := Profile{Harness: targetAgent.Harness}
	if err != nil {
		if !errors.Is(err, globaldb.ErrNotFound) {
			return ephemeralCallSetup{}, err
		}
		resolvedProfile, resolveErr := resolveStoredProfile(ctx, store, sourceRun.WorkspaceID, request.TargetAgentID)
		if resolveErr != nil {
			if errors.Is(resolveErr, globaldb.ErrNotFound) {
				return ephemeralCallSetup{}, unknownProfileError(request.TargetAgentID)
			}
			return ephemeralCallSetup{}, resolveErr
		}
		targetAgent = globaldb.HarnessSessionConfig{AgentID: resolvedProfile.ProfileID, WorkspaceID: resolvedProfile.WorkspaceID, Name: resolvedProfile.Name, Harness: resolvedProfile.Harness, Model: resolvedProfile.Model, Prompt: resolvedProfile.Prompt}
		if ensureErr := store.EnsureHarnessSessionConfig(ctx, targetAgent); ensureErr != nil {
			if errors.Is(ensureErr, globaldb.ErrInvalidInput) || errors.Is(ensureErr, globaldb.ErrNotFound) {
				return ephemeralCallSetup{}, rpc.NewHandlerError(rpc.InvalidParams, ensureErr.Error(), map[string]any{"reason": "profile_unavailable", "profile": request.TargetAgentID, "target_agent_id": targetAgent.AgentID, "start_invoked": false})
			}
			return ephemeralCallSetup{}, ensureErr
		}
		targetProfile = resolvedProfile
	} else if strings.TrimSpace(targetAgent.Name) != "" {
		if resolvedProfile, resolveErr := resolveStoredProfile(ctx, store, sourceRun.WorkspaceID, targetAgent.Name); resolveErr == nil {
			targetProfile = resolvedProfile
		} else if !isUnknownProfileError(resolveErr) {
			return ephemeralCallSetup{}, resolveErr
		}
	}
	if targetAgent.WorkspaceID != sourceRun.WorkspaceID {
		return ephemeralCallSetup{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "target_workspace_mismatch", "target_agent_id": targetAgent.AgentID, "source_workspace_id": sourceRun.WorkspaceID, "target_workspace_id": targetAgent.WorkspaceID, "start_invoked": false})
	}
	if messages, listErr := store.ListAgentMessages(ctx, sourceRun.WorkspaceID); listErr == nil {
		for _, message := range messages {
			if message.AgentMessageID == request.RequestAgentMessageID {
				return ephemeralCallSetup{}, rpc.NewHandlerError(rpc.InvalidParams, "agent message already exists", map[string]any{"reason": "request_agent_message_id_conflict", "agent_message_id": request.RequestAgentMessageID, "start_invoked": false})
			}
		}
	} else {
		return ephemeralCallSetup{}, listErr
	}
	for _, rawContextExcerptID := range contextExcerptIDs {
		contextExcerptID := strings.TrimSpace(rawContextExcerptID)
		if contextExcerptID == "" {
			continue
		}
		excerpt, excerptErr := store.GetContextExcerpt(ctx, contextExcerptID)
		if errors.Is(excerptErr, globaldb.ErrNotFound) {
			return ephemeralCallSetup{}, rpc.NewHandlerError(rpc.InvalidParams, excerptErr.Error(), map[string]any{"reason": "unknown_context_excerpt", "context_excerpt_id": contextExcerptID, "start_invoked": false})
		}
		if excerptErr != nil {
			return ephemeralCallSetup{}, excerptErr
		}
		if excerpt.TargetAgentID != "" && excerpt.TargetAgentID != targetAgent.AgentID {
			return ephemeralCallSetup{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "context_excerpt_mismatch", "context_excerpt_id": contextExcerptID, "start_invoked": false})
		}
	}
	run := globaldb.HarnessSession{SessionID: request.SessionID, WorkspaceID: sourceRun.WorkspaceID, AgentID: targetAgent.AgentID, Harness: targetAgent.Harness, Model: targetAgent.Model, Status: "running", Usage: "ephemeral", SourceSessionID: sourceRun.SessionID, SourceAgentID: sourceRun.AgentID}
	if err := store.CreateHarnessSession(ctx, run); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
			return ephemeralCallSetup{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "session_id_conflict", "session_id": request.SessionID, "start_invoked": false})
		}
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return ephemeralCallSetup{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "invalid_ephemeral_session", "call_id": request.CallID, "session_id": request.SessionID, "target_agent_id": targetAgent.AgentID, "start_invoked": false})
		}
		return ephemeralCallSetup{}, err
	}
	return ephemeralCallSetup{SourceRun: sourceRun, TargetAgent: targetAgent, TargetProfile: targetProfile, SessionID: request.SessionID}, nil
}

func createEphemeralRequestMessage(ctx context.Context, store *globaldb.Store, setup ephemeralCallSetup, request ephemeralCall, contextExcerptIDs []string) (globaldb.AgentMessage, error) {
	requestDM, err := store.SendAgentMessage(ctx, globaldb.AgentMessageSendParams{AgentMessageID: request.RequestAgentMessageID, SourceSessionID: setup.SourceRun.SessionID, TargetAgentID: setup.TargetAgent.AgentID, TargetSessionID: setup.SessionID, Body: request.Body, ContextExcerptIDs: contextExcerptIDs})
	if err != nil {
		errText := strings.ToLower(err.Error())
		if strings.Contains(errText, "unique constraint failed") && strings.Contains(errText, "agent_messages.agent_message_id") {
			return globaldb.AgentMessage{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "request_agent_message_id_conflict", "agent_message_id": request.RequestAgentMessageID, "start_invoked": false})
		}
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			if errors.Is(err, globaldb.ErrNotFound) && len(contextExcerptIDs) > 0 {
				contextExcerptID := strings.TrimSpace(contextExcerptIDs[0])
				if contextExcerptID != "" {
					if _, excerptErr := store.GetContextExcerpt(ctx, contextExcerptID); errors.Is(excerptErr, globaldb.ErrNotFound) {
						return globaldb.AgentMessage{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_context_excerpt", "context_excerpt_id": contextExcerptID, "start_invoked": false})
					}
				}
			}
			if errors.Is(err, globaldb.ErrInvalidInput) && len(contextExcerptIDs) > 0 {
				contextExcerptID := strings.TrimSpace(contextExcerptIDs[0])
				if contextExcerptID != "" {
					if excerpt, excerptErr := store.GetContextExcerpt(ctx, contextExcerptID); excerptErr == nil {
						if excerpt.TargetAgentID != "" && excerpt.TargetAgentID != setup.TargetAgent.AgentID {
							return globaldb.AgentMessage{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "context_excerpt_mismatch", "context_excerpt_id": contextExcerptID, "start_invoked": false})
						}
					}
				}
			}
			return globaldb.AgentMessage{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "invalid_ephemeral_request_message", "call_id": request.CallID, "agent_message_id": request.RequestAgentMessageID, "source_session_id": setup.SourceRun.SessionID, "target_session_id": setup.SessionID, "target_agent_id": setup.TargetAgent.AgentID, "start_invoked": false})
		}
		return globaldb.AgentMessage{}, err
	}
	return requestDM, nil
}

func (d *Daemon) runEphemeralHarness(ctx context.Context, store *globaldb.Store, setup ephemeralCallSetup, request ephemeralCall) (ephemeralHarnessResult, error) {
	primaryFolder, _ := lookupPrimaryFolder(ctx, store, setup.SourceRun.WorkspaceID)
	executor, err := d.resolveHarness(HarnessSessionStartRequest{Executor: setup.TargetAgent.Harness}, primaryFolder)
	if err != nil {
		return ephemeralHarnessResult{}, mapHarnessRunError(err)
	}
	options, err := harnessOptionsFromProfile(setup.TargetProfile)
	if err != nil {
		return ephemeralHarnessResult{}, err
	}
	providerRun, err := executor.Start(ctx, ExecutorStartRequest{WorkspaceID: setup.SourceRun.WorkspaceID, RunID: setup.SessionID, SessionID: setup.SessionID, ContextPacket: request.Body, Model: setup.TargetAgent.Model, Prompt: setup.TargetAgent.Prompt, InvocationClass: HarnessInvocationEphemeral, Options: options})
	if err != nil {
		return ephemeralHarnessResult{}, mapHarnessRunError(err)
	}
	providerSessionID := strings.TrimSpace(providerRun.SessionID)
	if providerSessionID == "" {
		providerSessionID = strings.TrimSpace(providerRun.RunID)
	}
	providerRunID := strings.TrimSpace(providerRun.ProviderRunID)
	if providerRunID == "" {
		providerRunID = strings.TrimSpace(providerRun.RunID)
	}
	items, err := executor.Items(ctx, providerSessionID)
	if err != nil {
		return ephemeralHarnessResult{}, err
	}
	if err := appendTimelineItemsAsRunLogMessages(ctx, store, setup.SessionID, items); err != nil {
		return ephemeralHarnessResult{}, err
	}
	invocationMode, usageBucket := harnessModeMetadataFromItems(items)
	providerMetadata, err := json.Marshal(map[string]any{"provider_session_id": providerSessionID, "provider_run_id": providerRunID, "invocation_mode": invocationMode, "usage_bucket": usageBucket})
	if err != nil {
		return ephemeralHarnessResult{}, err
	}
	if err := store.UpdateHarnessSessionProvider(ctx, setup.SessionID, providerSessionID, providerRunID, string(providerMetadata)); err != nil {
		return ephemeralHarnessResult{}, err
	}
	return ephemeralHarnessResult{Items: items, InvocationMode: invocationMode}, nil
}

func completeEphemeralCall(ctx context.Context, store *globaldb.Store, setup ephemeralCallSetup, request ephemeralCall, requestDM globaldb.AgentMessage, result ephemeralHarnessResult, replyAgentMessageID string) (EphemeralCallResponse, bool, error) {
	if result.InvocationMode == string(HarnessInvocationModeBackground) {
		storedRun, err := store.GetHarnessSession(ctx, setup.SessionID)
		if err != nil {
			return EphemeralCallResponse{}, false, err
		}
		return EphemeralCallResponse{Run: storedRun, Request: agentMessageResponse(requestDM)}, false, nil
	}
	replyBody := lastAgentText(result.Items)
	if replyBody == "" {
		replyBody = "completed"
	}
	replyID := strings.TrimSpace(replyAgentMessageID)
	if replyID == "" {
		replyID = request.CallID + "-reply"
	}
	replyDM, err := store.SendAgentMessage(ctx, globaldb.AgentMessageSendParams{AgentMessageID: replyID, SourceSessionID: setup.SessionID, TargetAgentID: setup.SourceRun.AgentID, TargetSessionID: setup.SourceRun.SessionID, Body: replyBody})
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			if errors.Is(err, globaldb.ErrNotFound) {
				return EphemeralCallResponse{}, true, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_reply_target_agent", "target_agent_id": setup.SourceRun.AgentID, "start_invoked": false})
			}
			return EphemeralCallResponse{}, true, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "invalid_ephemeral_reply_message", "call_id": request.CallID, "agent_message_id": replyID, "source_session_id": setup.SessionID, "target_session_id": setup.SourceRun.SessionID, "target_agent_id": setup.SourceRun.AgentID, "start_invoked": false})
		}
		return EphemeralCallResponse{}, true, err
	}
	if err := store.UpdateHarnessSessionStatus(ctx, setup.SessionID, "completed"); err != nil {
		return EphemeralCallResponse{}, true, err
	}
	storedRun, err := store.GetHarnessSession(ctx, setup.SessionID)
	if err != nil {
		return EphemeralCallResponse{}, false, err
	}
	links, _ := json.Marshal([]FinalResponseEvidenceLink{{Kind: "harness_session", ID: storedRun.SessionID}, {Kind: "agent_message", ID: requestDM.AgentMessageID}, {Kind: "agent_message", ID: replyDM.AgentMessageID}})
	if err := store.UpsertFinalResponse(ctx, globaldb.FinalResponse{FinalResponseID: "fr_" + replyID, HarnessSessionID: storedRun.SessionID, WorkspaceID: storedRun.WorkspaceID, TaskID: request.TaskID, ContextPacketID: request.ContextPacketID, ProfileID: storedRun.AgentID, Status: "completed", Text: replyBody, EvidenceLinksJSON: string(links)}); err != nil {
		return EphemeralCallResponse{}, false, err
	}
	return EphemeralCallResponse{Run: storedRun, Request: agentMessageResponse(requestDM), Reply: agentMessageResponse(replyDM)}, false, nil
}

func markEphemeralSessionFailed(ctx context.Context, store *globaldb.Store, sessionID string) {
	_ = store.UpdateHarnessSessionStatus(ctx, sessionID, "failed")
}

func appendTimelineItemsAsRunLogMessages(ctx context.Context, store *globaldb.Store, sessionID string, items []TimelineItem) error {
	next := 1
	if tail, err := store.TailRunLogMessages(ctx, sessionID, 1); err == nil && len(tail) == 1 {
		next = tail[0].Sequence + 1
	}
	for _, item := range items {
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		messageID := strings.TrimSpace(item.ID)
		if messageID == "" {
			messageID = fmt.Sprintf("%s-message-%d", sessionID, next)
		}
		if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: messageID, SessionID: sessionID, Sequence: next, Role: "assistant", Status: "completed", Parts: []globaldb.RunLogMessagePart{{PartID: messageID + "-part-1", Sequence: 1, Kind: "text", Text: text}}}); err != nil {
			return err
		}
		next++
	}
	return nil
}

func lastAgentText(items []TimelineItem) string {
	for i := len(items) - 1; i >= 0; i-- {
		if strings.TrimSpace(items[i].Text) != "" {
			return strings.TrimSpace(items[i].Text)
		}
	}
	return ""
}
