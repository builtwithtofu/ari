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

// SecretAuditEvent is one row of the daemon-global secret audit trail. It is
// deliberately separate from workspace event history: secret grants are not
// workspace-scoped facts, and audit rows must never carry secret material —
// only redacted metadata.
type SecretAuditEvent struct {
	EventID     string
	EventType   string
	SubjectType string
	SubjectID   string
	PayloadJSON string
	CreatedAt   time.Time
}

func (s *Store) appendSecretAuditEventRow(ctx context.Context, event SecretAuditEvent) error {
	event.EventID = strings.TrimSpace(event.EventID)
	event.EventType = strings.TrimSpace(event.EventType)
	event.SubjectType = strings.TrimSpace(event.SubjectType)
	event.SubjectID = strings.TrimSpace(event.SubjectID)
	if strings.TrimSpace(event.PayloadJSON) == "" {
		event.PayloadJSON = "{}"
	}
	if event.EventType == "" || event.SubjectType == "" || event.SubjectID == "" {
		return fmt.Errorf("%w: secret audit event required field is missing", ErrInvalidInput)
	}
	if !json.Valid([]byte(event.PayloadJSON)) {
		return fmt.Errorf("%w: secret audit event payload json is invalid", ErrInvalidInput)
	}
	if event.EventID == "" {
		event.EventID = newSecretAuditEventID()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if err := s.sqlcQueries().CreateSecretAuditEvent(ctx, dbsqlc.CreateSecretAuditEventParams{EventID: event.EventID, EventType: event.EventType, SubjectType: event.SubjectType, SubjectID: event.SubjectID, PayloadJson: event.PayloadJSON, CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano)}); err != nil {
		return fmt.Errorf("append secret audit event %q: %w", event.EventID, err)
	}
	return nil
}

func (s *Store) ListSecretAuditEvents(ctx context.Context, limit int) ([]SecretAuditEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.sqlcQueries().ListSecretAuditEvents(ctx, dbsqlc.ListSecretAuditEventsParams{Limit: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("list secret audit events: %w", err)
	}
	events := make([]SecretAuditEvent, 0, len(rows))
	for _, row := range rows {
		createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
		events = append(events, SecretAuditEvent{EventID: row.EventID, EventType: row.EventType, SubjectType: row.SubjectType, SubjectID: row.SubjectID, PayloadJSON: row.PayloadJson, CreatedAt: createdAt})
	}
	return events, nil
}

func newSecretAuditEventID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return "sae_" + hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("sae_%d", time.Now().UnixNano())
}
