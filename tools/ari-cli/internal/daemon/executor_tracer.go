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

type AgentSessionStartRequest struct {
	Executor string        `json:"executor"`
	Packet   ContextPacket `json:"packet"`
	Command  string        `json:"command,omitempty"`
	Args     []string      `json:"args,omitempty"`
}

type AgentSessionStartResponse struct {
	Run   AgentSession   `json:"run"`
	Items []TimelineItem `json:"items"`
}

type AgentSession struct {
	AgentSessionID    string                `json:"agent_session_id"`
	SessionID         string                `json:"session_id,omitempty"`
	WorkspaceID       string                `json:"workspace_id"`
	TaskID            string                `json:"task_id"`
	Executor          string                `json:"executor"`
	ProviderSessionID string                `json:"provider_session_id"`
	ProviderRunID     string                `json:"provider_run_id,omitempty"`
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

type HarnessFactory func(AgentSessionStartRequest, string, func(string, []TimelineItem)) (Executor, error)

type AgentProfile struct {
	ProfileID       string                 `json:"profile_id,omitempty"`
	Name            string                 `json:"name"`
	Harness         string                 `json:"harness"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	AuthSlotID      string                 `json:"auth_slot_id,omitempty"`
	AuthPool        HarnessAuthPool        `json:"auth_pool,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class"`
}

type AgentSessionDefaults struct {
	Harness         string                 `json:"harness,omitempty"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	AuthSlotID      string                 `json:"auth_slot_id,omitempty"`
	AuthPool        HarnessAuthPool        `json:"auth_pool,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class,omitempty"`
}

type AgentProfileRunRequest struct {
	Profile           string               `json:"profile,omitempty"`
	ProfileDefinition *AgentProfile        `json:"profile_definition,omitempty"`
	Defaults          AgentSessionDefaults `json:"defaults,omitempty"`
	Packet            ContextPacket        `json:"packet"`
}

type AgentProfileRunResponse struct {
	Profile string         `json:"profile"`
	Harness string         `json:"harness"`
	Run     AgentSession   `json:"run"`
	Items   []TimelineItem `json:"items"`
}

type AgentProfileCreateRequest struct {
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

type AgentSessionConfigCreateRequest struct {
	AgentID     string `json:"agent_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Name        string `json:"name"`
	Harness     string `json:"harness,omitempty"`
	Model       string `json:"model,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

type AgentSessionConfigCreateResponse struct {
	Agent AgentSessionConfigResponse `json:"agent"`
}

type AgentSessionConfigListRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type AgentSessionConfigListResponse struct {
	Agents []AgentSessionConfigResponse `json:"agents"`
}

type AgentSessionConfigUpdateRequest struct {
	AgentID     string `json:"agent_id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Harness     string `json:"harness,omitempty"`
	Model       string `json:"model,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

type AgentSessionConfigDeleteRequest struct {
	AgentID string `json:"agent_id"`
}

type AgentSessionConfigDeleteResponse struct {
	Deleted bool `json:"deleted"`
}

type AgentSessionConfigSessionRequest struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
	CWD       string `json:"cwd,omitempty"`
}

type AgentSessionConfigSessionResponse struct {
	Run globaldb.AgentSession `json:"run"`
}

type AgentSessionConfigResponse struct {
	AgentID     string `json:"agent_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Name        string `json:"name"`
	Harness     string `json:"harness,omitempty"`
	Model       string `json:"model,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

type AgentMessageSendRequest struct {
	AgentMessageID    string   `json:"agent_message_id"`
	SourceSessionID   string   `json:"source_session_id"`
	TargetAgentID     string   `json:"target_agent_id"`
	TargetSessionID   string   `json:"target_session_id,omitempty"`
	Body              string   `json:"body"`
	ContextExcerptIDs []string `json:"context_excerpt_ids,omitempty"`
	StartSessionID    string   `json:"start_session_id,omitempty"`
}

type AgentMessageSendResponse struct {
	AgentMessage AgentMessageResponse `json:"agent_message"`
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

type EphemeralAgentCallRequest struct {
	CallID              string   `json:"call_id"`
	SourceSessionID     string   `json:"source_session_id"`
	TargetAgentID       string   `json:"target_agent_id"`
	Body                string   `json:"body"`
	ContextExcerptIDs   []string `json:"context_excerpt_ids,omitempty"`
	SessionID           string   `json:"session_id,omitempty"`
	ReplyAgentMessageID string   `json:"reply_agent_message_id,omitempty"`
}

type EphemeralAgentCallResponse struct {
	Run     globaldb.AgentSession `json:"run"`
	Request AgentMessageResponse  `json:"request"`
	Reply   AgentMessageResponse  `json:"reply"`
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

type AgentProfileResponse struct {
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

func defaultAgentProfiles() map[string]AgentProfile {
	return make(map[string]AgentProfile)
}

var agentSessionProcessMetricsSampler = func(ctx context.Context, run AgentSession) ProcessMetricsSample {
	return sampleLinuxProcessMetrics(ctx, run)
}

func unknownProcessMetric(confidence string) ProcessMetricValue {
	return ProcessMetricValue{Known: false, Confidence: strings.TrimSpace(confidence)}
}

func (d *Daemon) resolveHarness(req AgentSessionStartRequest, primaryFolder string) (Executor, error) {
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
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentSessionStartRequest, AgentSessionStartResponse]{
		Name:        "agent.run",
		Description: "Start an executor-backed agent run from a context packet",
		Handler: func(ctx context.Context, req AgentSessionStartRequest) (AgentSessionStartResponse, error) {
			return d.startAgentSession(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register agent.run: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentProfileRunRequest, AgentProfileRunResponse]{
		Name:        "profile.run",
		Description: "Start an agent run from a named Ari profile",
		Handler: func(ctx context.Context, req AgentProfileRunRequest) (AgentProfileRunResponse, error) {
			profile, err := d.resolveAgentProfileRunRequest(ctx, store, req)
			if err != nil {
				return AgentProfileRunResponse{}, err
			}
			resp, err := d.startAgentSession(ctx, store, AgentSessionStartRequest{Executor: profile.Harness, Packet: req.Packet}, profile)
			if err != nil {
				return AgentProfileRunResponse{}, err
			}
			return AgentProfileRunResponse{Profile: profile.Name, Harness: profile.Harness, Run: resp.Run, Items: resp.Items}, nil
		},
	}); err != nil {
		return fmt.Errorf("register profile.run: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentProfileCreateRequest, AgentProfileResponse]{
		Name:        "profile.create",
		Description: "Create or update a durable Ari profile",
		Handler: func(ctx context.Context, req AgentProfileCreateRequest) (AgentProfileResponse, error) {
			return createStoredAgentProfile(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register profile.create: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentProfileGetRequest, AgentProfileResponse]{
		Name:        "profile.get",
		Description: "Get a durable Ari profile by name",
		Handler: func(ctx context.Context, req AgentProfileGetRequest) (AgentProfileResponse, error) {
			return getStoredAgentProfile(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register profile.get: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentProfileListRequest, AgentProfileListResponse]{
		Name:        "profile.list",
		Description: "List durable Ari profiles",
		Handler: func(ctx context.Context, req AgentProfileListRequest) (AgentProfileListResponse, error) {
			return listStoredAgentProfiles(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register profile.list: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[RunLogMessagesTailRequest, RunLogMessagesTailResponse]{
		Name:        "run.messages.tail",
		Description: "Select the last N normalized messages from a run in deterministic run order",
		Handler: func(ctx context.Context, req RunLogMessagesTailRequest) (RunLogMessagesTailResponse, error) {
			return tailRunLogMessages(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register run.messages.tail: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[RunLogMessagesListRequest, RunLogMessagesListResponse]{
		Name:        "run.messages.list",
		Description: "List normalized run messages after a sequence cursor with a limit",
		Handler: func(ctx context.Context, req RunLogMessagesListRequest) (RunLogMessagesListResponse, error) {
			return listRunLogMessages(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register run.messages.list: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[ContextExcerptCreateFromTailRequest, ContextExcerptResponse]{
		Name:        "context.excerpt.create_from_tail",
		Description: "Create an immutable visible context excerpt from the last N run messages",
		Handler: func(ctx context.Context, req ContextExcerptCreateFromTailRequest) (ContextExcerptResponse, error) {
			return createContextExcerptFromTail(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register context.excerpt.create_from_tail: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[ContextExcerptCreateFromRangeRequest, ContextExcerptResponse]{
		Name:        "context.excerpt.create_from_range",
		Description: "Create an immutable visible context excerpt from an inclusive run message sequence range",
		Handler: func(ctx context.Context, req ContextExcerptCreateFromRangeRequest) (ContextExcerptResponse, error) {
			return createContextExcerptFromRange(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register context.excerpt.create_from_range: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[ContextExcerptCreateFromExplicitIDsRequest, ContextExcerptResponse]{
		Name:        "context.excerpt.create_from_explicit_ids",
		Description: "Create an immutable visible context excerpt from explicit run message IDs",
		Handler: func(ctx context.Context, req ContextExcerptCreateFromExplicitIDsRequest) (ContextExcerptResponse, error) {
			return createContextExcerptFromExplicitIDs(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register context.excerpt.create_from_explicit_ids: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[ContextExcerptGetRequest, ContextExcerptResponse]{
		Name:        "context.excerpt.get",
		Description: "Get an immutable context excerpt and copied message items",
		Handler: func(ctx context.Context, req ContextExcerptGetRequest) (ContextExcerptResponse, error) {
			return getContextExcerpt(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register context.excerpt.get: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentSessionConfigCreateRequest, AgentSessionConfigCreateResponse]{
		Name:        "workspace.agent.create",
		Description: "Create a durable workspace agent without starting a run",
		Handler: func(ctx context.Context, req AgentSessionConfigCreateRequest) (AgentSessionConfigCreateResponse, error) {
			return createAgentSessionConfig(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register workspace.agent.create: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentSessionConfigListRequest, AgentSessionConfigListResponse]{Name: "workspace.agent.list", Description: "List durable workspace agents", Handler: func(ctx context.Context, req AgentSessionConfigListRequest) (AgentSessionConfigListResponse, error) {
		return listAgentSessionConfigs(ctx, store, req)
	}}); err != nil {
		return fmt.Errorf("register workspace.agent.list: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentSessionConfigUpdateRequest, AgentSessionConfigResponse]{Name: "workspace.agent.update", Description: "Update a durable workspace agent", Handler: func(ctx context.Context, req AgentSessionConfigUpdateRequest) (AgentSessionConfigResponse, error) {
		return updateAgentSessionConfig(ctx, store, req)
	}}); err != nil {
		return fmt.Errorf("register workspace.agent.update: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentSessionConfigDeleteRequest, AgentSessionConfigDeleteResponse]{Name: "workspace.agent.delete", Description: "Delete a durable workspace agent", Handler: func(ctx context.Context, req AgentSessionConfigDeleteRequest) (AgentSessionConfigDeleteResponse, error) {
		return deleteAgentSessionConfig(ctx, store, req)
	}}); err != nil {
		return fmt.Errorf("register workspace.agent.delete: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentSessionConfigSessionRequest, AgentSessionConfigSessionResponse]{Name: "workspace.agent.run", Description: "Create a run from a workspace agent", Handler: func(ctx context.Context, req AgentSessionConfigSessionRequest) (AgentSessionConfigSessionResponse, error) {
		return runAgentSessionConfig(ctx, store, req)
	}}); err != nil {
		return fmt.Errorf("register workspace.agent.run: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentMessageSendRequest, AgentMessageSendResponse]{
		Name:        "agent.message.send",
		Description: "Send a visible agent message between workspace agents",
		Handler: func(ctx context.Context, req AgentMessageSendRequest) (AgentMessageSendResponse, error) {
			return sendAgentMessage(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register agent.message.send: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[EphemeralAgentCallRequest, EphemeralAgentCallResponse]{
		Name:        "agent.call.ephemeral",
		Description: "Run a task-scoped target agent and route its response back to the caller",
		Handler: func(ctx context.Context, req EphemeralAgentCallRequest) (EphemeralAgentCallResponse, error) {
			return d.callEphemeralAgent(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register agent.call.ephemeral: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[DefaultHelperEnsureRequest, AgentProfileResponse]{
		Name:        "profile.helper.ensure",
		Description: "Ensure a workspace default helper profile exists",
		Handler: func(ctx context.Context, req DefaultHelperEnsureRequest) (AgentProfileResponse, error) {
			return ensureDefaultHelperProfile(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register profile.helper.ensure: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[DefaultHelperGetRequest, AgentProfileResponse]{
		Name:        "profile.helper.get",
		Description: "Get a workspace default helper profile",
		Handler: func(ctx context.Context, req DefaultHelperGetRequest) (AgentProfileResponse, error) {
			return getDefaultHelperProfile(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register profile.helper.get: %w", err)
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
	if err := rpc.RegisterMethod(registry, rpc.Method[HarnessAuthStartRequest, HarnessAuthStartResponse]{
		Name:        "auth.start",
		Description: "Start a provider-owned auth flow for a configured auth slot without handling secrets",
		Handler: func(ctx context.Context, req HarnessAuthStartRequest) (HarnessAuthStartResponse, error) {
			return d.harnessAuthStart(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register auth.start: %w", err)
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
	return nil
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
	executor, err := factory(AgentSessionStartRequest{Executor: slot.Harness}, primaryFolder, d.appendExecutorItems)
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
	executor, err := factory(AgentSessionStartRequest{Executor: slot.Harness}, primaryFolder, d.appendExecutorItems)
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
	executor, err := factory(AgentSessionStartRequest{Executor: slot.Harness}, primaryFolder, d.appendExecutorItems)
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
		executor, err := factory(AgentSessionStartRequest{Executor: harness}, primaryFolder, d.appendExecutorItems)
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
		status.Name = authStatusName(slot, status.Harness)
		statuses = append(statuses, status)
	}
	return HarnessAuthStatusResponse{Statuses: statuses}, nil
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

func authSlotResponseFromGlobal(slot globaldb.AuthSlot) AuthSlotResponse {
	return AuthSlotResponse{AuthSlotID: slot.AuthSlotID, Harness: slot.Harness, Label: slot.Label, ProviderLabel: slot.ProviderLabel, CredentialOwner: slot.CredentialOwner, Status: slot.Status}
}

func harnessAuthSlotFromGlobal(slot globaldb.AuthSlot) HarnessAuthSlot {
	return HarnessAuthSlot{AuthSlotID: slot.AuthSlotID, Harness: slot.Harness, Label: slot.Label, ProviderLabel: slot.ProviderLabel, CredentialOwner: HarnessCredentialOwner(slot.CredentialOwner), Status: HarnessAuthState(slot.Status)}
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
	authPoolJSON := "{}"
	if len(req.AuthPool.SlotIDs) > 0 || req.AuthPool.Strategy != "" {
		encoded, err := json.Marshal(req.AuthPool)
		if err != nil {
			return AgentProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "profile auth pool is invalid", map[string]any{"reason": "invalid_auth_pool"})
		}
		authPoolJSON = string(encoded)
	}
	stored := globaldb.AgentProfile{ProfileID: "ap_" + profileID, WorkspaceID: strings.TrimSpace(req.WorkspaceID), Name: name, Harness: strings.TrimSpace(req.Harness), Model: strings.TrimSpace(req.Model), Prompt: strings.TrimSpace(req.Prompt), AuthSlotID: strings.TrimSpace(req.AuthSlotID), AuthPoolJSON: authPoolJSON, InvocationClass: string(req.InvocationClass), DefaultsJSON: defaultsJSON}
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
	authPool := decodeStoredAuthPool(profile.AuthPoolJSON)
	return AgentProfileResponse{ProfileID: profile.ProfileID, WorkspaceID: profile.WorkspaceID, Name: profile.Name, Harness: profile.Harness, Model: profile.Model, Prompt: profile.Prompt, AuthSlotID: profile.AuthSlotID, AuthPool: authPool, InvocationClass: HarnessInvocationClass(profile.InvocationClass), Defaults: defaults}
}

func decodeStoredAuthPool(raw string) HarnessAuthPool {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "{}" {
		return HarnessAuthPool{}
	}
	var pool HarnessAuthPool
	_ = json.Unmarshal([]byte(raw), &pool)
	return pool
}

func ensureDefaultHelperProfile(ctx context.Context, store *globaldb.Store, req DefaultHelperEnsureRequest) (AgentProfileResponse, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return AgentProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", map[string]any{"reason": "missing_workspace_id"})
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = helperPrompt()
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
	return FinalResponseResponse{FinalResponseID: stored.FinalResponseID, SessionID: stored.SessionID, WorkspaceID: stored.WorkspaceID, TaskID: stored.TaskID, ContextPacketID: stored.ContextPacketID, ProfileID: stored.ProfileID, Status: stored.Status, Text: stored.Text, EvidenceLinks: links, CreatedAt: stored.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: updatedAt}
}

func telemetryRollup(ctx context.Context, store *globaldb.Store, req TelemetryRollupRequest) (TelemetryRollupResponse, error) {
	rollups, err := store.RollupAgentSessionTelemetry(ctx, req.WorkspaceID)
	if err != nil {
		return TelemetryRollupResponse{}, err
	}
	out := make([]TelemetryRollup, 0, len(rollups))
	for _, rollup := range rollups {
		out = append(out, telemetryRollupFromStore(rollup))
	}
	return TelemetryRollupResponse{Rollups: out}, nil
}

func telemetryRollupFromStore(rollup globaldb.AgentSessionTelemetryRollup) TelemetryRollup {
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
	profile = applyAgentSessionDefaults(profile, req.Defaults)
	if strings.TrimSpace(profile.Harness) == "" {
		return AgentProfile{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is required", map[string]any{"profile": strings.TrimSpace(profile.Name), "reason": "missing_harness", "start_invoked": false})
	}
	return profile, nil
}

func applyAgentSessionDefaults(profile AgentProfile, defaults AgentSessionDefaults) AgentProfile {
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
	return AgentProfile{ProfileID: stored.ProfileID, Name: stored.Name, Harness: stored.Harness, Model: stored.Model, Prompt: stored.Prompt, AuthSlotID: stored.AuthSlotID, AuthPool: decodeStoredAuthPool(stored.AuthPoolJSON), InvocationClass: HarnessInvocationClass(stored.InvocationClass)}, nil
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

func createAgentSessionConfig(ctx context.Context, store *globaldb.Store, req AgentSessionConfigCreateRequest) (AgentSessionConfigCreateResponse, error) {
	agent := globaldb.AgentSessionConfig{AgentID: strings.TrimSpace(req.AgentID), WorkspaceID: strings.TrimSpace(req.WorkspaceID), Name: strings.TrimSpace(req.Name), Harness: strings.TrimSpace(req.Harness), Model: strings.TrimSpace(req.Model), Prompt: strings.TrimSpace(req.Prompt)}
	if agent.AgentID == "" || agent.Name == "" {
		return AgentSessionConfigCreateResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "agent_id and name are required", nil)
	}
	if err := store.CreateAgentSessionConfig(ctx, agent); err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) {
			return AgentSessionConfigCreateResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return AgentSessionConfigCreateResponse{}, err
	}
	return AgentSessionConfigCreateResponse{Agent: AgentSessionConfigResponse{AgentID: agent.AgentID, WorkspaceID: agent.WorkspaceID, Name: agent.Name, Harness: agent.Harness, Model: agent.Model, Prompt: agent.Prompt}}, nil
}

func listAgentSessionConfigs(ctx context.Context, store *globaldb.Store, req AgentSessionConfigListRequest) (AgentSessionConfigListResponse, error) {
	agents, err := store.ListAgentSessionConfigs(ctx, strings.TrimSpace(req.WorkspaceID))
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) {
			return AgentSessionConfigListResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return AgentSessionConfigListResponse{}, err
	}
	resp := AgentSessionConfigListResponse{Agents: make([]AgentSessionConfigResponse, 0, len(agents))}
	for _, agent := range agents {
		resp.Agents = append(resp.Agents, agentSessionConfigResponse(agent))
	}
	return resp, nil
}

func updateAgentSessionConfig(ctx context.Context, store *globaldb.Store, req AgentSessionConfigUpdateRequest) (AgentSessionConfigResponse, error) {
	agent, err := store.UpdateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: strings.TrimSpace(req.AgentID), WorkspaceID: strings.TrimSpace(req.WorkspaceID), Name: strings.TrimSpace(req.Name), Harness: strings.TrimSpace(req.Harness), Model: strings.TrimSpace(req.Model), Prompt: strings.TrimSpace(req.Prompt)})
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return AgentSessionConfigResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return AgentSessionConfigResponse{}, err
	}
	return agentSessionConfigResponse(agent), nil
}

func deleteAgentSessionConfig(ctx context.Context, store *globaldb.Store, req AgentSessionConfigDeleteRequest) (AgentSessionConfigDeleteResponse, error) {
	if err := store.DeleteAgentSessionConfig(ctx, strings.TrimSpace(req.AgentID)); err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) {
			return AgentSessionConfigDeleteResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return AgentSessionConfigDeleteResponse{}, err
	}
	return AgentSessionConfigDeleteResponse{Deleted: true}, nil
}

func runAgentSessionConfig(ctx context.Context, store *globaldb.Store, req AgentSessionConfigSessionRequest) (AgentSessionConfigSessionResponse, error) {
	run, err := store.CreateSessionFromAgentSessionConfig(ctx, strings.TrimSpace(req.SessionID), strings.TrimSpace(req.AgentID), strings.TrimSpace(req.CWD))
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return AgentSessionConfigSessionResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return AgentSessionConfigSessionResponse{}, err
	}
	return AgentSessionConfigSessionResponse{Run: run}, nil
}

func agentSessionConfigResponse(agent globaldb.AgentSessionConfig) AgentSessionConfigResponse {
	return AgentSessionConfigResponse{AgentID: agent.AgentID, WorkspaceID: agent.WorkspaceID, Name: agent.Name, Harness: agent.Harness, Model: agent.Model, Prompt: agent.Prompt}
}

func sendAgentMessage(ctx context.Context, store *globaldb.Store, req AgentMessageSendRequest) (AgentMessageSendResponse, error) {
	dm, err := store.SendAgentMessage(ctx, globaldb.AgentMessageSendParams{AgentMessageID: req.AgentMessageID, SourceSessionID: req.SourceSessionID, TargetAgentID: req.TargetAgentID, TargetSessionID: req.TargetSessionID, Body: req.Body, ContextExcerptIDs: req.ContextExcerptIDs, StartSessionID: req.StartSessionID})
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return AgentMessageSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return AgentMessageSendResponse{}, err
	}
	return AgentMessageSendResponse{AgentMessage: AgentMessageResponse{AgentMessageID: dm.AgentMessageID, WorkspaceID: dm.WorkspaceID, SourceAgentID: dm.SourceAgentID, SourceSessionID: dm.SourceSessionID, TargetAgentID: dm.TargetAgentID, TargetSessionID: dm.TargetSessionID, Body: dm.Body, Status: dm.Status, DeliveredSessionID: dm.DeliveredSessionID, ContextExcerptIDs: dm.ContextExcerptIDs}}, nil
}

func agentMessageResponse(dm globaldb.AgentMessage) AgentMessageResponse {
	return AgentMessageResponse{AgentMessageID: dm.AgentMessageID, WorkspaceID: dm.WorkspaceID, SourceAgentID: dm.SourceAgentID, SourceSessionID: dm.SourceSessionID, TargetAgentID: dm.TargetAgentID, TargetSessionID: dm.TargetSessionID, Body: dm.Body, Status: dm.Status, DeliveredSessionID: dm.DeliveredSessionID, ContextExcerptIDs: dm.ContextExcerptIDs}
}

func (d *Daemon) callEphemeralAgent(ctx context.Context, store *globaldb.Store, req EphemeralAgentCallRequest) (EphemeralAgentCallResponse, error) {
	callID := strings.TrimSpace(req.CallID)
	sourceSessionID := strings.TrimSpace(req.SourceSessionID)
	targetAgentID := strings.TrimSpace(req.TargetAgentID)
	body := strings.TrimSpace(req.Body)
	if callID == "" || sourceSessionID == "" || targetAgentID == "" || body == "" {
		return EphemeralAgentCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "call_id, source_session_id, target_agent_id, and body are required", nil)
	}
	sourceRun, err := store.GetAgentSession(ctx, sourceSessionID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return EphemeralAgentCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return EphemeralAgentCallResponse{}, err
	}
	targetAgent, err := store.GetAgentSessionConfig(ctx, targetAgentID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return EphemeralAgentCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return EphemeralAgentCallResponse{}, err
	}
	if targetAgent.WorkspaceID != sourceRun.WorkspaceID {
		return EphemeralAgentCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), nil)
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = callID + "-run"
	}
	run := globaldb.AgentSession{SessionID: sessionID, WorkspaceID: sourceRun.WorkspaceID, AgentID: targetAgent.AgentID, Harness: targetAgent.Harness, Model: targetAgent.Model, Status: "running", Usage: "ephemeral", SourceSessionID: sourceRun.SessionID, SourceAgentID: sourceRun.AgentID}
	if err := store.CreateAgentSession(ctx, run); err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return EphemeralAgentCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return EphemeralAgentCallResponse{}, err
	}
	requestDM, err := store.SendAgentMessage(ctx, globaldb.AgentMessageSendParams{AgentMessageID: callID + "-request", SourceSessionID: sourceRun.SessionID, TargetAgentID: targetAgent.AgentID, TargetSessionID: sessionID, Body: body, ContextExcerptIDs: req.ContextExcerptIDs})
	if err != nil {
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return EphemeralAgentCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return EphemeralAgentCallResponse{}, err
	}
	primaryFolder, _ := lookupPrimaryFolder(ctx, store, sourceRun.WorkspaceID)
	executor, err := d.resolveHarness(AgentSessionStartRequest{Executor: targetAgent.Harness}, primaryFolder)
	if err != nil {
		_ = store.UpdateAgentSessionStatus(ctx, sessionID, "failed")
		return EphemeralAgentCallResponse{}, mapHarnessRunError(err)
	}
	providerRun, err := executor.Start(ctx, ExecutorStartRequest{WorkspaceID: sourceRun.WorkspaceID, RunID: sessionID, SessionID: sessionID, ContextPacket: body, Model: targetAgent.Model, Prompt: targetAgent.Prompt, InvocationClass: HarnessInvocationTemporary})
	if err != nil {
		_ = store.UpdateAgentSessionStatus(ctx, sessionID, "failed")
		return EphemeralAgentCallResponse{}, mapHarnessRunError(err)
	}
	providerSessionID := strings.TrimSpace(providerRun.SessionID)
	if providerSessionID == "" {
		providerSessionID = strings.TrimSpace(providerRun.RunID)
	}
	items, err := executor.Items(ctx, providerSessionID)
	if err != nil {
		_ = store.UpdateAgentSessionStatus(ctx, sessionID, "failed")
		return EphemeralAgentCallResponse{}, err
	}
	if err := appendTimelineItemsAsRunLogMessages(ctx, store, sessionID, items); err != nil {
		_ = store.UpdateAgentSessionStatus(ctx, sessionID, "failed")
		return EphemeralAgentCallResponse{}, err
	}
	replyBody := lastAgentText(items)
	if replyBody == "" {
		replyBody = "completed"
	}
	replyID := strings.TrimSpace(req.ReplyAgentMessageID)
	if replyID == "" {
		replyID = callID + "-reply"
	}
	replyDM, err := store.SendAgentMessage(ctx, globaldb.AgentMessageSendParams{AgentMessageID: replyID, SourceSessionID: sessionID, TargetAgentID: sourceRun.AgentID, TargetSessionID: sourceRun.SessionID, Body: replyBody})
	if err != nil {
		_ = store.UpdateAgentSessionStatus(ctx, sessionID, "failed")
		if errors.Is(err, globaldb.ErrInvalidInput) || errors.Is(err, globaldb.ErrNotFound) {
			return EphemeralAgentCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), nil)
		}
		return EphemeralAgentCallResponse{}, err
	}
	if err := store.UpdateAgentSessionStatus(ctx, sessionID, "completed"); err != nil {
		return EphemeralAgentCallResponse{}, err
	}
	storedRun, err := store.GetAgentSession(ctx, sessionID)
	if err != nil {
		return EphemeralAgentCallResponse{}, err
	}
	return EphemeralAgentCallResponse{Run: storedRun, Request: agentMessageResponse(requestDM), Reply: agentMessageResponse(replyDM)}, nil
}

func appendTimelineItemsAsRunLogMessages(ctx context.Context, store *globaldb.Store, sessionID string, items []TimelineItem) error {
	next := 1
	if tail, err := store.TailRunLogMessages(ctx, sessionID, 1); err == nil && len(tail) == 1 {
		next = tail[0].Sequence + 1
	}
	for _, item := range items {
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		messageID := strings.TrimSpace(item.ID)
		if messageID == "" {
			messageID = fmt.Sprintf("%s-message-%d", sessionID, next)
		}
		if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: messageID, SessionID: sessionID, Sequence: next, Role: "assistant", Status: "completed", Parts: []globaldb.RunLogMessagePart{{PartID: messageID + "-part-1", Sequence: 1, Kind: "text", Text: text}}}); err != nil {
			return err
		}
		next++
	}
	return nil
}

func lastAgentText(items []TimelineItem) string {
	for i := len(items) - 1; i >= 0; i-- {
		if strings.TrimSpace(items[i].Text) != "" {
			return strings.TrimSpace(items[i].Text)
		}
	}
	return ""
}

func (d *Daemon) startAgentSession(ctx context.Context, store *globaldb.Store, req AgentSessionStartRequest, profile ...AgentProfile) (AgentSessionStartResponse, error) {
	executorName := strings.TrimSpace(req.Executor)
	if executorName == "" {
		return AgentSessionStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "executor is required", nil)
	}
	primaryFolder, err := lookupPrimaryFolder(ctx, store, req.Packet.WorkspaceID)
	if err != nil {
		return AgentSessionStartResponse{}, mapWorkspaceStoreError(err, req.Packet.WorkspaceID)
	}
	executor, err := d.resolveHarness(req, primaryFolder)
	if err != nil {
		return AgentSessionStartResponse{}, mapHarnessRunError(err)
	}
	if len(profile) > 0 {
		selected, err := resolveProfileAuthSlot(ctx, store, executor, req.Executor, profile[0])
		if err != nil {
			return AgentSessionStartResponse{}, mapHarnessRunError(err)
		}
		profile[0].AuthSlotID = selected
	}
	if _, err := store.GetSession(ctx, req.Packet.WorkspaceID); err != nil {
		return AgentSessionStartResponse{}, mapWorkspaceStoreError(err, req.Packet.WorkspaceID)
	}
	result, err := StartExecutorRunResult(ctx, executor, req.Packet, profile...)
	if err != nil {
		return AgentSessionStartResponse{}, mapHarnessRunError(err)
	}
	run := result.AgentSession
	items := result.Items
	d.recordExecutorRun(run, items)
	if err := storeHarnessRunLogMessages(ctx, store, result, primaryFolder, profile...); err != nil {
		return AgentSessionStartResponse{}, err
	}
	if result.FinalResponse != nil {
		if err := storeFinalResponse(ctx, store, result, profile...); err != nil {
			return AgentSessionStartResponse{}, err
		}
	}
	sample := agentSessionProcessMetricsSampler(ctx, run)
	if run.ProcessSample != nil {
		sample = *run.ProcessSample
	}
	if err := storeAgentSessionTelemetry(ctx, store, result, sample, profile...); err != nil {
		return AgentSessionStartResponse{}, err
	}
	return AgentSessionStartResponse{Run: run, Items: items}, nil
}

func storeHarnessRunLogMessages(ctx context.Context, store *globaldb.Store, result HarnessCallResult, primaryFolder string, profile ...AgentProfile) error {
	run := result.AgentSession
	agentID := "wa_" + strings.TrimSpace(run.AgentSessionID)
	agentName := strings.TrimSpace(run.Executor)
	model := result.Telemetry.Model
	prompt := ""
	if len(profile) > 0 {
		if strings.TrimSpace(profile[0].ProfileID) != "" {
			agentID = "wa_" + strings.TrimSpace(run.WorkspaceID) + "_" + strings.TrimSpace(profile[0].ProfileID)
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
		agentName = agentID
	}
	if err := store.EnsureAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: agentID, WorkspaceID: run.WorkspaceID, Name: agentName, Harness: run.Executor, Model: model, Prompt: prompt}); err != nil {
		return err
	}
	providerMetadata, err := json.Marshal(map[string]any{"session_ref": result.SessionRef, "provider_session_id": run.ProviderSessionID, "capabilities": run.Capabilities})
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
	if err := store.CreateAgentSession(ctx, globaldb.AgentSession{SessionID: run.AgentSessionID, WorkspaceID: run.WorkspaceID, AgentID: agentID, Harness: run.Executor, Model: model, ProviderSessionID: result.SessionRef.ProviderSessionID, ProviderRunID: run.ProviderRunID, ProviderThreadID: result.SessionRef.ProviderThreadID, CWD: strings.TrimSpace(primaryFolder), FolderScopeJSON: folderScopeJSON, Status: run.Status, Usage: "durable", ContextPayloadIDsJSON: fmt.Sprintf("[%q]", run.ContextPacketID), ProviderMetadataJSON: string(providerMetadata)}); err != nil {
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
			messageID = fmt.Sprintf("%s-message-%d", run.AgentSessionID, sequence)
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
		if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: messageID, SessionID: run.AgentSessionID, Sequence: sequence, Role: role, Status: "completed", ProviderMessageID: providerFields.messageID, ProviderItemID: providerFields.itemID, ProviderTurnID: providerFields.turnID, ProviderResponseID: providerFields.responseID, ProviderCallID: providerFields.callID, ProviderChannel: providerFields.channel, ProviderKind: providerFields.kind, RawMetadataJSON: rawMetadata, Parts: []globaldb.RunLogMessagePart{{PartID: messageID + "-part-1", Sequence: 1, Kind: kind, Text: item.Text, ToolName: providerFields.toolName, ToolCallID: providerFields.callID}}}); err != nil {
			return err
		}
		sequence++
	}
	return nil
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

func resolveProfileAuthSlot(ctx context.Context, store *globaldb.Store, executor Executor, harness string, profile AgentProfile) (string, error) {
	if strings.TrimSpace(profile.AuthSlotID) == "" && len(profile.AuthPool.SlotIDs) == 0 {
		return "", nil
	}
	statuser, ok := executor.(harnessAuthStatuser)
	if !ok {
		return "", &HarnessUnavailableError{Harness: harness, Reason: "auth_slot_selection_unsupported", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
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
				return "", &HarnessUnavailableError{Harness: harness, Reason: "unknown_auth_slot", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
			}
			return "", err
		}
		slot := harnessAuthSlotFromGlobal(stored)
		if strings.TrimSpace(slot.Harness) != strings.TrimSpace(harness) {
			return "", &HarnessUnavailableError{Harness: harness, Reason: "auth_slot_harness_mismatch", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
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
		return "", &HarnessUnavailableError{Harness: harness, Reason: "auth_slot_not_ready", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
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

func StartExecutorRun(ctx context.Context, executor Executor, packet ContextPacket, profile ...AgentProfile) (AgentSession, []TimelineItem, error) {
	result, err := StartExecutorRunResult(ctx, executor, packet, profile...)
	if err != nil {
		return AgentSession{}, nil, err
	}
	return result.AgentSession, result.Items, nil
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
	call, err := NewAgentSessionHarnessCall(packet, nil)
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
		call.AuthSlotID = strings.TrimSpace(profile[0].AuthSlotID)
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
	links := []FinalResponseEvidenceLink{{Kind: "context_packet", ID: result.AgentSession.ContextPacketID}, {Kind: "agent_session", ID: result.AgentSession.AgentSessionID}}
	for _, item := range result.Items {
		if strings.TrimSpace(item.ID) != "" {
			links = append(links, FinalResponseEvidenceLink{Kind: "timeline_item", ID: item.ID})
		}
	}
	encodedLinks, err := json.Marshal(links)
	if err != nil {
		return err
	}
	return store.UpsertFinalResponse(ctx, globaldb.FinalResponse{FinalResponseID: "fr_" + responseID, SessionID: result.AgentSession.AgentSessionID, WorkspaceID: result.AgentSession.WorkspaceID, TaskID: result.AgentSession.TaskID, ContextPacketID: result.AgentSession.ContextPacketID, ProfileID: profileID, Status: result.FinalResponse.Status, Text: result.FinalResponse.Text, EvidenceLinksJSON: string(encodedLinks)})
}

func storeAgentSessionTelemetry(ctx context.Context, store *globaldb.Store, result HarnessCallResult, sample ProcessMetricsSample, profile ...AgentProfile) error {
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
	durationMS, durationKnown := agentSessionDurationMS(result.AgentSession)
	return store.UpsertAgentRunTelemetry(ctx, globaldb.AgentRunTelemetry{RunID: result.AgentSession.AgentSessionID, WorkspaceID: result.AgentSession.WorkspaceID, TaskID: result.AgentSession.TaskID, ProfileID: profileID, ProfileName: profileName, Harness: result.AgentSession.Executor, Model: model, InvocationClass: invocationClass, Status: result.AgentSession.Status, InputTokensKnown: result.Telemetry.InputTokens != nil, InputTokens: result.Telemetry.InputTokens, OutputTokensKnown: result.Telemetry.OutputTokens != nil, OutputTokens: result.Telemetry.OutputTokens, DurationMSKnown: durationKnown, DurationMS: durationMS, OwnedByAri: sample.OwnedByAri, PIDKnown: sample.PID.Known, PID: sample.PID.Value, CPUTimeMSKnown: sample.CPUTimeMS.Known, CPUTimeMS: sample.CPUTimeMS.Value, MemoryRSSBytesPeakKnown: sample.MemoryRSSBytesPeak.Known, MemoryRSSBytesPeak: sample.MemoryRSSBytesPeak.Value, ChildProcessesPeakKnown: sample.ChildProcessesPeak.Known, ChildProcessesPeak: sample.ChildProcessesPeak.Value, PortsJSON: portsJSON, OrphanState: sample.OrphanState, ExitCodeKnown: sample.ExitCode.Known, ExitCode: sample.ExitCode.Value})
}

func agentSessionDurationMS(run AgentSession) (*int64, bool) {
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

func (d *Daemon) recordExecutorRun(run AgentSession, items []TimelineItem) {
	if d == nil || strings.TrimSpace(run.AgentSessionID) == "" {
		return
	}
	d.executorMu.Lock()
	d.executorRuns[run.AgentSessionID] = run
	buffered := append([]TimelineItem(nil), d.executorItems[run.AgentSessionID]...)
	d.executorItems[run.AgentSessionID] = mergeExecutorItems(items, buffered)
	d.updateExecutorRunStatusLocked(run.AgentSessionID, d.executorItems[run.AgentSessionID])
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
