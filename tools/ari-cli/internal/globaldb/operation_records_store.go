package globaldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

const (
	OperationScopeGlobal    = "global"
	OperationScopeWorkspace = "workspace"
)

type OperationRecord struct {
	OperationID           string
	WorkspaceID           string
	OperationType         string
	Actor                 string
	Source                string
	Scope                 string
	RequestSummary        string
	Result                string
	TrustDecision         string
	ParentOperationID     string
	CheckpointOperationID string
	RollbackPointID       string
	RollbackDataJSON      string
	PayloadHash           string
	PayloadSnapshotJSON   string
	CreatedAt             time.Time
}

type AppendOperationRecordParams = OperationRecord

func (s *Store) AppendOperationRecord(ctx context.Context, record AppendOperationRecordParams) (OperationRecord, error) {
	record = normalizeOperationRecord(record)
	if err := validateOperationRecord(record); err != nil {
		return OperationRecord{}, err
	}
	if err := s.validateOperationRecordReferences(ctx, record); err != nil {
		return OperationRecord{}, err
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	if err := s.sqlcQueries().CreateOperationRecord(ctx, dbsqlc.CreateOperationRecordParams{OperationID: record.OperationID, WorkspaceID: optionalString(record.WorkspaceID), OperationType: record.OperationType, Actor: record.Actor, Source: record.Source, Scope: record.Scope, RequestSummary: record.RequestSummary, Result: record.Result, TrustDecision: optionalString(record.TrustDecision), ParentOperationID: optionalString(record.ParentOperationID), CheckpointOperationID: optionalString(record.CheckpointOperationID), RollbackPointID: optionalString(record.RollbackPointID), RollbackDataJson: record.RollbackDataJSON, PayloadHash: record.PayloadHash, PayloadSnapshotJson: record.PayloadSnapshotJSON, CreatedAt: record.CreatedAt.Format(time.RFC3339Nano)}); err != nil {
		return OperationRecord{}, fmt.Errorf("append operation record %q: %w", record.OperationID, err)
	}
	return record, nil
}

func (s *Store) GetOperationRecord(ctx context.Context, operationID string) (OperationRecord, error) {
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return OperationRecord{}, fmt.Errorf("%w: operation id is required", ErrInvalidInput)
	}
	row, err := s.sqlcQueries().GetOperationRecord(ctx, dbsqlc.GetOperationRecordParams{OperationID: operationID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OperationRecord{}, ErrNotFound
		}
		return OperationRecord{}, fmt.Errorf("query operation record %q: %w", operationID, err)
	}
	return operationRecordFromSQLC(row), nil
}

func (s *Store) ListOperationRecords(ctx context.Context, workspaceID string) ([]OperationRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	var rows []dbsqlc.OperationRecord
	var err error
	if workspaceID == "" {
		rows, err = s.sqlcQueries().ListOperationRecords(ctx)
	} else {
		rows, err = s.sqlcQueries().ListOperationRecordsByWorkspace(ctx, dbsqlc.ListOperationRecordsByWorkspaceParams{WorkspaceID: optionalString(workspaceID)})
	}
	if err != nil {
		return nil, fmt.Errorf("list operation records: %w", err)
	}
	records := make([]OperationRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, operationRecordFromSQLC(row))
	}
	return records, nil
}

func normalizeOperationRecord(record OperationRecord) OperationRecord {
	record.OperationID = strings.TrimSpace(record.OperationID)
	record.WorkspaceID = strings.TrimSpace(record.WorkspaceID)
	record.OperationType = strings.TrimSpace(record.OperationType)
	record.Actor = strings.TrimSpace(record.Actor)
	record.Source = strings.TrimSpace(record.Source)
	record.Scope = strings.TrimSpace(record.Scope)
	record.RequestSummary = strings.TrimSpace(record.RequestSummary)
	record.Result = strings.TrimSpace(record.Result)
	record.TrustDecision = strings.TrimSpace(record.TrustDecision)
	record.ParentOperationID = strings.TrimSpace(record.ParentOperationID)
	record.CheckpointOperationID = strings.TrimSpace(record.CheckpointOperationID)
	record.RollbackPointID = strings.TrimSpace(record.RollbackPointID)
	record.PayloadHash = strings.TrimSpace(record.PayloadHash)
	if strings.TrimSpace(record.RollbackDataJSON) == "" {
		record.RollbackDataJSON = "{}"
	}
	if strings.TrimSpace(record.PayloadSnapshotJSON) == "" {
		record.PayloadSnapshotJSON = "{}"
	}
	return record
}

func validateOperationRecord(record OperationRecord) error {
	if record.OperationID == "" || record.OperationType == "" || record.Actor == "" || record.Source == "" || record.RequestSummary == "" || record.Result == "" || record.PayloadHash == "" {
		return fmt.Errorf("%w: operation record required field is missing", ErrInvalidInput)
	}
	if record.Scope != OperationScopeGlobal && record.Scope != OperationScopeWorkspace {
		return fmt.Errorf("%w: operation scope must be global or workspace", ErrInvalidInput)
	}
	if record.Scope == OperationScopeWorkspace && record.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace-scoped operation requires workspace id", ErrInvalidInput)
	}
	if !json.Valid([]byte(record.RollbackDataJSON)) {
		return fmt.Errorf("%w: rollback data json is invalid", ErrInvalidInput)
	}
	if !json.Valid([]byte(record.PayloadSnapshotJSON)) {
		return fmt.Errorf("%w: payload snapshot json is invalid", ErrInvalidInput)
	}
	return nil
}

func (s *Store) validateOperationRecordReferences(ctx context.Context, record OperationRecord) error {
	if record.Scope == OperationScopeWorkspace {
		if _, err := s.GetSession(ctx, record.WorkspaceID); err != nil {
			return err
		}
	}
	for _, operationID := range []string{record.ParentOperationID, record.CheckpointOperationID, record.RollbackPointID} {
		if operationID == "" {
			continue
		}
		if _, err := s.GetOperationRecord(ctx, operationID); err != nil {
			return err
		}
	}
	return nil
}

func operationRecordFromSQLC(row dbsqlc.OperationRecord) OperationRecord {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	return OperationRecord{OperationID: row.OperationID, WorkspaceID: stringValue(row.WorkspaceID), OperationType: row.OperationType, Actor: row.Actor, Source: row.Source, Scope: row.Scope, RequestSummary: row.RequestSummary, Result: row.Result, TrustDecision: stringValue(row.TrustDecision), ParentOperationID: stringValue(row.ParentOperationID), CheckpointOperationID: stringValue(row.CheckpointOperationID), RollbackPointID: stringValue(row.RollbackPointID), RollbackDataJSON: row.RollbackDataJson, PayloadHash: row.PayloadHash, PayloadSnapshotJSON: row.PayloadSnapshotJson, CreatedAt: createdAt}
}
