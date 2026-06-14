package globaldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

type FanoutGroup struct {
	FanoutGroupID         string
	WorkspaceID           string
	SourceSessionID       string
	SourceAgentID         string
	RequestAgentMessageID string
	Status                string
	Body                  string
	CreatedAt             string
	UpdatedAt             string
}

type FanoutMember struct {
	FanoutMemberID        string
	FanoutGroupID         string
	WorkspaceID           string
	WorkerSessionID       string
	TargetProfileID       string
	RequestAgentMessageID string
	ReplyAgentMessageID   string
	FinalResponseID       string
	Status                string
	CreatedAt             string
	UpdatedAt             string
}

func (s *Store) CreateFanoutGroup(ctx context.Context, group FanoutGroup) error {
	if strings.TrimSpace(group.FanoutGroupID) == "" || strings.TrimSpace(group.WorkspaceID) == "" || strings.TrimSpace(group.SourceSessionID) == "" {
		return ErrInvalidInput
	}
	status := defaultString(strings.TrimSpace(group.Status), "running")
	_, err := s.db.ExecContext(ctx, `INSERT INTO fanout_groups (fanout_group_id, workspace_id, source_session_id, source_agent_id, request_agent_message_id, status, body, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, strings.TrimSpace(group.FanoutGroupID), strings.TrimSpace(group.WorkspaceID), strings.TrimSpace(group.SourceSessionID), strings.TrimSpace(group.SourceAgentID), strings.TrimSpace(group.RequestAgentMessageID), status, strings.TrimSpace(group.Body))
	return err
}

func (s *Store) GetFanoutGroup(ctx context.Context, groupID string) (FanoutGroup, error) {
	var group FanoutGroup
	err := s.db.QueryRowContext(ctx, `SELECT fanout_group_id, workspace_id, source_session_id, source_agent_id, request_agent_message_id, status, body, created_at, updated_at FROM fanout_groups WHERE fanout_group_id = ?`, strings.TrimSpace(groupID)).Scan(&group.FanoutGroupID, &group.WorkspaceID, &group.SourceSessionID, &group.SourceAgentID, &group.RequestAgentMessageID, &group.Status, &group.Body, &group.CreatedAt, &group.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return FanoutGroup{}, ErrNotFound
	}
	if err != nil {
		return FanoutGroup{}, err
	}
	return group, nil
}

// ProjectFanoutMember materializes a fanout member row from workspace event
// history. Event facts win for status; identity and evidence links are only
// filled in, never blanked, so replayed/late events cannot erase linkage.
func (s *Store) ProjectFanoutMember(ctx context.Context, member FanoutMember) error {
	if err := validateFanoutMemberProjection(member); err != nil {
		return err
	}
	return upsertFanoutMemberWithQueries(ctx, s.sqlcQueries(), member)
}

func validateFanoutMemberProjection(member FanoutMember) error {
	if strings.TrimSpace(member.FanoutMemberID) == "" || strings.TrimSpace(member.FanoutGroupID) == "" || strings.TrimSpace(member.WorkspaceID) == "" || strings.TrimSpace(member.WorkerSessionID) == "" {
		return ErrInvalidInput
	}
	return nil
}

func upsertFanoutMemberWithQueries(ctx context.Context, queries *dbsqlc.Queries, member FanoutMember) error {
	status := defaultString(strings.TrimSpace(member.Status), "running")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	createdAt := defaultString(strings.TrimSpace(member.CreatedAt), now)
	updatedAt := defaultString(strings.TrimSpace(member.UpdatedAt), now)
	if err := queries.UpsertFanoutMember(ctx, dbsqlc.UpsertFanoutMemberParams{FanoutMemberID: strings.TrimSpace(member.FanoutMemberID), FanoutGroupID: strings.TrimSpace(member.FanoutGroupID), WorkspaceID: strings.TrimSpace(member.WorkspaceID), WorkerSessionID: strings.TrimSpace(member.WorkerSessionID), TargetProfileID: strings.TrimSpace(member.TargetProfileID), RequestAgentMessageID: strings.TrimSpace(member.RequestAgentMessageID), ReplyAgentMessageID: strings.TrimSpace(member.ReplyAgentMessageID), FinalResponseID: strings.TrimSpace(member.FinalResponseID), Status: status, CreatedAt: createdAt, UpdatedAt: updatedAt}); err != nil {
		return fmt.Errorf("upsert fanout member %q: %w", member.FanoutMemberID, err)
	}
	return nil
}

func (s *Store) GetFanoutMemberByWorkerSession(ctx context.Context, workerSessionID string) (FanoutMember, error) {
	workerSessionID = strings.TrimSpace(workerSessionID)
	if workerSessionID == "" {
		return FanoutMember{}, ErrInvalidInput
	}
	var member FanoutMember
	err := s.db.QueryRowContext(ctx, `SELECT fanout_member_id, fanout_group_id, workspace_id, worker_session_id, target_profile_id, request_agent_message_id, reply_agent_message_id, final_response_id, status, created_at, updated_at FROM fanout_members WHERE worker_session_id = ?`, workerSessionID).Scan(&member.FanoutMemberID, &member.FanoutGroupID, &member.WorkspaceID, &member.WorkerSessionID, &member.TargetProfileID, &member.RequestAgentMessageID, &member.ReplyAgentMessageID, &member.FinalResponseID, &member.Status, &member.CreatedAt, &member.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return FanoutMember{}, ErrNotFound
	}
	if err != nil {
		return FanoutMember{}, err
	}
	return member, nil
}

func (s *Store) ListFanoutMembers(ctx context.Context, groupID string) ([]FanoutMember, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT fanout_member_id, fanout_group_id, workspace_id, worker_session_id, target_profile_id, request_agent_message_id, reply_agent_message_id, final_response_id, status, created_at, updated_at FROM fanout_members WHERE fanout_group_id = ? ORDER BY created_at ASC, fanout_member_id ASC`, strings.TrimSpace(groupID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var members []FanoutMember
	for rows.Next() {
		var member FanoutMember
		if err := rows.Scan(&member.FanoutMemberID, &member.FanoutGroupID, &member.WorkspaceID, &member.WorkerSessionID, &member.TargetProfileID, &member.RequestAgentMessageID, &member.ReplyAgentMessageID, &member.FinalResponseID, &member.Status, &member.CreatedAt, &member.UpdatedAt); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (s *Store) ListFanoutMembersByWorkspace(ctx context.Context, workspaceID string) ([]FanoutMember, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT fanout_member_id, fanout_group_id, workspace_id, worker_session_id, target_profile_id, request_agent_message_id, reply_agent_message_id, final_response_id, status, created_at, updated_at FROM fanout_members WHERE workspace_id = ? ORDER BY created_at ASC, fanout_member_id ASC`, strings.TrimSpace(workspaceID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var members []FanoutMember
	for rows.Next() {
		var member FanoutMember
		if err := rows.Scan(&member.FanoutMemberID, &member.FanoutGroupID, &member.WorkspaceID, &member.WorkerSessionID, &member.TargetProfileID, &member.RequestAgentMessageID, &member.ReplyAgentMessageID, &member.FinalResponseID, &member.Status, &member.CreatedAt, &member.UpdatedAt); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
