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
	HarnessNamePTY      = "pty"
)

type HarnessFactory func(HarnessSessionStartRequest, string, func(string, []TimelineItem)) (Executor, error)

type Profile struct {
	ProfileID       string                 `json:"profile_id,omitempty"`
	WorkspaceID     string                 `json:"workspace_id,omitempty"`
	Name            string                 `json:"name"`
	Harness         string                 `json:"harness"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	AuthSlotID      string                 `json:"auth_slot_id,omitempty"`
	AuthPool        HarnessAuthPool        `json:"auth_pool,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class"`
	Defaults        map[string]any         `json:"defaults,omitempty"`
}

type HarnessSessionDefaults struct {
	Harness         string                 `json:"harness,omitempty"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	AuthSlotID      string                 `json:"auth_slot_id,omitempty"`
	AuthPool        HarnessAuthPool        `json:"auth_pool,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class,omitempty"`
	Settings        map[string]any         `json:"settings,omitempty"`
}

type ProfileRunRequest struct {
	Profile           string                 `json:"profile,omitempty"`
	Executor          string                 `json:"executor,omitempty"`
	ProfileDefinition *Profile               `json:"profile_definition,omitempty"`
	Defaults          HarnessSessionDefaults `json:"defaults,omitempty"`
	Packet            ContextPacket          `json:"packet"`
}

type ProfileRunResponse struct {
	Profile string         `json:"profile"`
	Harness string         `json:"harness"`
	Run     HarnessSession `json:"run"`
	Items   []TimelineItem `json:"items"`
}

type ProfileCreateRequest struct {
	WorkspaceID     string                 `json:"workspace_id,omitempty"`
	Name            string                 `json:"name"`
	Harness         string                 `json:"harness,omitempty"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	AuthSlotID      string                 `json:"auth_slot_id,omitempty"`
	AuthPool        HarnessAuthPool        `json:"auth_pool,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class,omitempty"`
	Defaults        map[string]any         `json:"defaults,omitempty"`
}

type ProfileGetRequest struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	Name        string `json:"name"`
}

type ProfileListRequest struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type ProfileListResponse struct {
	Profiles []ProfileResponse `json:"profiles"`
}

type RunLogMessagesTailRequest struct {
	SessionID string `json:"session_id"`
	Count     int    `json:"count"`
}

type RunLogMessagesTailResponse struct {
	Messages []RunLogMessageResponse `json:"messages"`
}

type RunLogMessagesListRequest struct {
	SessionID     string `json:"session_id"`
	AfterSequence int    `json:"after_sequence,omitempty"`
	Limit         int    `json:"limit"`
}

type RunLogMessagesListResponse struct {
	Messages []RunLogMessageResponse `json:"messages"`
}

type RunLogMessageResponse struct {
	MessageID          string                      `json:"message_id"`
	SessionID          string                      `json:"session_id"`
	AgentID            string                      `json:"agent_id"`
	Sequence           int                         `json:"sequence"`
	Role               string                      `json:"role"`
	Status             string                      `json:"status"`
	ProviderMessageID  string                      `json:"provider_message_id,omitempty"`
	ProviderItemID     string                      `json:"provider_item_id,omitempty"`
	ProviderTurnID     string                      `json:"provider_turn_id,omitempty"`
	ProviderResponseID string                      `json:"provider_response_id,omitempty"`
	ProviderCallID     string                      `json:"provider_call_id,omitempty"`
	ProviderChannel    string                      `json:"provider_channel,omitempty"`
	ProviderKind       string                      `json:"provider_kind,omitempty"`
	RawMetadataJSON    string                      `json:"raw_metadata_json,omitempty"`
	Parts              []RunLogMessagePartResponse `json:"parts"`
}

type RunLogMessagePartResponse struct {
	PartID     string `json:"part_id"`
	Sequence   int    `json:"sequence"`
	Kind       string `json:"kind"`
	Text       string `json:"text,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
	URI        string `json:"uri,omitempty"`
	Name       string `json:"name,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	RawJSON    string `json:"raw_json,omitempty"`
}

type ContextExcerptCreateFromTailRequest struct {
	ContextExcerptID string `json:"context_excerpt_id"`
	SourceSessionID  string `json:"source_session_id"`
	TargetAgentID    string `json:"target_agent_id,omitempty"`
	Count            int    `json:"count"`
	AppendedMessage  string `json:"appended_message,omitempty"`
}

type ContextExcerptCreateFromRangeRequest struct {
	ContextExcerptID string `json:"context_excerpt_id"`
	SourceSessionID  string `json:"source_session_id"`
	TargetAgentID    string `json:"target_agent_id,omitempty"`
	StartSequence    int    `json:"start_sequence"`
	EndSequence      int    `json:"end_sequence"`
	AppendedMessage  string `json:"appended_message,omitempty"`
}

type ContextExcerptCreateFromExplicitIDsRequest struct {
	ContextExcerptID string   `json:"context_excerpt_id"`
	SourceSessionID  string   `json:"source_session_id"`
	TargetAgentID    string   `json:"target_agent_id,omitempty"`
	MessageIDs       []string `json:"message_ids"`
	AppendedMessage  string   `json:"appended_message,omitempty"`
}

type ContextExcerptGetRequest struct {
	ContextExcerptID string `json:"context_excerpt_id"`
}

type ContextExcerptResponse struct {
	ContextExcerptID string                       `json:"context_excerpt_id"`
	WorkspaceID      string                       `json:"workspace_id"`
	SourceSessionID  string                       `json:"source_session_id"`
	SourceAgentID    string                       `json:"source_agent_id"`
	TargetAgentID    string                       `json:"target_agent_id,omitempty"`
	TargetSessionID  string                       `json:"target_session_id,omitempty"`
	SelectorType     string                       `json:"selector_type"`
	SelectorJSON     string                       `json:"selector_json"`
	Visibility       string                       `json:"visibility"`
	AppendedMessage  string                       `json:"appended_message,omitempty"`
	ContentHash      string                       `json:"content_hash"`
	Items            []ContextExcerptItemResponse `json:"items"`
}

type ContextExcerptItemResponse struct {
	Sequence        int                         `json:"sequence"`
	SourceMessageID string                      `json:"source_message_id"`
	CopiedRole      string                      `json:"copied_role"`
	CopiedText      string                      `json:"copied_text"`
	CopiedParts     []RunLogMessagePartResponse `json:"copied_parts"`
}

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

type AgentMessageSendRequest struct {
	AgentMessageID    string   `json:"agent_message_id"`
	FanoutGroupID     string   `json:"fanout_group_id,omitempty"`
	WorkspaceID       string   `json:"workspace_id,omitempty"`
	SourceSessionID   string   `json:"source_session_id"`
	TargetAgentID     string   `json:"target_agent_id"`
	TargetProfileIDs  []string `json:"target_profile_ids,omitempty"`
	TargetSessionID   string   `json:"target_session_id,omitempty"`
	Body              string   `json:"body"`
	ContextExcerptIDs []string `json:"context_excerpt_ids,omitempty"`
	StartSessionID    string   `json:"start_session_id,omitempty"`
}

type AgentMessageSendResponse struct {
	AgentMessage  AgentMessageResponse   `json:"agent_message"`
	FanoutGroupID string                 `json:"fanout_group_id,omitempty"`
	FanoutMembers []FanoutMemberResponse `json:"fanout_members,omitempty"`
}

type FanoutMemberResponse struct {
	FanoutMemberID  string                  `json:"fanout_member_id"`
	TargetProfileID string                  `json:"target_profile_id"`
	Session         globaldb.HarnessSession `json:"session"`
	Request         AgentMessageResponse    `json:"request"`
}

type AgentMessageResponse struct {
	AgentMessageID     string   `json:"agent_message_id"`
	WorkspaceID        string   `json:"workspace_id"`
	SourceAgentID      string   `json:"source_agent_id"`
	SourceSessionID    string   `json:"source_session_id"`
	TargetAgentID      string   `json:"target_agent_id"`
	TargetSessionID    string   `json:"target_session_id"`
	Body               string   `json:"body"`
	Status             string   `json:"status"`
	DeliveredSessionID string   `json:"delivered_session_id"`
	ContextExcerptIDs  []string `json:"context_excerpt_ids,omitempty"`
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

type HarnessAuthStatusRequest struct {
	WorkspaceID string            `json:"workspace_id,omitempty"`
	Slots       []HarnessAuthSlot `json:"slots,omitempty"`
}

type HarnessAuthStatusResponse struct {
	Statuses []HarnessAuthStatus `json:"statuses"`
}

type HarnessAuthDiagnoseRequest struct {
	WorkspaceID             string `json:"workspace_id,omitempty"`
	DiscoverProviderMethods bool   `json:"discover_provider_methods,omitempty"`
}

type HarnessAuthDiagnoseResponse struct {
	Harnesses []HarnessAuthDiagnostic `json:"harnesses"`
}

type HarnessAuthDiagnostic struct {
	Harness         string                              `json:"harness"`
	Installed       bool                                `json:"installed"`
	Status          HarnessAuthState                    `json:"status"`
	DefaultSlot     HarnessAuthStatus                   `json:"default_slot"`
	NamedSlots      []AuthSlotResponse                  `json:"named_slots,omitempty"`
	Auth            HarnessAuthDescriptor               `json:"auth"`
	ProviderMethods HarnessAuthProviderMethodDiagnostic `json:"provider_methods"`
	NextStep        string                              `json:"next_step,omitempty"`
}

type HarnessAuthProviderMethodDiagnostic struct {
	Status    string                             `json:"status"`
	Connected []string                           `json:"connected,omitempty"`
	Providers map[string][]HarnessAuthMethodInfo `json:"providers,omitempty"`
}

type HarnessAuthProviderMethodsRequest struct {
	Harness     string `json:"harness"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type HarnessAuthProviderMethodsResponse struct {
	Status    string                             `json:"status"`
	Connected []string                           `json:"connected,omitempty"`
	Providers map[string][]HarnessAuthMethodInfo `json:"providers,omitempty"`
}

type HarnessAuthMethodInfo struct {
	Type  string `json:"type"`
	Label string `json:"label,omitempty"`
}

type HarnessAuthStartRequest struct {
	AuthSlotID  string `json:"auth_slot_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Method      string `json:"method,omitempty"`
}

type HarnessAuthStartResponse struct {
	Status HarnessAuthStatus `json:"status"`
}

type HarnessAuthCancelRequest struct {
	AuthSlotID  string `json:"auth_slot_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	FlowID      string `json:"flow_id"`
}

type HarnessAuthCancelResponse struct {
	Status HarnessAuthStatus `json:"status"`
}

type HarnessAuthLogoutRequest struct {
	AuthSlotID  string `json:"auth_slot_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type HarnessAuthLogoutResponse struct {
	Status HarnessAuthStatus `json:"status"`
}

type AuthSlotListRequest struct {
	Harness string `json:"harness,omitempty"`
}

type AuthSlotGetRequest struct {
	AuthSlotID string `json:"auth_slot_id"`
}

type AuthSlotSaveRequest struct {
	AuthSlotID    string `json:"auth_slot_id"`
	Harness       string `json:"harness"`
	Label         string `json:"label"`
	ProviderLabel string `json:"provider_label,omitempty"`
}

type AuthSlotRemoveRequest struct {
	AuthSlotID string `json:"auth_slot_id"`
}

type AuthSlotRemoveResponse struct {
	Status     string `json:"status"`
	AuthSlotID string `json:"auth_slot_id"`
}

type AuthSlotResponse struct {
	AuthSlotID      string `json:"auth_slot_id"`
	Harness         string `json:"harness"`
	Label           string `json:"label"`
	ProviderLabel   string `json:"provider_label,omitempty"`
	CredentialOwner string `json:"credential_owner"`
	Status          string `json:"status"`
}

type AuthSlotListResponse struct {
	Slots []AuthSlotResponse `json:"slots"`
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
	SessionID       string `json:"session_id,omitempty"`
}

type FinalResponseListRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type FinalResponseListResponse struct {
	FinalResponses []FinalResponseResponse `json:"final_responses"`
}

type FinalResponseResponse struct {
	FinalResponseID string                      `json:"final_response_id"`
	SessionID       string                      `json:"session_id"`
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

type ProfileResponse struct {
	ProfileID       string                 `json:"profile_id"`
	WorkspaceID     string                 `json:"workspace_id,omitempty"`
	Name            string                 `json:"name"`
	Harness         string                 `json:"harness,omitempty"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	AuthSlotID      string                 `json:"auth_slot_id,omitempty"`
	AuthPool        HarnessAuthPool        `json:"auth_pool,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class,omitempty"`
	Defaults        map[string]any         `json:"defaults,omitempty"`
}

func defaultProfiles() map[string]Profile {
	return make(map[string]Profile)
}

var agentSessionProcessMetricsSampler = func(ctx context.Context, run HarnessSession) ProcessMetricsSample {
	return sampleLinuxProcessMetrics(ctx, run)
}

func unknownProcessMetric(confidence string) ProcessMetricValue {
	return ProcessMetricValue{Known: false, Confidence: strings.TrimSpace(confidence)}
}

func (d *Daemon) resolveHarness(req HarnessSessionStartRequest, primaryFolder string) (Executor, error) {
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

func (d *Daemon) setProfileForTest(profile Profile) {
	if d == nil {
		return
	}
	if d.agentProfiles == nil {
		d.agentProfiles = make(map[string]Profile)
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

func agentSessionStartUsesProfile(req HarnessSessionStartRequest) bool {
	return strings.TrimSpace(req.Profile) != "" || req.ProfileDefinition != nil || agentSessionDefaultsSet(req.Defaults)
}

func agentSessionDefaultsSet(defaults HarnessSessionDefaults) bool {
	return strings.TrimSpace(defaults.Harness) != "" || strings.TrimSpace(defaults.Model) != "" || strings.TrimSpace(defaults.Prompt) != "" || strings.TrimSpace(defaults.AuthSlotID) != "" || len(defaults.AuthPool.SlotIDs) > 0 || defaults.InvocationClass != "" || len(defaults.Settings) > 0
}

type harnessAuthStatuser interface {
	AuthStatus(context.Context, HarnessAuthSlot) (HarnessAuthStatus, error)
}

type harnessAuthStarter interface {
	AuthStart(context.Context, HarnessAuthSlot, string) (HarnessAuthStatus, error)
}

type harnessAuthCanceller interface {
	AuthCancel(context.Context, HarnessAuthSlot, string) (HarnessAuthStatus, error)
}

type harnessAuthLoggerOuter interface {
	AuthLogout(context.Context, HarnessAuthSlot) (HarnessAuthStatus, error)
}

type harnessAuthProviderMethodDiscoverer interface {
	AuthProviderMethods(context.Context) (HarnessAuthProviderMethodsResponse, error)
}

func (d *Daemon) harnessAuthStart(ctx context.Context, store *globaldb.Store, req HarnessAuthStartRequest) (HarnessAuthStartResponse, error) {
	stored, err := store.GetAuthSlot(ctx, req.AuthSlotID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return HarnessAuthStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "auth slot is not configured", map[string]any{"reason": "unknown_auth_slot", "auth_slot_id": strings.TrimSpace(req.AuthSlotID), "start_invoked": false})
		}
		return HarnessAuthStartResponse{}, mapWorkspaceStoreError(err, req.AuthSlotID)
	}
	slot := harnessAuthSlotFromGlobal(stored)
	primaryFolder := ""
	if strings.TrimSpace(req.WorkspaceID) != "" {
		primaryFolder, err = lookupPrimaryFolder(ctx, store, req.WorkspaceID)
		if err != nil {
			return HarnessAuthStartResponse{}, mapWorkspaceStoreError(err, req.WorkspaceID)
		}
	}
	factory, ok := d.harnessRegistry.Resolve(slot.Harness)
	if !ok {
		return HarnessAuthStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is not available", map[string]any{"harness": strings.TrimSpace(slot.Harness), "reason": "unknown_harness", "start_invoked": false})
	}
	executor, err := factory(HarnessSessionStartRequest{Executor: slot.Harness}, primaryFolder, d.appendExecutorItems)
	if err != nil {
		return HarnessAuthStartResponse{}, mapHarnessRunError(err)
	}
	starter, ok := executor.(harnessAuthStarter)
	if !ok {
		return HarnessAuthStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness auth start is not supported", map[string]any{"harness": strings.TrimSpace(slot.Harness), "reason": "auth_start_unsupported", "start_invoked": false})
	}
	status, err := starter.AuthStart(ctx, slot, req.Method)
	if err != nil {
		return HarnessAuthStartResponse{}, mapHarnessRunError(err)
	}
	if status.Status != "" && status.Status != HarnessAuthUnknown {
		stored.Status = string(status.Status)
		if err := store.UpsertAuthSlot(ctx, stored); err != nil {
			return HarnessAuthStartResponse{}, err
		}
	}
	return HarnessAuthStartResponse{Status: status}, nil
}

func (d *Daemon) harnessAuthCancel(ctx context.Context, store *globaldb.Store, req HarnessAuthCancelRequest) (HarnessAuthCancelResponse, error) {
	stored, err := store.GetAuthSlot(ctx, req.AuthSlotID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return HarnessAuthCancelResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "auth slot is not configured", map[string]any{"reason": "unknown_auth_slot", "auth_slot_id": strings.TrimSpace(req.AuthSlotID), "cancel_invoked": false})
		}
		return HarnessAuthCancelResponse{}, mapWorkspaceStoreError(err, req.AuthSlotID)
	}
	flowID := strings.TrimSpace(req.FlowID)
	if flowID == "" {
		return HarnessAuthCancelResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "flow id is required", map[string]any{"reason": "missing_flow_id", "auth_slot_id": strings.TrimSpace(req.AuthSlotID), "cancel_invoked": false})
	}
	slot := harnessAuthSlotFromGlobal(stored)
	primaryFolder := ""
	if strings.TrimSpace(req.WorkspaceID) != "" {
		primaryFolder, err = lookupPrimaryFolder(ctx, store, req.WorkspaceID)
		if err != nil {
			return HarnessAuthCancelResponse{}, mapWorkspaceStoreError(err, req.WorkspaceID)
		}
	}
	factory, ok := d.harnessRegistry.Resolve(slot.Harness)
	if !ok {
		return HarnessAuthCancelResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is not available", map[string]any{"harness": strings.TrimSpace(slot.Harness), "reason": "unknown_harness", "cancel_invoked": false})
	}
	executor, err := factory(HarnessSessionStartRequest{Executor: slot.Harness}, primaryFolder, d.appendExecutorItems)
	if err != nil {
		return HarnessAuthCancelResponse{}, mapHarnessRunError(err)
	}
	canceller, ok := executor.(harnessAuthCanceller)
	if !ok {
		return HarnessAuthCancelResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness auth cancel is not supported", map[string]any{"harness": strings.TrimSpace(slot.Harness), "reason": "auth_cancel_unsupported", "cancel_invoked": false})
	}
	status, err := canceller.AuthCancel(ctx, slot, flowID)
	if err != nil {
		return HarnessAuthCancelResponse{}, mapHarnessRunError(err)
	}
	if status.Status != "" && status.Status != HarnessAuthUnknown {
		stored.Status = string(status.Status)
		if err := store.UpsertAuthSlot(ctx, stored); err != nil {
			return HarnessAuthCancelResponse{}, err
		}
	}
	return HarnessAuthCancelResponse{Status: status}, nil
}

func (d *Daemon) harnessAuthLogout(ctx context.Context, store *globaldb.Store, req HarnessAuthLogoutRequest) (HarnessAuthLogoutResponse, error) {
	stored, err := store.GetAuthSlot(ctx, req.AuthSlotID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return HarnessAuthLogoutResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "auth account is not configured", map[string]any{"reason": "unknown_auth_slot", "auth_slot_id": strings.TrimSpace(req.AuthSlotID), "logout_invoked": false})
		}
		return HarnessAuthLogoutResponse{}, mapWorkspaceStoreError(err, req.AuthSlotID)
	}
	slot := harnessAuthSlotFromGlobal(stored)
	primaryFolder := ""
	if strings.TrimSpace(req.WorkspaceID) != "" {
		primaryFolder, err = lookupPrimaryFolder(ctx, store, req.WorkspaceID)
		if err != nil {
			return HarnessAuthLogoutResponse{}, mapWorkspaceStoreError(err, req.WorkspaceID)
		}
	}
	factory, ok := d.harnessRegistry.Resolve(slot.Harness)
	if !ok {
		return HarnessAuthLogoutResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is not available", map[string]any{"harness": strings.TrimSpace(slot.Harness), "reason": "unknown_harness", "logout_invoked": false})
	}
	executor, err := factory(HarnessSessionStartRequest{Executor: slot.Harness}, primaryFolder, d.appendExecutorItems)
	if err != nil {
		return HarnessAuthLogoutResponse{}, mapHarnessRunError(err)
	}
	loggerOuter, ok := executor.(harnessAuthLoggerOuter)
	if !ok {
		return HarnessAuthLogoutResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness auth logout is not supported", map[string]any{"harness": strings.TrimSpace(slot.Harness), "reason": "auth_logout_unsupported", "logout_invoked": false, "ari_secret_storage": string(HarnessAriSecretStorageNone)})
	}
	status, err := loggerOuter.AuthLogout(ctx, slot)
	if err != nil {
		return HarnessAuthLogoutResponse{}, mapHarnessRunError(err)
	}
	if status.Status != "" && status.Status != HarnessAuthUnknown {
		stored.Status = string(status.Status)
		if err := store.UpsertAuthSlot(ctx, stored); err != nil {
			return HarnessAuthLogoutResponse{}, rpc.NewHandlerError(rpc.InternalError, "provider logout completed but Ari could not persist auth status", map[string]any{"reason": "auth_logout_status_persist_failed", "auth_slot_id": strings.TrimSpace(slot.AuthSlotID), "harness": strings.TrimSpace(slot.Harness), "status": string(status.Status), "logout_invoked": true, "ari_secret_storage": string(HarnessAriSecretStorageNone)})
		}
	}
	return HarnessAuthLogoutResponse{Status: status}, nil
}

func (d *Daemon) harnessAuthDiagnose(ctx context.Context, store *globaldb.Store, req HarnessAuthDiagnoseRequest) (HarnessAuthDiagnoseResponse, error) {
	primaryFolder := ""
	if strings.TrimSpace(req.WorkspaceID) != "" {
		var err error
		primaryFolder, err = lookupPrimaryFolder(ctx, store, req.WorkspaceID)
		if err != nil {
			return HarnessAuthDiagnoseResponse{}, mapWorkspaceStoreError(err, req.WorkspaceID)
		}
	}
	storedSlots, err := store.ListAuthSlots(ctx, "")
	if err != nil {
		return HarnessAuthDiagnoseResponse{}, err
	}
	storedByID := make(map[string]globaldb.AuthSlot, len(storedSlots))
	slotsByHarness := map[string][]globaldb.AuthSlot{}
	for _, stored := range storedSlots {
		storedByID[stored.AuthSlotID] = stored
		slotsByHarness[stored.Harness] = append(slotsByHarness[stored.Harness], stored)
	}
	resp := HarnessAuthDiagnoseResponse{Harnesses: make([]HarnessAuthDiagnostic, 0, len(providerAuthHarnesses()))}
	for _, harness := range providerAuthHarnesses() {
		auth := HarnessAuthDescriptor{}
		if descriptor, ok := d.harnessRegistry.ResolveDescriptor(harness); ok {
			auth = descriptor.Auth
		}
		factory, ok := d.harnessRegistry.Resolve(harness)
		if !ok {
			status := HarnessAuthStatus{Harness: harness, AuthSlotID: authSlotIDForName(harness, "default"), Name: "default", Status: HarnessAuthUnknown, AriSecretStorage: HarnessAriSecretStorageNone}
			resp.Harnesses = append(resp.Harnesses, HarnessAuthDiagnostic{Harness: harness, Installed: true, Status: HarnessAuthUnknown, DefaultSlot: status, Auth: auth, NextStep: authDiagnosticNextStep(status)})
			continue
		}
		executor, err := factory(HarnessSessionStartRequest{Executor: harness}, primaryFolder, d.appendExecutorItems)
		diagnostic := HarnessAuthDiagnostic{Harness: harness, Installed: true, Status: HarnessAuthUnknown, DefaultSlot: HarnessAuthStatus{Harness: harness, AuthSlotID: authSlotIDForName(harness, "default"), Name: "default", Status: HarnessAuthUnknown, AriSecretStorage: HarnessAriSecretStorageNone}, Auth: auth}
		if err != nil {
			diagnostic.DefaultSlot = NewHarnessAuthRequired(harness, diagnostic.DefaultSlot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, SecretOwnedBy: harness})
			diagnostic.DefaultSlot.Name = "default"
			diagnostic.Status = diagnostic.DefaultSlot.Status
			diagnostic.NextStep = authDiagnosticNextStep(diagnostic.DefaultSlot)
			resp.Harnesses = append(resp.Harnesses, diagnostic)
			continue
		}
		if describer, ok := executor.(HarnessDescriber); ok {
			diagnostic.Auth = describer.Descriptor().Auth
		}
		defaultSlot := harnessAuthSlotFromGlobal(globaldb.AuthSlot{AuthSlotID: authSlotIDForName(harness, "default"), Harness: harness, Label: "default", CredentialOwner: string(HarnessCredentialOwnerProvider), Status: string(HarnessAuthUnknown)})
		if stored, ok := storedByID[defaultSlot.AuthSlotID]; ok {
			defaultSlot = harnessAuthSlotFromGlobal(stored)
		}
		if statuser, ok := executor.(harnessAuthStatuser); ok {
			status, err := statuser.AuthStatus(ctx, defaultSlot)
			if err != nil {
				var unavailable *HarnessUnavailableError
				if errors.As(err, &unavailable) && unavailable.Reason == "missing_executable" {
					status = HarnessAuthStatus{Harness: harness, AuthSlotID: defaultSlot.AuthSlotID, Status: HarnessAuthNotInstalled, AriSecretStorage: HarnessAriSecretStorageNone}
					diagnostic.Installed = false
				} else {
					return HarnessAuthDiagnoseResponse{}, err
				}
			}
			status.Name = authStatusName(defaultSlot, harness)
			diagnostic.DefaultSlot = status
			diagnostic.Status = status.Status
			diagnostic.NextStep = authDiagnosticNextStep(status)
		}
		diagnostic.ProviderMethods = authProviderMethodDiagnostic(ctx, executor, req.DiscoverProviderMethods)
		for _, stored := range slotsByHarness[harness] {
			if stored.AuthSlotID == defaultSlot.AuthSlotID {
				continue
			}
			diagnostic.NamedSlots = append(diagnostic.NamedSlots, authSlotResponseFromGlobal(stored))
		}
		resp.Harnesses = append(resp.Harnesses, diagnostic)
	}
	return resp, nil
}

func providerAuthHarnesses() []string {
	return []string{HarnessNameClaude, HarnessNameCodex, HarnessNameOpenCode}
}

func authProviderMethodDiagnostic(ctx context.Context, executor Executor, discover bool) HarnessAuthProviderMethodDiagnostic {
	if !discover {
		return HarnessAuthProviderMethodDiagnostic{Status: "skipped"}
	}
	discoverer, ok := executor.(harnessAuthProviderMethodDiscoverer)
	if !ok {
		return HarnessAuthProviderMethodDiagnostic{Status: "unsupported"}
	}
	methods, err := discoverer.AuthProviderMethods(ctx)
	if err != nil {
		return HarnessAuthProviderMethodDiagnostic{Status: "error"}
	}
	return HarnessAuthProviderMethodDiagnostic(methods)
}

func (d *Daemon) harnessAuthProviderMethods(ctx context.Context, store *globaldb.Store, req HarnessAuthProviderMethodsRequest) (HarnessAuthProviderMethodsResponse, error) {
	primaryFolder := ""
	if strings.TrimSpace(req.WorkspaceID) != "" {
		var err error
		primaryFolder, err = lookupPrimaryFolder(ctx, store, req.WorkspaceID)
		if err != nil {
			return HarnessAuthProviderMethodsResponse{}, mapWorkspaceStoreError(err, req.WorkspaceID)
		}
	}
	harness := strings.TrimSpace(req.Harness)
	if harness == "" {
		return HarnessAuthProviderMethodsResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is required", map[string]any{"reason": "harness_required"})
	}
	factory, ok := d.harnessRegistry.Resolve(harness)
	if !ok {
		return HarnessAuthProviderMethodsResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is not registered", map[string]any{"harness": harness, "reason": "harness_not_registered"})
	}
	executor, err := factory(HarnessSessionStartRequest{Executor: harness}, primaryFolder, d.appendExecutorItems)
	if err != nil {
		return HarnessAuthProviderMethodsResponse{}, mapHarnessRunError(err)
	}
	discoverer, ok := executor.(harnessAuthProviderMethodDiscoverer)
	if !ok {
		return HarnessAuthProviderMethodsResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness auth provider methods are not supported", map[string]any{"harness": harness, "reason": "auth_provider_methods_unsupported"})
	}
	methods, err := discoverer.AuthProviderMethods(ctx)
	if err != nil {
		return HarnessAuthProviderMethodsResponse{}, mapHarnessRunError(err)
	}
	return methods, nil
}

func authSlotIDForName(harness, name string) string {
	harness = strings.TrimSpace(harness)
	name = strings.TrimSpace(name)
	if name == "" || name == "default" {
		return harness + "-default"
	}
	return harness + "-" + name
}

func authDiagnosticNextStep(status HarnessAuthStatus) string {
	if status.Status == HarnessAuthAuthenticated {
		return ""
	}
	if status.Status == HarnessAuthNotInstalled {
		return "Install " + authDiagnosticHarnessDisplayName(status.Harness) + ", then run `ari auth login --harness " + status.Harness + "`."
	}
	method := ""
	if status.Remediation != nil && strings.TrimSpace(status.Remediation.Method) != "" {
		method = strings.TrimSpace(status.Remediation.Method)
	}
	switch method {
	case "device_code":
		return "Run `ari auth login --harness " + status.Harness + "` and complete the provider's device-code login."
	case "opencode_interactive":
		return "Run `ari auth login --harness opencode` and complete OpenCode's provider login."
	case "browser":
		return "Run `ari auth login --harness " + status.Harness + "` and complete the provider browser login."
	case "api_key", "api_key_provider_setup":
		return "Run `ari auth login --harness " + status.Harness + "`; Ari will not store the provider API key."
	case "provider_config", "provider_login", "":
		return "Run `ari auth login --harness " + status.Harness + "` or check the provider's native auth setup."
	default:
		return "Resolve provider auth for " + status.Harness + ": " + method + "."
	}
}

func authDiagnosticHarnessDisplayName(harness string) string {
	switch harness {
	case HarnessNameClaude:
		return "Claude Code"
	case HarnessNameCodex:
		return "Codex"
	case HarnessNameOpenCode:
		return "OpenCode"
	default:
		return harness
	}
}

func (d *Daemon) harnessAuthStatus(ctx context.Context, store *globaldb.Store, req HarnessAuthStatusRequest) (HarnessAuthStatusResponse, error) {
	primaryFolder := ""
	if strings.TrimSpace(req.WorkspaceID) != "" {
		var err error
		primaryFolder, err = lookupPrimaryFolder(ctx, store, req.WorkspaceID)
		if err != nil {
			return HarnessAuthStatusResponse{}, mapWorkspaceStoreError(err, req.WorkspaceID)
		}
	}
	slots := req.Slots
	storedSlots, err := store.ListAuthSlots(ctx, "")
	if err != nil {
		return HarnessAuthStatusResponse{}, err
	}
	storedByID := make(map[string]globaldb.AuthSlot, len(storedSlots))
	for _, stored := range storedSlots {
		storedByID[stored.AuthSlotID] = stored
	}
	if len(slots) == 0 {
		for _, stored := range storedSlots {
			slots = append(slots, harnessAuthSlotFromGlobal(stored))
		}
	} else {
		validated := make([]HarnessAuthSlot, 0, len(slots))
		for _, requested := range slots {
			stored, ok := storedByID[strings.TrimSpace(requested.AuthSlotID)]
			if !ok {
				return HarnessAuthStatusResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "auth slot is not configured", map[string]any{"reason": "unknown_auth_slot", "auth_slot_id": strings.TrimSpace(requested.AuthSlotID)})
			}
			validated = append(validated, harnessAuthSlotFromGlobal(stored))
		}
		slots = validated
	}
	if len(slots) == 0 {
		return HarnessAuthStatusResponse{Statuses: []HarnessAuthStatus{}}, nil
	}
	statuses := make([]HarnessAuthStatus, 0, len(slots))
	for _, slot := range slots {
		harness := strings.TrimSpace(slot.Harness)
		factory, ok := d.harnessRegistry.Resolve(harness)
		if !ok {
			status := HarnessAuthStatus{Harness: harness, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthUnknown, AriSecretStorage: HarnessAriSecretStorageNone}
			status.Name = authStatusName(slot, status.Harness)
			statuses = append(statuses, status)
			continue
		}
		executor, err := factory(HarnessSessionStartRequest{Executor: harness}, primaryFolder, d.appendExecutorItems)
		if err != nil {
			status := NewHarnessAuthRequired(harness, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, SecretOwnedBy: harness})
			status.Name = authStatusName(slot, harness)
			statuses = append(statuses, status)
			continue
		}
		statuser, ok := executor.(harnessAuthStatuser)
		if !ok {
			status := HarnessAuthStatus{Harness: harness, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthUnknown, AriSecretStorage: HarnessAriSecretStorageNone}
			status.Name = authStatusName(slot, harness)
			statuses = append(statuses, status)
			continue
		}
		status, err := statuser.AuthStatus(ctx, slot)
		if err != nil {
			var unavailable *HarnessUnavailableError
			if errors.As(err, &unavailable) && unavailable.Reason == "missing_executable" {
				status := HarnessAuthStatus{Harness: harness, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthNotInstalled, AriSecretStorage: HarnessAriSecretStorageNone}
				status.Name = authStatusName(slot, harness)
				if err := storePersistAuthStatus(ctx, store, storedByID, slot.AuthSlotID, status.Status); err != nil {
					return HarnessAuthStatusResponse{}, err
				}
				statuses = append(statuses, status)
				continue
			}
			return HarnessAuthStatusResponse{}, err
		}
		if err := storePersistAuthStatus(ctx, store, storedByID, slot.AuthSlotID, status.Status); err != nil {
			return HarnessAuthStatusResponse{}, err
		}
		if status.Status == HarnessAuthAuthenticated && opencodeNamedSlotMissingProjection(storedByID[slot.AuthSlotID]) {
			status = NewHarnessAuthRequired(harness, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "ari_secret_projection_required", SecretOwnedBy: HarnessNameOpenCode})
		}
		status.Name = authStatusName(slot, status.Harness)
		statuses = append(statuses, status)
	}
	return HarnessAuthStatusResponse{Statuses: statuses}, nil
}

func opencodeNamedSlotMissingProjection(slot globaldb.AuthSlot) bool {
	if strings.TrimSpace(slot.Harness) != HarnessNameOpenCode || authSlotIsDefaultForHarness(HarnessNameOpenCode, slot.AuthSlotID) {
		return false
	}
	_, err := opencodeProjectionSecretID(slot.MetadataJSON)
	return err != nil
}

func authStatusName(slot HarnessAuthSlot, harness string) string {
	if strings.TrimSpace(slot.AuthSlotID) == strings.TrimSpace(harness)+"-default" {
		return "default"
	}
	return strings.TrimSpace(slot.Label)
}

func storePersistAuthStatus(ctx context.Context, store *globaldb.Store, storedByID map[string]globaldb.AuthSlot, authSlotID string, status HarnessAuthState) error {
	if status == "" || status == HarnessAuthUnknown {
		return nil
	}
	stored := storedByID[strings.TrimSpace(authSlotID)]
	stored.Status = string(status)
	return store.UpsertAuthSlot(ctx, stored)
}

func listAuthSlots(ctx context.Context, store *globaldb.Store, req AuthSlotListRequest) (AuthSlotListResponse, error) {
	slots, err := store.ListAuthSlots(ctx, req.Harness)
	if err != nil {
		return AuthSlotListResponse{}, err
	}
	resp := AuthSlotListResponse{Slots: make([]AuthSlotResponse, 0, len(slots))}
	for _, slot := range slots {
		resp.Slots = append(resp.Slots, authSlotResponseFromGlobal(slot))
	}
	return resp, nil
}

func getAuthSlot(ctx context.Context, store *globaldb.Store, req AuthSlotGetRequest) (AuthSlotResponse, error) {
	slot, err := store.GetAuthSlot(ctx, req.AuthSlotID)
	if err != nil {
		return AuthSlotResponse{}, mapWorkspaceStoreError(err, req.AuthSlotID)
	}
	return authSlotResponseFromGlobal(slot), nil
}

func saveAuthSlot(ctx context.Context, store *globaldb.Store, req AuthSlotSaveRequest) (AuthSlotResponse, error) {
	slot := globaldb.AuthSlot{AuthSlotID: strings.TrimSpace(req.AuthSlotID), Harness: strings.TrimSpace(req.Harness), Label: strings.TrimSpace(req.Label), ProviderLabel: strings.TrimSpace(req.ProviderLabel), CredentialOwner: "provider", Status: string(HarnessAuthUnknown), MetadataJSON: "{}"}
	if slot.Label == "" {
		slot.Label = authStatusName(HarnessAuthSlot{AuthSlotID: slot.AuthSlotID}, slot.Harness)
	}
	if err := store.UpsertAuthSlot(ctx, slot); err != nil {
		return AuthSlotResponse{}, mapWorkspaceStoreError(err, slot.AuthSlotID)
	}
	return authSlotResponseFromGlobal(slot), nil
}

func (d *Daemon) removeAuthSlot(ctx context.Context, store *globaldb.Store, req AuthSlotRemoveRequest) (AuthSlotRemoveResponse, error) {
	authSlotID := strings.TrimSpace(req.AuthSlotID)
	if authSlotID == "" {
		return AuthSlotRemoveResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "auth_slot_id is required", map[string]any{"reason": "missing_auth_slot_id"})
	}
	if stored, err := store.GetAuthSlot(ctx, authSlotID); err == nil && stored.Harness == HarnessNameOpenCode {
		if secretID, err := opencodeProjectionSecretID(stored.MetadataJSON); err == nil {
			if err := store.DeleteSecret(ctx, d.secretBackend, secretID); err != nil && !errors.Is(err, globaldb.ErrNotFound) {
				return AuthSlotRemoveResponse{}, mapWorkspaceStoreError(err, secretID)
			}
		}
	}
	if err := store.DeleteAuthSlot(ctx, authSlotID); err != nil {
		return AuthSlotRemoveResponse{}, mapWorkspaceStoreError(err, authSlotID)
	}
	return AuthSlotRemoveResponse{Status: "removed", AuthSlotID: authSlotID}, nil
}

func authSlotResponseFromGlobal(slot globaldb.AuthSlot) AuthSlotResponse {
	return AuthSlotResponse{AuthSlotID: slot.AuthSlotID, Harness: slot.Harness, Label: slot.Label, ProviderLabel: slot.ProviderLabel, CredentialOwner: slot.CredentialOwner, Status: slot.Status}
}

func harnessAuthSlotFromGlobal(slot globaldb.AuthSlot) HarnessAuthSlot {
	return HarnessAuthSlot{AuthSlotID: slot.AuthSlotID, Harness: slot.Harness, Label: slot.Label, ProviderLabel: slot.ProviderLabel, CredentialOwner: HarnessCredentialOwner(slot.CredentialOwner), Status: HarnessAuthState(slot.Status)}
}

func createStoredProfile(ctx context.Context, store *globaldb.Store, req ProfileCreateRequest) (ProfileResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "profile name is required", map[string]any{"reason": "missing_profile_name"})
	}
	if req.InvocationClass != "" && req.InvocationClass != HarnessInvocationSticky && req.InvocationClass != HarnessInvocationEphemeral {
		return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "invocation class is invalid", map[string]any{"reason": "invalid_invocation_class"})
	}
	profileID, err := newAriULID()
	if err != nil {
		return ProfileResponse{}, err
	}
	defaultsJSON := "{}"
	if len(req.Defaults) > 0 {
		encoded, err := json.Marshal(req.Defaults)
		if err != nil {
			return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "profile defaults are invalid", map[string]any{"reason": "invalid_defaults"})
		}
		defaultsJSON = string(encoded)
	}
	authPoolJSON := "{}"
	if len(req.AuthPool.SlotIDs) > 0 || req.AuthPool.Strategy != "" {
		encoded, err := json.Marshal(req.AuthPool)
		if err != nil {
			return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "profile auth pool is invalid", map[string]any{"reason": "invalid_auth_pool"})
		}
		authPoolJSON = string(encoded)
	}
	stored := globaldb.Profile{ProfileID: "ap_" + profileID, WorkspaceID: strings.TrimSpace(req.WorkspaceID), Name: name, Harness: strings.TrimSpace(req.Harness), Model: strings.TrimSpace(req.Model), Prompt: strings.TrimSpace(req.Prompt), AuthSlotID: strings.TrimSpace(req.AuthSlotID), AuthPoolJSON: authPoolJSON, InvocationClass: string(req.InvocationClass), DefaultsJSON: defaultsJSON}
	if err := store.UpsertProfile(ctx, stored); err != nil {
		return ProfileResponse{}, err
	}
	persisted, err := store.GetProfile(ctx, stored.WorkspaceID, stored.Name)
	if err != nil {
		return ProfileResponse{}, err
	}
	return agentProfileResponseFromStore(persisted, req.Defaults), nil
}

func getStoredProfile(ctx context.Context, store *globaldb.Store, req ProfileGetRequest) (ProfileResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "profile name is required", map[string]any{"reason": "missing_profile_name"})
	}
	stored, err := store.GetProfile(ctx, req.WorkspaceID, name)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return ProfileResponse{}, unknownProfileError(name)
		}
		return ProfileResponse{}, err
	}
	defaults := map[string]any{}
	if strings.TrimSpace(stored.DefaultsJSON) != "" {
		_ = json.Unmarshal([]byte(stored.DefaultsJSON), &defaults)
	}
	return agentProfileResponseFromStore(stored, defaults), nil
}

func listStoredProfiles(ctx context.Context, store *globaldb.Store, req ProfileListRequest) (ProfileListResponse, error) {
	stored, err := store.ListProfiles(ctx, req.WorkspaceID)
	if err != nil {
		return ProfileListResponse{}, err
	}
	profiles := make([]ProfileResponse, 0, len(stored))
	for _, profile := range stored {
		defaults := map[string]any{}
		if strings.TrimSpace(profile.DefaultsJSON) != "" {
			_ = json.Unmarshal([]byte(profile.DefaultsJSON), &defaults)
		}
		profiles = append(profiles, agentProfileResponseFromStore(profile, defaults))
	}
	return ProfileListResponse{Profiles: profiles}, nil
}

func agentProfileResponseFromStore(profile globaldb.Profile, defaults map[string]any) ProfileResponse {
	authPool := decodeStoredAuthPool(profile.AuthPoolJSON)
	return ProfileResponse{ProfileID: profile.ProfileID, WorkspaceID: profile.WorkspaceID, Name: profile.Name, Harness: profile.Harness, Model: profile.Model, Prompt: profile.Prompt, AuthSlotID: profile.AuthSlotID, AuthPool: authPool, InvocationClass: HarnessInvocationClass(profile.InvocationClass), Defaults: defaults}
}

func decodeStoredAuthPool(raw string) HarnessAuthPool {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "{}" {
		return HarnessAuthPool{}
	}
	var pool HarnessAuthPool
	_ = json.Unmarshal([]byte(raw), &pool)
	return pool
}

func ensureDefaultHelperProfile(ctx context.Context, store *globaldb.Store, req DefaultHelperEnsureRequest) (ProfileResponse, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", map[string]any{"reason": "missing_workspace_id"})
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = helperPrompt()
	}
	stored, err := store.EnsureDefaultHelperProfile(ctx, workspaceID, req.Harness, prompt)
	if err != nil {
		return ProfileResponse{}, err
	}
	return agentProfileResponseFromStore(stored, map[string]any{}), nil
}

func getDefaultHelperProfile(ctx context.Context, store *globaldb.Store, req DefaultHelperGetRequest) (ProfileResponse, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", map[string]any{"reason": "missing_workspace_id"})
	}
	stored, err := store.GetDefaultHelperProfile(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "default helper profile is not set up for this workspace", map[string]any{"reason": "helper_setup_required", "workspace_id": workspaceID})
		}
		return ProfileResponse{}, err
	}
	return agentProfileResponseFromStore(stored, map[string]any{}), nil
}

func getFinalResponse(ctx context.Context, store *globaldb.Store, req FinalResponseGetRequest) (FinalResponseResponse, error) {
	var stored globaldb.FinalResponse
	var err error
	if strings.TrimSpace(req.FinalResponseID) != "" {
		stored, err = store.GetFinalResponseByID(ctx, req.FinalResponseID)
	} else if strings.TrimSpace(req.SessionID) != "" {
		stored, err = store.GetFinalResponseBySessionID(ctx, req.SessionID)
	} else {
		return FinalResponseResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "final_response_id or session_id is required", map[string]any{"reason": "missing_final_response_ref"})
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
	return FinalResponseResponse{FinalResponseID: stored.FinalResponseID, SessionID: stored.HarnessSessionID, WorkspaceID: stored.WorkspaceID, TaskID: stored.TaskID, ContextPacketID: stored.ContextPacketID, ProfileID: stored.ProfileID, Status: stored.Status, Text: stored.Text, EvidenceLinks: links, CreatedAt: stored.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: updatedAt}
}

func telemetryRollup(ctx context.Context, store *globaldb.Store, req TelemetryRollupRequest) (TelemetryRollupResponse, error) {
	rollups, err := store.RollupHarnessSessionTelemetry(ctx, req.WorkspaceID)
	if err != nil {
		return TelemetryRollupResponse{}, err
	}
	out := make([]TelemetryRollup, 0, len(rollups))
	for _, rollup := range rollups {
		out = append(out, telemetryRollupFromStore(rollup))
	}
	return TelemetryRollupResponse{Rollups: out}, nil
}

func telemetryRollupFromStore(rollup globaldb.HarnessSessionTelemetryRollup) TelemetryRollup {
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

func (d *Daemon) resolveProfileRunRequest(ctx context.Context, store *globaldb.Store, req ProfileRunRequest) (Profile, error) {
	name := strings.TrimSpace(req.Profile)
	if name != "" && req.ProfileDefinition != nil {
		return Profile{}, rpc.NewHandlerError(rpc.InvalidParams, "profile input is ambiguous", map[string]any{"profile": name, "profile_definition": strings.TrimSpace(req.ProfileDefinition.Name), "reason": "ambiguous_profile", "start_invoked": false})
	}
	var profile Profile
	if req.ProfileDefinition != nil {
		profile = *req.ProfileDefinition
		if strings.TrimSpace(profile.Name) == "" {
			profile.Name = name
		}
	} else if name != "" {
		resolved, err := d.resolveProfile(ctx, store, req.Packet.WorkspaceID, name)
		if err != nil {
			return Profile{}, err
		}
		profile = resolved
	} else if executor := strings.TrimSpace(req.Executor); executor != "" {
		profile = Profile{Name: executor, Harness: executor}
	}
	profile = applyHarnessSessionDefaults(profile, req.Defaults)
	if strings.TrimSpace(profile.Harness) == "" {
		return Profile{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is required", map[string]any{"profile": strings.TrimSpace(profile.Name), "reason": "missing_harness", "start_invoked": false})
	}
	return profile, nil
}

func applyHarnessSessionDefaults(profile Profile, defaults HarnessSessionDefaults) Profile {
	if strings.TrimSpace(profile.Harness) == "" {
		profile.Harness = strings.TrimSpace(defaults.Harness)
	}
	if strings.TrimSpace(profile.Model) == "" {
		profile.Model = strings.TrimSpace(defaults.Model)
	}
	if strings.TrimSpace(profile.Prompt) == "" {
		profile.Prompt = strings.TrimSpace(defaults.Prompt)
	}
	if strings.TrimSpace(profile.AuthSlotID) == "" {
		profile.AuthSlotID = strings.TrimSpace(defaults.AuthSlotID)
	}
	if len(profile.AuthPool.SlotIDs) == 0 {
		profile.AuthPool = defaults.AuthPool
	}
	if profile.InvocationClass == "" {
		profile.InvocationClass = defaults.InvocationClass
	}
	if profile.InvocationClass == "" {
		profile.InvocationClass = HarnessInvocationSticky
	}
	profile.Defaults = mergeSettings(profile.Defaults, defaults.Settings)
	return profile
}

func mergeSettings(base, override map[string]any) map[string]any {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	merged := make(map[string]any, len(base)+len(override))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range override {
		merged[key] = value
	}
	return merged
}

func decodeStoredDefaults(raw string) (map[string]any, error) {
	defaults := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &defaults); err != nil {
			return nil, fmt.Errorf("decode profile defaults: %w", err)
		}
	}
	return defaults, nil
}

func (d *Daemon) resolveProfile(ctx context.Context, store *globaldb.Store, workspaceID, name string) (Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Profile{}, rpc.NewHandlerError(rpc.InvalidParams, "profile is required", map[string]any{"reason": "missing_profile", "start_invoked": false})
	}
	if d == nil || d.agentProfiles == nil {
		return resolveStoredProfile(ctx, store, workspaceID, name)
	}
	profile, ok := d.agentProfiles[name]
	if !ok {
		return resolveStoredProfile(ctx, store, workspaceID, name)
	}
	return profile, nil
}

func resolveStoredProfile(ctx context.Context, store *globaldb.Store, workspaceID, name string) (Profile, error) {
	if store == nil {
		return Profile{}, unknownProfileError(name)
	}
	stored, err := store.GetProfile(ctx, workspaceID, name)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return Profile{}, unknownProfileError(name)
		}
		return Profile{}, err
	}
	defaults, err := decodeStoredDefaults(stored.DefaultsJSON)
	if err != nil {
		return Profile{}, err
	}
	return Profile{ProfileID: stored.ProfileID, WorkspaceID: stored.WorkspaceID, Name: stored.Name, Harness: stored.Harness, Model: stored.Model, Prompt: stored.Prompt, AuthSlotID: stored.AuthSlotID, AuthPool: decodeStoredAuthPool(stored.AuthPoolJSON), InvocationClass: HarnessInvocationClass(stored.InvocationClass), Defaults: defaults}, nil
}

func tailRunLogMessages(ctx context.Context, store *globaldb.Store, req RunLogMessagesTailRequest) (RunLogMessagesTailResponse, error) {
	if strings.TrimSpace(req.SessionID) == "" {
		return RunLogMessagesTailResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
	}
	if req.Count <= 0 {
		return RunLogMessagesTailResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "count must be greater than zero", nil)
	}
	messages, err := store.TailRunLogMessages(ctx, req.SessionID, req.Count)
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return RunLogMessagesTailResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return RunLogMessagesTailResponse{}, err
	}
	resp := RunLogMessagesTailResponse{Messages: make([]RunLogMessageResponse, 0, len(messages))}
	for _, msg := range messages {
		resp.Messages = append(resp.Messages, runLogMessageResponse(msg))
	}
	return resp, nil
}

func listRunLogMessages(ctx context.Context, store *globaldb.Store, req RunLogMessagesListRequest) (RunLogMessagesListResponse, error) {
	if strings.TrimSpace(req.SessionID) == "" {
		return RunLogMessagesListResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
	}
	if req.AfterSequence < 0 {
		return RunLogMessagesListResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "after_sequence must be zero or greater", nil)
	}
	if req.Limit <= 0 {
		return RunLogMessagesListResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "limit must be greater than zero", nil)
	}
	messages, err := store.ListRunLogMessages(ctx, req.SessionID, req.AfterSequence, req.Limit)
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return RunLogMessagesListResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return RunLogMessagesListResponse{}, err
	}
	resp := RunLogMessagesListResponse{Messages: make([]RunLogMessageResponse, 0, len(messages))}
	for _, msg := range messages {
		resp.Messages = append(resp.Messages, runLogMessageResponse(msg))
	}
	return resp, nil
}

func createContextExcerptFromTail(ctx context.Context, store *globaldb.Store, req ContextExcerptCreateFromTailRequest) (ContextExcerptResponse, error) {
	excerpt, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: strings.TrimSpace(req.ContextExcerptID), SourceSessionID: strings.TrimSpace(req.SourceSessionID), TargetAgentID: strings.TrimSpace(req.TargetAgentID), Count: req.Count, AppendedMessage: req.AppendedMessage})
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return ContextExcerptResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return ContextExcerptResponse{}, err
	}
	return contextExcerptResponse(excerpt), nil
}

func createContextExcerptFromRange(ctx context.Context, store *globaldb.Store, req ContextExcerptCreateFromRangeRequest) (ContextExcerptResponse, error) {
	excerpt, err := store.CreateContextExcerptFromRange(ctx, globaldb.CreateContextExcerptFromRangeParams{ContextExcerptID: strings.TrimSpace(req.ContextExcerptID), SourceSessionID: strings.TrimSpace(req.SourceSessionID), TargetAgentID: strings.TrimSpace(req.TargetAgentID), StartSequence: req.StartSequence, EndSequence: req.EndSequence, AppendedMessage: req.AppendedMessage})
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return ContextExcerptResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return ContextExcerptResponse{}, err
	}
	return contextExcerptResponse(excerpt), nil
}

func createContextExcerptFromExplicitIDs(ctx context.Context, store *globaldb.Store, req ContextExcerptCreateFromExplicitIDsRequest) (ContextExcerptResponse, error) {
	excerpt, err := store.CreateContextExcerptFromExplicitIDs(ctx, globaldb.CreateContextExcerptFromExplicitIDsParams{ContextExcerptID: strings.TrimSpace(req.ContextExcerptID), SourceSessionID: strings.TrimSpace(req.SourceSessionID), TargetAgentID: strings.TrimSpace(req.TargetAgentID), MessageIDs: req.MessageIDs, AppendedMessage: req.AppendedMessage})
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return ContextExcerptResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return ContextExcerptResponse{}, err
	}
	return contextExcerptResponse(excerpt), nil
}

func getContextExcerpt(ctx context.Context, store *globaldb.Store, req ContextExcerptGetRequest) (ContextExcerptResponse, error) {
	if strings.TrimSpace(req.ContextExcerptID) == "" {
		return ContextExcerptResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "context_excerpt_id is required", nil)
	}
	excerpt, err := store.GetContextExcerpt(ctx, strings.TrimSpace(req.ContextExcerptID))
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return ContextExcerptResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return ContextExcerptResponse{}, err
	}
	return contextExcerptResponse(excerpt), nil
}

func contextExcerptResponse(excerpt globaldb.ContextExcerpt) ContextExcerptResponse {
	resp := ContextExcerptResponse{ContextExcerptID: excerpt.ContextExcerptID, WorkspaceID: excerpt.WorkspaceID, SourceSessionID: excerpt.SourceSessionID, SourceAgentID: excerpt.SourceAgentID, TargetAgentID: excerpt.TargetAgentID, TargetSessionID: excerpt.TargetSessionID, SelectorType: excerpt.SelectorType, SelectorJSON: excerpt.SelectorJSON, Visibility: excerpt.Visibility, AppendedMessage: excerpt.AppendedMessage, ContentHash: excerpt.ContentHash, Items: make([]ContextExcerptItemResponse, 0, len(excerpt.Items))}
	for _, item := range excerpt.Items {
		partResponses := make([]RunLogMessagePartResponse, 0, len(item.CopiedParts))
		for _, part := range item.CopiedParts {
			partResponses = append(partResponses, runLogMessagePartResponse(part))
		}
		resp.Items = append(resp.Items, ContextExcerptItemResponse{Sequence: item.Sequence, SourceMessageID: item.SourceMessageID, CopiedRole: item.CopiedRole, CopiedText: item.CopiedText, CopiedParts: partResponses})
	}
	return resp
}

func runLogMessagePartResponse(part globaldb.RunLogMessagePart) RunLogMessagePartResponse {
	return RunLogMessagePartResponse{PartID: part.PartID, Sequence: part.Sequence, Kind: part.Kind, Text: part.Text, MimeType: part.MimeType, URI: part.URI, Name: part.Name, ToolName: part.ToolName, ToolCallID: part.ToolCallID, RawJSON: part.RawJSON}
}

func runLogMessageResponse(msg globaldb.RunLogMessage) RunLogMessageResponse {
	resp := RunLogMessageResponse{MessageID: msg.MessageID, SessionID: msg.SessionID, AgentID: msg.AgentID, Sequence: msg.Sequence, Role: msg.Role, Status: msg.Status, ProviderMessageID: msg.ProviderMessageID, ProviderItemID: msg.ProviderItemID, ProviderTurnID: msg.ProviderTurnID, ProviderResponseID: msg.ProviderResponseID, ProviderCallID: msg.ProviderCallID, ProviderChannel: msg.ProviderChannel, ProviderKind: msg.ProviderKind, RawMetadataJSON: msg.RawMetadataJSON, Parts: make([]RunLogMessagePartResponse, 0, len(msg.Parts))}
	for _, part := range msg.Parts {
		resp.Parts = append(resp.Parts, runLogMessagePartResponse(part))
	}
	return resp
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

func sendAgentMessage(ctx context.Context, store *globaldb.Store, req AgentMessageSendRequest) (AgentMessageSendResponse, error) {
	agentMessageID := strings.TrimSpace(req.AgentMessageID)
	if agentMessageID == "" {
		generated, err := newAriULID()
		if err != nil {
			return AgentMessageSendResponse{}, err
		}
		agentMessageID = "am_" + generated
	}
	sourceSessionID := strings.TrimSpace(req.SourceSessionID)
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	body := strings.TrimSpace(req.Body)
	targetAgentID := strings.TrimSpace(req.TargetAgentID)
	targetSessionID := strings.TrimSpace(req.TargetSessionID)
	startSessionID := strings.TrimSpace(req.StartSessionID)
	contextExcerptIDs := trimNonEmptyStrings(req.ContextExcerptIDs)
	effectiveTargetSessionID := targetSessionID
	if effectiveTargetSessionID == "" {
		effectiveTargetSessionID = startSessionID
	}
	if sourceSessionID == "" || body == "" || (targetAgentID == "" && targetSessionID == "" && startSessionID == "") {
		missingField := ""
		switch {
		case sourceSessionID == "":
			missingField = "source_session_id"
		case body == "":
			missingField = "body"
		default:
			missingField = "target_agent_id_or_target_session_id"
		}
		return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": missingField, "start_invoked": false})
	}
	if workspaceID != "" {
		sourceRun, sourceErr := store.GetHarnessSession(ctx, sourceSessionID)
		if sourceErr != nil {
			if errors.Is(sourceErr, globaldb.ErrNotFound) {
				return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, sourceErr.Error(), map[string]any{"reason": "unknown_source_session", "source_session_id": sourceSessionID, "workspace_id": workspaceID, "start_invoked": false})
			}
			return AgentMessageSendResponse{}, sourceErr
		}
		if strings.TrimSpace(sourceRun.WorkspaceID) != workspaceID {
			return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "source_workspace_mismatch", "source_session_id": sourceSessionID, "source_workspace_id": sourceRun.WorkspaceID, "workspace_id": workspaceID, "start_invoked": false})
		}
	}
	dm, err := store.SendAgentMessage(ctx, globaldb.AgentMessageSendParams{AgentMessageID: agentMessageID, SourceSessionID: sourceSessionID, TargetAgentID: targetAgentID, TargetSessionID: targetSessionID, Body: body, ContextExcerptIDs: contextExcerptIDs, StartSessionID: startSessionID, WorkspaceEvent: &globaldb.WorkspaceEvent{EventType: workspaceEventMessageSent, SubjectType: "agent_message", SubjectID: agentMessageID, ProducerType: workspaceEventProducerSession, ProducerID: sourceSessionID, PayloadJSON: daemonEventPayload(map[string]string{"source_session_id": sourceSessionID, "target_agent_id": targetAgentID, "target_session_id": effectiveTargetSessionID})}})
	if err != nil {
		errText := strings.ToLower(err.Error())
		if strings.Contains(errText, "unique constraint failed") && strings.Contains(errText, "agent_messages.agent_message_id") {
			return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "agent_message_id_conflict", "agent_message_id": agentMessageID, "start_invoked": false})
		}
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			resolvedTargetAgentID := targetAgentID
			if resolvedTargetAgentID == "" && effectiveTargetSessionID != "" {
				if targetRun, targetErr := store.GetHarnessSession(ctx, effectiveTargetSessionID); targetErr == nil {
					resolvedTargetAgentID = strings.TrimSpace(targetRun.AgentID)
				}
			}
			if errors.Is(err, globaldb.ErrNotFound) && len(contextExcerptIDs) > 0 {
				contextExcerptID := contextExcerptIDs[0]
				if contextExcerptID != "" {
					if _, excerptErr := store.GetContextExcerpt(ctx, contextExcerptID); errors.Is(excerptErr, globaldb.ErrNotFound) {
						return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_context_excerpt", "context_excerpt_id": contextExcerptID, "start_invoked": false})
					}
				}
			}
			if errors.Is(err, globaldb.ErrInvalidInput) && len(contextExcerptIDs) > 0 {
				contextExcerptID := contextExcerptIDs[0]
				if contextExcerptID != "" {
					if excerpt, excerptErr := store.GetContextExcerpt(ctx, contextExcerptID); excerptErr == nil {
						if excerpt.TargetAgentID != "" && resolvedTargetAgentID != "" && excerpt.TargetAgentID != resolvedTargetAgentID {
							return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "context_excerpt_mismatch", "context_excerpt_id": contextExcerptID, "target_session_id": effectiveTargetSessionID, "target_agent_id": resolvedTargetAgentID, "start_invoked": false})
						}
					}
				}
			}
			if errors.Is(err, globaldb.ErrInvalidInput) && effectiveTargetSessionID != "" && targetAgentID != "" {
				if targetRun, targetErr := store.GetHarnessSession(ctx, effectiveTargetSessionID); targetErr == nil {
					if strings.TrimSpace(targetRun.AgentID) != "" && targetRun.AgentID != targetAgentID {
						return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "target_session_mismatch", "target_session_id": effectiveTargetSessionID, "target_agent_id": targetAgentID, "start_invoked": false})
					}
				}
			}
			if errors.Is(err, globaldb.ErrInvalidInput) && effectiveTargetSessionID != "" {
				if sourceRun, sourceErr := store.GetHarnessSession(ctx, sourceSessionID); sourceErr == nil {
					if targetRun, targetErr := store.GetHarnessSession(ctx, effectiveTargetSessionID); targetErr == nil {
						if strings.TrimSpace(targetRun.WorkspaceID) != "" && targetRun.WorkspaceID != sourceRun.WorkspaceID {
							return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "target_workspace_mismatch", "target_session_id": effectiveTargetSessionID, "source_workspace_id": sourceRun.WorkspaceID, "target_workspace_id": targetRun.WorkspaceID, "start_invoked": false})
						}
					}
				}
			}
			if errors.Is(err, globaldb.ErrInvalidInput) && targetAgentID != "" {
				if sourceRun, sourceErr := store.GetHarnessSession(ctx, sourceSessionID); sourceErr == nil {
					if targetCfg, targetErr := store.GetHarnessSessionConfig(ctx, targetAgentID); targetErr == nil {
						if strings.TrimSpace(targetCfg.WorkspaceID) != "" && targetCfg.WorkspaceID != sourceRun.WorkspaceID {
							return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "target_workspace_mismatch", "target_agent_id": targetAgentID, "source_workspace_id": sourceRun.WorkspaceID, "target_workspace_id": targetCfg.WorkspaceID, "start_invoked": false})
						}
					}
				}
			}
			if errors.Is(err, globaldb.ErrNotFound) {
				if sourceSessionID != "" {
					if _, sourceErr := store.GetHarnessSession(ctx, sourceSessionID); errors.Is(sourceErr, globaldb.ErrNotFound) {
						return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_source_session", "source_session_id": sourceSessionID, "start_invoked": false})
					}
				}
				if effectiveTargetSessionID != "" && (targetSessionID != "" || targetAgentID == "") {
					if _, targetErr := store.GetHarnessSession(ctx, effectiveTargetSessionID); errors.Is(targetErr, globaldb.ErrNotFound) {
						return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_target_session", "target_session_id": effectiveTargetSessionID, "start_invoked": false})
					}
				}
				if targetAgentID != "" {
					return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_target_agent", "target_agent_id": targetAgentID, "start_invoked": false})
				}
			}
			return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "invalid_session_message", "agent_message_id": agentMessageID, "source_session_id": sourceSessionID, "target_session_id": effectiveTargetSessionID, "target_agent_id": targetAgentID, "start_invoked": false})
		}
		return AgentMessageSendResponse{}, err
	}
	return AgentMessageSendResponse{AgentMessage: AgentMessageResponse{AgentMessageID: dm.AgentMessageID, WorkspaceID: dm.WorkspaceID, SourceAgentID: dm.SourceAgentID, SourceSessionID: dm.SourceSessionID, TargetAgentID: dm.TargetAgentID, TargetSessionID: dm.TargetSessionID, Body: dm.Body, Status: dm.Status, DeliveredSessionID: dm.DeliveredSessionID, ContextExcerptIDs: dm.ContextExcerptIDs}}, nil
}

func trimNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func agentMessageResponse(dm globaldb.AgentMessage) AgentMessageResponse {
	return AgentMessageResponse{AgentMessageID: dm.AgentMessageID, WorkspaceID: dm.WorkspaceID, SourceAgentID: dm.SourceAgentID, SourceSessionID: dm.SourceSessionID, TargetAgentID: dm.TargetAgentID, TargetSessionID: dm.TargetSessionID, Body: dm.Body, Status: dm.Status, DeliveredSessionID: dm.DeliveredSessionID, ContextExcerptIDs: dm.ContextExcerptIDs}
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
	executor, err := d.resolveHarness(req, primaryFolder)
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
	if run.Status == "running" {
		_, cancel := context.WithCancel(context.Background())
		d.registerActiveHarnessRun(run.WorkspaceID, run.HarnessSessionID, run.ProviderSessionID, executor, cancel)
	}
	d.recordExecutorRun(run, items)
	if err := newHarnessLifecycle(store).persistNewStickyResult(ctx, result, primaryFolder, profile...); err != nil {
		return HarnessSessionStartResponse{}, err
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
	providerMetadata, err := json.Marshal(map[string]any{"session_ref": result.SessionRef, "provider_session_id": run.ProviderSessionID, "capabilities": run.Capabilities, "invocation_mode": run.InvocationMode, "usage_bucket": run.UsageBucket})
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

func resolveProfileAuthSlot(ctx context.Context, store *globaldb.Store, executor Executor, harness string, profile Profile) (string, error) {
	if strings.TrimSpace(profile.AuthSlotID) == "" && len(profile.AuthPool.SlotIDs) == 0 {
		return "", nil
	}
	statuser, ok := executor.(harnessAuthStatuser)
	if !ok {
		return "", &HarnessUnavailableError{Harness: harness, Reason: "auth_slot_selection_unsupported", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}
	slotIDs := []string{}
	if strings.TrimSpace(profile.AuthSlotID) != "" {
		slotIDs = append(slotIDs, strings.TrimSpace(profile.AuthSlotID))
	} else {
		for _, slotID := range profile.AuthPool.SlotIDs {
			if strings.TrimSpace(slotID) != "" {
				slotIDs = append(slotIDs, strings.TrimSpace(slotID))
			}
		}
	}
	slots := make([]HarnessAuthSlot, 0, len(slotIDs))
	isPoolSelection := strings.TrimSpace(profile.AuthSlotID) == ""
	for _, slotID := range slotIDs {
		stored, err := store.GetAuthSlot(ctx, slotID)
		if err != nil {
			if errors.Is(err, globaldb.ErrNotFound) {
				if isPoolSelection {
					continue
				}
				return "", &HarnessUnavailableError{Harness: harness, Reason: "unknown_auth_slot", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
			}
			return "", err
		}
		slot := harnessAuthSlotFromGlobal(stored)
		if strings.TrimSpace(slot.Harness) != strings.TrimSpace(harness) {
			return "", &HarnessUnavailableError{Harness: harness, Reason: "auth_slot_harness_mismatch", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
		}
		status, err := statuser.AuthStatus(ctx, slot)
		if err != nil {
			return "", err
		}
		slot.Status = status.Status
		slots = append(slots, slot)
	}
	selected, _, err := ResolveHarnessAuthSlot(HarnessAuthSelection{ProfileSlotID: profile.AuthSlotID, ProfilePool: profile.AuthPool, Harness: harness}, slots)
	if err != nil {
		return "", &HarnessUnavailableError{Harness: harness, Reason: "auth_slot_not_ready", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}
	return selected.AuthSlotID, nil
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

func isUnknownProfileError(err error) bool {
	handlerErr := &rpc.HandlerError{}
	if !errors.As(err, &handlerErr) {
		return false
	}
	data, ok := handlerErr.Data.(map[string]any)
	if !ok {
		return false
	}
	reason, _ := data["reason"].(string)
	return reason == "unknown_profile"
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
		options, err := harnessOptionsFromProfile(profile[0])
		if err != nil {
			return HarnessCallResult{}, err
		}
		call.Options = options
	}
	call.Input = json.RawMessage(renderContextPacket(packet))
	call.AuthProjection = projection
	return StartHarnessCallResult(ctx, executor, call)
}

func authSlotIDFromProfiles(profile ...Profile) string {
	if len(profile) == 0 {
		return ""
	}
	return strings.TrimSpace(profile[0].AuthSlotID)
}

func (d *Daemon) authProjectionForStart(ctx context.Context, store *globaldb.Store, harness, workspaceID, authSlotID string) (HarnessAuthProjectionPlan, error) {
	harness = strings.TrimSpace(harness)
	authSlotID = strings.TrimSpace(authSlotID)
	if harness != HarnessNameOpenCode || authSlotIsDefaultForHarness(HarnessNameOpenCode, authSlotID) {
		return HarnessAuthProjectionPlan{}, nil
	}
	slot, err := store.GetAuthSlot(ctx, authSlotID)
	if err != nil {
		return HarnessAuthProjectionPlan{}, err
	}
	secretID, err := opencodeProjectionSecretID(slot.MetadataJSON)
	if err != nil {
		return HarnessAuthProjectionPlan{}, err
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return HarnessAuthProjectionPlan{}, fmt.Errorf("%w: workspace_id is required for opencode secret projection", globaldb.ErrPermissionDenied)
	}
	value, err := store.ProjectSecretWithGrant(ctx, d.secretBackend, secretID, globaldb.SecretGrantSubjectWorkspace, workspaceID)
	if err != nil {
		return HarnessAuthProjectionPlan{}, err
	}
	return HarnessAuthProjectionPlan{Owner: HarnessAuthProjectionOwnerAri, Kind: HarnessAuthProjectionAuthContent, Env: map[string]string{"OPENCODE_AUTH_CONTENT": string(value)}, RiskLabels: []string{"provider_owned", "provider_hint_matching", "ari_projected_auth_content", "env_projection_downgrade_risk"}}, nil
}

func opencodeProjectionSecretID(metadataJSON string) (string, error) {
	var metadata map[string]string
	if err := json.Unmarshal([]byte(defaultString(strings.TrimSpace(metadataJSON), "{}")), &metadata); err != nil {
		return "", fmt.Errorf("%w: auth slot metadata json is invalid", globaldb.ErrInvalidInput)
	}
	secretID := strings.TrimSpace(metadata["projection_ref"])
	if secretID == "" {
		return "", fmt.Errorf("%w: opencode auth slot projection_ref is required", globaldb.ErrInvalidInput)
	}
	return secretID, nil
}

func storeFinalResponse(ctx context.Context, store *globaldb.Store, result HarnessCallResult, profile ...Profile) (string, error) {
	responseID, err := newAriULID()
	if err != nil {
		return "", err
	}
	finalResponseID := "fr_" + responseID
	profileID := ""
	if len(profile) > 0 {
		profileID = strings.TrimSpace(profile[0].ProfileID)
	}
	links := []FinalResponseEvidenceLink{{Kind: "context_packet", ID: result.HarnessSession.ContextPacketID}, {Kind: "harness_session", ID: result.HarnessSession.HarnessSessionID}}
	for _, item := range result.Items {
		if strings.TrimSpace(item.ID) != "" {
			links = append(links, FinalResponseEvidenceLink{Kind: "timeline_item", ID: item.ID})
		}
	}
	encodedLinks, err := json.Marshal(links)
	if err != nil {
		return "", err
	}
	if err := store.UpsertFinalResponse(ctx, globaldb.FinalResponse{FinalResponseID: finalResponseID, HarnessSessionID: result.HarnessSession.HarnessSessionID, WorkspaceID: result.HarnessSession.WorkspaceID, TaskID: result.HarnessSession.TaskID, ContextPacketID: result.HarnessSession.ContextPacketID, ProfileID: profileID, Status: result.FinalResponse.Status, Text: result.FinalResponse.Text, EvidenceLinksJSON: string(encodedLinks)}); err != nil {
		return "", err
	}
	return finalResponseID, nil
}

func storeHarnessSessionTelemetry(ctx context.Context, store *globaldb.Store, result HarnessCallResult, sample ProcessMetricsSample, profile ...Profile) error {
	profileID := ""
	profileName := ""
	invocationClass := string(HarnessInvocationSticky)
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
	durationMS, durationKnown := agentSessionDurationMS(result.HarnessSession)
	return store.UpsertHarnessSessionTelemetry(ctx, globaldb.HarnessSessionTelemetry{HarnessSessionID: result.HarnessSession.HarnessSessionID, WorkspaceID: result.HarnessSession.WorkspaceID, TaskID: result.HarnessSession.TaskID, ProfileID: profileID, ProfileName: profileName, Harness: result.HarnessSession.Executor, Model: model, InvocationClass: invocationClass, Status: result.HarnessSession.Status, InputTokensKnown: result.Telemetry.InputTokens != nil, InputTokens: result.Telemetry.InputTokens, OutputTokensKnown: result.Telemetry.OutputTokens != nil, OutputTokens: result.Telemetry.OutputTokens, DurationMSKnown: durationKnown, DurationMS: durationMS, OwnedByAri: sample.OwnedByAri, PIDKnown: sample.PID.Known, PID: sample.PID.Value, CPUTimeMSKnown: sample.CPUTimeMS.Known, CPUTimeMS: sample.CPUTimeMS.Value, MemoryRSSBytesPeakKnown: sample.MemoryRSSBytesPeak.Known, MemoryRSSBytesPeak: sample.MemoryRSSBytesPeak.Value, ChildProcessesPeakKnown: sample.ChildProcessesPeak.Known, ChildProcessesPeak: sample.ChildProcessesPeak.Value, PortsJSON: portsJSON, OrphanState: sample.OrphanState, ExitCodeKnown: sample.ExitCode.Known, ExitCode: sample.ExitCode.Value})
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
