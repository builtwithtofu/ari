package globaldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

var (
	ErrInvalidInput     = errors.New("invalid globaldb input")
	ErrNotFound         = errors.New("globaldb record not found")
	ErrPermissionDenied = errors.New("globaldb permission denied")
)

const (
	statusActive        = "active"
	statusSuspended     = "suspended"
	cleanupPolicyManual = "manual"

	vcsTypeGit     = "git"
	vcsTypeJJ      = "jj"
	vcsTypeUnknown = "unknown"

	commandStatusRunning = "running"
	commandStatusExited  = "exited"
	commandStatusLost    = "lost"
	commandStatusStopped = "stopped"
)

type Store struct {
	db             *sql.DB
	sqlc           *dbsqlc.Queries
	agentMessageMu sync.Mutex
}

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

type KnownInt64 struct {
	Known bool
	Value *int64
}

type HarnessSessionTelemetry struct {
	HarnessSessionID        string
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

type HarnessSessionTelemetryGroup struct {
	ProfileID       string
	ProfileName     string
	Harness         string
	Model           string
	InvocationClass string
}

type HarnessSessionTelemetryRollup struct {
	Group         HarnessSessionTelemetryGroup
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

func (s *Store) UpsertHarnessSessionTelemetry(ctx context.Context, telemetry HarnessSessionTelemetry) error {
	telemetry.HarnessSessionID = strings.TrimSpace(telemetry.HarnessSessionID)
	telemetry.WorkspaceID = strings.TrimSpace(telemetry.WorkspaceID)
	telemetry.TaskID = strings.TrimSpace(telemetry.TaskID)
	telemetry.ProfileID = strings.TrimSpace(telemetry.ProfileID)
	telemetry.ProfileName = strings.TrimSpace(telemetry.ProfileName)
	telemetry.Harness = strings.TrimSpace(telemetry.Harness)
	telemetry.Model = strings.TrimSpace(telemetry.Model)
	telemetry.InvocationClass = strings.TrimSpace(telemetry.InvocationClass)
	telemetry.Status = strings.TrimSpace(telemetry.Status)
	if telemetry.HarnessSessionID == "" {
		return fmt.Errorf("%w: harness session id is required", ErrInvalidInput)
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
		telemetry.InvocationClass = HarnessSessionUsageSticky
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
	params := dbsqlc.UpsertHarnessSessionTelemetryParams{SessionID: telemetry.HarnessSessionID, WorkspaceID: telemetry.WorkspaceID, TaskID: telemetry.TaskID, ProfileID: optionalString(telemetry.ProfileID), ProfileName: optionalString(telemetry.ProfileName), Harness: telemetry.Harness, Model: telemetry.Model, InvocationClass: telemetry.InvocationClass, Status: telemetry.Status, InputTokensKnown: boolInt64(telemetry.InputTokensKnown), InputTokens: telemetry.InputTokens, OutputTokensKnown: boolInt64(telemetry.OutputTokensKnown), OutputTokens: telemetry.OutputTokens, EstimatedCostKnown: boolInt64(telemetry.EstimatedCostKnown), EstimatedCostMicros: telemetry.EstimatedCostMicros, DurationMsKnown: boolInt64(telemetry.DurationMSKnown), DurationMs: telemetry.DurationMS, ExitCodeKnown: boolInt64(telemetry.ExitCodeKnown), ExitCode: telemetry.ExitCode, OwnedByAri: boolInt64(telemetry.OwnedByAri), PidKnown: boolInt64(telemetry.PIDKnown), Pid: telemetry.PID, CpuTimeMsKnown: boolInt64(telemetry.CPUTimeMSKnown), CpuTimeMs: telemetry.CPUTimeMS, MemoryRssBytesPeakKnown: boolInt64(telemetry.MemoryRSSBytesPeakKnown), MemoryRssBytesPeak: telemetry.MemoryRSSBytesPeak, ChildProcessesPeakKnown: boolInt64(telemetry.ChildProcessesPeakKnown), ChildProcessesPeak: telemetry.ChildProcessesPeak, PortsJson: telemetry.PortsJSON, OrphanState: telemetry.OrphanState, CreatedAt: telemetry.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: telemetry.UpdatedAt.Format(time.RFC3339Nano)}
	if err := s.sqlcQueries().UpsertHarnessSessionTelemetry(ctx, params); err != nil {
		return fmt.Errorf("upsert harness session telemetry %q: %w", telemetry.HarnessSessionID, err)
	}
	return nil
}

func (s *Store) RollupHarnessSessionTelemetry(ctx context.Context, workspaceID string) ([]HarnessSessionTelemetryRollup, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	rows, err := s.sqlcQueries().ListHarnessSessionTelemetryByWorkspace(ctx, dbsqlc.ListHarnessSessionTelemetryByWorkspaceParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, fmt.Errorf("list harness session telemetry: %w", err)
	}
	byGroup := map[HarnessSessionTelemetryGroup]*HarnessSessionTelemetryRollup{}
	order := []HarnessSessionTelemetryGroup{}
	for _, row := range rows {
		group := HarnessSessionTelemetryGroup{ProfileID: stringValue(row.ProfileID), ProfileName: stringValue(row.ProfileName), Harness: row.Harness, Model: row.Model, InvocationClass: row.InvocationClass}
		rollup := byGroup[group]
		if rollup == nil {
			rollup = &HarnessSessionTelemetryRollup{Group: group}
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
	rollups := make([]HarnessSessionTelemetryRollup, 0, len(order))
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

func finalResponseFromSQLC(row dbsqlc.FinalResponse) FinalResponse {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	var updatedAt *time.Time
	if row.UpdatedAt != nil {
		parsed, _ := time.Parse(time.RFC3339Nano, *row.UpdatedAt)
		updatedAt = &parsed
	}
	return FinalResponse{FinalResponseID: row.FinalResponseID, HarnessSessionID: row.SessionID, WorkspaceID: row.WorkspaceID, TaskID: row.TaskID, ContextPacketID: row.ContextPacketID, ProfileID: stringValue(row.ProfileID), Status: row.Status, Text: row.Text, EvidenceLinksJSON: row.EvidenceLinks, CreatedAt: createdAt, UpdatedAt: updatedAt}
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

func isConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "constraint failed") || strings.Contains(message, "unique constraint")
}

func (s *Store) MarkRunningHarnessSessionsLost(ctx context.Context) error {
	if err := s.sqlcQueries().MarkRunningHarnessSessionsLost(ctx); err != nil {
		return fmt.Errorf("mark running harness sessions lost: %w", err)
	}

	return nil
}
