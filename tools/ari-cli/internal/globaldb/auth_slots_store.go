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

type AuthSlot struct {
	AuthSlotID      string
	Harness         string
	Label           string
	ProviderLabel   string
	CredentialOwner string
	Status          string
	MetadataJSON    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (s *Store) UpsertAuthSlot(ctx context.Context, slot AuthSlot) error {
	slot.AuthSlotID = strings.TrimSpace(slot.AuthSlotID)
	slot.Harness = strings.TrimSpace(slot.Harness)
	slot.Label = strings.TrimSpace(slot.Label)
	slot.ProviderLabel = strings.TrimSpace(slot.ProviderLabel)
	slot.CredentialOwner = strings.TrimSpace(slot.CredentialOwner)
	slot.Status = strings.TrimSpace(slot.Status)
	if slot.AuthSlotID == "" {
		return fmt.Errorf("%w: auth slot id is required", ErrInvalidInput)
	}
	if slot.Harness == "" {
		return fmt.Errorf("%w: auth slot harness is required", ErrInvalidInput)
	}
	if slot.Label == "" {
		return fmt.Errorf("%w: auth slot label is required", ErrInvalidInput)
	}
	if slot.CredentialOwner == "" {
		slot.CredentialOwner = "provider"
	}
	if slot.CredentialOwner != "provider" {
		return fmt.Errorf("%w: auth slot credential owner must be provider", ErrInvalidInput)
	}
	if slot.Status == "" {
		slot.Status = "unknown"
	}
	if !validAuthSlotStatus(slot.Status) {
		return fmt.Errorf("%w: invalid auth slot status %q", ErrInvalidInput, slot.Status)
	}
	if strings.TrimSpace(slot.MetadataJSON) == "" {
		slot.MetadataJSON = "{}"
	}
	if !json.Valid([]byte(slot.MetadataJSON)) {
		return fmt.Errorf("%w: auth slot metadata json is invalid", ErrInvalidInput)
	}
	if jsonContainsSecretLikeFields(slot.MetadataJSON) {
		return fmt.Errorf("%w: auth slot metadata must not include secret-like fields", ErrInvalidInput)
	}
	now := time.Now().UTC()
	if existing, err := s.GetAuthSlot(ctx, slot.AuthSlotID); err == nil && !existing.CreatedAt.IsZero() {
		slot.CreatedAt = existing.CreatedAt
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if slot.CreatedAt.IsZero() {
		slot.CreatedAt = now
	}
	slot.UpdatedAt = now
	if err := s.sqlcQueries().UpsertAuthSlot(ctx, dbsqlc.UpsertAuthSlotParams{AuthSlotID: slot.AuthSlotID, Harness: slot.Harness, Label: slot.Label, ProviderLabel: optionalString(slot.ProviderLabel), CredentialOwner: slot.CredentialOwner, Status: slot.Status, MetadataJson: slot.MetadataJSON, CreatedAt: slot.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: slot.UpdatedAt.Format(time.RFC3339Nano)}); err != nil {
		return fmt.Errorf("upsert auth slot %q: %w", slot.AuthSlotID, err)
	}
	return nil
}

func (s *Store) GetAuthSlot(ctx context.Context, authSlotID string) (AuthSlot, error) {
	authSlotID = strings.TrimSpace(authSlotID)
	if authSlotID == "" {
		return AuthSlot{}, fmt.Errorf("%w: auth slot id is required", ErrInvalidInput)
	}
	row, err := s.sqlcQueries().GetAuthSlot(ctx, dbsqlc.GetAuthSlotParams{AuthSlotID: authSlotID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AuthSlot{}, ErrNotFound
		}
		return AuthSlot{}, fmt.Errorf("query auth slot %q: %w", authSlotID, err)
	}
	return authSlotFromSQLC(row), nil
}

func (s *Store) ListAuthSlots(ctx context.Context, harness string) ([]AuthSlot, error) {
	harness = strings.TrimSpace(harness)
	var rows []dbsqlc.AuthSlot
	var err error
	if harness == "" {
		rows, err = s.sqlcQueries().ListAuthSlots(ctx)
	} else {
		rows, err = s.sqlcQueries().ListAuthSlotsByHarness(ctx, dbsqlc.ListAuthSlotsByHarnessParams{Harness: harness})
	}
	if err != nil {
		return nil, fmt.Errorf("list auth slots: %w", err)
	}
	slots := make([]AuthSlot, 0, len(rows))
	for _, row := range rows {
		slots = append(slots, authSlotFromSQLC(row))
	}
	return slots, nil
}

func (s *Store) DeleteAuthSlot(ctx context.Context, authSlotID string) error {
	authSlotID = strings.TrimSpace(authSlotID)
	if authSlotID == "" {
		return fmt.Errorf("%w: auth slot id is required", ErrInvalidInput)
	}
	rows, err := s.sqlcQueries().DeleteAuthSlot(ctx, dbsqlc.DeleteAuthSlotParams{AuthSlotID: authSlotID})
	if err != nil {
		return fmt.Errorf("delete auth slot %q: %w", authSlotID, err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func validAuthSlotStatus(status string) bool {
	switch status {
	case "authenticated", "auth_required", "auth_in_progress", "auth_failed", "cancelled", "unknown", "not_installed":
		return true
	default:
		return false
	}
}

func authSlotFromSQLC(row dbsqlc.AuthSlot) AuthSlot {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	return AuthSlot{AuthSlotID: row.AuthSlotID, Harness: row.Harness, Label: row.Label, ProviderLabel: stringValue(row.ProviderLabel), CredentialOwner: row.CredentialOwner, Status: row.Status, MetadataJSON: row.MetadataJson, CreatedAt: createdAt, UpdatedAt: updatedAt}
}
