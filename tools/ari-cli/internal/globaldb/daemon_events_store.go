package globaldb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

type DaemonEvent struct {
	EventID            string
	WorkspaceID        string
	SessionID          string
	EventType          string
	SubjectType        string
	SubjectID          string
	PayloadJSON        string
	AttentionRequired  bool
	AttentionClearedAt *time.Time
	CreatedAt          time.Time
}

type AppendDaemonEventParams = DaemonEvent

func (s *Store) AppendDaemonEvent(ctx context.Context, event AppendDaemonEventParams) (DaemonEvent, error) {
	event = normalizeDaemonEvent(event)
	if err := validateDaemonEvent(event); err != nil {
		return DaemonEvent{}, err
	}
	if event.EventID == "" {
		event.EventID = newDaemonEventID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	var clearedAt *string
	if event.AttentionClearedAt != nil {
		formatted := event.AttentionClearedAt.UTC().Format(time.RFC3339Nano)
		clearedAt = &formatted
	}
	if err := s.sqlcQueries().CreateDaemonEvent(ctx, dbsqlc.CreateDaemonEventParams{EventID: event.EventID, WorkspaceID: optionalString(event.WorkspaceID), SessionID: optionalString(event.SessionID), EventType: event.EventType, SubjectType: event.SubjectType, SubjectID: event.SubjectID, PayloadJson: event.PayloadJSON, AttentionRequired: boolInt64(event.AttentionRequired), AttentionClearedAt: clearedAt, CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano)}); err != nil {
		return DaemonEvent{}, fmt.Errorf("append daemon event %q: %w", event.EventID, err)
	}
	return event, nil
}

func (s *Store) ListDaemonEventsAfter(ctx context.Context, afterEventID string, limit int) ([]DaemonEvent, error) {
	afterEventID = strings.TrimSpace(afterEventID)
	if limit <= 0 {
		limit = 100
	}
	if afterEventID != "" {
		count, err := s.sqlcQueries().CountDaemonEventsByID(ctx, dbsqlc.CountDaemonEventsByIDParams{EventID: afterEventID})
		if err != nil {
			return nil, fmt.Errorf("check daemon event cursor %q: %w", afterEventID, err)
		}
		if count == 0 {
			return nil, ErrNotFound
		}
	}
	rows, err := s.sqlcQueries().ListDaemonEventsAfter(ctx, dbsqlc.ListDaemonEventsAfterParams{EventID: afterEventID, Column2: afterEventID, EventID_2: afterEventID, EventID_3: afterEventID, Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("list daemon events after %q: %w", afterEventID, err)
	}
	return daemonEventsFromSQLC(rows), nil
}

func (s *Store) ListDaemonAttentionEvents(ctx context.Context) ([]DaemonEvent, error) {
	rows, err := s.sqlcQueries().ListDaemonAttentionEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list daemon attention events: %w", err)
	}
	return daemonEventsFromSQLC(rows), nil
}

func (s *Store) ClearDaemonEventAttention(ctx context.Context, eventID string) error {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return fmt.Errorf("%w: event id is required", ErrInvalidInput)
	}
	clearedAt := time.Now().UTC().Format(time.RFC3339Nano)
	rows, err := s.sqlcQueries().ClearDaemonEventAttention(ctx, dbsqlc.ClearDaemonEventAttentionParams{AttentionClearedAt: &clearedAt, EventID: eventID})
	if err != nil {
		return fmt.Errorf("clear daemon event attention %q: %w", eventID, err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func normalizeDaemonEvent(event DaemonEvent) DaemonEvent {
	event.EventID = strings.TrimSpace(event.EventID)
	event.WorkspaceID = strings.TrimSpace(event.WorkspaceID)
	event.SessionID = strings.TrimSpace(event.SessionID)
	event.EventType = strings.TrimSpace(event.EventType)
	event.SubjectType = strings.TrimSpace(event.SubjectType)
	event.SubjectID = strings.TrimSpace(event.SubjectID)
	if strings.TrimSpace(event.PayloadJSON) == "" {
		event.PayloadJSON = "{}"
	}
	return event
}

func validateDaemonEvent(event DaemonEvent) error {
	if event.EventType == "" || event.SubjectType == "" || event.SubjectID == "" {
		return fmt.Errorf("%w: daemon event required field is missing", ErrInvalidInput)
	}
	if !json.Valid([]byte(event.PayloadJSON)) {
		return fmt.Errorf("%w: daemon event payload json is invalid", ErrInvalidInput)
	}
	return nil
}

func daemonEventsFromSQLC(rows []dbsqlc.DaemonEvent) []DaemonEvent {
	events := make([]DaemonEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, daemonEventFromSQLC(row))
	}
	return events
}

func daemonEventFromSQLC(row dbsqlc.DaemonEvent) DaemonEvent {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	var clearedAt *time.Time
	if row.AttentionClearedAt != nil {
		parsed, _ := time.Parse(time.RFC3339Nano, *row.AttentionClearedAt)
		clearedAt = &parsed
	}
	return DaemonEvent{EventID: row.EventID, WorkspaceID: stringValue(row.WorkspaceID), SessionID: stringValue(row.SessionID), EventType: row.EventType, SubjectType: row.SubjectType, SubjectID: row.SubjectID, PayloadJSON: row.PayloadJson, AttentionRequired: row.AttentionRequired != 0, AttentionClearedAt: clearedAt, CreatedAt: createdAt}
}

func newDaemonEventID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return "de_" + hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("de_%d", time.Now().UnixNano())
}
