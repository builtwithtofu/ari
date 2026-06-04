package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
		_ = newHarnessLifecycle(store).markFailed(ctx, setup.SessionID)
		return EphemeralCallResponse{}, err
	}
	if strings.TrimSpace(req.FanoutMemberID) != "" || strings.TrimSpace(req.FanoutGroupID) != "" {
		member := globaldb.FanoutMember{FanoutMemberID: strings.TrimSpace(req.FanoutMemberID), FanoutGroupID: strings.TrimSpace(req.FanoutGroupID), WorkspaceID: setup.SourceRun.WorkspaceID, WorkerSessionID: setup.SessionID, TargetProfileID: setup.TargetAgent.AgentID, RequestAgentMessageID: requestDM.AgentMessageID, Status: "running"}
		if err := appendFanoutWorkerWorkspaceEvent(ctx, store, member, workspaceEventWorkerStarted, setup.SourceRun.SessionID, requestDM.AgentMessageID, "", false); err != nil {
			_ = newHarnessLifecycle(store).markFailed(ctx, setup.SessionID)
			return EphemeralCallResponse{}, err
		}
	}
	d.startHarnessLifecycleWork(func(runCtx context.Context) {
		d.completeEphemeralCallAsync(runCtx, store, setup, request, requestDM, req.ReplyAgentMessageID, req.SuppressReply)
	})
	return EphemeralCallResponse{Run: setup.initialRun(), Request: agentMessageResponse(requestDM)}, nil
}

func (d *Daemon) completeEphemeralCallAsync(ctx context.Context, store *globaldb.Store, setup ephemeralCallSetup, request ephemeralCall, requestDM globaldb.AgentMessage, replyAgentMessageID string, suppressReply bool) {
	result, err := d.runEphemeralHarness(ctx, store, setup, request)
	if err != nil {
		if ephemeralSessionStatus(ctx, store, setup.SessionID) == "stopped" {
			return
		}
		markEphemeralFailedWithFinalResponse(context.Background(), store, setup, request, ephemeralFailureText(err))
		markFanoutMemberForWorkerSession(context.Background(), store, setup.SessionID, "failed", "", "fr_"+request.CallID+"-failed")
		return
	}
	completed, markFailed, err := completeEphemeralCall(context.Background(), store, setup, request, requestDM, result, replyAgentMessageID, suppressReply)
	if err != nil && markFailed {
		markEphemeralFailedWithFinalResponse(context.Background(), store, setup, request, ephemeralFailureText(err))
		markFanoutMemberForWorkerSession(context.Background(), store, setup.SessionID, "failed", "", "fr_"+request.CallID+"-failed")
		return
	}
	if err == nil && completed.Run.Status == "completed" {
		markFanoutMemberForWorkerSession(context.Background(), store, setup.SessionID, "completed", completed.Reply.AgentMessageID, completed.FinalResponse.FinalResponseID)
	}
}

func markFanoutMemberForWorkerSession(ctx context.Context, store *globaldb.Store, workerSessionID, status, replyAgentMessageID, finalResponseID string) {
	member, err := store.GetFanoutMemberByWorkerSession(ctx, workerSessionID)
	if err != nil {
		// Not a fanout worker, or projection lookup failed. The worker's harness
		// session/final response remain the source records, so do not fail worker
		// completion over fanout projection state.
		return
	}
	eventType := workspaceEventTypeForFanoutWorkerStatus(status)
	if eventType == "" {
		return
	}
	_ = appendFanoutWorkerWorkspaceEvent(ctx, store, member, eventType, workerSessionID, replyAgentMessageID, finalResponseID, status == "failed")
}

func ephemeralSessionStatus(ctx context.Context, store *globaldb.Store, sessionID string) string {
	run, err := store.GetHarnessSession(ctx, sessionID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(run.Status)
}

func markEphemeralFailedWithFinalResponse(ctx context.Context, store *globaldb.Store, setup ephemeralCallSetup, request ephemeralCall, text string) {
	newHarnessLifecycle(store).markFailedWithFinalResponse(ctx, setup.SessionID, globaldb.FinalResponse{FinalResponseID: "fr_" + request.CallID + "-failed", WorkspaceID: setup.SourceRun.WorkspaceID, TaskID: request.TaskID, ContextPacketID: request.ContextPacketID, ProfileID: setup.TargetAgent.AgentID, Text: text})
}

type ephemeralCall struct {
	CallID                string
	WorkspaceID           string
	SourceSessionID       string
	TargetAgentID         string
	Body                  string
	SessionID             string
	TaskID                string
	ContextPacketID       string
	RequestAgentMessageID string
	Timeout               time.Duration
}

type ephemeralCallSetup struct {
	SourceRun     globaldb.HarnessSession
	TargetAgent   globaldb.HarnessSessionConfig
	TargetProfile Profile
	SessionID     string
}

func (setup ephemeralCallSetup) initialRun() globaldb.HarnessSession {
	return globaldb.HarnessSession{SessionID: setup.SessionID, WorkspaceID: setup.SourceRun.WorkspaceID, AgentID: setup.TargetAgent.AgentID, Harness: setup.TargetAgent.Harness, Model: setup.TargetAgent.Model, Status: "running", Usage: globaldb.HarnessSessionUsageEphemeral, SourceSessionID: setup.SourceRun.SessionID, SourceAgentID: setup.SourceRun.AgentID}
}

type ephemeralHarnessResult struct {
	Items          []TimelineItem
	InvocationMode string
	FinalText      string
}

func newEphemeralCall(req EphemeralCallRequest) (ephemeralCall, error) {
	callID := strings.TrimSpace(req.CallID)
	if callID == "" {
		generated, err := newAriULID()
		if err != nil {
			return ephemeralCall{}, err
		}
		callID = "ec_" + generated
	}
	sourceSessionID := strings.TrimSpace(req.SourceSessionID)
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	targetAgentID := strings.TrimSpace(req.TargetAgentID)
	body := strings.TrimSpace(req.Body)
	if sourceSessionID == "" || targetAgentID == "" || body == "" {
		missingField := ""
		switch {
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
	timeout := time.Duration(0)
	if req.TimeoutMS > 0 {
		timeout = time.Duration(req.TimeoutMS) * time.Millisecond
	}
	return ephemeralCall{CallID: callID, WorkspaceID: workspaceID, SourceSessionID: sourceSessionID, TargetAgentID: targetAgentID, Body: body, SessionID: sessionID, TaskID: callID, ContextPacketID: callID + "-context", RequestAgentMessageID: callID + "-request", Timeout: timeout}, nil
}

func createEphemeralSession(ctx context.Context, store *globaldb.Store, request ephemeralCall, contextExcerptIDs []string) (ephemeralCallSetup, error) {
	sourceRun, err := store.GetHarnessSession(ctx, request.SourceSessionID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return ephemeralCallSetup{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_source_session", "source_session_id": request.SourceSessionID, "start_invoked": false})
		}
		return ephemeralCallSetup{}, err
	}
	if request.WorkspaceID != "" && sourceRun.WorkspaceID != request.WorkspaceID {
		return ephemeralCallSetup{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "source_workspace_mismatch", "source_session_id": request.SourceSessionID, "source_workspace_id": sourceRun.WorkspaceID, "workspace_id": request.WorkspaceID, "start_invoked": false})
	}
	if err := requireWorkspaceCanStartRuntime(ctx, store, sourceRun.WorkspaceID); err != nil {
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
	setup := ephemeralCallSetup{SourceRun: sourceRun, TargetAgent: targetAgent, TargetProfile: targetProfile, SessionID: request.SessionID}
	run := setup.initialRun()
	if err := store.CreateHarnessSession(ctx, run); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
			return ephemeralCallSetup{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "session_id_conflict", "session_id": request.SessionID, "start_invoked": false})
		}
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return ephemeralCallSetup{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "invalid_ephemeral_session", "call_id": request.CallID, "session_id": request.SessionID, "target_agent_id": targetAgent.AgentID, "start_invoked": false})
		}
		return ephemeralCallSetup{}, err
	}
	return setup, nil
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
	profile := setup.TargetProfile
	profile.ProfileID = setup.TargetAgent.AgentID
	profile.WorkspaceID = setup.TargetAgent.WorkspaceID
	profile.Name = setup.TargetAgent.Name
	profile.Harness = setup.TargetAgent.Harness
	profile.Model = setup.TargetAgent.Model
	profile.Prompt = setup.TargetAgent.Prompt
	profile.InvocationClass = HarnessInvocationEphemeral
	selected, err := resolveProfileAuthSlot(ctx, store, executor, setup.TargetAgent.Harness, profile)
	if err != nil {
		return ephemeralHarnessResult{}, mapHarnessRunError(err)
	}
	profile.AuthSlotID = selected
	packet := ContextPacket{ID: request.ContextPacketID, WorkspaceID: setup.SourceRun.WorkspaceID, TaskID: request.TaskID, Sections: []ContextSection{{Name: "message", Content: request.Body}}}
	var runCtx context.Context
	var cancel context.CancelFunc
	if request.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, request.Timeout)
	} else {
		runCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()
	executor = &trackedHarnessExecutor{Executor: executor, daemon: d, store: store, workspaceID: setup.SourceRun.WorkspaceID, sessionID: setup.SessionID, cancel: cancel}
	projection, err := d.authProjectionForStart(runCtx, store, setup.TargetAgent.Harness, packet.WorkspaceID, profile.AuthSlotID)
	if err != nil {
		return ephemeralHarnessResult{}, mapHarnessRunError(err)
	}
	result, err := StartExecutorRunResultWithProjection(runCtx, executor, packet, setup.SessionID, projection, profile)
	if err != nil {
		return ephemeralHarnessResult{}, mapHarnessRunError(err)
	}
	if err := newHarnessLifecycle(store).persistExistingEphemeralResult(ctx, result, profile); err != nil {
		return ephemeralHarnessResult{}, err
	}
	items := result.Items
	invocationMode, _ := harnessModeMetadataFromItems(items)
	finalText := ""
	if result.FinalResponse != nil {
		finalText = strings.TrimSpace(result.FinalResponse.Text)
	}
	return ephemeralHarnessResult{Items: items, InvocationMode: invocationMode, FinalText: finalText}, nil
}

func ephemeralFailureText(err error) string {
	if err == nil {
		return "failed"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timed out"
	}
	var handlerErr *rpc.HandlerError
	if errors.As(err, &handlerErr) {
		if data, ok := handlerErr.Data.(map[string]any); ok {
			if reason, ok := data["reason"].(string); ok && strings.TrimSpace(reason) != "" {
				return strings.TrimSpace(reason) + ": " + handlerErr.Error()
			}
		}
	}
	text := strings.TrimSpace(err.Error())
	if text == "" {
		return "failed"
	}
	return text
}

func completeEphemeralCall(ctx context.Context, store *globaldb.Store, setup ephemeralCallSetup, request ephemeralCall, requestDM globaldb.AgentMessage, result ephemeralHarnessResult, replyAgentMessageID string, suppressReply bool) (EphemeralCallResponse, bool, error) {
	if result.InvocationMode == string(HarnessInvocationModeBackground) {
		storedRun, err := store.GetHarnessSession(ctx, setup.SessionID)
		if err != nil {
			return EphemeralCallResponse{}, false, err
		}
		return EphemeralCallResponse{Run: storedRun, Request: agentMessageResponse(requestDM)}, false, nil
	}
	replyBody := strings.TrimSpace(result.FinalText)
	if replyBody == "" {
		replyBody = lastAgentText(result.Items)
	}
	if replyBody == "" {
		replyBody = "completed"
	}
	if suppressReply {
		links, _ := json.Marshal([]FinalResponseEvidenceLink{{Kind: "harness_session", ID: setup.SessionID}, {Kind: "agent_message", ID: requestDM.AgentMessageID}})
		final := globaldb.FinalResponse{FinalResponseID: "fr_" + request.CallID + "-completed", HarnessSessionID: setup.SessionID, WorkspaceID: setup.SourceRun.WorkspaceID, TaskID: request.TaskID, ContextPacketID: request.ContextPacketID, ProfileID: setup.TargetAgent.AgentID, Status: "completed", Text: replyBody, EvidenceLinksJSON: string(links)}
		if err := newHarnessLifecycle(store).markCompletedWithFinalResponse(ctx, setup.SessionID, final); err != nil {
			return EphemeralCallResponse{}, true, err
		}
		storedRun, err := store.GetHarnessSession(ctx, setup.SessionID)
		if err != nil {
			return EphemeralCallResponse{}, false, err
		}
		return EphemeralCallResponse{Run: storedRun, Request: agentMessageResponse(requestDM), FinalResponse: final}, false, nil
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
	links, _ := json.Marshal([]FinalResponseEvidenceLink{{Kind: "harness_session", ID: setup.SessionID}, {Kind: "agent_message", ID: requestDM.AgentMessageID}, {Kind: "agent_message", ID: replyDM.AgentMessageID}})
	final := globaldb.FinalResponse{FinalResponseID: "fr_" + replyID, HarnessSessionID: setup.SessionID, WorkspaceID: setup.SourceRun.WorkspaceID, TaskID: request.TaskID, ContextPacketID: request.ContextPacketID, ProfileID: setup.TargetAgent.AgentID, Status: "completed", Text: replyBody, EvidenceLinksJSON: string(links)}
	if err := newHarnessLifecycle(store).markCompletedWithFinalResponse(ctx, setup.SessionID, final); err != nil {
		return EphemeralCallResponse{}, true, err
	}
	storedRun, err := store.GetHarnessSession(ctx, setup.SessionID)
	if err != nil {
		return EphemeralCallResponse{}, false, err
	}
	return EphemeralCallResponse{Run: storedRun, Request: agentMessageResponse(requestDM), Reply: agentMessageResponse(replyDM)}, false, nil
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
