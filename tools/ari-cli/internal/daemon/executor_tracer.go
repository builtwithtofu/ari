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

type HarnessSessionStartRequest struct {
	Executor          string                    `json:"executor"`
	Packet            ContextPacket             `json:"packet"`
	Command           string                    `json:"command,omitempty"`
	Args              []string                  `json:"args,omitempty"`
	WorkspaceID       string                    `json:"workspace_id,omitempty"`
	Profile           string                    `json:"profile,omitempty"`
	ProfileDefinition *Profile                  `json:"profile_definition,omitempty"`
	Defaults          HarnessSessionDefaults    `json:"defaults,omitempty"`
	SessionID         string                    `json:"session_id,omitempty"`
	Message           string                    `json:"message,omitempty"`
	Prompt            string                    `json:"prompt,omitempty"`
	AuthProjection    HarnessAuthProjectionPlan `json:"-"`
}

type HarnessSessionStartResponse struct {
	Run   HarnessSession `json:"run"`
	Items []TimelineItem `json:"items"`
}

type SessionGetRequest struct {
	SessionID string `json:"session_id"`
}

type SessionGetResponse struct {
	Session HarnessSession `json:"session"`
}

type SessionListRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type SessionListResponse struct {
	Sessions []HarnessSession `json:"sessions"`
}

type SessionLogsRequest struct {
	SessionID string `json:"session_id"`
}

type SessionLogsResponse struct {
	SessionID         string   `json:"session_id"`
	ProviderSessionID string   `json:"provider_session_id"`
	Command           []string `json:"command"`
	Output            string   `json:"output"`
}

type SessionAttachRequest struct {
	SessionID string `json:"session_id"`
}

type SessionAttachResponse struct {
	SessionID         string   `json:"session_id"`
	ProviderSessionID string   `json:"provider_session_id"`
	Command           []string `json:"command"`
}

type HarnessSession struct {
	HarnessSessionID  string                `json:"harness_session_id"`
	SessionID         string                `json:"session_id,omitempty"`
	WorkspaceID       string                `json:"workspace_id"`
	Usage             string                `json:"usage,omitempty"`
	SourceSessionID   string                `json:"source_session_id,omitempty"`
	SourceAgentID     string                `json:"source_agent_id,omitempty"`
	TaskID            string                `json:"task_id"`
	Executor          string                `json:"executor"`
	ProviderSessionID string                `json:"provider_session_id"`
	ProviderRunID     string                `json:"provider_run_id,omitempty"`
	InvocationMode    string                `json:"invocation_mode,omitempty"`
	UsageBucket       string                `json:"usage_bucket,omitempty"`
	AuthSlotID        string                `json:"auth_slot_id,omitempty"`
	Status            string                `json:"status"`
	ContextPacketID   string                `json:"context_packet_id"`
	StartedAt         string                `json:"started_at"`
	FinishedAt        string                `json:"finished_at,omitempty"`
	PID               int                   `json:"pid,omitempty"`
	ExitCode          *int                  `json:"exit_code,omitempty"`
	ProcessSample     *ProcessMetricsSample `json:"-"`
	Capabilities      []string              `json:"capabilities"`
}

const (
	HarnessNameCodex    = "codex"
	HarnessNameClaude   = "claude"
	HarnessNameOpenCode = "opencode"
	HarnessNamePi       = "pi"
	HarnessNameGrok     = "grok"
	HarnessNamePTY      = "pty"
)

type HarnessFactory func(HarnessSessionStartRequest, string, func(string, []TimelineItem)) (Executor, error)

type HarnessSessionConfigCreateRequest struct {
	AgentID     string `json:"agent_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Name        string `json:"name"`
	Harness     string `json:"harness,omitempty"`
	Model       string `json:"model,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

type HarnessSessionConfigCreateResponse struct {
	Config HarnessSessionConfigResponse `json:"config"`
}

type HarnessSessionConfigListRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type HarnessSessionConfigListResponse struct {
	Configs []HarnessSessionConfigResponse `json:"configs"`
}

type HarnessSessionConfigUpdateRequest struct {
	AgentID     string `json:"agent_id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Harness     string `json:"harness,omitempty"`
	Model       string `json:"model,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

type HarnessSessionConfigDeleteRequest struct {
	AgentID string `json:"agent_id"`
}

type HarnessSessionConfigDeleteResponse struct {
	Deleted bool `json:"deleted"`
}

type HarnessSessionConfigSessionRequest struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
	CWD       string `json:"cwd,omitempty"`
}

type HarnessSessionConfigSessionResponse struct {
	Run globaldb.HarnessSession `json:"run"`
}

type HarnessSessionConfigResponse struct {
	AgentID     string `json:"agent_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Name        string `json:"name"`
	Harness     string `json:"harness,omitempty"`
	Model       string `json:"model,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

type EphemeralCallRequest struct {
	CallID              string   `json:"call_id"`
	WorkspaceID         string   `json:"workspace_id,omitempty"`
	SourceSessionID     string   `json:"source_session_id"`
	TargetAgentID       string   `json:"target_agent_id"`
	Body                string   `json:"body"`
	ContextExcerptIDs   []string `json:"context_excerpt_ids,omitempty"`
	SessionID           string   `json:"session_id,omitempty"`
	ReplyAgentMessageID string   `json:"reply_agent_message_id,omitempty"`
	FanoutGroupID       string   `json:"fanout_group_id,omitempty"`
	FanoutMemberID      string   `json:"fanout_member_id,omitempty"`
	SuppressReply       bool     `json:"suppress_reply,omitempty"`
	TimeoutMS           int64    `json:"timeout_ms,omitempty"`
}

type EphemeralCallResponse struct {
	Run           globaldb.HarnessSession `json:"run"`
	Request       AgentMessageResponse    `json:"request"`
	Reply         AgentMessageResponse    `json:"reply"`
	FinalResponse globaldb.FinalResponse  `json:"final_response,omitempty"`
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

var agentSessionProcessMetricsSampler = func(ctx context.Context, run HarnessSession) ProcessMetricsSample {
	return sampleLinuxProcessMetrics(ctx, run)
}

func unknownProcessMetric(confidence string) ProcessMetricValue {
	return ProcessMetricValue{Known: false, Confidence: strings.TrimSpace(confidence)}
}

func (d *Daemon) resolveHarness(ctx context.Context, store *globaldb.Store, req HarnessSessionStartRequest, primaryFolder string) (Executor, error) {
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
	return factory(req, primaryFolder, d.appendExecutorItemsToStore(ctx, store))
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

func (d *Daemon) registerExecutorMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}
	if err := d.registerProfileSessionMethods(registry, store); err != nil {
		return err
	}
	if err := d.registerMessageContextMethods(registry, store); err != nil {
		return err
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[EphemeralCallRequest, EphemeralCallResponse]{
		Name:        "session.call.ephemeral",
		Description: "Run a task-scoped target profile and route its response back to the caller",
		Handler: func(ctx context.Context, req EphemeralCallRequest) (EphemeralCallResponse, error) {
			return d.callEphemeral(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register session.call.ephemeral: %w", err)
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
	if err := rpc.RegisterMethod(registry, rpc.Method[HarnessAuthStatusRequest, HarnessAuthStatusResponse]{
		Name:        "auth.status",
		Description: "Summarize provider-owned harness auth readiness without reading secrets",
		Handler: func(ctx context.Context, req HarnessAuthStatusRequest) (HarnessAuthStatusResponse, error) {
			return d.harnessAuthStatus(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register auth.status: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[HarnessAuthDiagnoseRequest, HarnessAuthDiagnoseResponse]{
		Name:        "auth.diagnose",
		Description: "Diagnose harness auth readiness, slots, and capability limits without reading secrets",
		Handler: func(ctx context.Context, req HarnessAuthDiagnoseRequest) (HarnessAuthDiagnoseResponse, error) {
			return d.harnessAuthDiagnose(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register auth.diagnose: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[HarnessAuthStartRequest, HarnessAuthStartResponse]{
		Name:        "auth.start",
		Description: "Start a provider-owned auth flow for a configured auth slot without handling secrets",
		Handler: func(ctx context.Context, req HarnessAuthStartRequest) (HarnessAuthStartResponse, error) {
			return d.harnessAuthStart(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register auth.start: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[HarnessAuthProviderMethodsRequest, HarnessAuthProviderMethodsResponse]{
		Name:        "auth.provider_methods",
		Description: "Discover provider auth methods through the harness adapter without exposing secrets",
		Handler: func(ctx context.Context, req HarnessAuthProviderMethodsRequest) (HarnessAuthProviderMethodsResponse, error) {
			return d.harnessAuthProviderMethods(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register auth.provider_methods: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[HarnessAuthCancelRequest, HarnessAuthCancelResponse]{
		Name:        "auth.cancel",
		Description: "Cancel a provider-owned auth flow for a configured auth slot when supported",
		Handler: func(ctx context.Context, req HarnessAuthCancelRequest) (HarnessAuthCancelResponse, error) {
			return d.harnessAuthCancel(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register auth.cancel: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[HarnessAuthLogoutRequest, HarnessAuthLogoutResponse]{
		Name:        "auth.logout",
		Description: "Log out a provider-owned auth account when the harness supports doing so without exposing secrets",
		Handler: func(ctx context.Context, req HarnessAuthLogoutRequest) (HarnessAuthLogoutResponse, error) {
			return d.harnessAuthLogout(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register auth.logout: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AuthSlotListRequest, AuthSlotListResponse]{
		Name:        "auth.slot.list",
		Description: "List Ari auth slot metadata without credential sources or secrets",
		Handler: func(ctx context.Context, req AuthSlotListRequest) (AuthSlotListResponse, error) {
			return listAuthSlots(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register auth.slot.list: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AuthSlotGetRequest, AuthSlotResponse]{
		Name:        "auth.slot.get",
		Description: "Get Ari auth slot metadata without credential sources or secrets",
		Handler: func(ctx context.Context, req AuthSlotGetRequest) (AuthSlotResponse, error) {
			return getAuthSlot(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register auth.slot.get: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AuthSlotSaveRequest, AuthSlotResponse]{
		Name:        "auth.slot.save",
		Description: "Save Ari auth account metadata without credential sources or secrets",
		Handler: func(ctx context.Context, req AuthSlotSaveRequest) (AuthSlotResponse, error) {
			return saveAuthSlot(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register auth.slot.save: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AuthSlotRemoveRequest, AuthSlotRemoveResponse]{
		Name:        "auth.slot.remove",
		Description: "Remove Ari auth slot metadata without touching provider-owned credentials",
		Handler: func(ctx context.Context, req AuthSlotRemoveRequest) (AuthSlotRemoveResponse, error) {
			return d.removeAuthSlot(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register auth.slot.remove: %w", err)
	}
	return nil
}

func getWorkspaceSession(ctx context.Context, store *globaldb.Store, req SessionGetRequest) (HarnessSession, error) {
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		return HarnessSession{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", map[string]any{"reason": "missing_session_id"})
	}
	session, err := store.GetHarnessSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return HarnessSession{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_session", "session_id": sessionID})
		}
		return HarnessSession{}, err
	}
	return agentSessionResponseFromStore(session), nil
}

func listWorkspaceSessions(ctx context.Context, store *globaldb.Store, req SessionListRequest) ([]HarnessSession, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return nil, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", map[string]any{"reason": "missing_workspace"})
	}
	sessions, err := store.ListHarnessSessions(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	resp := make([]HarnessSession, 0, len(sessions))
	for _, session := range sessions {
		resp = append(resp, agentSessionResponseFromStore(session))
	}
	return resp, nil
}

func agentSessionResponseFromStore(session globaldb.HarnessSession) HarnessSession {
	invocationMode, usageBucket := agentSessionModeFromProviderMetadata(session.ProviderMetadataJSON)
	return HarnessSession{HarnessSessionID: session.SessionID, SessionID: session.SessionID, WorkspaceID: session.WorkspaceID, Usage: session.Usage, SourceSessionID: session.SourceSessionID, SourceAgentID: session.SourceAgentID, Executor: session.Harness, ProviderSessionID: session.ProviderSessionID, ProviderRunID: session.ProviderRunID, InvocationMode: invocationMode, UsageBucket: usageBucket, Status: session.Status}
}

func agentSessionModeFromProviderMetadata(raw string) (string, string) {
	metadata := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return "", ""
	}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return "", ""
	}
	return stringMetadata(metadata, "invocation_mode"), stringMetadata(metadata, "usage_bucket")
}

func startProfileSession(d *Daemon, ctx context.Context, store *globaldb.Store, req HarnessSessionStartRequest) (HarnessSessionStartResponse, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(req.Packet.WorkspaceID)
	}
	if workspaceID == "" {
		return HarnessSessionStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", map[string]any{"reason": "missing_workspace", "start_invoked": false})
	}
	profile, err := d.resolveProfileRunRequest(ctx, store, ProfileRunRequest{Profile: req.Profile, Executor: req.Executor, ProfileDefinition: req.ProfileDefinition, Defaults: req.Defaults, Packet: ContextPacket{WorkspaceID: workspaceID}})
	if err != nil {
		return HarnessSessionStartResponse{}, err
	}
	if override := strings.TrimSpace(req.Prompt); override != "" {
		profile.Prompt = override
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		generated, err := newAriULID()
		if err != nil {
			return HarnessSessionStartResponse{}, err
		}
		sessionID = "as_" + generated
	} else {
		existing, err := store.GetHarnessSession(ctx, sessionID)
		if err == nil {
			if existing.WorkspaceID != workspaceID {
				return HarnessSessionStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id belongs to a different workspace", map[string]any{"reason": "session_workspace_mismatch", "session_id": sessionID, "workspace_id": workspaceID, "existing_workspace_id": existing.WorkspaceID, "start_invoked": false})
			}
			if strings.TrimSpace(existing.AgentID) != "" && strings.TrimSpace(profile.ProfileID) != "" && existing.AgentID != profile.ProfileID {
				existingProfile := strings.TrimSpace(existing.AgentID)
				if storedProfile, profileErr := store.GetHarnessSessionConfig(ctx, existing.AgentID); profileErr == nil && strings.TrimSpace(storedProfile.Name) != "" {
					existingProfile = strings.TrimSpace(storedProfile.Name)
				}
				return HarnessSessionStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id belongs to a different profile", map[string]any{"reason": "session_profile_mismatch", "session_id": sessionID, "profile": strings.TrimSpace(profile.Name), "existing_profile": existingProfile, "start_invoked": false})
			}
			return HarnessSessionStartResponse{Run: agentSessionResponseFromStore(existing)}, nil
		}
		if !errors.Is(err, globaldb.ErrNotFound) {
			return HarnessSessionStartResponse{}, err
		}
	}
	harnessReq := req
	harnessReq.Executor = profile.Harness
	harnessReq.SessionID = sessionID
	harnessReq.Packet.WorkspaceID = workspaceID
	if strings.TrimSpace(harnessReq.Packet.ID) == "" {
		if message := strings.TrimSpace(req.Message); strings.HasPrefix(message, "ctx_") {
			harnessReq.Packet.ID = message
		} else {
			id, err := newAriULID()
			if err != nil {
				return HarnessSessionStartResponse{}, err
			}
			harnessReq.Packet.ID = "ctx_" + id
		}
	}
	if strings.TrimSpace(harnessReq.Packet.TaskID) == "" {
		harnessReq.Packet.TaskID = sessionID
	}
	if strings.TrimSpace(req.Message) != "" && len(harnessReq.Packet.Sections) == 0 {
		harnessReq.Packet.Sections = []ContextSection{{Name: "message", Content: strings.TrimSpace(req.Message)}}
	}
	return d.startHarnessSession(ctx, store, harnessReq, profile)
}

func (d *Daemon) startHarnessSession(ctx context.Context, store *globaldb.Store, req HarnessSessionStartRequest, profile ...Profile) (HarnessSessionStartResponse, error) {
	executorName := strings.TrimSpace(req.Executor)
	if executorName == "" {
		return HarnessSessionStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "executor is required", nil)
	}
	if err := requireWorkspaceCanStartRuntime(ctx, store, req.Packet.WorkspaceID); err != nil {
		return HarnessSessionStartResponse{}, err
	}
	primaryFolder, err := lookupPrimaryFolder(ctx, store, req.Packet.WorkspaceID)
	if err != nil {
		return HarnessSessionStartResponse{}, mapWorkspaceStoreError(err, req.Packet.WorkspaceID)
	}
	executor, err := d.resolveHarness(ctx, store, req, primaryFolder)
	if err != nil {
		return HarnessSessionStartResponse{}, mapHarnessRunError(err)
	}
	if len(profile) > 0 {
		selected, err := resolveProfileAuthSlot(ctx, store, executor, req.Executor, profile[0])
		if err != nil {
			return HarnessSessionStartResponse{}, mapHarnessRunError(err)
		}
		profile[0].AuthSlotID = selected
	}
	if projection, err := d.authProjectionForStart(ctx, store, strings.TrimSpace(req.Executor), req.Packet.WorkspaceID, authSlotIDFromProfiles(profile...)); err != nil {
		return HarnessSessionStartResponse{}, mapHarnessRunError(err)
	} else if projection.Kind != "" {
		req.AuthProjection = projection
	}
	result, err := StartExecutorRunResultWithProjection(ctx, executor, req.Packet, strings.TrimSpace(req.SessionID), req.AuthProjection, profile...)
	if err != nil {
		return HarnessSessionStartResponse{}, mapHarnessRunError(err)
	}
	run := result.HarnessSession
	items := result.Items
	d.recordExecutorRun(run, items)
	if err := newHarnessLifecycle(store).persistNewStickyResult(ctx, result, primaryFolder, profile...); err != nil {
		return HarnessSessionStartResponse{}, err
	}
	if run.Status == "running" {
		_, cancel := context.WithCancel(context.Background())
		d.registerActiveHarnessRun(run.WorkspaceID, run.HarnessSessionID, run.ProviderSessionID, executor, cancel)
	} else if run.Status == "completed" && activeHarnessDeclaresDeliveryCapability(executor, HarnessDeliveryVisiblePromptTurn) {
		d.registerHarnessDeliveryTarget(run.WorkspaceID, run.HarnessSessionID, run.ProviderSessionID, executor)
	}
	return HarnessSessionStartResponse{Run: run, Items: items}, nil
}

func storeHarnessRunLogMessages(ctx context.Context, store *globaldb.Store, result HarnessCallResult, primaryFolder string, profile ...Profile) error {
	run := result.HarnessSession
	agentName := strings.TrimSpace(run.Executor)
	agentID := ""
	agentConfigWorkspaceID := strings.TrimSpace(run.WorkspaceID)
	model := result.Telemetry.Model
	prompt := ""
	if len(profile) > 0 {
		if strings.TrimSpace(profile[0].ProfileID) != "" {
			agentID = strings.TrimSpace(profile[0].ProfileID)
			agentConfigWorkspaceID = strings.TrimSpace(profile[0].WorkspaceID)
		}
		if strings.TrimSpace(profile[0].Name) != "" {
			agentName = strings.TrimSpace(profile[0].Name)
		}
		if strings.TrimSpace(model) == "" {
			model = strings.TrimSpace(profile[0].Model)
		}
		prompt = strings.TrimSpace(profile[0].Prompt)
	}
	if agentName == "" {
		agentName = "executor"
	}
	if agentID == "" {
		agentID = "wa_" + stableRuntimeAgentIDSegment(run.WorkspaceID) + "_" + stableRuntimeAgentIDSegment(agentName)
	}
	if err := store.EnsureHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: agentID, WorkspaceID: agentConfigWorkspaceID, Name: agentName, Harness: run.Executor, Model: model, Prompt: prompt}); err != nil {
		return err
	}
	metadata := map[string]any{"session_ref": result.SessionRef, "provider_session_id": run.ProviderSessionID, "capabilities": run.Capabilities, "invocation_mode": run.InvocationMode, "usage_bucket": run.UsageBucket}
	if len(profile) > 0 {
		if authSlotID := strings.TrimSpace(profile[0].AuthSlotID); authSlotID != "" {
			metadata["auth_slot_id"] = authSlotID
		}
	}
	providerMetadata, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	folderScopeJSON := "[]"
	if strings.TrimSpace(primaryFolder) != "" {
		encoded, err := json.Marshal([]string{strings.TrimSpace(primaryFolder)})
		if err != nil {
			return err
		}
		folderScopeJSON = string(encoded)
	}
	if err := store.CreateHarnessSession(ctx, globaldb.HarnessSession{SessionID: run.HarnessSessionID, WorkspaceID: run.WorkspaceID, AgentID: agentID, Harness: run.Executor, Model: model, ProviderSessionID: result.SessionRef.ProviderSessionID, ProviderRunID: run.ProviderRunID, ProviderThreadID: result.SessionRef.ProviderThreadID, CWD: strings.TrimSpace(primaryFolder), FolderScopeJSON: folderScopeJSON, Status: run.Status, Usage: globaldb.HarnessSessionUsageSticky, ContextPayloadIDsJSON: fmt.Sprintf("[%q]", run.ContextPacketID), ProviderMetadataJSON: string(providerMetadata)}); err != nil {
		return err
	}
	sequence := 1
	for _, item := range result.Items {
		if strings.TrimSpace(item.Text) == "" {
			continue
		}
		role := "assistant"
		kind := "text"
		if strings.TrimSpace(item.Kind) != "agent_text" {
			kind = strings.TrimSpace(item.Kind)
		}
		messageID := strings.TrimSpace(item.ID)
		if messageID == "" {
			messageID = fmt.Sprintf("%s-message-%d", run.HarnessSessionID, sequence)
		}
		rawMetadata := "{}"
		if len(item.Metadata) > 0 {
			encoded, err := json.Marshal(item.Metadata)
			if err != nil {
				return err
			}
			rawMetadata = string(encoded)
		}
		providerFields := providerMessageFieldsFromTimelineItem(item)
		if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: messageID, SessionID: run.HarnessSessionID, Sequence: sequence, Role: role, Status: "completed", ProviderMessageID: providerFields.messageID, ProviderItemID: providerFields.itemID, ProviderTurnID: providerFields.turnID, ProviderResponseID: providerFields.responseID, ProviderCallID: providerFields.callID, ProviderChannel: providerFields.channel, ProviderKind: providerFields.kind, RawMetadataJSON: rawMetadata, Parts: []globaldb.RunLogMessagePart{{PartID: messageID + "-part-1", Sequence: 1, Kind: kind, Text: item.Text, ToolName: providerFields.toolName, ToolCallID: providerFields.callID}}}); err != nil {
			return err
		}
		sequence++
	}
	return nil
}

func stableRuntimeAgentIDSegment(value string) string {
	segment := strings.ToLower(strings.TrimSpace(value))
	segment = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, segment)
	segment = strings.Trim(segment, "_")
	if segment == "" {
		return "executor"
	}
	return segment
}

type providerMessageFields struct {
	messageID  string
	itemID     string
	turnID     string
	responseID string
	callID     string
	channel    string
	kind       string
	toolName   string
}

func providerMessageFieldsFromTimelineItem(item TimelineItem) providerMessageFields {
	metadata := item.Metadata
	return providerMessageFields{
		messageID:  metadataString(metadata, "provider_message_id", "message_id"),
		itemID:     metadataString(metadata, "provider_item_id", "item_id", "id"),
		turnID:     metadataString(metadata, "provider_turn_id", "turn_id", "turn"),
		responseID: metadataString(metadata, "provider_response_id", "response_id"),
		callID:     metadataString(metadata, "provider_call_id", "tool_call_id", "call_id"),
		channel:    metadataString(metadata, "provider_channel", "channel"),
		kind:       defaultString(metadataString(metadata, "provider_kind"), item.Kind),
		toolName:   metadataString(metadata, "tool_name", "name"),
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func metadataString(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return typed
			}
		case fmt.Stringer:
			if strings.TrimSpace(typed.String()) != "" {
				return typed.String()
			}
		}
	}
	return ""
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

func unknownHarnessError(harness string) error {
	return rpc.NewHandlerError(rpc.InvalidParams, "harness is not available", map[string]any{"harness": strings.TrimSpace(harness), "reason": "unknown_harness", "start_invoked": false})
}

func (d *Daemon) appendExecutorItems(sessionID string, items []TimelineItem) {
	if d == nil || strings.TrimSpace(sessionID) == "" || len(items) == 0 {
		return
	}
	d.executorMu.Lock()
	d.executorItems[sessionID] = append(d.executorItems[sessionID], items...)
	d.updateExecutorRunStatusLocked(sessionID, items)
	d.executorMu.Unlock()
}

func (d *Daemon) appendExecutorItemsToStore(ctx context.Context, store *globaldb.Store) func(string, []TimelineItem) {
	return func(sessionID string, items []TimelineItem) {
		d.appendExecutorItems(sessionID, items)
		if store == nil || strings.TrimSpace(sessionID) == "" || len(items) == 0 {
			return
		}
		emitCtx := ctx
		if emitCtx == nil {
			emitCtx = context.Background()
		}
		stored, err := store.GetHarnessSession(emitCtx, sessionID)
		if err != nil {
			return
		}
		run := HarnessSession{HarnessSessionID: stored.SessionID, SessionID: stored.SessionID, WorkspaceID: stored.WorkspaceID, Executor: stored.Harness, Status: stored.Status}
		_ = appendHarnessRuntimeWorkspaceEvents(emitCtx, store, run, harnessRuntimeEventsFromItems(run, items))
	}
}

func StartExecutorRun(ctx context.Context, executor Executor, packet ContextPacket, profile ...Profile) (HarnessSession, []TimelineItem, error) {
	result, err := StartExecutorRunResult(ctx, executor, packet, "", profile...)
	if err != nil {
		return HarnessSession{}, nil, err
	}
	return result.HarnessSession, result.Items, nil
}

func StartExecutorRunResult(ctx context.Context, executor Executor, packet ContextPacket, ariSessionID string, profile ...Profile) (HarnessCallResult, error) {
	return StartExecutorRunResultWithProjection(ctx, executor, packet, ariSessionID, HarnessAuthProjectionPlan{}, profile...)
}

func StartExecutorRunResultWithProjection(ctx context.Context, executor Executor, packet ContextPacket, ariSessionID string, projection HarnessAuthProjectionPlan, profile ...Profile) (HarnessCallResult, error) {
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
	call, err := NewHarnessSessionHarnessCall(packet, nil)
	if err != nil {
		return HarnessCallResult{}, err
	}
	call.AriSessionID = strings.TrimSpace(ariSessionID)
	if len(profile) > 0 {
		call.SourceProfileID = strings.TrimSpace(profile[0].ProfileID)
		if call.SourceProfileID == "" {
			call.SourceProfileID = strings.TrimSpace(profile[0].Name)
		}
		call.Model = strings.TrimSpace(profile[0].Model)
		call.Prompt = strings.TrimSpace(profile[0].Prompt)
		call.AuthSlotID = strings.TrimSpace(profile[0].AuthSlotID)
		if profile[0].InvocationClass != "" {
			call.InvocationClass = profile[0].InvocationClass
		}
		options, err := harnessOptionsFromProfile(executor, profile[0])
		if err != nil {
			return HarnessCallResult{}, err
		}
		call.Options = options
	}
	call.Input = json.RawMessage(renderContextPacket(packet))
	call.AuthProjection = projection
	return StartHarnessCallResult(ctx, executor, call)
}

func agentSessionDurationMS(run HarnessSession) (*int64, bool) {
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

func (d *Daemon) recordExecutorRun(run HarnessSession, items []TimelineItem) {
	if d == nil || strings.TrimSpace(run.HarnessSessionID) == "" {
		return
	}
	d.executorMu.Lock()
	d.executorRuns[run.HarnessSessionID] = run
	buffered := append([]TimelineItem(nil), d.executorItems[run.HarnessSessionID]...)
	d.executorItems[run.HarnessSessionID] = mergeExecutorItems(items, buffered)
	d.updateExecutorRunStatusLocked(run.HarnessSessionID, d.executorItems[run.HarnessSessionID])
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

func (d *Daemon) updateExecutorRunStatusLocked(sessionID string, items []TimelineItem) {
	run, ok := d.executorRuns[sessionID]
	if !ok {
		return
	}
	status := executorRunStatusFromItems(items)
	if status == "running" {
		return
	}
	run.Status = status
	d.executorRuns[sessionID] = run
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
