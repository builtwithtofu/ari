package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

func (d *Daemon) fanoutSession(ctx context.Context, store *globaldb.Store, req AgentMessageSendRequest) (AgentMessageSendResponse, error) {
	if len(req.TargetProfileIDs) == 0 {
		return sendAgentMessage(ctx, store, req)
	}
	sourceRun, err := store.GetHarnessSession(ctx, req.SourceSessionID)
	if err != nil {
		return AgentMessageSendResponse{}, err
	}
	if err := requireWorkspaceCanStartRuntime(ctx, store, sourceRun.WorkspaceID); err != nil {
		return AgentMessageSendResponse{}, err
	}
	seenProfiles := make(map[string]struct{}, len(req.TargetProfileIDs))
	for _, rawProfileID := range req.TargetProfileIDs {
		profileID := strings.TrimSpace(rawProfileID)
		if profileID == "" {
			continue
		}
		if _, ok := seenProfiles[profileID]; ok {
			return AgentMessageSendResponse{}, fmt.Errorf("duplicate target profile %q", profileID)
		}
		seenProfiles[profileID] = struct{}{}
	}
	groupID := strings.TrimSpace(req.AgentMessageID)
	if groupID == "" {
		generated, genErr := newAriULID()
		if genErr != nil {
			return AgentMessageSendResponse{}, genErr
		}
		groupID = "fg_" + generated
	}
	if err := store.CreateFanoutGroup(ctx, globaldb.FanoutGroup{FanoutGroupID: groupID, WorkspaceID: sourceRun.WorkspaceID, SourceSessionID: sourceRun.SessionID, SourceAgentID: sourceRun.AgentID, RequestAgentMessageID: strings.TrimSpace(req.AgentMessageID), Body: req.Body}); err != nil {
		return AgentMessageSendResponse{}, err
	}
	resp := AgentMessageSendResponse{FanoutGroupID: groupID, FanoutMembers: make([]FanoutMemberResponse, 0, len(req.TargetProfileIDs))}
	for i, rawProfileID := range req.TargetProfileIDs {
		profileID := strings.TrimSpace(rawProfileID)
		if profileID == "" {
			continue
		}
		memberID := groupID + "-m" + stableRuntimeAgentIDSegment(profileID)
		callID := groupID + "-c" + stableRuntimeAgentIDSegment(profileID)
		callResp, callErr := d.callEphemeral(ctx, store, EphemeralCallRequest{CallID: callID, SessionID: callID + "-run", WorkspaceID: sourceRun.WorkspaceID, SourceSessionID: sourceRun.SessionID, TargetAgentID: profileID, Body: req.Body, ContextExcerptIDs: req.ContextExcerptIDs, FanoutGroupID: groupID, FanoutMemberID: memberID, SuppressReply: true})
		if callErr != nil {
			return AgentMessageSendResponse{}, callErr
		}
		resp.FanoutMembers = append(resp.FanoutMembers, FanoutMemberResponse{FanoutMemberID: memberID, TargetProfileID: profileID, Session: callResp.Run, Request: callResp.Request})
		if i == 0 {
			resp.AgentMessage = callResp.Request
		}
	}
	return resp, nil
}
