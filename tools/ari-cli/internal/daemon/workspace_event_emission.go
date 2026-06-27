package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

func appendHarnessSessionWorkspaceEvent(ctx context.Context, store *globaldb.Store, run HarnessSession, eventType, status, finalResponseID string, attentionRequired bool) error {
	if store == nil {
		return nil
	}
	event := globaldb.NewHarnessSessionWorkspaceEvent(globaldb.HarnessSessionWorkspaceEventParams{WorkspaceID: run.WorkspaceID, SessionID: run.HarnessSessionID, Harness: run.Executor, TaskID: run.TaskID, EventType: eventType, Status: status, FinalResponseID: finalResponseID, AttentionRequired: attentionRequired})
	_, err := store.AppendWorkspaceEvent(ctx, event)
	return err
}

func appendHarnessRuntimeWorkspaceEvents(ctx context.Context, store *globaldb.Store, run HarnessSession, events []HarnessRuntimeEvent) error {
	if store == nil {
		return nil
	}
	for _, runtimeEvent := range events {
		kind := strings.TrimSpace(runtimeEvent.Kind)
		if kind == "" {
			continue
		}
		sessionID := strings.TrimSpace(runtimeEvent.SessionID)
		if sessionID == "" {
			sessionID = strings.TrimSpace(run.HarnessSessionID)
		}
		runtimeEventID := strings.TrimSpace(runtimeEvent.EventID)
		if runtimeEventID == "" {
			runtimeEventID = fmt.Sprintf("%s:event-%d", sessionID, runtimeEvent.Sequence)
		}
		event, err := globaldb.NewHarnessRuntimeWorkspaceEvent(globaldb.HarnessRuntimeWorkspaceEventParams{EventID: runtimeEventID, WorkspaceID: run.WorkspaceID, SessionID: sessionID, RootSessionID: run.HarnessSessionID, Kind: kind, Sequence: runtimeEvent.Sequence, Payload: runtimeEvent.Payload, RunID: runtimeEvent.RunID, ProviderKind: runtimeEvent.ProviderKind, CreatedAt: runtimeEvent.CreatedAt})
		if err != nil {
			return err
		}
		_, err = store.AppendWorkspaceEvent(ctx, event)
		if err != nil {
			return err
		}
		if kind == string(HarnessEventApproval) {
			if err := appendSessionNeedsInputWorkspaceEvent(ctx, store, run, sessionID, runtimeEventID); err != nil {
				return err
			}
		}
	}
	return nil
}

// appendSessionNeedsInputWorkspaceEvent normalizes a harness approval signal
// into the Ari-level session.needs_input fact so orchestrators and humans can
// subscribe to "a session is blocked on input" without provider ontology.
func appendSessionNeedsInputWorkspaceEvent(ctx context.Context, store *globaldb.Store, run HarnessSession, sessionID, runtimeEventID string) error {
	if store == nil {
		return nil
	}
	event := globaldb.NewSessionNeedsInputWorkspaceEvent(globaldb.SessionNeedsInputWorkspaceEventParams{WorkspaceID: run.WorkspaceID, SessionID: sessionID, RootSessionID: run.HarnessSessionID, Harness: run.Executor, HarnessEventID: runtimeEventID})
	_, err := store.AppendWorkspaceEvent(ctx, event)
	return err
}

// appendSessionIdleWorkspaceEvent records that a sticky session finished a
// turn and is available for the next input — the durable fact behind
// wake-when-idle orchestration. Ephemeral sessions terminate instead of
// idling and must not emit it.
func appendSessionIdleWorkspaceEvent(ctx context.Context, store *globaldb.Store, run HarnessSession) error {
	if store == nil {
		return nil
	}
	event := globaldb.NewSessionIdleWorkspaceEvent(globaldb.HarnessSessionWorkspaceEventParams{WorkspaceID: run.WorkspaceID, SessionID: run.HarnessSessionID, Harness: run.Executor})
	_, err := store.AppendWorkspaceEvent(ctx, event)
	return err
}

// appendFanoutWorkerWorkspaceEvent records a fanout worker fact: the daemon
// derives the event and both projections (member row, terminal inbox item),
// then the store commits them in one transaction so event history and
// materialized projections can never diverge.
func appendFanoutWorkerWorkspaceEvent(ctx context.Context, store *globaldb.Store, member globaldb.FanoutMember, eventType, producerID, causationID, finalResponseID string, attentionRequired bool) error {
	if store == nil {
		return nil
	}
	group, err := store.GetFanoutGroup(ctx, member.FanoutGroupID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(group.WorkspaceID) != strings.TrimSpace(member.WorkspaceID) {
		return fmt.Errorf("%w: fanout group workspace does not match worker event", globaldb.ErrInvalidInput)
	}
	if strings.TrimSpace(causationID) == "" && eventType != globaldb.WorkspaceEventWorkerCompleted {
		causationID = member.RequestAgentMessageID
	}
	event := globaldb.NewFanoutWorkerWorkspaceEvent(globaldb.FanoutWorkerWorkspaceEventParams{WorkspaceID: member.WorkspaceID, EventType: eventType, WorkerSessionID: member.WorkerSessionID, ProducerID: producerID, CausationID: causationID, FinalResponseID: finalResponseID, AttentionRequired: attentionRequired, FanoutGroupID: member.FanoutGroupID, FanoutMemberID: member.FanoutMemberID, SourceSessionID: group.SourceSessionID, SourceAgentID: group.SourceAgentID, TargetProfileID: member.TargetProfileID, RequestAgentMessageID: member.RequestAgentMessageID})
	_, err = store.AppendWorkspaceEvent(ctx, event)
	return err
}
