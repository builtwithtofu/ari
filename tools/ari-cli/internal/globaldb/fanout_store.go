package globaldb

import (
	"context"
	"strings"
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

type StickyInboxItem struct {
	InboxItemID     string
	WorkspaceID     string
	TargetSessionID string
	FanoutGroupID   string
	FanoutMemberID  string
	WorkerSessionID string
	FinalResponseID string
	Kind            string
	Status          string
	Summary         string
	CreatedAt       string
	UpdatedAt       string
}

func (s *Store) CreateFanoutGroup(ctx context.Context, group FanoutGroup) error {
	if strings.TrimSpace(group.FanoutGroupID) == "" || strings.TrimSpace(group.WorkspaceID) == "" || strings.TrimSpace(group.SourceSessionID) == "" {
		return ErrInvalidInput
	}
	status := defaultString(strings.TrimSpace(group.Status), "running")
	_, err := s.db.ExecContext(ctx, `INSERT INTO fanout_groups (fanout_group_id, workspace_id, source_session_id, source_agent_id, request_agent_message_id, status, body, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, strings.TrimSpace(group.FanoutGroupID), strings.TrimSpace(group.WorkspaceID), strings.TrimSpace(group.SourceSessionID), strings.TrimSpace(group.SourceAgentID), strings.TrimSpace(group.RequestAgentMessageID), status, strings.TrimSpace(group.Body))
	return err
}

func (s *Store) AddFanoutMember(ctx context.Context, member FanoutMember) error {
	if strings.TrimSpace(member.FanoutMemberID) == "" || strings.TrimSpace(member.FanoutGroupID) == "" || strings.TrimSpace(member.WorkspaceID) == "" || strings.TrimSpace(member.WorkerSessionID) == "" {
		return ErrInvalidInput
	}
	status := defaultString(strings.TrimSpace(member.Status), "running")
	_, err := s.db.ExecContext(ctx, `INSERT INTO fanout_members (fanout_member_id, fanout_group_id, workspace_id, worker_session_id, target_profile_id, request_agent_message_id, reply_agent_message_id, final_response_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, strings.TrimSpace(member.FanoutMemberID), strings.TrimSpace(member.FanoutGroupID), strings.TrimSpace(member.WorkspaceID), strings.TrimSpace(member.WorkerSessionID), strings.TrimSpace(member.TargetProfileID), strings.TrimSpace(member.RequestAgentMessageID), strings.TrimSpace(member.ReplyAgentMessageID), strings.TrimSpace(member.FinalResponseID), status)
	return err
}

func (s *Store) UpdateFanoutMemberStatus(ctx context.Context, memberID, status, replyAgentMessageID, finalResponseID string) error {
	if strings.TrimSpace(memberID) == "" || strings.TrimSpace(status) == "" {
		return ErrInvalidInput
	}
	result, err := s.db.ExecContext(ctx, `UPDATE fanout_members SET status = ?, reply_agent_message_id = COALESCE(NULLIF(?, ''), reply_agent_message_id), final_response_id = COALESCE(NULLIF(?, ''), final_response_id), updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE fanout_member_id = ?`, strings.TrimSpace(status), strings.TrimSpace(replyAgentMessageID), strings.TrimSpace(finalResponseID), strings.TrimSpace(memberID))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateFanoutMemberStatusAndInboxByWorkerSession(ctx context.Context, workerSessionID, status, replyAgentMessageID, finalResponseID, summary string) error {
	workerSessionID = strings.TrimSpace(workerSessionID)
	status = strings.TrimSpace(status)
	if workerSessionID == "" || status == "" {
		return ErrInvalidInput
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()
	result, err := conn.ExecContext(ctx, `UPDATE fanout_members SET status = ?, reply_agent_message_id = COALESCE(NULLIF(?, ''), reply_agent_message_id), final_response_id = COALESCE(NULLIF(?, ''), final_response_id), updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE worker_session_id = ?`, status, strings.TrimSpace(replyAgentMessageID), strings.TrimSpace(finalResponseID), workerSessionID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	var member FanoutMember
	var targetSessionID string
	err = conn.QueryRowContext(ctx, `SELECT m.fanout_member_id, m.fanout_group_id, m.workspace_id, m.worker_session_id, m.target_profile_id, m.request_agent_message_id, m.reply_agent_message_id, m.final_response_id, m.status, m.created_at, m.updated_at, g.source_session_id FROM fanout_members m JOIN fanout_groups g ON g.fanout_group_id = m.fanout_group_id WHERE m.worker_session_id = ?`, workerSessionID).Scan(&member.FanoutMemberID, &member.FanoutGroupID, &member.WorkspaceID, &member.WorkerSessionID, &member.TargetProfileID, &member.RequestAgentMessageID, &member.ReplyAgentMessageID, &member.FinalResponseID, &member.Status, &member.CreatedAt, &member.UpdatedAt, &targetSessionID)
	if err != nil {
		return err
	}
	kind := "worker_" + status
	inboxItemID := "inbox-" + member.FanoutMemberID
	_, err = conn.ExecContext(ctx, `INSERT INTO sticky_inbox_items (inbox_item_id, workspace_id, target_session_id, fanout_group_id, fanout_member_id, worker_session_id, final_response_id, kind, status, summary, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'unread', ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) ON CONFLICT(inbox_item_id) DO UPDATE SET workspace_id = excluded.workspace_id, target_session_id = excluded.target_session_id, fanout_group_id = excluded.fanout_group_id, fanout_member_id = excluded.fanout_member_id, worker_session_id = excluded.worker_session_id, final_response_id = excluded.final_response_id, kind = excluded.kind, status = sticky_inbox_items.status, summary = excluded.summary, updated_at = excluded.updated_at`, inboxItemID, member.WorkspaceID, targetSessionID, member.FanoutGroupID, member.FanoutMemberID, member.WorkerSessionID, member.FinalResponseID, kind, strings.TrimSpace(summary))
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *Store) CreateStickyInboxItem(ctx context.Context, item StickyInboxItem) error {
	if strings.TrimSpace(item.InboxItemID) == "" || strings.TrimSpace(item.WorkspaceID) == "" || strings.TrimSpace(item.TargetSessionID) == "" || strings.TrimSpace(item.Kind) == "" {
		return ErrInvalidInput
	}
	status := defaultString(strings.TrimSpace(item.Status), "unread")
	_, err := s.db.ExecContext(ctx, `INSERT INTO sticky_inbox_items (inbox_item_id, workspace_id, target_session_id, fanout_group_id, fanout_member_id, worker_session_id, final_response_id, kind, status, summary, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, strings.TrimSpace(item.InboxItemID), strings.TrimSpace(item.WorkspaceID), strings.TrimSpace(item.TargetSessionID), strings.TrimSpace(item.FanoutGroupID), strings.TrimSpace(item.FanoutMemberID), strings.TrimSpace(item.WorkerSessionID), strings.TrimSpace(item.FinalResponseID), strings.TrimSpace(item.Kind), status, strings.TrimSpace(item.Summary))
	return err
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

func (s *Store) ListStickyInboxItems(ctx context.Context, workspaceID, targetSessionID string) ([]StickyInboxItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT inbox_item_id, workspace_id, target_session_id, fanout_group_id, fanout_member_id, worker_session_id, final_response_id, kind, status, summary, created_at, updated_at FROM sticky_inbox_items WHERE workspace_id = ? AND target_session_id = ? ORDER BY created_at DESC, inbox_item_id ASC`, strings.TrimSpace(workspaceID), strings.TrimSpace(targetSessionID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var items []StickyInboxItem
	for rows.Next() {
		var item StickyInboxItem
		if err := rows.Scan(&item.InboxItemID, &item.WorkspaceID, &item.TargetSessionID, &item.FanoutGroupID, &item.FanoutMemberID, &item.WorkerSessionID, &item.FinalResponseID, &item.Kind, &item.Status, &item.Summary, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
