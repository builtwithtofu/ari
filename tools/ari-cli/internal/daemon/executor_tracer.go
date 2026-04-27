package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type AgentRunStartRequest struct {
	Executor string        `json:"executor"`
	Packet   ContextPacket `json:"packet"`
	Command  string        `json:"command,omitempty"`
	Args     []string      `json:"args,omitempty"`
}

type AgentRunStartResponse struct {
	Run   AgentRun       `json:"run"`
	Items []TimelineItem `json:"items"`
}

type AgentRun struct {
	AgentRunID      string                `json:"agent_run_id"`
	WorkspaceID     string                `json:"workspace_id"`
	TaskID          string                `json:"task_id"`
	Executor        string                `json:"executor"`
	ProviderRunID   string                `json:"provider_run_id"`
	Status          string                `json:"status"`
	ContextPacketID string                `json:"context_packet_id"`
	StartedAt       string                `json:"started_at"`
	FinishedAt      string                `json:"finished_at,omitempty"`
	PID             int                   `json:"pid,omitempty"`
	ExitCode        *int                  `json:"exit_code,omitempty"`
	ProcessSample   *ProcessMetricsSample `json:"-"`
	Capabilities    []string              `json:"capabilities"`
}

const (
	HarnessNameCodex    = "codex"
	HarnessNameClaude   = "claude"
	HarnessNameOpenCode = "opencode"
	HarnessNamePTY      = "pty"
)

type HarnessFactory func(AgentRunStartRequest, string, func(string, []TimelineItem)) (Executor, error)

type AgentProfile struct {
	ProfileID       string                 `json:"profile_id,omitempty"`
	Name            string                 `json:"name"`
	Harness         string                 `json:"harness"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class"`
}

type AgentRunDefaults struct {
	Harness         string                 `json:"harness,omitempty"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class,omitempty"`
}

type AgentProfileRunRequest struct {
	Profile           string           `json:"profile,omitempty"`
	ProfileDefinition *AgentProfile    `json:"profile_definition,omitempty"`
	Defaults          AgentRunDefaults `json:"defaults,omitempty"`
	Packet            ContextPacket    `json:"packet"`
}

type AgentProfileRunResponse struct {
	Profile string         `json:"profile"`
	Harness string         `json:"harness"`
	Run     AgentRun       `json:"run"`
	Items   []TimelineItem `json:"items"`
}

type AgentProfileCreateRequest struct {
	WorkspaceID     string                 `json:"workspace_id,omitempty"`
	Name            string                 `json:"name"`
	Harness         string                 `json:"harness,omitempty"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class,omitempty"`
	Defaults        map[string]any         `json:"defaults,omitempty"`
}

type AgentProfileGetRequest struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	Name        string `json:"name"`
}

type AgentProfileListRequest struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type AgentProfileListResponse struct {
	Profiles []AgentProfileResponse `json:"profiles"`
}

type DefaultHelperEnsureRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Harness     string `json:"harness,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

type DefaultHelperGetRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type ProcessMetricValue struct {
	Known      bool   `json:"known"`
	Value      *int64 `json:"value,omitempty"`
	Confidence string `json:"confidence,omitempty"`
}

type ProcessPortObservation struct {
	Port       int    `json:"port"`
	Protocol   string `json:"protocol"`
	Confidence string `json:"confidence"`
}

type ProcessMetricsSample struct {
	OwnedByAri         bool
	PID                ProcessMetricValue
	CPUTimeMS          ProcessMetricValue
	MemoryRSSBytesPeak ProcessMetricValue
	ChildProcessesPeak ProcessMetricValue
	Ports              []ProcessPortObservation
	OrphanState        string
	ExitCode           ProcessMetricValue
}

type TelemetryKnownInt64 struct {
	Known bool   `json:"known"`
	Value *int64 `json:"value,omitempty"`
}

type TelemetryRollupRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type TelemetryRollupResponse struct {
	Rollups []TelemetryRollup `json:"rollups"`
}

type TelemetryRollupGroup struct {
	ProfileID       string `json:"profile_id,omitempty"`
	Profile         string `json:"profile,omitempty"`
	Harness         string `json:"harness"`
	Model           string `json:"model"`
	InvocationClass string `json:"invocation_class"`
}

type TelemetryProcessRollup struct {
	OwnedByAri         bool                     `json:"owned_by_ari"`
	PID                TelemetryKnownInt64      `json:"pid"`
	CPUTimeMS          TelemetryKnownInt64      `json:"cpu_time_ms"`
	MemoryRSSBytesPeak TelemetryKnownInt64      `json:"memory_rss_bytes_peak"`
	ChildProcessesPeak TelemetryKnownInt64      `json:"child_processes_peak"`
	Ports              []ProcessPortObservation `json:"ports"`
	OrphanState        string                   `json:"orphan_state"`
	ExitCode           TelemetryKnownInt64      `json:"exit_code"`
}

type TelemetryRollup struct {
	Group         TelemetryRollupGroup   `json:"group"`
	Runs          int                    `json:"runs"`
	Completed     int                    `json:"completed"`
	Failed        int                    `json:"failed"`
	InputTokens   TelemetryKnownInt64    `json:"input_tokens"`
	OutputTokens  TelemetryKnownInt64    `json:"output_tokens"`
	EstimatedCost TelemetryKnownInt64    `json:"estimated_cost"`
	DurationMS    TelemetryKnownInt64    `json:"duration_ms"`
	Process       TelemetryProcessRollup `json:"process"`
}

type FinalResponseEvidenceLink struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type FinalResponseGetRequest struct {
	FinalResponseID string `json:"final_response_id,omitempty"`
	RunID           string `json:"run_id,omitempty"`
}

type FinalResponseListRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type FinalResponseListResponse struct {
	FinalResponses []FinalResponseResponse `json:"final_responses"`
}

type FinalResponseResponse struct {
	FinalResponseID string                      `json:"final_response_id"`
	RunID           string                      `json:"run_id"`
	WorkspaceID     string                      `json:"workspace_id"`
	TaskID          string                      `json:"task_id"`
	ContextPacketID string                      `json:"context_packet_id"`
	ProfileID       string                      `json:"profile_id,omitempty"`
	Status          string                      `json:"status"`
	Text            string                      `json:"text"`
	EvidenceLinks   []FinalResponseEvidenceLink `json:"evidence_links"`
	CreatedAt       string                      `json:"created_at"`
	UpdatedAt       string                      `json:"updated_at,omitempty"`
}

type AgentProfileResponse struct {
	ProfileID       string                 `json:"profile_id"`
	WorkspaceID     string                 `json:"workspace_id,omitempty"`
	Name            string                 `json:"name"`
	Harness         string                 `json:"harness,omitempty"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class,omitempty"`
	Defaults        map[string]any         `json:"defaults,omitempty"`
}

func defaultAgentProfiles() map[string]AgentProfile {
	return make(map[string]AgentProfile)
}

var agentRunProcessMetricsSampler = func(ctx context.Context, run AgentRun) ProcessMetricsSample {
	return sampleLinuxProcessMetrics(ctx, run)
}

func unknownProcessMetric(confidence string) ProcessMetricValue {
	return ProcessMetricValue{Known: false, Confidence: strings.TrimSpace(confidence)}
}

func (d *Daemon) resolveHarness(req AgentRunStartRequest, primaryFolder string) (Executor, error) {
	if d == nil {
		return nil, fmt.Errorf("daemon is required")
	}
	name := strings.TrimSpace(req.Executor)
	if name == "" {
		return nil, rpc.NewHandlerError(rpc.InvalidParams, "executor is required", nil)
	}
	factory, ok := d.harnessRegistry.Resolve(name)
	if !ok {
		return nil, unknownHarnessError(name)
	}
	return factory(req, primaryFolder, d.appendExecutorItems)
}

func (d *Daemon) setHarnessFactoryForTest(name string, factory HarnessFactory) {
	if d == nil {
		return
	}
	if d.harnessRegistry == nil {
		d.harnessRegistry = NewHarnessRegistry()
	}
	if err := d.harnessRegistry.ReplaceForTest(name, factory); err != nil {
		panic(fmt.Sprintf("set test harness factory: %v", err))
	}
}

func (d *Daemon) setAgentProfileForTest(profile AgentProfile) {
	if d == nil {
		return
	}
	if d.agentProfiles == nil {
		d.agentProfiles = make(map[string]AgentProfile)
	}
	d.agentProfiles[strings.TrimSpace(profile.Name)] = profile
}

func (d *Daemon) registerExecutorMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentRunStartRequest, AgentRunStartResponse]{
		Name:        "agent.run",
		Description: "Start an executor-backed agent run from a context packet",
		Handler: func(ctx context.Context, req AgentRunStartRequest) (AgentRunStartResponse, error) {
			return d.startAgentRun(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register agent.run: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentProfileRunRequest, AgentProfileRunResponse]{
		Name:        "agent.profile.run",
		Description: "Start an agent run from a named Ari agent profile",
		Handler: func(ctx context.Context, req AgentProfileRunRequest) (AgentProfileRunResponse, error) {
			profile, err := d.resolveAgentProfileRunRequest(ctx, store, req)
			if err != nil {
				return AgentProfileRunResponse{}, err
			}
			resp, err := d.startAgentRun(ctx, store, AgentRunStartRequest{Executor: profile.Harness, Packet: req.Packet}, profile)
			if err != nil {
				return AgentProfileRunResponse{}, err
			}
			return AgentProfileRunResponse{Profile: profile.Name, Harness: profile.Harness, Run: resp.Run, Items: resp.Items}, nil
		},
	}); err != nil {
		return fmt.Errorf("register agent.profile.run: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentProfileCreateRequest, AgentProfileResponse]{
		Name:        "agent.profile.create",
		Description: "Create or update a durable Ari agent profile",
		Handler: func(ctx context.Context, req AgentProfileCreateRequest) (AgentProfileResponse, error) {
			return createStoredAgentProfile(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register agent.profile.create: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentProfileGetRequest, AgentProfileResponse]{
		Name:        "agent.profile.get",
		Description: "Get a durable Ari agent profile by name",
		Handler: func(ctx context.Context, req AgentProfileGetRequest) (AgentProfileResponse, error) {
			return getStoredAgentProfile(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register agent.profile.get: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentProfileListRequest, AgentProfileListResponse]{
		Name:        "agent.profile.list",
		Description: "List durable Ari agent profiles",
		Handler: func(ctx context.Context, req AgentProfileListRequest) (AgentProfileListResponse, error) {
			return listStoredAgentProfiles(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register agent.profile.list: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[DefaultHelperEnsureRequest, AgentProfileResponse]{
		Name:        "agent.profile.helper.ensure",
		Description: "Ensure a workspace default helper profile exists",
		Handler: func(ctx context.Context, req DefaultHelperEnsureRequest) (AgentProfileResponse, error) {
			return ensureDefaultHelperProfile(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register agent.profile.helper.ensure: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[DefaultHelperGetRequest, AgentProfileResponse]{
		Name:        "agent.profile.helper.get",
		Description: "Get a workspace default helper profile",
		Handler: func(ctx context.Context, req DefaultHelperGetRequest) (AgentProfileResponse, error) {
			return getDefaultHelperProfile(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register agent.profile.helper.get: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[FinalResponseGetRequest, FinalResponseResponse]{
		Name:        "final_response.get",
		Description: "Get a final response artifact by id or run id",
		Handler: func(ctx context.Context, req FinalResponseGetRequest) (FinalResponseResponse, error) {
			return getFinalResponse(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register final_response.get: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[FinalResponseListRequest, FinalResponseListResponse]{
		Name:        "final_response.list",
		Description: "List final response artifacts for a workspace",
		Handler: func(ctx context.Context, req FinalResponseListRequest) (FinalResponseListResponse, error) {
			return listFinalResponses(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register final_response.list: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[TelemetryRollupRequest, TelemetryRollupResponse]{
		Name:        "telemetry.rollup",
		Description: "Roll up local agent run telemetry for a workspace",
		Handler: func(ctx context.Context, req TelemetryRollupRequest) (TelemetryRollupResponse, error) {
			return telemetryRollup(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register telemetry.rollup: %w", err)
	}
	return nil
}

func createStoredAgentProfile(ctx context.Context, store *globaldb.Store, req AgentProfileCreateRequest) (AgentProfileResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return AgentProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "profile name is required", map[string]any{"reason": "missing_profile_name"})
	}
	if req.InvocationClass != "" && req.InvocationClass != HarnessInvocationAgent && req.InvocationClass != HarnessInvocationTemporary {
		return AgentProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "invocation class is invalid", map[string]any{"reason": "invalid_invocation_class"})
	}
	profileID, err := newAriULID()
	if err != nil {
		return AgentProfileResponse{}, err
	}
	defaultsJSON := "{}"
	if len(req.Defaults) > 0 {
		encoded, err := json.Marshal(req.Defaults)
		if err != nil {
			return AgentProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "profile defaults are invalid", map[string]any{"reason": "invalid_defaults"})
		}
		defaultsJSON = string(encoded)
	}
	stored := globaldb.AgentProfile{ProfileID: "ap_" + profileID, WorkspaceID: strings.TrimSpace(req.WorkspaceID), Name: name, Harness: strings.TrimSpace(req.Harness), Model: strings.TrimSpace(req.Model), Prompt: strings.TrimSpace(req.Prompt), InvocationClass: string(req.InvocationClass), DefaultsJSON: defaultsJSON}
	if err := store.UpsertAgentProfile(ctx, stored); err != nil {
		return AgentProfileResponse{}, err
	}
	persisted, err := store.GetAgentProfile(ctx, stored.WorkspaceID, stored.Name)
	if err != nil {
		return AgentProfileResponse{}, err
	}
	return agentProfileResponseFromStore(persisted, req.Defaults), nil
}

func getStoredAgentProfile(ctx context.Context, store *globaldb.Store, req AgentProfileGetRequest) (AgentProfileResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return AgentProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "profile name is required", map[string]any{"reason": "missing_profile_name"})
	}
	stored, err := store.GetAgentProfile(ctx, req.WorkspaceID, name)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return AgentProfileResponse{}, unknownProfileError(name)
		}
		return AgentProfileResponse{}, err
	}
	defaults := map[string]any{}
	if strings.TrimSpace(stored.DefaultsJSON) != "" {
		_ = json.Unmarshal([]byte(stored.DefaultsJSON), &defaults)
	}
	return agentProfileResponseFromStore(stored, defaults), nil
}

func listStoredAgentProfiles(ctx context.Context, store *globaldb.Store, req AgentProfileListRequest) (AgentProfileListResponse, error) {
	stored, err := store.ListAgentProfiles(ctx, req.WorkspaceID)
	if err != nil {
		return AgentProfileListResponse{}, err
	}
	profiles := make([]AgentProfileResponse, 0, len(stored))
	for _, profile := range stored {
		defaults := map[string]any{}
		if strings.TrimSpace(profile.DefaultsJSON) != "" {
			_ = json.Unmarshal([]byte(profile.DefaultsJSON), &defaults)
		}
		profiles = append(profiles, agentProfileResponseFromStore(profile, defaults))
	}
	return AgentProfileListResponse{Profiles: profiles}, nil
}

func agentProfileResponseFromStore(profile globaldb.AgentProfile, defaults map[string]any) AgentProfileResponse {
	return AgentProfileResponse{ProfileID: profile.ProfileID, WorkspaceID: profile.WorkspaceID, Name: profile.Name, Harness: profile.Harness, Model: profile.Model, Prompt: profile.Prompt, InvocationClass: HarnessInvocationClass(profile.InvocationClass), Defaults: defaults}
}

func ensureDefaultHelperProfile(ctx context.Context, store *globaldb.Store, req DefaultHelperEnsureRequest) (AgentProfileResponse, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return AgentProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", map[string]any{"reason": "missing_workspace_id"})
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = projectHelperPrompt()
	}
	stored, err := store.EnsureDefaultHelperProfile(ctx, workspaceID, req.Harness, prompt)
	if err != nil {
		return AgentProfileResponse{}, err
	}
	return agentProfileResponseFromStore(stored, map[string]any{}), nil
}

func getDefaultHelperProfile(ctx context.Context, store *globaldb.Store, req DefaultHelperGetRequest) (AgentProfileResponse, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return AgentProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", map[string]any{"reason": "missing_workspace_id"})
	}
	stored, err := store.GetDefaultHelperProfile(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return AgentProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "default helper profile is not set up for this workspace", map[string]any{"reason": "helper_setup_required", "workspace_id": workspaceID})
		}
		return AgentProfileResponse{}, err
	}
	return agentProfileResponseFromStore(stored, map[string]any{}), nil
}

func getFinalResponse(ctx context.Context, store *globaldb.Store, req FinalResponseGetRequest) (FinalResponseResponse, error) {
	var stored globaldb.FinalResponse
	var err error
	if strings.TrimSpace(req.FinalResponseID) != "" {
		stored, err = store.GetFinalResponseByID(ctx, req.FinalResponseID)
	} else if strings.TrimSpace(req.RunID) != "" {
		stored, err = store.GetFinalResponseByRunID(ctx, req.RunID)
	} else {
		return FinalResponseResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "final_response_id or run_id is required", map[string]any{"reason": "missing_final_response_ref"})
	}
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return FinalResponseResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "final response is not available", map[string]any{"reason": "unknown_final_response"})
		}
		return FinalResponseResponse{}, err
	}
	return finalResponseResponseFromStore(stored), nil
}

func listFinalResponses(ctx context.Context, store *globaldb.Store, req FinalResponseListRequest) (FinalResponseListResponse, error) {
	stored, err := store.ListFinalResponses(ctx, req.WorkspaceID)
	if err != nil {
		return FinalResponseListResponse{}, err
	}
	responses := make([]FinalResponseResponse, 0, len(stored))
	for _, response := range stored {
		responses = append(responses, finalResponseResponseFromStore(response))
	}
	return FinalResponseListResponse{FinalResponses: responses}, nil
}

func finalResponseResponseFromStore(stored globaldb.FinalResponse) FinalResponseResponse {
	links := []FinalResponseEvidenceLink{}
	if strings.TrimSpace(stored.EvidenceLinksJSON) != "" {
		_ = json.Unmarshal([]byte(stored.EvidenceLinksJSON), &links)
	}
	updatedAt := ""
	if stored.UpdatedAt != nil {
		updatedAt = stored.UpdatedAt.Format(time.RFC3339Nano)
	}
	return FinalResponseResponse{FinalResponseID: stored.FinalResponseID, RunID: stored.RunID, WorkspaceID: stored.WorkspaceID, TaskID: stored.TaskID, ContextPacketID: stored.ContextPacketID, ProfileID: stored.ProfileID, Status: stored.Status, Text: stored.Text, EvidenceLinks: links, CreatedAt: stored.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: updatedAt}
}

func telemetryRollup(ctx context.Context, store *globaldb.Store, req TelemetryRollupRequest) (TelemetryRollupResponse, error) {
	rollups, err := store.RollupAgentRunTelemetry(ctx, req.WorkspaceID)
	if err != nil {
		return TelemetryRollupResponse{}, err
	}
	out := make([]TelemetryRollup, 0, len(rollups))
	for _, rollup := range rollups {
		out = append(out, telemetryRollupFromStore(rollup))
	}
	return TelemetryRollupResponse{Rollups: out}, nil
}

func telemetryRollupFromStore(rollup globaldb.AgentRunTelemetryRollup) TelemetryRollup {
	ports := []ProcessPortObservation{}
	if strings.TrimSpace(rollup.PortsJSON) != "" {
		_ = json.Unmarshal([]byte(rollup.PortsJSON), &ports)
	}
	orphanState := strings.TrimSpace(rollup.OrphanState)
	if orphanState == "" {
		orphanState = "unknown"
	}
	return TelemetryRollup{Group: TelemetryRollupGroup{ProfileID: rollup.Group.ProfileID, Profile: rollup.Group.ProfileName, Harness: rollup.Group.Harness, Model: rollup.Group.Model, InvocationClass: rollup.Group.InvocationClass}, Runs: rollup.Runs, Completed: rollup.Completed, Failed: rollup.Failed, InputTokens: telemetryKnownInt64FromStore(rollup.InputTokens), OutputTokens: telemetryKnownInt64FromStore(rollup.OutputTokens), EstimatedCost: telemetryKnownInt64FromStore(rollup.EstimatedCost), DurationMS: telemetryKnownInt64FromStore(rollup.DurationMS), Process: TelemetryProcessRollup{OwnedByAri: rollup.OwnedByAri, PID: telemetryKnownInt64FromStore(rollup.PID), CPUTimeMS: telemetryKnownInt64FromStore(rollup.CPUTimeMS), MemoryRSSBytesPeak: telemetryKnownInt64FromStore(rollup.MemoryRSS), ChildProcessesPeak: telemetryKnownInt64FromStore(rollup.ChildCount), Ports: ports, OrphanState: orphanState, ExitCode: telemetryKnownInt64FromStore(rollup.ExitCode)}}
}

func telemetryKnownInt64FromStore(value globaldb.KnownInt64) TelemetryKnownInt64 {
	return TelemetryKnownInt64{Known: value.Known, Value: value.Value}
}

func (d *Daemon) resolveAgentProfileRunRequest(ctx context.Context, store *globaldb.Store, req AgentProfileRunRequest) (AgentProfile, error) {
	name := strings.TrimSpace(req.Profile)
	if name != "" && req.ProfileDefinition != nil {
		return AgentProfile{}, rpc.NewHandlerError(rpc.InvalidParams, "profile input is ambiguous", map[string]any{"profile": name, "profile_definition": strings.TrimSpace(req.ProfileDefinition.Name), "reason": "ambiguous_profile", "start_invoked": false})
	}
	var profile AgentProfile
	if req.ProfileDefinition != nil {
		profile = *req.ProfileDefinition
		if strings.TrimSpace(profile.Name) == "" {
			profile.Name = name
		}
	} else if name != "" {
		resolved, err := d.resolveAgentProfile(ctx, store, req.Packet.WorkspaceID, name)
		if err != nil {
			return AgentProfile{}, err
		}
		profile = resolved
	}
	profile = applyAgentRunDefaults(profile, req.Defaults)
	if strings.TrimSpace(profile.Harness) == "" {
		return AgentProfile{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is required", map[string]any{"profile": strings.TrimSpace(profile.Name), "reason": "missing_harness", "start_invoked": false})
	}
	return profile, nil
}

func applyAgentRunDefaults(profile AgentProfile, defaults AgentRunDefaults) AgentProfile {
	if strings.TrimSpace(profile.Harness) == "" {
		profile.Harness = strings.TrimSpace(defaults.Harness)
	}
	if strings.TrimSpace(profile.Model) == "" {
		profile.Model = strings.TrimSpace(defaults.Model)
	}
	if strings.TrimSpace(profile.Prompt) == "" {
		profile.Prompt = strings.TrimSpace(defaults.Prompt)
	}
	if profile.InvocationClass == "" {
		profile.InvocationClass = defaults.InvocationClass
	}
	if profile.InvocationClass == "" {
		profile.InvocationClass = HarnessInvocationAgent
	}
	return profile
}

func (d *Daemon) resolveAgentProfile(ctx context.Context, store *globaldb.Store, workspaceID, name string) (AgentProfile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return AgentProfile{}, rpc.NewHandlerError(rpc.InvalidParams, "profile is required", map[string]any{"reason": "missing_profile", "start_invoked": false})
	}
	if d == nil || d.agentProfiles == nil {
		return resolveStoredAgentProfile(ctx, store, workspaceID, name)
	}
	profile, ok := d.agentProfiles[name]
	if !ok {
		return resolveStoredAgentProfile(ctx, store, workspaceID, name)
	}
	return profile, nil
}

func resolveStoredAgentProfile(ctx context.Context, store *globaldb.Store, workspaceID, name string) (AgentProfile, error) {
	if store == nil {
		return AgentProfile{}, unknownProfileError(name)
	}
	stored, err := store.GetAgentProfile(ctx, workspaceID, name)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return AgentProfile{}, unknownProfileError(name)
		}
		return AgentProfile{}, err
	}
	return AgentProfile{ProfileID: stored.ProfileID, Name: stored.Name, Harness: stored.Harness, Model: stored.Model, Prompt: stored.Prompt, InvocationClass: HarnessInvocationClass(stored.InvocationClass)}, nil
}

func (d *Daemon) startAgentRun(ctx context.Context, store *globaldb.Store, req AgentRunStartRequest, profile ...AgentProfile) (AgentRunStartResponse, error) {
	executorName := strings.TrimSpace(req.Executor)
	if executorName == "" {
		return AgentRunStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "executor is required", nil)
	}
	primaryFolder, err := lookupPrimaryFolder(ctx, store, req.Packet.WorkspaceID)
	if err != nil {
		return AgentRunStartResponse{}, mapWorkspaceStoreError(err, req.Packet.WorkspaceID)
	}
	executor, err := d.resolveHarness(req, primaryFolder)
	if err != nil {
		return AgentRunStartResponse{}, mapHarnessRunError(err)
	}
	if _, err := store.GetSession(ctx, req.Packet.WorkspaceID); err != nil {
		return AgentRunStartResponse{}, mapWorkspaceStoreError(err, req.Packet.WorkspaceID)
	}
	result, err := StartExecutorRunResult(ctx, executor, req.Packet, profile...)
	if err != nil {
		return AgentRunStartResponse{}, mapHarnessRunError(err)
	}
	run := result.AgentRun
	items := result.Items
	d.recordExecutorRun(run, items)
	if result.FinalResponse != nil {
		if err := storeFinalResponse(ctx, store, result, profile...); err != nil {
			return AgentRunStartResponse{}, err
		}
	}
	sample := agentRunProcessMetricsSampler(ctx, run)
	if run.ProcessSample != nil {
		sample = *run.ProcessSample
	}
	if err := storeAgentRunTelemetry(ctx, store, result, sample, profile...); err != nil {
		return AgentRunStartResponse{}, err
	}
	return AgentRunStartResponse{Run: run, Items: items}, nil
}

func mapHarnessRunError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*rpc.HandlerError); ok {
		return err
	}
	if unsupported, ok := err.(*UnsupportedHarnessCapabilitiesError); ok {
		return rpc.NewHandlerError(rpc.InvalidParams, unsupported.Error(), map[string]any{"unsupported_capabilities": harnessCapabilitiesToStrings(unsupported.Capabilities), "start_invoked": false})
	}
	if unavailable, ok := err.(*HarnessUnavailableError); ok {
		return rpc.NewHandlerError(rpc.InvalidParams, unavailable.Error(), unavailable.Data())
	}
	if validation, ok := err.(*HarnessValidationError); ok {
		return rpc.NewHandlerError(rpc.InvalidParams, validation.Error(), validation.Data())
	}
	return err
}

func unknownProfileError(profile string) error {
	return rpc.NewHandlerError(rpc.InvalidParams, "profile is not available", map[string]any{"profile": strings.TrimSpace(profile), "reason": "unknown_profile", "start_invoked": false})
}

func unknownHarnessError(harness string) error {
	return rpc.NewHandlerError(rpc.InvalidParams, "harness is not available", map[string]any{"harness": strings.TrimSpace(harness), "reason": "unknown_harness", "start_invoked": false})
}

func (d *Daemon) appendExecutorItems(runID string, items []TimelineItem) {
	if d == nil || strings.TrimSpace(runID) == "" || len(items) == 0 {
		return
	}
	d.executorMu.Lock()
	d.executorItems[runID] = append(d.executorItems[runID], items...)
	d.updateExecutorRunStatusLocked(runID, items)
	d.executorMu.Unlock()
}

func StartExecutorRun(ctx context.Context, executor Executor, packet ContextPacket, profile ...AgentProfile) (AgentRun, []TimelineItem, error) {
	result, err := StartExecutorRunResult(ctx, executor, packet, profile...)
	if err != nil {
		return AgentRun{}, nil, err
	}
	return result.AgentRun, result.Items, nil
}

func StartExecutorRunResult(ctx context.Context, executor Executor, packet ContextPacket, profile ...AgentProfile) (HarnessCallResult, error) {
	if ctx == nil {
		return HarnessCallResult{}, fmt.Errorf("context is required")
	}
	if executor == nil {
		return HarnessCallResult{}, fmt.Errorf("executor is required")
	}
	if strings.TrimSpace(packet.ID) == "" {
		return HarnessCallResult{}, &HarnessValidationError{Message: "context packet id is required", Field: "packet.id"}
	}
	if strings.TrimSpace(packet.WorkspaceID) == "" {
		return HarnessCallResult{}, &HarnessValidationError{Message: "workspace id is required", Field: "packet.workspace_id"}
	}
	if strings.TrimSpace(packet.TaskID) == "" {
		return HarnessCallResult{}, &HarnessValidationError{Message: "task id is required", Field: "packet.task_id"}
	}
	call, err := NewAgentRunHarnessCall(packet, nil)
	if err != nil {
		return HarnessCallResult{}, err
	}
	if len(profile) > 0 {
		call.SourceProfileID = strings.TrimSpace(profile[0].ProfileID)
		if call.SourceProfileID == "" {
			call.SourceProfileID = strings.TrimSpace(profile[0].Name)
		}
		call.Model = strings.TrimSpace(profile[0].Model)
		call.Prompt = strings.TrimSpace(profile[0].Prompt)
		if profile[0].InvocationClass != "" {
			call.InvocationClass = profile[0].InvocationClass
		}
	}
	call.Input = json.RawMessage(renderContextPacket(packet))
	return StartHarnessCallResult(ctx, executor, call)
}

func storeFinalResponse(ctx context.Context, store *globaldb.Store, result HarnessCallResult, profile ...AgentProfile) error {
	responseID, err := newAriULID()
	if err != nil {
		return err
	}
	profileID := ""
	if len(profile) > 0 {
		profileID = strings.TrimSpace(profile[0].ProfileID)
	}
	links := []FinalResponseEvidenceLink{{Kind: "context_packet", ID: result.AgentRun.ContextPacketID}, {Kind: "agent_run", ID: result.AgentRun.AgentRunID}}
	for _, item := range result.Items {
		if strings.TrimSpace(item.ID) != "" {
			links = append(links, FinalResponseEvidenceLink{Kind: "timeline_item", ID: item.ID})
		}
	}
	encodedLinks, err := json.Marshal(links)
	if err != nil {
		return err
	}
	return store.UpsertFinalResponse(ctx, globaldb.FinalResponse{FinalResponseID: "fr_" + responseID, RunID: result.AgentRun.AgentRunID, WorkspaceID: result.AgentRun.WorkspaceID, TaskID: result.AgentRun.TaskID, ContextPacketID: result.AgentRun.ContextPacketID, ProfileID: profileID, Status: result.FinalResponse.Status, Text: result.FinalResponse.Text, EvidenceLinksJSON: string(encodedLinks)})
}

func storeAgentRunTelemetry(ctx context.Context, store *globaldb.Store, result HarnessCallResult, sample ProcessMetricsSample, profile ...AgentProfile) error {
	profileID := ""
	profileName := ""
	invocationClass := string(HarnessInvocationAgent)
	if len(profile) > 0 {
		profileID = strings.TrimSpace(profile[0].ProfileID)
		profileName = strings.TrimSpace(profile[0].Name)
		if profile[0].InvocationClass != "" {
			invocationClass = string(profile[0].InvocationClass)
		}
	}
	portsJSON := "[]"
	if len(sample.Ports) > 0 {
		encoded, err := json.Marshal(sample.Ports)
		if err != nil {
			return err
		}
		portsJSON = string(encoded)
	}
	model := strings.TrimSpace(result.Telemetry.Model)
	if model == "" {
		model = "unknown"
	}
	durationMS, durationKnown := agentRunDurationMS(result.AgentRun)
	return store.UpsertAgentRunTelemetry(ctx, globaldb.AgentRunTelemetry{RunID: result.AgentRun.AgentRunID, WorkspaceID: result.AgentRun.WorkspaceID, TaskID: result.AgentRun.TaskID, ProfileID: profileID, ProfileName: profileName, Harness: result.AgentRun.Executor, Model: model, InvocationClass: invocationClass, Status: result.AgentRun.Status, InputTokensKnown: result.Telemetry.InputTokens != nil, InputTokens: result.Telemetry.InputTokens, OutputTokensKnown: result.Telemetry.OutputTokens != nil, OutputTokens: result.Telemetry.OutputTokens, DurationMSKnown: durationKnown, DurationMS: durationMS, OwnedByAri: sample.OwnedByAri, PIDKnown: sample.PID.Known, PID: sample.PID.Value, CPUTimeMSKnown: sample.CPUTimeMS.Known, CPUTimeMS: sample.CPUTimeMS.Value, MemoryRSSBytesPeakKnown: sample.MemoryRSSBytesPeak.Known, MemoryRSSBytesPeak: sample.MemoryRSSBytesPeak.Value, ChildProcessesPeakKnown: sample.ChildProcessesPeak.Known, ChildProcessesPeak: sample.ChildProcessesPeak.Value, PortsJSON: portsJSON, OrphanState: sample.OrphanState, ExitCodeKnown: sample.ExitCode.Known, ExitCode: sample.ExitCode.Value})
}

func agentRunDurationMS(run AgentRun) (*int64, bool) {
	startedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(run.StartedAt))
	if err != nil {
		return nil, false
	}
	finishedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(run.FinishedAt))
	if err != nil {
		return nil, false
	}
	value := finishedAt.Sub(startedAt).Milliseconds()
	if value < 0 {
		return nil, false
	}
	return &value, true
}

func (d *Daemon) recordExecutorRun(run AgentRun, items []TimelineItem) {
	if d == nil || strings.TrimSpace(run.AgentRunID) == "" {
		return
	}
	d.executorMu.Lock()
	d.executorRuns[run.AgentRunID] = run
	buffered := append([]TimelineItem(nil), d.executorItems[run.AgentRunID]...)
	d.executorItems[run.AgentRunID] = mergeExecutorItems(items, buffered)
	d.updateExecutorRunStatusLocked(run.AgentRunID, d.executorItems[run.AgentRunID])
	d.executorMu.Unlock()
}

func mergeExecutorItems(primary, buffered []TimelineItem) []TimelineItem {
	out := make([]TimelineItem, 0, len(primary)+len(buffered))
	seen := make(map[string]bool, len(primary)+len(buffered))
	for _, item := range primary {
		out = append(out, item)
		if strings.TrimSpace(item.ID) != "" {
			seen[item.ID] = true
		}
	}
	for _, item := range buffered {
		if strings.TrimSpace(item.ID) != "" && seen[item.ID] {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (d *Daemon) updateExecutorRunStatusLocked(runID string, items []TimelineItem) {
	run, ok := d.executorRuns[runID]
	if !ok {
		return
	}
	status := executorRunStatusFromItems(items)
	if status == "running" {
		return
	}
	run.Status = status
	d.executorRuns[runID] = run
}

func executorRunStatusFromItems(items []TimelineItem) string {
	if len(items) == 0 {
		return "running"
	}
	completed := false
	for _, item := range items {
		switch strings.TrimSpace(item.Status) {
		case "failed":
			return "failed"
		case "completed":
			completed = true
		}
	}
	if completed {
		return "completed"
	}
	return "running"
}

func renderContextPacket(packet ContextPacket) string {
	encoded, err := json.Marshal(packet)
	if err != nil {
		panic(fmt.Sprintf("render context packet: %v", err))
	}
	return string(encoded)
}
