package globaldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

const (
	inboxItemStatusUnread = "unread"
	inboxItemStatusRead   = "read"
)

type InboxItem struct {
	InboxItemID       string
	WorkspaceID       string
	SourceSessionID   string
	WorkspaceEventID  string
	EventType         string
	FanoutGroupID     string
	FanoutMemberID    string
	WorkerSessionID   string
	FinalResponseID   string
	Kind              string
	Status            string
	AttentionRequired bool
	Summary           string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type InboxCounts struct {
	TotalCount  int64
	UnreadCount int64
	ReadCount   int64
}

func createInboxItemWithQueries(ctx context.Context, queries *dbsqlc.Queries, item InboxItem) error {
	if err := queries.CreateInboxItem(ctx, dbsqlc.CreateInboxItemParams{InboxItemID: item.InboxItemID, WorkspaceID: item.WorkspaceID, SourceSessionID: item.SourceSessionID, WorkspaceEventID: item.WorkspaceEventID, EventType: item.EventType, FanoutGroupID: item.FanoutGroupID, FanoutMemberID: item.FanoutMemberID, WorkerSessionID: item.WorkerSessionID, FinalResponseID: item.FinalResponseID, Kind: item.Kind, AttentionRequired: boolInt64(item.AttentionRequired), Summary: item.Summary, CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339Nano)}); err != nil {
		return fmt.Errorf("project inbox item %q: %w", item.InboxItemID, err)
	}
	return nil
}

func (s *Store) GetInboxItem(ctx context.Context, inboxItemID string) (InboxItem, error) {
	inboxItemID = strings.TrimSpace(inboxItemID)
	if inboxItemID == "" {
		return InboxItem{}, fmt.Errorf("%w: inbox item id is required", ErrInvalidInput)
	}
	row, err := s.sqlcQueries().GetInboxItem(ctx, dbsqlc.GetInboxItemParams{InboxItemID: inboxItemID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return InboxItem{}, ErrNotFound
		}
		return InboxItem{}, fmt.Errorf("get inbox item %q: %w", inboxItemID, err)
	}
	return inboxItemFromGetRow(row), nil
}

func (s *Store) ListInboxItems(ctx context.Context, workspaceID, sourceSessionID string) ([]InboxItem, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	sourceSessionID = strings.TrimSpace(sourceSessionID)
	if workspaceID == "" || sourceSessionID == "" {
		return nil, fmt.Errorf("%w: workspace id and source session id are required", ErrInvalidInput)
	}
	rows, err := s.sqlcQueries().ListInboxItemsBySession(ctx, dbsqlc.ListInboxItemsBySessionParams{WorkspaceID: workspaceID, SourceSessionID: sourceSessionID})
	if err != nil {
		return nil, fmt.Errorf("list inbox items for %q/%q: %w", workspaceID, sourceSessionID, err)
	}
	items := make([]InboxItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, inboxItemFromListRow(row))
	}
	return items, nil
}

func (s *Store) CountInboxItems(ctx context.Context, workspaceID, sourceSessionID string) (InboxCounts, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	sourceSessionID = strings.TrimSpace(sourceSessionID)
	if workspaceID == "" || sourceSessionID == "" {
		return InboxCounts{}, fmt.Errorf("%w: workspace id and source session id are required", ErrInvalidInput)
	}
	row, err := s.sqlcQueries().CountInboxItemsBySession(ctx, dbsqlc.CountInboxItemsBySessionParams{WorkspaceID: workspaceID, SourceSessionID: sourceSessionID})
	if err != nil {
		return InboxCounts{}, fmt.Errorf("count inbox items for %q/%q: %w", workspaceID, sourceSessionID, err)
	}
	unreadCount, err := inboxSQLCountValue(row.UnreadCount)
	if err != nil {
		return InboxCounts{}, fmt.Errorf("count unread inbox items for %q/%q: %w", workspaceID, sourceSessionID, err)
	}
	readCount, err := inboxSQLCountValue(row.ReadCount)
	if err != nil {
		return InboxCounts{}, fmt.Errorf("count read inbox items for %q/%q: %w", workspaceID, sourceSessionID, err)
	}
	return InboxCounts{TotalCount: row.TotalCount, UnreadCount: unreadCount, ReadCount: readCount}, nil
}

func (s *Store) MarkInboxItemsRead(ctx context.Context, workspaceID, sourceSessionID string, inboxItemIDs []string) (int64, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	sourceSessionID = strings.TrimSpace(sourceSessionID)
	if workspaceID == "" || sourceSessionID == "" {
		return 0, fmt.Errorf("%w: workspace id and source session id are required", ErrInvalidInput)
	}
	trimmedIDs := normalizeStringSet(inboxItemIDs)
	if len(trimmedIDs) == 0 {
		return 0, fmt.Errorf("%w: inbox item ids are required", ErrInvalidInput)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var marked int64
	if err := s.withImmediateQueries(ctx, func(ctx context.Context, queries *dbsqlc.Queries) error {
		for _, inboxItemID := range trimmedIDs {
			rows, err := queries.MarkInboxItemRead(ctx, dbsqlc.MarkInboxItemReadParams{ReadAt: now, UpdatedAt: now, WorkspaceID: workspaceID, SourceSessionID: sourceSessionID, InboxItemID: inboxItemID})
			if err != nil {
				return fmt.Errorf("mark inbox item %q read: %w", inboxItemID, err)
			}
			marked += rows
		}
		return nil
	}); err != nil {
		return 0, err
	}
	return marked, nil
}

func normalizeInboxItem(item InboxItem) InboxItem {
	item.InboxItemID = strings.TrimSpace(item.InboxItemID)
	item.WorkspaceID = strings.TrimSpace(item.WorkspaceID)
	item.SourceSessionID = strings.TrimSpace(item.SourceSessionID)
	item.WorkspaceEventID = strings.TrimSpace(item.WorkspaceEventID)
	item.EventType = strings.TrimSpace(item.EventType)
	item.FanoutGroupID = strings.TrimSpace(item.FanoutGroupID)
	item.FanoutMemberID = strings.TrimSpace(item.FanoutMemberID)
	item.WorkerSessionID = strings.TrimSpace(item.WorkerSessionID)
	item.FinalResponseID = strings.TrimSpace(item.FinalResponseID)
	item.Kind = strings.TrimSpace(item.Kind)
	item.Status = strings.TrimSpace(item.Status)
	if item.Status == "" {
		item.Status = inboxItemStatusUnread
	}
	item.Summary = strings.TrimSpace(item.Summary)
	return item
}

func validateInboxItem(item InboxItem) error {
	if item.InboxItemID == "" || item.WorkspaceID == "" || item.SourceSessionID == "" || item.WorkspaceEventID == "" || item.EventType == "" || item.Kind == "" {
		return fmt.Errorf("%w: inbox item required field is missing", ErrInvalidInput)
	}
	if item.Status != inboxItemStatusUnread && item.Status != inboxItemStatusRead {
		return fmt.Errorf("%w: inbox item status is invalid", ErrInvalidInput)
	}
	return nil
}

func inboxItemFromGetRow(row dbsqlc.GetInboxItemRow) InboxItem {
	return inboxItemFromProjectedRow(row.InboxItemID, row.WorkspaceID, row.SourceSessionID, row.WorkspaceEventID, row.EventType, row.FanoutGroupID, row.FanoutMemberID, row.WorkerSessionID, row.FinalResponseID, row.Kind, row.Status, row.AttentionRequired, row.Summary, row.CreatedAt, row.UpdatedAt)
}

func inboxItemFromListRow(row dbsqlc.ListInboxItemsBySessionRow) InboxItem {
	return inboxItemFromProjectedRow(row.InboxItemID, row.WorkspaceID, row.SourceSessionID, row.WorkspaceEventID, row.EventType, row.FanoutGroupID, row.FanoutMemberID, row.WorkerSessionID, row.FinalResponseID, row.Kind, row.Status, row.AttentionRequired, row.Summary, row.CreatedAt, row.UpdatedAt)
}

func inboxItemFromProjectedRow(inboxItemID, workspaceID, sourceSessionID, workspaceEventID, eventType, fanoutGroupID, fanoutMemberID, workerSessionID, finalResponseID, kind, status string, attentionRequired int64, summary, createdAtRaw, updatedAtRaw string) InboxItem {
	createdAt, _ := time.Parse(time.RFC3339Nano, createdAtRaw)
	updatedAt, _ := time.Parse(time.RFC3339Nano, updatedAtRaw)
	return InboxItem{InboxItemID: inboxItemID, WorkspaceID: workspaceID, SourceSessionID: sourceSessionID, WorkspaceEventID: workspaceEventID, EventType: eventType, FanoutGroupID: fanoutGroupID, FanoutMemberID: fanoutMemberID, WorkerSessionID: workerSessionID, FinalResponseID: finalResponseID, Kind: kind, Status: status, AttentionRequired: attentionRequired != 0, Summary: summary, CreatedAt: createdAt, UpdatedAt: updatedAt}
}

func inboxSQLCountValue(value any) (int64, error) {
	switch typed := value.(type) {
	case nil:
		return 0, nil
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case int32:
		return int64(typed), nil
	case int16:
		return int64(typed), nil
	case int8:
		return int64(typed), nil
	case float64:
		return int64(typed), nil
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	case string:
		return strconv.ParseInt(typed, 10, 64)
	default:
		return 0, fmt.Errorf("unsupported inbox count type %T", value)
	}
}
