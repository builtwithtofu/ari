package globaldb

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

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
