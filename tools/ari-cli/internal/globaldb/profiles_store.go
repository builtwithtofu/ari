package globaldb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

type Profile struct {
	ProfileID       string
	WorkspaceID     string
	Name            string
	Harness         string
	Model           string
	Prompt          string
	AuthSlotID      string
	AuthPoolJSON    string
	InvocationClass string
	DefaultsJSON    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

const DefaultHelperProfileName = "helper"

func (s *Store) UpsertProfile(ctx context.Context, profile Profile) error {
	profile.ProfileID = strings.TrimSpace(profile.ProfileID)
	profile.WorkspaceID = strings.TrimSpace(profile.WorkspaceID)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.Harness = strings.TrimSpace(profile.Harness)
	profile.Model = strings.TrimSpace(profile.Model)
	profile.AuthSlotID = strings.TrimSpace(profile.AuthSlotID)
	profile.InvocationClass = strings.TrimSpace(profile.InvocationClass)
	if profile.ProfileID == "" {
		return fmt.Errorf("%w: profile id is required", ErrInvalidInput)
	}
	if profile.Name == "" {
		return fmt.Errorf("%w: profile name is required", ErrInvalidInput)
	}
	if existing, err := s.getExactProfile(ctx, profile.WorkspaceID, profile.Name); err == nil {
		profile.ProfileID = existing.ProfileID
		if profile.CreatedAt.IsZero() {
			profile.CreatedAt = existing.CreatedAt
		}
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if strings.TrimSpace(profile.DefaultsJSON) == "" {
		profile.DefaultsJSON = "{}"
	}
	if strings.TrimSpace(profile.AuthPoolJSON) == "" {
		profile.AuthPoolJSON = "{}"
	}
	if !json.Valid([]byte(profile.DefaultsJSON)) {
		return fmt.Errorf("%w: profile defaults json is invalid", ErrInvalidInput)
	}
	if jsonContainsSecretLikeFields(profile.DefaultsJSON) {
		return fmt.Errorf("%w: profile defaults json must not include secret-like fields", ErrInvalidInput)
	}
	if !json.Valid([]byte(profile.AuthPoolJSON)) {
		return fmt.Errorf("%w: profile auth pool json is invalid", ErrInvalidInput)
	}
	now := time.Now().UTC()
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = now
	}
	profile.UpdatedAt = now
	if err := s.sqlcQueries().UpsertProfile(ctx, dbsqlc.UpsertProfileParams{ProfileID: profile.ProfileID, WorkspaceID: optionalString(profile.WorkspaceID), Name: profile.Name, Harness: optionalString(profile.Harness), Model: optionalString(profile.Model), Prompt: optionalString(profile.Prompt), AuthSlotID: optionalString(profile.AuthSlotID), AuthPoolJson: profile.AuthPoolJSON, InvocationClass: optionalString(profile.InvocationClass), DefaultsJson: profile.DefaultsJSON, CreatedAt: profile.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: profile.UpdatedAt.Format(time.RFC3339Nano)}); err != nil {
		return fmt.Errorf("upsert agent profile %q: %w", profile.Name, err)
	}
	return nil
}

func (s *Store) EnsureDefaultHelperProfile(ctx context.Context, workspaceID, harness, prompt string) (Profile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return Profile{}, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if _, err := s.GetWorkspace(ctx, workspaceID); err != nil {
		return Profile{}, err
	}
	if existing, err := s.getExactProfile(ctx, workspaceID, DefaultHelperProfileName); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Profile{}, err
	}
	profileID, err := newProfileID()
	if err != nil {
		return Profile{}, err
	}
	profile := Profile{ProfileID: profileID, WorkspaceID: workspaceID, Name: DefaultHelperProfileName, Harness: strings.TrimSpace(harness), Prompt: strings.TrimSpace(prompt), InvocationClass: HarnessSessionUsageSticky, DefaultsJSON: "{}"}
	if err := s.UpsertProfile(ctx, profile); err != nil {
		return Profile{}, err
	}
	return s.getExactProfile(ctx, workspaceID, DefaultHelperProfileName)
}

func newProfileID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("generate agent profile id: %w", err)
	}
	return "ap_" + hex.EncodeToString(data[:]), nil
}

func (s *Store) GetDefaultHelperProfile(ctx context.Context, workspaceID string) (Profile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return Profile{}, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	return s.getExactProfile(ctx, workspaceID, DefaultHelperProfileName)
}

func (s *Store) getExactProfile(ctx context.Context, workspaceID, name string) (Profile, error) {
	if strings.TrimSpace(workspaceID) != "" {
		profile, err := s.sqlcQueries().GetWorkspaceProfileByName(ctx, dbsqlc.GetWorkspaceProfileByNameParams{WorkspaceID: optionalString(workspaceID), Name: name})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Profile{}, ErrNotFound
			}
			return Profile{}, fmt.Errorf("query exact workspace agent profile: %w", err)
		}
		return profileFromWorkspaceNameRow(profile)
	}
	profile, err := s.sqlcQueries().GetGlobalProfileByName(ctx, dbsqlc.GetGlobalProfileByNameParams{Name: name})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("query exact global agent profile: %w", err)
	}
	return profileFromGlobalNameRow(profile)
}

func (s *Store) GetProfile(ctx context.Context, workspaceID, name string) (Profile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	name = strings.TrimSpace(name)
	if name == "" {
		return Profile{}, fmt.Errorf("%w: profile name is required", ErrInvalidInput)
	}
	if workspaceID != "" {
		profile, err := s.sqlcQueries().GetWorkspaceProfileByName(ctx, dbsqlc.GetWorkspaceProfileByNameParams{WorkspaceID: optionalString(workspaceID), Name: name})
		if err == nil {
			return profileFromWorkspaceNameRow(profile)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Profile{}, fmt.Errorf("query workspace agent profile: %w", err)
		}
	}
	profile, err := s.sqlcQueries().GetGlobalProfileByName(ctx, dbsqlc.GetGlobalProfileByNameParams{Name: name})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, fmt.Errorf("query global agent profile: %w", err)
	}
	return profileFromGlobalNameRow(profile)
}

func (s *Store) ListProfiles(ctx context.Context, workspaceID string) ([]Profile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		rows, err := s.sqlcQueries().ListGlobalProfiles(ctx)
		if err != nil {
			return nil, fmt.Errorf("list agent profiles: %w", err)
		}
		profiles := make([]Profile, 0, len(rows))
		for _, row := range rows {
			profile, err := profileFromGlobalListRow(row)
			if err != nil {
				return nil, fmt.Errorf("list agent profiles: %w", err)
			}
			profiles = append(profiles, profile)
		}
		return profiles, nil
	}

	rows, err := s.sqlcQueries().ListWorkspaceProfiles(ctx, dbsqlc.ListWorkspaceProfilesParams{WorkspaceID: optionalString(workspaceID)})
	if err != nil {
		return nil, fmt.Errorf("list agent profiles: %w", err)
	}
	profiles := make([]Profile, 0, len(rows))
	for _, row := range rows {
		profile, err := profileFromWorkspaceListRow(row)
		if err != nil {
			return nil, fmt.Errorf("list agent profiles: %w", err)
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func profileFromWorkspaceNameRow(row dbsqlc.GetWorkspaceProfileByNameRow) (Profile, error) {
	return profileFromFields(row.ProfileID, row.WorkspaceID, row.Name, row.Harness, row.Model, row.Prompt, row.AuthSlotID, row.AuthPoolJson, row.InvocationClass, row.DefaultsJson, row.CreatedAt, row.UpdatedAt)
}

func profileFromGlobalNameRow(row dbsqlc.GetGlobalProfileByNameRow) (Profile, error) {
	return profileFromFields(row.ProfileID, row.WorkspaceID, row.Name, row.Harness, row.Model, row.Prompt, row.AuthSlotID, row.AuthPoolJson, row.InvocationClass, row.DefaultsJson, row.CreatedAt, row.UpdatedAt)
}

func profileFromWorkspaceListRow(row dbsqlc.ListWorkspaceProfilesRow) (Profile, error) {
	return profileFromFields(row.ProfileID, row.WorkspaceID, row.Name, row.Harness, row.Model, row.Prompt, row.AuthSlotID, row.AuthPoolJson, row.InvocationClass, row.DefaultsJson, row.CreatedAt, row.UpdatedAt)
}

func profileFromGlobalListRow(row dbsqlc.ListGlobalProfilesRow) (Profile, error) {
	return profileFromFields(row.ProfileID, row.WorkspaceID, row.Name, row.Harness, row.Model, row.Prompt, row.AuthSlotID, row.AuthPoolJson, row.InvocationClass, row.DefaultsJson, row.CreatedAt, row.UpdatedAt)
}

func profileFromFields(profileID string, workspaceID *string, name string, harness *string, model *string, prompt *string, authSlotID *string, authPoolJSON string, invocationClass *string, defaultsJSON string, createdAtValue string, updatedAtValue string) (Profile, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtValue)
	if err != nil {
		return Profile{}, fmt.Errorf("parse profile %q created_at %q: %w", profileID, createdAtValue, err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtValue)
	if err != nil {
		return Profile{}, fmt.Errorf("parse profile %q updated_at %q: %w", profileID, updatedAtValue, err)
	}
	return Profile{ProfileID: profileID, WorkspaceID: stringValue(workspaceID), Name: name, Harness: stringValue(harness), Model: stringValue(model), Prompt: stringValue(prompt), AuthSlotID: stringValue(authSlotID), AuthPoolJSON: authPoolJSON, InvocationClass: stringValue(invocationClass), DefaultsJSON: defaultsJSON, CreatedAt: createdAt, UpdatedAt: updatedAt}, nil
}
