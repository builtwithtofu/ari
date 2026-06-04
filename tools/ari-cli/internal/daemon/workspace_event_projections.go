package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

// fanout_members is a materialized projection: every fanout worker workspace
// event synchronously upserts the member row (projectFanoutWorkerMember), so
// reads go straight to the table.
func fanoutMemberProjectionForGroup(ctx context.Context, store *globaldb.Store, group globaldb.FanoutGroup) ([]globaldb.FanoutMember, error) {
	return store.ListFanoutMembers(ctx, group.FanoutGroupID)
}

func fanoutMemberProjectionForWorkspace(ctx context.Context, store *globaldb.Store, workspaceID string) ([]globaldb.FanoutMember, error) {
	return store.ListFanoutMembersByWorkspace(ctx, workspaceID)
}

// fanoutMembersFromWorkspaceEvents rebuilds fanout member projections by
// replaying workspace event history. It is the rebuild/repair primitive that
// proves the materialized fanout_members rows derive from events; it is not a
// serving read path.
func fanoutMembersFromWorkspaceEvents(ctx context.Context, store *globaldb.Store, workspaceID, groupID string) ([]globaldb.FanoutMember, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	groupID = strings.TrimSpace(groupID)
	if workspaceID == "" {
		return nil, nil
	}
	const pageSize = 500
	sequence := int64(0)
	projected := map[string]globaldb.FanoutMember{}
	order := make([]string, 0)
	for {
		events, err := store.ListWorkspaceEventsAfterSequence(ctx, workspaceID, sequence, pageSize)
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			break
		}
		for _, event := range events {
			sequence = event.Sequence
			if !isFanoutWorkerWorkspaceEvent(event.EventType) || strings.TrimSpace(event.CorrelationID) == "" || (groupID != "" && event.CorrelationID != groupID) {
				continue
			}
			payload := workspaceEventStringPayload(event.PayloadJSON)
			memberID := strings.TrimSpace(payload["fanout_member_id"])
			workerSessionID := strings.TrimSpace(event.SubjectID)
			key := memberID
			if key == "" {
				key = workerSessionID
			}
			if key == "" {
				continue
			}
			member, ok := projected[key]
			if !ok {
				member = globaldb.FanoutMember{FanoutGroupID: event.CorrelationID, WorkspaceID: workspaceID}
				order = append(order, key)
			}
			if memberID != "" {
				member.FanoutMemberID = memberID
			}
			if workerSessionID != "" {
				member.WorkerSessionID = workerSessionID
			}
			if targetProfileID := strings.TrimSpace(payload["target_profile_id"]); targetProfileID != "" {
				member.TargetProfileID = targetProfileID
			}
			if member.CreatedAt == "" {
				member.CreatedAt = event.CreatedAt.Format(time.RFC3339Nano)
			}
			member.UpdatedAt = event.CreatedAt.Format(time.RFC3339Nano)
			member.Status = workerEventStatus(event.EventType)
			switch event.EventType {
			case workspaceEventWorkerStarted:
				if causationID := strings.TrimSpace(event.CausationID); causationID != "" {
					member.RequestAgentMessageID = causationID
				}
			case workspaceEventWorkerCompleted:
				if causationID := strings.TrimSpace(event.CausationID); causationID != "" {
					member.ReplyAgentMessageID = causationID
				}
			}
			if finalResponseID := finalResponseIDFromWorkspaceEventRef(event.PayloadRefJSON); finalResponseID != "" {
				member.FinalResponseID = finalResponseID
			}
			projected[key] = member
		}
		if len(events) < pageSize {
			break
		}
	}
	members := make([]globaldb.FanoutMember, 0, len(order))
	for _, key := range order {
		members = append(members, projected[key])
	}
	return members, nil
}

func isFanoutWorkerWorkspaceEvent(eventType string) bool {
	switch eventType {
	case workspaceEventWorkerStarted, workspaceEventWorkerCompleted, workspaceEventWorkerFailed, workspaceEventWorkerStopped:
		return true
	default:
		return false
	}
}

func workspaceEventStringPayload(raw string) map[string]string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(payload))
	for key, value := range payload {
		if text, ok := value.(string); ok {
			out[key] = text
		}
	}
	return out
}

func finalResponseIDFromWorkspaceEventRef(raw string) string {
	ref := workspaceEventStringPayload(raw)
	if ref["kind"] != "final_response" {
		return ""
	}
	return strings.TrimSpace(ref["id"])
}
