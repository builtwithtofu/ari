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
	group.FanoutGroupID = strings.TrimSpace(group.FanoutGroupID)
	group.WorkspaceID = strings.TrimSpace(group.WorkspaceID)
	group.SourceSessionID = strings.TrimSpace(group.SourceSessionID)
	if group.FanoutGroupID == "" || group.WorkspaceID == "" || group.SourceSessionID == "" {
		return ErrInvalidInput
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	createdAt := defaultString(strings.TrimSpace(group.CreatedAt), now)
	updatedAt := defaultString(strings.TrimSpace(group.UpdatedAt), createdAt)
	if err := s.sqlcQueries().CreateFanoutGroup(ctx, dbsqlc.CreateFanoutGroupParams{FanoutGroupID: group.FanoutGroupID, WorkspaceID: group.WorkspaceID, SourceSessionID: group.SourceSessionID, SourceAgentID: strings.TrimSpace(group.SourceAgentID), RequestAgentMessageID: strings.TrimSpace(group.RequestAgentMessageID), Status: defaultString(strings.TrimSpace(group.Status), "running"), Body: strings.TrimSpace(group.Body), CreatedAt: createdAt, UpdatedAt: updatedAt}); err != nil {
		return fmt.Errorf("create fanout group %q: %w", group.FanoutGroupID, err)
	}
	return nil
}

func (s *Store) GetFanoutGroup(ctx context.Context, groupID string) (FanoutGroup, error) {
	row, err := s.sqlcQueries().GetFanoutGroup(ctx, dbsqlc.GetFanoutGroupParams{FanoutGroupID: strings.TrimSpace(groupID)})
	if errors.Is(err, sql.ErrNoRows) {
		return FanoutGroup{}, ErrNotFound
	}
	if err != nil {
		return FanoutGroup{}, fmt.Errorf("get fanout group %q: %w", groupID, err)
	}
	return fanoutGroupFromSQLC(row), nil
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
	row, err := s.sqlcQueries().GetFanoutMemberByWorkerSession(ctx, dbsqlc.GetFanoutMemberByWorkerSessionParams{WorkerSessionID: workerSessionID})
	if errors.Is(err, sql.ErrNoRows) {
		return FanoutMember{}, ErrNotFound
	}
	if err != nil {
		return FanoutMember{}, fmt.Errorf("get fanout member for worker session %q: %w", workerSessionID, err)
	}
	return fanoutMemberFromSQLC(row), nil
}

func (s *Store) ListFanoutMembers(ctx context.Context, groupID string) ([]FanoutMember, error) {
	rows, err := s.sqlcQueries().ListFanoutMembersByGroup(ctx, dbsqlc.ListFanoutMembersByGroupParams{FanoutGroupID: strings.TrimSpace(groupID)})
	if err != nil {
		return nil, fmt.Errorf("list fanout members for group %q: %w", groupID, err)
	}
	return fanoutMembersFromSQLC(rows), nil
}

func (s *Store) ListFanoutMembersByWorkspace(ctx context.Context, workspaceID string) ([]FanoutMember, error) {
	rows, err := s.sqlcQueries().ListFanoutMembersByWorkspace(ctx, dbsqlc.ListFanoutMembersByWorkspaceParams{WorkspaceID: strings.TrimSpace(workspaceID)})
	if err != nil {
		return nil, fmt.Errorf("list fanout members for workspace %q: %w", workspaceID, err)
	}
	return fanoutMembersFromSQLC(rows), nil
}

func fanoutGroupFromSQLC(row dbsqlc.FanoutGroup) FanoutGroup {
	return FanoutGroup{FanoutGroupID: row.FanoutGroupID, WorkspaceID: row.WorkspaceID, SourceSessionID: row.SourceSessionID, SourceAgentID: row.SourceAgentID, RequestAgentMessageID: row.RequestAgentMessageID, Status: row.Status, Body: row.Body, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func fanoutMemberFromSQLC(row dbsqlc.FanoutMember) FanoutMember {
	return FanoutMember{FanoutMemberID: row.FanoutMemberID, FanoutGroupID: row.FanoutGroupID, WorkspaceID: row.WorkspaceID, WorkerSessionID: row.WorkerSessionID, TargetProfileID: row.TargetProfileID, RequestAgentMessageID: row.RequestAgentMessageID, ReplyAgentMessageID: row.ReplyAgentMessageID, FinalResponseID: row.FinalResponseID, Status: row.Status, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func fanoutMembersFromSQLC(rows []dbsqlc.FanoutMember) []FanoutMember {
	members := make([]FanoutMember, 0, len(rows))
	for _, row := range rows {
		members = append(members, fanoutMemberFromSQLC(row))
	}
	return members
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
