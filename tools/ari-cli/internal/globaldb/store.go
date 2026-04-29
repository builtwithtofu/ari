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

var (
	ErrInvalidInput  = errors.New("invalid globaldb input")
	ErrNotFound      = errors.New("globaldb record not found")
	ErrSessionClosed = errors.New("workspace is closed")
	ErrLastFolder    = errors.New("cannot remove last workspace folder")
)

const (
	statusActive    = "active"
	statusSuspended = "suspended"
	statusClosed    = "closed"

	cleanupPolicyManual  = "manual"
	cleanupPolicyOnClose = "on_close"

	vcsTypeGit     = "git"
	vcsTypeJJ      = "jj"
	vcsTypeUnknown = "unknown"

	commandStatusRunning = "running"
	commandStatusExited  = "exited"
	commandStatusLost    = "lost"

	agentStatusRunning = "running"
	agentStatusStopped = "stopped"
	agentStatusExited  = "exited"
	agentStatusLost    = "lost"
)

type Session struct {
	ID            string
	Name          string
	Status        string
	VCSPreference string
	OriginRoot    string
	CleanupPolicy string
	CreatedAt     string
	UpdatedAt     string
}

type SessionFolder struct {
	WorkspaceID string
	FolderPath  string
	VCSType     string
	IsPrimary   bool
	AddedAt     string
}

type Command struct {
	CommandID   string
	WorkspaceID string
	Command     string
	Args        string
	Status      string
	ExitCode    *int
	StartedAt   string
	FinishedAt  *string
}

type WorkspaceCommandDefinition struct {
	CommandID   string
	WorkspaceID string
	Name        string
	Command     string
	Args        string
	CreatedAt   string
	UpdatedAt   string
}

type CreateCommandParams struct {
	CommandID   string
	WorkspaceID string
	Command     string
	Args        string
	Status      string
	StartedAt   string
	ExitCode    *int
	FinishedAt  *string
}

type UpdateCommandStatusParams struct {
	WorkspaceID string
	CommandID   string
	Status      string
	ExitCode    *int
	FinishedAt  *string
}

type CreateWorkspaceCommandDefinitionParams struct {
	CommandID   string
	WorkspaceID string
	Name        string
	Command     string
	Args        string
}

type Agent struct {
	AgentID            string
	WorkspaceID        string
	Name               *string
	Command            string
	Args               string
	Status             string
	ExitCode           *int
	StartedAt          string
	StoppedAt          *string
	Harness            *string
	HarnessResumableID *string
	HarnessMetadata    string
	InvocationClass    string
}

type CreateAgentParams struct {
	AgentID            string
	WorkspaceID        string
	Name               *string
	Command            string
	Args               string
	Status             string
	ExitCode           *int
	StartedAt          string
	StoppedAt          *string
	Harness            *string
	HarnessResumableID *string
	HarnessMetadata    string
	InvocationClass    string
}

type UpdateAgentStatusParams struct {
	WorkspaceID string
	AgentID     string
	Status      string
	ExitCode    *int
	StoppedAt   *string
}

type Store struct {
	db   *sql.DB
	sqlc *dbsqlc.Queries
}

type AgentProfile struct {
	ProfileID       string
	WorkspaceID     string
	Name            string
	Harness         string
	Model           string
	Prompt          string
	InvocationClass string
	DefaultsJSON    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

const DefaultHelperProfileName = "helper"

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

type FinalResponse struct {
	FinalResponseID   string
	RunID             string
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

type KnownInt64 struct {
	Known bool
	Value *int64
}

type AgentRunTelemetry struct {
	RunID                   string
	WorkspaceID             string
	TaskID                  string
	ProfileID               string
	ProfileName             string
	Harness                 string
	Model                   string
	InvocationClass         string
	Status                  string
	InputTokensKnown        bool
	InputTokens             *int64
	OutputTokensKnown       bool
	OutputTokens            *int64
	EstimatedCostKnown      bool
	EstimatedCostMicros     *int64
	DurationMSKnown         bool
	DurationMS              *int64
	ExitCodeKnown           bool
	ExitCode                *int64
	OwnedByAri              bool
	PIDKnown                bool
	PID                     *int64
	CPUTimeMSKnown          bool
	CPUTimeMS               *int64
	MemoryRSSBytesPeakKnown bool
	MemoryRSSBytesPeak      *int64
	ChildProcessesPeakKnown bool
	ChildProcessesPeak      *int64
	PortsJSON               string
	OrphanState             string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type AgentRunTelemetryGroup struct {
	ProfileID       string
	ProfileName     string
	Harness         string
	Model           string
	InvocationClass string
}

type AgentRunTelemetryRollup struct {
	Group         AgentRunTelemetryGroup
	Runs          int
	Completed     int
	Failed        int
	InputTokens   KnownInt64
	OutputTokens  KnownInt64
	EstimatedCost KnownInt64
	DurationMS    KnownInt64
	ExitCode      KnownInt64
	PID           KnownInt64
	CPUTimeMS     KnownInt64
	MemoryRSS     KnownInt64
	ChildCount    KnownInt64
	OwnedByAri    bool
	PortsJSON     string
	OrphanState   string
}

func NewSQLStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: db is required", ErrInvalidInput)
	}
	return &Store{db: db, sqlc: dbsqlc.New(db)}, nil
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	if key == "" {
		return fmt.Errorf("%w: key is required", ErrInvalidInput)
	}

	if err := s.sqlcQueries().UpsertMeta(ctx, dbsqlc.UpsertMetaParams{Key: key, Value: value}); err != nil {
		return fmt.Errorf("set meta %q: %w", key, err)
	}

	return nil
}

func (s *Store) GetMeta(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: key is required", ErrInvalidInput)
	}

	value, err := s.sqlcQueries().GetMetaValue(ctx, dbsqlc.GetMetaValueParams{Key: key})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w: key %q", ErrNotFound, key)
		}
		return "", err
	}

	return value, nil
}

func (s *Store) CompareAndSwapMeta(ctx context.Context, key, oldValue, newValue string) (bool, error) {
	if key == "" {
		return false, fmt.Errorf("%w: key is required", ErrInvalidInput)
	}

	changed, err := s.sqlcQueries().CompareAndSwapMeta(ctx, dbsqlc.CompareAndSwapMetaParams{Value: newValue, Key: key, Value_2: oldValue})
	if err != nil {
		return false, fmt.Errorf("compare and swap meta %q: %w", key, err)
	}
	return changed == 1, nil
}

func (s *Store) UpsertAgentProfile(ctx context.Context, profile AgentProfile) error {
	profile.ProfileID = strings.TrimSpace(profile.ProfileID)
	profile.WorkspaceID = strings.TrimSpace(profile.WorkspaceID)
	profile.Name = strings.TrimSpace(profile.Name)
	profile.Harness = strings.TrimSpace(profile.Harness)
	profile.Model = strings.TrimSpace(profile.Model)
	profile.InvocationClass = strings.TrimSpace(profile.InvocationClass)
	if profile.ProfileID == "" {
		return fmt.Errorf("%w: profile id is required", ErrInvalidInput)
	}
	if profile.Name == "" {
		return fmt.Errorf("%w: profile name is required", ErrInvalidInput)
	}
	if existing, err := s.getExactAgentProfile(ctx, profile.WorkspaceID, profile.Name); err == nil {
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
	if !json.Valid([]byte(profile.DefaultsJSON)) {
		return fmt.Errorf("%w: profile defaults json is invalid", ErrInvalidInput)
	}
	now := time.Now().UTC()
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = now
	}
	profile.UpdatedAt = now
	if err := s.sqlcQueries().UpsertAgentProfile(ctx, dbsqlc.UpsertAgentProfileParams{ProfileID: profile.ProfileID, WorkspaceID: optionalString(profile.WorkspaceID), Name: profile.Name, Harness: optionalString(profile.Harness), Model: optionalString(profile.Model), Prompt: optionalString(profile.Prompt), InvocationClass: optionalString(profile.InvocationClass), DefaultsJson: profile.DefaultsJSON, CreatedAt: profile.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: profile.UpdatedAt.Format(time.RFC3339Nano)}); err != nil {
		return fmt.Errorf("upsert agent profile %q: %w", profile.Name, err)
	}
	return nil
}

func (s *Store) EnsureDefaultHelperProfile(ctx context.Context, workspaceID, harness, prompt string) (AgentProfile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return AgentProfile{}, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if _, err := s.GetSession(ctx, workspaceID); err != nil {
		return AgentProfile{}, err
	}
	if existing, err := s.getExactAgentProfile(ctx, workspaceID, DefaultHelperProfileName); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return AgentProfile{}, err
	}
	profileID, err := newAgentProfileID()
	if err != nil {
		return AgentProfile{}, err
	}
	profile := AgentProfile{ProfileID: profileID, WorkspaceID: workspaceID, Name: DefaultHelperProfileName, Harness: strings.TrimSpace(harness), Prompt: strings.TrimSpace(prompt), InvocationClass: "agent", DefaultsJSON: "{}"}
	if err := s.UpsertAgentProfile(ctx, profile); err != nil {
		return AgentProfile{}, err
	}
	return s.getExactAgentProfile(ctx, workspaceID, DefaultHelperProfileName)
}

func newAgentProfileID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("generate agent profile id: %w", err)
	}
	return "ap_" + hex.EncodeToString(data[:]), nil
}

func (s *Store) GetDefaultHelperProfile(ctx context.Context, workspaceID string) (AgentProfile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return AgentProfile{}, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	return s.getExactAgentProfile(ctx, workspaceID, DefaultHelperProfileName)
}

func (s *Store) getExactAgentProfile(ctx context.Context, workspaceID, name string) (AgentProfile, error) {
	if strings.TrimSpace(workspaceID) != "" {
		profile, err := s.sqlcQueries().GetWorkspaceAgentProfileByName(ctx, dbsqlc.GetWorkspaceAgentProfileByNameParams{WorkspaceID: optionalString(workspaceID), Name: name})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return AgentProfile{}, ErrNotFound
			}
			return AgentProfile{}, fmt.Errorf("query exact workspace agent profile: %w", err)
		}
		return agentProfileFromSQLC(profile), nil
	}
	profile, err := s.sqlcQueries().GetGlobalAgentProfileByName(ctx, dbsqlc.GetGlobalAgentProfileByNameParams{Name: name})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AgentProfile{}, ErrNotFound
		}
		return AgentProfile{}, fmt.Errorf("query exact global agent profile: %w", err)
	}
	return agentProfileFromSQLC(profile), nil
}

func (s *Store) GetAgentProfile(ctx context.Context, workspaceID, name string) (AgentProfile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	name = strings.TrimSpace(name)
	if name == "" {
		return AgentProfile{}, fmt.Errorf("%w: profile name is required", ErrInvalidInput)
	}
	if workspaceID != "" {
		profile, err := s.sqlcQueries().GetWorkspaceAgentProfileByName(ctx, dbsqlc.GetWorkspaceAgentProfileByNameParams{WorkspaceID: optionalString(workspaceID), Name: name})
		if err == nil {
			return agentProfileFromSQLC(profile), nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return AgentProfile{}, fmt.Errorf("query workspace agent profile: %w", err)
		}
	}
	profile, err := s.sqlcQueries().GetGlobalAgentProfileByName(ctx, dbsqlc.GetGlobalAgentProfileByNameParams{Name: name})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AgentProfile{}, ErrNotFound
		}
		return AgentProfile{}, fmt.Errorf("query global agent profile: %w", err)
	}
	return agentProfileFromSQLC(profile), nil
}

func (s *Store) ListAgentProfiles(ctx context.Context, workspaceID string) ([]AgentProfile, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	var rows []dbsqlc.AgentProfile
	var err error
	if workspaceID == "" {
		rows, err = s.sqlcQueries().ListGlobalAgentProfiles(ctx)
	} else {
		rows, err = s.sqlcQueries().ListWorkspaceAgentProfiles(ctx, dbsqlc.ListWorkspaceAgentProfilesParams{WorkspaceID: optionalString(workspaceID)})
	}
	if err != nil {
		return nil, fmt.Errorf("list agent profiles: %w", err)
	}
	profiles := make([]AgentProfile, 0, len(rows))
	for _, row := range rows {
		profiles = append(profiles, agentProfileFromSQLC(row))
	}
	return profiles, nil
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
	if authSlotMetadataContainsSourceFields(slot.MetadataJSON) {
		return fmt.Errorf("%w: auth slot metadata must not include credential source fields", ErrInvalidInput)
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

func validAuthSlotStatus(status string) bool {
	switch status {
	case "authenticated", "auth_required", "auth_in_progress", "auth_failed", "cancelled", "unknown", "not_installed":
		return true
	default:
		return false
	}
}

func authSlotMetadataContainsSourceFields(raw string) bool {
	var metadata any
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return true
	}
	return authSlotMetadataValueContainsSourceFields(metadata)
}

func authSlotMetadataValueContainsSourceFields(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.TrimSpace(key))
			if normalized == "source" || normalized == "source_ref" || normalized == "credential_source" || normalized == "credential_source_ref" {
				return true
			}
			if authSlotMetadataValueContainsSourceFields(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if authSlotMetadataValueContainsSourceFields(child) {
				return true
			}
		}
	}
	return false
}

func (s *Store) UpsertFinalResponse(ctx context.Context, response FinalResponse) error {
	response.FinalResponseID = strings.TrimSpace(response.FinalResponseID)
	response.RunID = strings.TrimSpace(response.RunID)
	response.WorkspaceID = strings.TrimSpace(response.WorkspaceID)
	response.TaskID = strings.TrimSpace(response.TaskID)
	response.ContextPacketID = strings.TrimSpace(response.ContextPacketID)
	response.ProfileID = strings.TrimSpace(response.ProfileID)
	response.Status = strings.TrimSpace(response.Status)
	response.Text = strings.TrimSpace(response.Text)
	if response.FinalResponseID == "" {
		return fmt.Errorf("%w: final response id is required", ErrInvalidInput)
	}
	if response.RunID == "" {
		return fmt.Errorf("%w: run id is required", ErrInvalidInput)
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
	if err := s.sqlcQueries().UpsertFinalResponse(ctx, dbsqlc.UpsertFinalResponseParams{FinalResponseID: response.FinalResponseID, RunID: response.RunID, WorkspaceID: response.WorkspaceID, TaskID: response.TaskID, ContextPacketID: response.ContextPacketID, ProfileID: optionalString(response.ProfileID), Status: response.Status, Text: response.Text, EvidenceLinks: response.EvidenceLinksJSON, CreatedAt: response.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: updatedAt}); err != nil {
		return fmt.Errorf("upsert final response %q: %w", response.FinalResponseID, err)
	}
	return nil
}

func (s *Store) GetFinalResponseByRunID(ctx context.Context, runID string) (FinalResponse, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return FinalResponse{}, fmt.Errorf("%w: run id is required", ErrInvalidInput)
	}
	row, err := s.sqlcQueries().GetFinalResponseByRunID(ctx, dbsqlc.GetFinalResponseByRunIDParams{RunID: runID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FinalResponse{}, ErrNotFound
		}
		return FinalResponse{}, fmt.Errorf("query final response by run id: %w", err)
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

func (s *Store) UpsertAgentRunTelemetry(ctx context.Context, telemetry AgentRunTelemetry) error {
	telemetry.RunID = strings.TrimSpace(telemetry.RunID)
	telemetry.WorkspaceID = strings.TrimSpace(telemetry.WorkspaceID)
	telemetry.TaskID = strings.TrimSpace(telemetry.TaskID)
	telemetry.ProfileID = strings.TrimSpace(telemetry.ProfileID)
	telemetry.ProfileName = strings.TrimSpace(telemetry.ProfileName)
	telemetry.Harness = strings.TrimSpace(telemetry.Harness)
	telemetry.Model = strings.TrimSpace(telemetry.Model)
	telemetry.InvocationClass = strings.TrimSpace(telemetry.InvocationClass)
	telemetry.Status = strings.TrimSpace(telemetry.Status)
	if telemetry.RunID == "" {
		return fmt.Errorf("%w: run id is required", ErrInvalidInput)
	}
	if telemetry.WorkspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if telemetry.TaskID == "" {
		return fmt.Errorf("%w: task id is required", ErrInvalidInput)
	}
	if telemetry.Harness == "" {
		return fmt.Errorf("%w: harness is required", ErrInvalidInput)
	}
	if telemetry.Model == "" {
		telemetry.Model = "unknown"
	}
	if telemetry.InvocationClass == "" {
		telemetry.InvocationClass = "agent"
	}
	if telemetry.Status == "" {
		telemetry.Status = "unknown"
	}
	if strings.TrimSpace(telemetry.PortsJSON) == "" {
		telemetry.PortsJSON = "[]"
	}
	if !json.Valid([]byte(telemetry.PortsJSON)) {
		return fmt.Errorf("%w: ports json is invalid", ErrInvalidInput)
	}
	if strings.TrimSpace(telemetry.OrphanState) == "" {
		telemetry.OrphanState = "unknown"
	}
	now := time.Now().UTC()
	if telemetry.CreatedAt.IsZero() {
		telemetry.CreatedAt = now
	}
	if telemetry.UpdatedAt.IsZero() {
		telemetry.UpdatedAt = now
	}
	params := dbsqlc.UpsertAgentRunTelemetryParams{RunID: telemetry.RunID, WorkspaceID: telemetry.WorkspaceID, TaskID: telemetry.TaskID, ProfileID: optionalString(telemetry.ProfileID), ProfileName: optionalString(telemetry.ProfileName), Harness: telemetry.Harness, Model: telemetry.Model, InvocationClass: telemetry.InvocationClass, Status: telemetry.Status, InputTokensKnown: boolInt64(telemetry.InputTokensKnown), InputTokens: telemetry.InputTokens, OutputTokensKnown: boolInt64(telemetry.OutputTokensKnown), OutputTokens: telemetry.OutputTokens, EstimatedCostKnown: boolInt64(telemetry.EstimatedCostKnown), EstimatedCostMicros: telemetry.EstimatedCostMicros, DurationMsKnown: boolInt64(telemetry.DurationMSKnown), DurationMs: telemetry.DurationMS, ExitCodeKnown: boolInt64(telemetry.ExitCodeKnown), ExitCode: telemetry.ExitCode, OwnedByAri: boolInt64(telemetry.OwnedByAri), PidKnown: boolInt64(telemetry.PIDKnown), Pid: telemetry.PID, CpuTimeMsKnown: boolInt64(telemetry.CPUTimeMSKnown), CpuTimeMs: telemetry.CPUTimeMS, MemoryRssBytesPeakKnown: boolInt64(telemetry.MemoryRSSBytesPeakKnown), MemoryRssBytesPeak: telemetry.MemoryRSSBytesPeak, ChildProcessesPeakKnown: boolInt64(telemetry.ChildProcessesPeakKnown), ChildProcessesPeak: telemetry.ChildProcessesPeak, PortsJson: telemetry.PortsJSON, OrphanState: telemetry.OrphanState, CreatedAt: telemetry.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: telemetry.UpdatedAt.Format(time.RFC3339Nano)}
	if err := s.sqlcQueries().UpsertAgentRunTelemetry(ctx, params); err != nil {
		return fmt.Errorf("upsert agent run telemetry %q: %w", telemetry.RunID, err)
	}
	return nil
}

func (s *Store) RollupAgentRunTelemetry(ctx context.Context, workspaceID string) ([]AgentRunTelemetryRollup, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	rows, err := s.sqlcQueries().ListAgentRunTelemetryByWorkspace(ctx, dbsqlc.ListAgentRunTelemetryByWorkspaceParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, fmt.Errorf("list agent run telemetry: %w", err)
	}
	byGroup := map[AgentRunTelemetryGroup]*AgentRunTelemetryRollup{}
	order := []AgentRunTelemetryGroup{}
	for _, row := range rows {
		group := AgentRunTelemetryGroup{ProfileID: stringValue(row.ProfileID), ProfileName: stringValue(row.ProfileName), Harness: row.Harness, Model: row.Model, InvocationClass: row.InvocationClass}
		rollup := byGroup[group]
		if rollup == nil {
			rollup = &AgentRunTelemetryRollup{Group: group}
			byGroup[group] = rollup
			order = append(order, group)
		}
		rollup.Runs++
		switch row.Status {
		case "completed":
			rollup.Completed++
		case "failed":
			rollup.Failed++
		}
		addKnownInt64(&rollup.InputTokens, row.InputTokensKnown, row.InputTokens)
		addKnownInt64(&rollup.OutputTokens, row.OutputTokensKnown, row.OutputTokens)
		addKnownInt64(&rollup.EstimatedCost, row.EstimatedCostKnown, row.EstimatedCostMicros)
		addKnownInt64(&rollup.DurationMS, row.DurationMsKnown, row.DurationMs)
		addKnownInt64(&rollup.ExitCode, row.ExitCodeKnown, row.ExitCode)
		addKnownInt64(&rollup.PID, row.PidKnown, row.Pid)
		addKnownInt64(&rollup.CPUTimeMS, row.CpuTimeMsKnown, row.CpuTimeMs)
		maxKnownInt64(&rollup.MemoryRSS, row.MemoryRssBytesPeakKnown, row.MemoryRssBytesPeak)
		maxKnownInt64(&rollup.ChildCount, row.ChildProcessesPeakKnown, row.ChildProcessesPeak)
		rollup.OwnedByAri = rollup.OwnedByAri || row.OwnedByAri != 0
		if rollup.PortsJSON == "" && strings.TrimSpace(row.PortsJson) != "" && strings.TrimSpace(row.PortsJson) != "[]" {
			rollup.PortsJSON = row.PortsJson
		}
		if (rollup.OrphanState == "" || rollup.OrphanState == "unknown") && strings.TrimSpace(row.OrphanState) != "" {
			rollup.OrphanState = row.OrphanState
		}
	}
	rollups := make([]AgentRunTelemetryRollup, 0, len(order))
	for _, group := range order {
		if byGroup[group].Runs != 1 {
			byGroup[group].PID = KnownInt64{}
			byGroup[group].ExitCode = KnownInt64{}
		}
		rollups = append(rollups, *byGroup[group])
	}
	return rollups, nil
}

func boolInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func addKnownInt64(total *KnownInt64, known int64, value *int64) {
	if known == 0 || value == nil {
		return
	}
	if total.Value == nil {
		zero := int64(0)
		total.Value = &zero
	}
	total.Known = true
	*total.Value += *value
}

func maxKnownInt64(total *KnownInt64, known int64, value *int64) {
	if known == 0 || value == nil {
		return
	}
	if total.Value == nil || *value > *total.Value {
		v := *value
		total.Value = &v
	}
	total.Known = true
}

func (s *Store) sqlcQueries() *dbsqlc.Queries {
	if s.sqlc != nil {
		return s.sqlc
	}
	s.sqlc = dbsqlc.New(s.db)
	return s.sqlc
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func agentProfileFromSQLC(row dbsqlc.AgentProfile) AgentProfile {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	return AgentProfile{ProfileID: row.ProfileID, WorkspaceID: stringValue(row.WorkspaceID), Name: row.Name, Harness: stringValue(row.Harness), Model: stringValue(row.Model), Prompt: stringValue(row.Prompt), InvocationClass: stringValue(row.InvocationClass), DefaultsJSON: row.DefaultsJson, CreatedAt: createdAt, UpdatedAt: updatedAt}
}

func authSlotFromSQLC(row dbsqlc.AuthSlot) AuthSlot {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	return AuthSlot{AuthSlotID: row.AuthSlotID, Harness: row.Harness, Label: row.Label, ProviderLabel: stringValue(row.ProviderLabel), CredentialOwner: row.CredentialOwner, Status: row.Status, MetadataJSON: row.MetadataJson, CreatedAt: createdAt, UpdatedAt: updatedAt}
}

func finalResponseFromSQLC(row dbsqlc.FinalResponse) FinalResponse {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	var updatedAt *time.Time
	if row.UpdatedAt != nil {
		parsed, _ := time.Parse(time.RFC3339Nano, *row.UpdatedAt)
		updatedAt = &parsed
	}
	return FinalResponse{FinalResponseID: row.FinalResponseID, RunID: row.RunID, WorkspaceID: row.WorkspaceID, TaskID: row.TaskID, ContextPacketID: row.ContextPacketID, ProfileID: stringValue(row.ProfileID), Status: row.Status, Text: row.Text, EvidenceLinksJSON: row.EvidenceLinks, CreatedAt: createdAt, UpdatedAt: updatedAt}
}

func ptrString(value string) *string { return &value }

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *Store) withImmediateQueries(ctx context.Context, fn func(*dbsqlc.Queries) error) error {
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
	if err := fn(dbsqlc.New(conn)); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

func optionalInt(value *int) *int64 {
	if value == nil {
		return nil
	}
	out := int64(*value)
	return &out
}

func intPtrFromInt64(value *int64) *int {
	if value == nil {
		return nil
	}
	out := int(*value)
	return &out
}

func sessionFromSQLC(row dbsqlc.Workspace) Session {
	return Session{ID: row.WorkspaceID, Name: row.Name, Status: row.Status, VCSPreference: row.VcsPreference, OriginRoot: row.OriginRoot, CleanupPolicy: row.CleanupPolicy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func sessionFolderFromSQLC(row dbsqlc.WorkspaceFolder) SessionFolder {
	return SessionFolder{WorkspaceID: row.WorkspaceID, FolderPath: row.FolderPath, VCSType: row.VcsType, IsPrimary: row.IsPrimary != 0, AddedAt: row.AddedAt}
}

func commandFromSQLC(row dbsqlc.Command) Command {
	return Command{CommandID: row.CommandID, WorkspaceID: row.WorkspaceID, Command: row.Command, Args: row.Args, Status: row.Status, ExitCode: intPtrFromInt64(row.ExitCode), StartedAt: row.StartedAt, FinishedAt: row.FinishedAt}
}

func workspaceCommandDefinitionFromSQLC(row dbsqlc.WorkspaceCommandDefinition) WorkspaceCommandDefinition {
	return WorkspaceCommandDefinition{CommandID: row.CommandID, WorkspaceID: row.WorkspaceID, Name: row.Name, Command: row.Command, Args: row.Args, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func agentFromSQLC(row dbsqlc.Agent) Agent {
	return Agent{AgentID: row.AgentID, WorkspaceID: row.WorkspaceID, Name: row.Name, Command: row.Command, Args: row.Args, Status: row.Status, ExitCode: intPtrFromInt64(row.ExitCode), StartedAt: row.StartedAt, StoppedAt: row.StoppedAt, Harness: row.Harness, HarnessResumableID: row.HarnessResumableID, HarnessMetadata: row.HarnessMetadata, InvocationClass: row.InvocationClass}
}

func (s *Store) CreateSession(ctx context.Context, id, name, originRoot, cleanupPolicy, vcsPreference string) error {
	if id = strings.TrimSpace(id); id == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if name = strings.TrimSpace(name); name == "" {
		return fmt.Errorf("%w: session name is required", ErrInvalidInput)
	}
	originRoot = strings.TrimSpace(originRoot)
	if err := validateCleanupPolicy(cleanupPolicy); err != nil {
		return err
	}
	if err := validateVCSPreference(vcsPreference); err != nil {
		return err
	}
	if originRoot == "" {
		return fmt.Errorf("%w: origin root is required", ErrInvalidInput)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.sqlcQueries().CreateWorkspace(ctx, dbsqlc.CreateWorkspaceParams{WorkspaceID: id, Name: name, Status: statusActive, VcsPreference: vcsPreference, OriginRoot: originRoot, CleanupPolicy: cleanupPolicy, CreatedAt: now, UpdatedAt: now}); err != nil {
		return fmt.Errorf("create session %q: %w", id, err)
	}

	return nil
}

func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	if id = strings.TrimSpace(id); id == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}

	row, err := s.sqlcQueries().GetWorkspaceByID(ctx, dbsqlc.GetWorkspaceByIDParams{WorkspaceID: id})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: session id %q", ErrNotFound, id)
		}
		return nil, err
	}
	session := sessionFromSQLC(row)
	return &session, nil
}

func (s *Store) GetSessionByName(ctx context.Context, name string) (*Session, error) {
	if name = strings.TrimSpace(name); name == "" {
		return nil, fmt.Errorf("%w: session name is required", ErrInvalidInput)
	}

	row, err := s.sqlcQueries().GetWorkspaceByName(ctx, dbsqlc.GetWorkspaceByNameParams{Name: name})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: session name %q", ErrNotFound, name)
		}
		return nil, err
	}
	session := sessionFromSQLC(row)
	return &session, nil
}

func (s *Store) ListSessions(ctx context.Context) ([]Session, error) {
	rows, err := s.sqlcQueries().ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(rows))
	for _, row := range rows {
		out = append(out, sessionFromSQLC(row))
	}
	return out, nil
}

func (s *Store) UpdateSessionStatus(ctx context.Context, id, status string) error {
	if id = strings.TrimSpace(id); id == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if status = strings.TrimSpace(status); status == "" {
		return fmt.Errorf("%w: session status is required", ErrInvalidInput)
	}
	if !isValidSessionStatus(status) {
		return fmt.Errorf("%w: invalid status %q", ErrInvalidInput, status)
	}

	session, err := s.GetSession(ctx, id)
	if err != nil {
		return err
	}
	if !canTransitionSessionStatus(session.Status, status) {
		if session.Status == statusClosed {
			return fmt.Errorf("%w: session id %q", ErrSessionClosed, id)
		}
		return fmt.Errorf("%w: invalid session transition %q -> %q", ErrInvalidInput, session.Status, status)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	rowsAffected, err := s.sqlcQueries().UpdateWorkspaceStatus(ctx, dbsqlc.UpdateWorkspaceStatusParams{Status: status, UpdatedAt: now, WorkspaceID: id})
	if err != nil {
		return fmt.Errorf("update session status %q: %w", id, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: session id %q", ErrNotFound, id)
	}

	return nil
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if id = strings.TrimSpace(id); id == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}

	rowsAffected, err := s.sqlcQueries().DeleteWorkspace(ctx, dbsqlc.DeleteWorkspaceParams{WorkspaceID: id})
	if err != nil {
		return fmt.Errorf("delete session %q: %w", id, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: session id %q", ErrNotFound, id)
	}

	return nil
}

func (s *Store) AddFolder(ctx context.Context, sessionID, folderPath, vcsType string, isPrimary bool) error {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if folderPath = strings.TrimSpace(folderPath); folderPath == "" {
		return fmt.Errorf("%w: folder path is required", ErrInvalidInput)
	}
	if vcsType = strings.TrimSpace(vcsType); vcsType == "" {
		return fmt.Errorf("%w: vcs type is required", ErrInvalidInput)
	}
	if !isValidVCSType(vcsType) {
		return fmt.Errorf("%w: invalid vcs type %q", ErrInvalidInput, vcsType)
	}

	return s.withImmediateQueries(ctx, func(queries *dbsqlc.Queries) error {
		return addFolderInTransaction(ctx, queries, sessionID, folderPath, vcsType, isPrimary)
	})
}

func (s *Store) RemoveFolder(ctx context.Context, sessionID, folderPath string) error {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if folderPath = strings.TrimSpace(folderPath); folderPath == "" {
		return fmt.Errorf("%w: folder path is required", ErrInvalidInput)
	}

	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session.Status == statusClosed {
		return fmt.Errorf("%w: session id %q", ErrSessionClosed, sessionID)
	}

	rowsAffected, err := s.sqlcQueries().DeleteWorkspaceFolderIfNotLast(ctx, dbsqlc.DeleteWorkspaceFolderIfNotLastParams{WorkspaceID: sessionID, FolderPath: folderPath, WorkspaceID_2: sessionID})
	if err != nil {
		return fmt.Errorf("remove session folder %q: %w", folderPath, err)
	}
	if rowsAffected == 0 {
		folders, listErr := s.ListFolders(ctx, sessionID)
		if listErr != nil {
			return listErr
		}

		for _, folder := range folders {
			if folder.FolderPath == folderPath {
				return fmt.Errorf("%w: session id %q", ErrLastFolder, sessionID)
			}
		}

		return fmt.Errorf("%w: folder %q for session %q", ErrNotFound, folderPath, sessionID)
	}

	folders, err := s.ListFolders(ctx, sessionID)
	if err != nil {
		return err
	}
	if len(folders) == 0 {
		return fmt.Errorf("%w: session id %q", ErrLastFolder, sessionID)
	}

	hasPrimary := false
	for _, folder := range folders {
		if folder.IsPrimary {
			hasPrimary = true
			break
		}
	}
	if !hasPrimary {
		if err := s.sqlcQueries().PromotePrimaryWorkspaceFolder(ctx, dbsqlc.PromotePrimaryWorkspaceFolderParams{FolderPath: folders[0].FolderPath, WorkspaceID: sessionID}); err != nil {
			return fmt.Errorf("promote session primary folder %q: %w", folders[0].FolderPath, err)
		}
	}

	return nil
}

func (s *Store) ListFolders(ctx context.Context, sessionID string) ([]SessionFolder, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}

	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	_ = session

	rows, err := s.sqlcQueries().ListWorkspaceFolders(ctx, dbsqlc.ListWorkspaceFoldersParams{WorkspaceID: sessionID})
	if err != nil {
		return nil, err
	}
	out := make([]SessionFolder, 0, len(rows))
	for _, row := range rows {
		out = append(out, sessionFolderFromSQLC(row))
	}

	return out, nil
}

func addFolderInTransaction(ctx context.Context, queries *dbsqlc.Queries, sessionID, folderPath, vcsType string, isPrimary bool) error {
	row, err := queries.GetWorkspaceByID(ctx, dbsqlc.GetWorkspaceByIDParams{WorkspaceID: sessionID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: session id %q", ErrNotFound, sessionID)
		}
		return err
	}
	if row.Status == statusClosed {
		return fmt.Errorf("%w: session id %q", ErrSessionClosed, sessionID)
	}
	owners, err := workspaceOwnersByFolderPath(ctx, queries, folderPath)
	if err != nil {
		return err
	}
	for _, owner := range owners {
		if owner.WorkspaceID != sessionID && owner.Status != statusClosed {
			return fmt.Errorf("%w: folder %q already belongs to workspace %q", ErrInvalidInput, folderPath, owner.WorkspaceID)
		}
	}

	primary := 0
	if isPrimary {
		primary = 1
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := queries.CreateWorkspaceFolder(ctx, dbsqlc.CreateWorkspaceFolderParams{WorkspaceID: sessionID, FolderPath: folderPath, VcsType: vcsType, IsPrimary: int64(primary), AddedAt: now}); err != nil {
		return fmt.Errorf("add session folder %q: %w", folderPath, err)
	}

	if isPrimary {
		if err := queries.PromotePrimaryWorkspaceFolder(ctx, dbsqlc.PromotePrimaryWorkspaceFolderParams{FolderPath: folderPath, WorkspaceID: sessionID}); err != nil {
			return fmt.Errorf("promote session primary folder %q: %w", folderPath, err)
		}
	}

	return nil
}

type workspaceFolderOwner struct {
	WorkspaceID string
	Status      string
}

func workspaceOwnersByFolderPath(ctx context.Context, queries *dbsqlc.Queries, folderPath string) ([]workspaceFolderOwner, error) {
	folderPath = strings.TrimSpace(folderPath)
	if folderPath == "" {
		return nil, fmt.Errorf("%w: folder path is required", ErrInvalidInput)
	}

	rows, err := queries.ListWorkspaceOwnersByFolderPath(ctx, dbsqlc.ListWorkspaceOwnersByFolderPathParams{FolderPath: folderPath})
	if err != nil {
		return nil, fmt.Errorf("lookup workspaces by folder path %q: %w", folderPath, err)
	}

	owners := make([]workspaceFolderOwner, 0, len(rows))
	for _, row := range rows {
		owner := workspaceFolderOwner{WorkspaceID: row.WorkspaceID, Status: row.Status}
		owner.WorkspaceID = strings.TrimSpace(owner.WorkspaceID)
		owner.Status = strings.TrimSpace(owner.Status)
		if owner.WorkspaceID == "" {
			return nil, fmt.Errorf("%w: folder %q has empty workspace id", ErrInvalidInput, folderPath)
		}
		if owner.Status == "" {
			return nil, fmt.Errorf("%w: folder %q owner %q has empty workspace status", ErrInvalidInput, folderPath, owner.WorkspaceID)
		}
		owners = append(owners, owner)
	}

	return owners, nil
}

func (s *Store) CreateCommand(ctx context.Context, params CreateCommandParams) error {
	if params.CommandID = strings.TrimSpace(params.CommandID); params.CommandID == "" {
		return fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}
	if params.WorkspaceID = strings.TrimSpace(params.WorkspaceID); params.WorkspaceID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if _, err := s.GetSession(ctx, params.WorkspaceID); err != nil {
		return err
	}
	if params.Command = strings.TrimSpace(params.Command); params.Command == "" {
		return fmt.Errorf("%w: command is required", ErrInvalidInput)
	}
	if params.Args = strings.TrimSpace(params.Args); params.Args == "" {
		params.Args = "[]"
	}
	if params.Status = strings.TrimSpace(params.Status); params.Status == "" {
		params.Status = commandStatusRunning
	}
	if !isValidCommandStatus(params.Status) {
		return fmt.Errorf("%w: invalid command status %q", ErrInvalidInput, params.Status)
	}
	if params.StartedAt = strings.TrimSpace(params.StartedAt); params.StartedAt == "" {
		params.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	if err := s.sqlcQueries().CreateCommand(ctx, dbsqlc.CreateCommandParams{CommandID: params.CommandID, WorkspaceID: params.WorkspaceID, Command: params.Command, Args: params.Args, Status: params.Status, ExitCode: optionalInt(params.ExitCode), StartedAt: params.StartedAt, FinishedAt: params.FinishedAt}); err != nil {
		return fmt.Errorf("create command %q: %w", params.CommandID, err)
	}

	return nil
}

func (s *Store) GetCommand(ctx context.Context, sessionID, commandID string) (*Command, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if commandID = strings.TrimSpace(commandID); commandID == "" {
		return nil, fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}

	row, err := s.sqlcQueries().GetCommandByID(ctx, dbsqlc.GetCommandByIDParams{WorkspaceID: sessionID, CommandID: commandID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: command id %q for session %q", ErrNotFound, commandID, sessionID)
		}
		return nil, err
	}
	command := commandFromSQLC(row)
	return &command, nil
}

func (s *Store) ListCommands(ctx context.Context, sessionID string) ([]Command, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}

	rows, err := s.sqlcQueries().ListCommandsByWorkspace(ctx, dbsqlc.ListCommandsByWorkspaceParams{WorkspaceID: sessionID})
	if err != nil {
		return nil, err
	}
	out := make([]Command, 0, len(rows))
	for _, row := range rows {
		out = append(out, commandFromSQLC(row))
	}
	return out, nil
}

func (s *Store) UpdateCommandStatus(ctx context.Context, params UpdateCommandStatusParams) error {
	if params.WorkspaceID = strings.TrimSpace(params.WorkspaceID); params.WorkspaceID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if params.CommandID = strings.TrimSpace(params.CommandID); params.CommandID == "" {
		return fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}
	if params.Status = strings.TrimSpace(params.Status); params.Status == "" {
		return fmt.Errorf("%w: status is required", ErrInvalidInput)
	}
	if !isValidCommandStatus(params.Status) {
		return fmt.Errorf("%w: invalid command status %q", ErrInvalidInput, params.Status)
	}

	rowsAffected, err := s.sqlcQueries().UpdateCommandStatus(ctx, dbsqlc.UpdateCommandStatusParams{Status: params.Status, ExitCode: optionalInt(params.ExitCode), FinishedAt: params.FinishedAt, WorkspaceID: params.WorkspaceID, CommandID: params.CommandID})
	if err != nil {
		return fmt.Errorf("update command status %q: %w", params.CommandID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: command id %q for session %q", ErrNotFound, params.CommandID, params.WorkspaceID)
	}

	return nil
}

func (s *Store) MarkRunningCommandsLost(ctx context.Context) error {
	if err := s.sqlcQueries().MarkRunningCommandsLost(ctx); err != nil {
		return fmt.Errorf("mark running commands lost: %w", err)
	}

	return nil
}

func (s *Store) CreateWorkspaceCommandDefinition(ctx context.Context, params CreateWorkspaceCommandDefinitionParams) error {
	if params.CommandID = strings.TrimSpace(params.CommandID); params.CommandID == "" {
		return fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}
	if params.WorkspaceID = strings.TrimSpace(params.WorkspaceID); params.WorkspaceID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if params.Name = strings.TrimSpace(params.Name); params.Name == "" {
		return fmt.Errorf("%w: command name is required", ErrInvalidInput)
	}
	if params.Command = strings.TrimSpace(params.Command); params.Command == "" {
		return fmt.Errorf("%w: command is required", ErrInvalidInput)
	}
	if params.Args = strings.TrimSpace(params.Args); params.Args == "" {
		params.Args = "[]"
	}
	if !json.Valid([]byte(params.Args)) {
		return fmt.Errorf("%w: command args must be valid json", ErrInvalidInput)
	}
	trimmedArgs := strings.TrimSpace(params.Args)
	if !strings.HasPrefix(trimmedArgs, "[") || !strings.HasSuffix(trimmedArgs, "]") {
		return fmt.Errorf("%w: command args must be a json string array", ErrInvalidInput)
	}
	decodedArgs := make([]string, 0)
	if err := json.Unmarshal([]byte(params.Args), &decodedArgs); err != nil {
		return fmt.Errorf("%w: command args must be a json string array", ErrInvalidInput)
	}

	return s.withImmediateQueries(ctx, func(queries *dbsqlc.Queries) error {
		return createWorkspaceCommandDefinitionInTransaction(ctx, queries, params)
	})
}

func createWorkspaceCommandDefinitionInTransaction(ctx context.Context, queries *dbsqlc.Queries, params CreateWorkspaceCommandDefinitionParams) error {
	workspace, err := queries.GetWorkspaceByID(ctx, dbsqlc.GetWorkspaceByIDParams{WorkspaceID: params.WorkspaceID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: session id %q", ErrNotFound, params.WorkspaceID)
		}
		return err
	}
	if workspace.Status == statusClosed {
		return fmt.Errorf("%w: session id %q", ErrSessionClosed, params.WorkspaceID)
	}
	if existingByID, err := getWorkspaceCommandDefinition(ctx, queries, params.WorkspaceID, params.Name); err == nil && existingByID != nil {
		return fmt.Errorf("%w: command name %q collides with existing command id", ErrInvalidInput, params.Name)
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if existingByName, err := getWorkspaceCommandDefinitionByName(ctx, queries, params.WorkspaceID, params.CommandID); err == nil && existingByName != nil {
		return fmt.Errorf("%w: command id %q collides with existing command name", ErrInvalidInput, params.CommandID)
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := queries.CreateWorkspaceCommandDefinition(ctx, dbsqlc.CreateWorkspaceCommandDefinitionParams{CommandID: params.CommandID, WorkspaceID: params.WorkspaceID, Name: params.Name, Command: params.Command, Args: params.Args, CreatedAt: now, UpdatedAt: now}); err != nil {
		if isConstraintError(err) {
			return fmt.Errorf("%w: command definition %q already exists in workspace", ErrInvalidInput, params.CommandID)
		}
		return fmt.Errorf("create workspace command definition %q: %w", params.CommandID, err)
	}

	return nil
}

func isConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "constraint failed") || strings.Contains(message, "unique constraint")
}

func (s *Store) GetWorkspaceCommandDefinition(ctx context.Context, sessionID, commandID string) (*WorkspaceCommandDefinition, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if commandID = strings.TrimSpace(commandID); commandID == "" {
		return nil, fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}

	return getWorkspaceCommandDefinition(ctx, s.sqlcQueries(), sessionID, commandID)
}

func getWorkspaceCommandDefinition(ctx context.Context, queries *dbsqlc.Queries, sessionID, commandID string) (*WorkspaceCommandDefinition, error) {
	row, err := queries.GetWorkspaceCommandDefinitionByID(ctx, dbsqlc.GetWorkspaceCommandDefinitionByIDParams{WorkspaceID: sessionID, CommandID: commandID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: command id %q for session %q", ErrNotFound, commandID, sessionID)
		}
		return nil, err
	}
	def := workspaceCommandDefinitionFromSQLC(row)
	return &def, nil
}

func (s *Store) GetWorkspaceCommandDefinitionByName(ctx context.Context, sessionID, name string) (*WorkspaceCommandDefinition, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if name = strings.TrimSpace(name); name == "" {
		return nil, fmt.Errorf("%w: command name is required", ErrInvalidInput)
	}

	return getWorkspaceCommandDefinitionByName(ctx, s.sqlcQueries(), sessionID, name)
}

func getWorkspaceCommandDefinitionByName(ctx context.Context, queries *dbsqlc.Queries, sessionID, name string) (*WorkspaceCommandDefinition, error) {
	row, err := queries.GetWorkspaceCommandDefinitionByName(ctx, dbsqlc.GetWorkspaceCommandDefinitionByNameParams{WorkspaceID: sessionID, Name: name})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: command name %q for session %q", ErrNotFound, name, sessionID)
		}
		return nil, err
	}
	def := workspaceCommandDefinitionFromSQLC(row)
	return &def, nil
}

func (s *Store) ListWorkspaceCommandDefinitions(ctx context.Context, sessionID string) ([]WorkspaceCommandDefinition, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}

	rows, err := s.sqlcQueries().ListWorkspaceCommandDefinitionsByWorkspace(ctx, dbsqlc.ListWorkspaceCommandDefinitionsByWorkspaceParams{WorkspaceID: sessionID})
	if err != nil {
		return nil, err
	}
	out := make([]WorkspaceCommandDefinition, 0, len(rows))
	for _, row := range rows {
		out = append(out, workspaceCommandDefinitionFromSQLC(row))
	}
	return out, nil
}

func (s *Store) DeleteWorkspaceCommandDefinition(ctx context.Context, sessionID, commandID string) error {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if commandID = strings.TrimSpace(commandID); commandID == "" {
		return fmt.Errorf("%w: command id is required", ErrInvalidInput)
	}

	rowsAffected, err := s.sqlcQueries().DeleteWorkspaceCommandDefinitionByID(ctx, dbsqlc.DeleteWorkspaceCommandDefinitionByIDParams{WorkspaceID: sessionID, CommandID: commandID})
	if err != nil {
		return fmt.Errorf("delete workspace command definition %q: %w", commandID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: command id %q for session %q", ErrNotFound, commandID, sessionID)
	}

	return nil
}

func (s *Store) CreateAgent(ctx context.Context, params CreateAgentParams) error {
	if params.AgentID = strings.TrimSpace(params.AgentID); params.AgentID == "" {
		return fmt.Errorf("%w: agent id is required", ErrInvalidInput)
	}
	if params.WorkspaceID = strings.TrimSpace(params.WorkspaceID); params.WorkspaceID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if _, err := s.GetSession(ctx, params.WorkspaceID); err != nil {
		return err
	}
	if params.Name != nil {
		trimmedName := strings.TrimSpace(*params.Name)
		if trimmedName == "" {
			params.Name = nil
		} else {
			params.Name = &trimmedName
		}
	}
	if params.Command = strings.TrimSpace(params.Command); params.Command == "" {
		return fmt.Errorf("%w: command is required", ErrInvalidInput)
	}
	if params.Args = strings.TrimSpace(params.Args); params.Args == "" {
		params.Args = "[]"
	}
	if params.Status = strings.TrimSpace(params.Status); params.Status == "" {
		params.Status = agentStatusRunning
	}
	if !isValidAgentStatus(params.Status) {
		return fmt.Errorf("%w: invalid agent status %q", ErrInvalidInput, params.Status)
	}
	if params.StartedAt = strings.TrimSpace(params.StartedAt); params.StartedAt == "" {
		params.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if params.Harness != nil {
		trimmedHarness := strings.TrimSpace(*params.Harness)
		if trimmedHarness == "" {
			params.Harness = nil
		} else {
			params.Harness = &trimmedHarness
		}
	}
	if params.HarnessResumableID != nil {
		trimmedResumableID := strings.TrimSpace(*params.HarnessResumableID)
		if trimmedResumableID == "" {
			params.HarnessResumableID = nil
		} else {
			params.HarnessResumableID = &trimmedResumableID
		}
	}
	if params.HarnessMetadata = strings.TrimSpace(params.HarnessMetadata); params.HarnessMetadata == "" {
		params.HarnessMetadata = "{}"
	}
	if !json.Valid([]byte(params.HarnessMetadata)) {
		return fmt.Errorf("%w: harness metadata must be valid json", ErrInvalidInput)
	}
	if params.InvocationClass = strings.TrimSpace(params.InvocationClass); params.InvocationClass == "" {
		params.InvocationClass = "agent"
	}
	if params.InvocationClass != "agent" && params.InvocationClass != "temporary" {
		return fmt.Errorf("%w: invalid invocation class %q", ErrInvalidInput, params.InvocationClass)
	}

	if err := s.sqlcQueries().CreateAgent(ctx, dbsqlc.CreateAgentParams{AgentID: params.AgentID, WorkspaceID: params.WorkspaceID, Name: params.Name, Command: params.Command, Args: params.Args, Status: params.Status, ExitCode: optionalInt(params.ExitCode), StartedAt: params.StartedAt, StoppedAt: params.StoppedAt, Harness: params.Harness, HarnessResumableID: params.HarnessResumableID, HarnessMetadata: params.HarnessMetadata, InvocationClass: params.InvocationClass}); err != nil {
		return fmt.Errorf("create agent %q: %w", params.AgentID, err)
	}

	return nil
}

func (s *Store) GetAgent(ctx context.Context, sessionID, agentID string) (*Agent, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if agentID = strings.TrimSpace(agentID); agentID == "" {
		return nil, fmt.Errorf("%w: agent id is required", ErrInvalidInput)
	}

	row, err := s.sqlcQueries().GetAgentByID(ctx, dbsqlc.GetAgentByIDParams{WorkspaceID: sessionID, AgentID: agentID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: agent id %q for session %q", ErrNotFound, agentID, sessionID)
		}
		return nil, err
	}
	agent := agentFromSQLC(row)
	return &agent, nil
}

func (s *Store) GetAgentByName(ctx context.Context, sessionID, name string) (*Agent, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if name = strings.TrimSpace(name); name == "" {
		return nil, fmt.Errorf("%w: agent name is required", ErrInvalidInput)
	}

	row, err := s.sqlcQueries().GetAgentByName(ctx, dbsqlc.GetAgentByNameParams{WorkspaceID: sessionID, Name: optionalString(name)})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: agent name %q for session %q", ErrNotFound, name, sessionID)
		}
		return nil, err
	}
	agent := agentFromSQLC(row)
	return &agent, nil
}

func (s *Store) ListAgents(ctx context.Context, sessionID string) ([]Agent, error) {
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return nil, fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}

	rows, err := s.sqlcQueries().ListAgentsByWorkspace(ctx, dbsqlc.ListAgentsByWorkspaceParams{WorkspaceID: sessionID})
	if err != nil {
		return nil, err
	}
	out := make([]Agent, 0, len(rows))
	for _, row := range rows {
		out = append(out, agentFromSQLC(row))
	}
	return out, nil
}

func (s *Store) UpdateAgentStatus(ctx context.Context, params UpdateAgentStatusParams) error {
	if params.WorkspaceID = strings.TrimSpace(params.WorkspaceID); params.WorkspaceID == "" {
		return fmt.Errorf("%w: session id is required", ErrInvalidInput)
	}
	if params.AgentID = strings.TrimSpace(params.AgentID); params.AgentID == "" {
		return fmt.Errorf("%w: agent id is required", ErrInvalidInput)
	}
	if params.Status = strings.TrimSpace(params.Status); params.Status == "" {
		return fmt.Errorf("%w: status is required", ErrInvalidInput)
	}
	if !isValidAgentStatus(params.Status) {
		return fmt.Errorf("%w: invalid agent status %q", ErrInvalidInput, params.Status)
	}

	rowsAffected, err := s.sqlcQueries().UpdateAgentStatus(ctx, dbsqlc.UpdateAgentStatusParams{Status: params.Status, ExitCode: optionalInt(params.ExitCode), StoppedAt: params.StoppedAt, WorkspaceID: params.WorkspaceID, AgentID: params.AgentID})
	if err != nil {
		return fmt.Errorf("update agent status %q: %w", params.AgentID, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: agent id %q for session %q", ErrNotFound, params.AgentID, params.WorkspaceID)
	}

	return nil
}

func (s *Store) MarkRunningAgentsLost(ctx context.Context) error {
	if err := s.sqlcQueries().MarkRunningAgentsLost(ctx); err != nil {
		return fmt.Errorf("mark running agents lost: %w", err)
	}

	return nil
}

func isValidSessionStatus(status string) bool {
	switch status {
	case statusActive, statusSuspended, statusClosed:
		return true
	default:
		return false
	}
}

func canTransitionSessionStatus(from, to string) bool {
	if from == to {
		return from != statusClosed
	}

	switch from {
	case statusActive:
		return to == statusSuspended || to == statusClosed
	case statusSuspended:
		return to == statusActive || to == statusClosed
	case statusClosed:
		return false
	default:
		return false
	}
}

func validateCleanupPolicy(cleanupPolicy string) error {
	cleanupPolicy = strings.TrimSpace(cleanupPolicy)
	if cleanupPolicy == "" {
		return fmt.Errorf("%w: cleanup policy is required", ErrInvalidInput)
	}

	if cleanupPolicy != cleanupPolicyManual && cleanupPolicy != cleanupPolicyOnClose {
		return fmt.Errorf("%w: invalid cleanup policy %q", ErrInvalidInput, cleanupPolicy)
	}

	return nil
}

func validateVCSPreference(vcsPreference string) error {
	vcsPreference = strings.TrimSpace(vcsPreference)
	if vcsPreference == "" {
		return fmt.Errorf("%w: vcs preference is required", ErrInvalidInput)
	}

	if vcsPreference != "auto" && vcsPreference != "jj" && vcsPreference != "git" {
		return fmt.Errorf("%w: invalid vcs preference %q", ErrInvalidInput, vcsPreference)
	}

	return nil
}

func isValidVCSType(vcsType string) bool {
	switch vcsType {
	case vcsTypeGit, vcsTypeJJ, vcsTypeUnknown:
		return true
	default:
		return false
	}
}

func isValidCommandStatus(status string) bool {
	switch status {
	case commandStatusRunning, commandStatusExited, commandStatusLost:
		return true
	default:
		return false
	}
}

func isValidAgentStatus(status string) bool {
	switch status {
	case agentStatusRunning, agentStatusStopped, agentStatusExited, agentStatusLost:
		return true
	default:
		return false
	}
}
