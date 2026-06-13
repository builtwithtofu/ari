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

type FinalResponse struct {
	FinalResponseID   string
	HarnessSessionID  string
	WorkspaceID       string
	TaskID            string
	ContextPacketID   string
	ProfileID         string
	Status            string
	Text              string
	EvidenceLinksJSON string
	CreatedAt         time.Time
	UpdatedAt         *time.Time
}

func (s *Store) UpsertFinalResponse(ctx context.Context, response FinalResponse) error {
	response.FinalResponseID = strings.TrimSpace(response.FinalResponseID)
	response.HarnessSessionID = strings.TrimSpace(response.HarnessSessionID)
	response.WorkspaceID = strings.TrimSpace(response.WorkspaceID)
	response.TaskID = strings.TrimSpace(response.TaskID)
	response.ContextPacketID = strings.TrimSpace(response.ContextPacketID)
	response.ProfileID = strings.TrimSpace(response.ProfileID)
	response.Status = strings.TrimSpace(response.Status)
	response.Text = strings.TrimSpace(response.Text)
	if response.FinalResponseID == "" {
		return fmt.Errorf("%w: final response id is required", ErrInvalidInput)
	}
	if response.HarnessSessionID == "" {
		return fmt.Errorf("%w: harness session id is required", ErrInvalidInput)
	}
	if response.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if response.TaskID == "" {
		return fmt.Errorf("%w: task id is required", ErrInvalidInput)
	}
	if response.ContextPacketID == "" {
		return fmt.Errorf("%w: context packet id is required", ErrInvalidInput)
	}
	if !validFinalResponseStatus(response.Status) {
		return fmt.Errorf("%w: invalid final response status %q", ErrInvalidInput, response.Status)
	}
	if strings.TrimSpace(response.EvidenceLinksJSON) == "" {
		response.EvidenceLinksJSON = "[]"
	}
	if !json.Valid([]byte(response.EvidenceLinksJSON)) {
		return fmt.Errorf("%w: evidence links json is invalid", ErrInvalidInput)
	}
	now := time.Now().UTC()
	if response.CreatedAt.IsZero() {
		response.CreatedAt = now
	}
	var updatedAt *string
	if response.UpdatedAt != nil {
		updatedAt = ptrString(response.UpdatedAt.UTC().Format(time.RFC3339Nano))
	}
	if err := s.sqlcQueries().UpsertFinalResponse(ctx, dbsqlc.UpsertFinalResponseParams{FinalResponseID: response.FinalResponseID, SessionID: response.HarnessSessionID, WorkspaceID: response.WorkspaceID, TaskID: response.TaskID, ContextPacketID: response.ContextPacketID, ProfileID: optionalString(response.ProfileID), Status: response.Status, Text: response.Text, EvidenceLinks: response.EvidenceLinksJSON, CreatedAt: response.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: updatedAt}); err != nil {
		return fmt.Errorf("upsert final response %q: %w", response.FinalResponseID, err)
	}
	return nil
}

func (s *Store) GetFinalResponseBySessionID(ctx context.Context, sessionID string) (FinalResponse, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return FinalResponse{}, fmt.Errorf("%w: harness session id is required", ErrInvalidInput)
	}
	row, err := s.sqlcQueries().GetFinalResponseBySessionID(ctx, dbsqlc.GetFinalResponseBySessionIDParams{SessionID: sessionID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FinalResponse{}, ErrNotFound
		}
		return FinalResponse{}, fmt.Errorf("query final response by harness session id: %w", err)
	}
	return finalResponseFromSQLC(row), nil
}

func (s *Store) GetFinalResponseByID(ctx context.Context, finalResponseID string) (FinalResponse, error) {
	finalResponseID = strings.TrimSpace(finalResponseID)
	if finalResponseID == "" {
		return FinalResponse{}, fmt.Errorf("%w: final response id is required", ErrInvalidInput)
	}
	row, err := s.sqlcQueries().GetFinalResponseByID(ctx, dbsqlc.GetFinalResponseByIDParams{FinalResponseID: finalResponseID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FinalResponse{}, ErrNotFound
		}
		return FinalResponse{}, fmt.Errorf("query final response by id: %w", err)
	}
	return finalResponseFromSQLC(row), nil
}

func (s *Store) ListFinalResponses(ctx context.Context, workspaceID string) ([]FinalResponse, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	rows, err := s.sqlcQueries().ListFinalResponsesByWorkspace(ctx, dbsqlc.ListFinalResponsesByWorkspaceParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, fmt.Errorf("list final responses: %w", err)
	}
	responses := make([]FinalResponse, 0, len(rows))
	for _, row := range rows {
		responses = append(responses, finalResponseFromSQLC(row))
	}
	return responses, nil
}

func validFinalResponseStatus(status string) bool {
	switch status {
	case "completed", "failed", "partial", "unavailable":
		return true
	default:
		return false
	}
}

func finalResponseFromSQLC(row dbsqlc.FinalResponse) FinalResponse {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	var updatedAt *time.Time
	if row.UpdatedAt != nil {
		parsed, _ := time.Parse(time.RFC3339Nano, *row.UpdatedAt)
		updatedAt = &parsed
	}
	return FinalResponse{FinalResponseID: row.FinalResponseID, HarnessSessionID: row.SessionID, WorkspaceID: row.WorkspaceID, TaskID: row.TaskID, ContextPacketID: row.ContextPacketID, ProfileID: stringValue(row.ProfileID), Status: row.Status, Text: row.Text, EvidenceLinksJSON: row.EvidenceLinks, CreatedAt: createdAt, UpdatedAt: updatedAt}
}
