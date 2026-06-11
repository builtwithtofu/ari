package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type AriToolListRequest struct{}

type AriToolListResponse struct {
	Tools []AriToolSchema `json:"tools"`
}

type AriToolSchema struct {
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	ScopeRequired       bool     `json:"scope_required"`
	RequiredScopeFields []string `json:"required_scope_fields"`
	ApprovalRequired    bool     `json:"approval_required"`
	ReadOnly            bool     `json:"read_only"`
	OperationKind       string   `json:"operation_kind"`
	TrustChoices        []string `json:"trust_choices"`
}

type AriToolCallRequest struct {
	Name     string          `json:"name"`
	Scope    AriToolScope    `json:"scope"`
	Input    any             `json:"input,omitempty"`
	Approval AriToolApproval `json:"approval,omitempty"`
}

type AriToolScope struct {
	SourceRunID        string `json:"source_run_id"`
	WorkspaceID        string `json:"workspace_id"`
	ProfileID          string `json:"profile_id"`
	ProfileName        string `json:"profile_name"`
	ToolName           string `json:"tool_name"`
	WithinDefaultScope bool   `json:"within_default_scope"`
}

type AriToolApproval struct {
	ApprovalID  string               `json:"approval_id"`
	ApprovedBy  string               `json:"approved_by"`
	ApprovedAt  string               `json:"approved_at"`
	Scope       AriToolApprovalScope `json:"scope"`
	RequestHash string               `json:"request_hash"`
}

type AriToolApprovalScope struct {
	WorkspaceID string `json:"workspace_id"`
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name"`
	ToolName    string `json:"tool_name"`
	SourceRunID string `json:"source_run_id"`
}

type AriToolCallResponse struct {
	Status            string         `json:"status"`
	ApplicationStatus string         `json:"application_status,omitempty"`
	Output            map[string]any `json:"output,omitempty"`
}

type storedAriApproval struct {
	Approval AriToolApproval `json:"approval"`
	Consumed bool            `json:"consumed"`
}

var ariTools = []AriToolSchema{
	{Name: "ari.defaults.get", Description: "Read Ari default harness, model, and invocation settings", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true, OperationKind: daemonOperationKindReadOnly},
	{Name: "ari.defaults.set", Description: "Update Ari defaults after scoped approval", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ApprovalRequired: true, OperationKind: daemonOperationKindMutating, TrustChoices: ariToolTrustChoices()},
	{Name: "ari.profile.draft", Description: "Draft a profile spec without persisting it", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true, OperationKind: daemonOperationKindReadOnly},
	{Name: "ari.profile.save", Description: "Persist an approved profile spec", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ApprovalRequired: true, OperationKind: daemonOperationKindMutating, TrustChoices: ariToolTrustChoices()},
	{Name: "ari.self_check", Description: "Read Ari daemon, config, workspace, profile, and harness health", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true, OperationKind: daemonOperationKindReadOnly},
	{Name: "ari.run.explain_latest", Description: "Summarize the latest available Ari run evidence", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true, OperationKind: daemonOperationKindReadOnly},
	{Name: "ari.session.fanout", Description: "Launch one or more ephemeral worker profiles from a scoped sticky source session", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), OperationKind: daemonOperationKindMutating},
	{Name: "ari.fanout.status", Description: "Read durable fanout group and member status", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true, OperationKind: daemonOperationKindReadOnly},
	{Name: "ari.inbox.list", Description: "List durable sticky-session inbox items", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true, OperationKind: daemonOperationKindReadOnly},
	{Name: "ari.inbox.count", Description: "Count durable sticky-session inbox items by read state", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true, OperationKind: daemonOperationKindReadOnly},
	{Name: "ari.inbox.mark_read", Description: "Mark durable sticky-session inbox items read", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), OperationKind: daemonOperationKindMutating},
	{Name: "ari.workspace.events.next", Description: "Read unread events from a durable workspace event subscription, optionally blocking until min_events arrive within timeout_ms (max 60000)", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true, OperationKind: daemonOperationKindReadOnly},
	{Name: "ari.workspace.events.ack", Description: "Advance a durable workspace event subscription cursor", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), OperationKind: daemonOperationKindMutating},
	{Name: "ari.workspace.signals.send", Description: "Send a workspace-scoped signal event from the scoped source session", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), OperationKind: daemonOperationKindMutating},
	{Name: "ari.workspace.timers.create", Description: "Create a durable workspace timer owned by the scoped source session", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), OperationKind: daemonOperationKindMutating},
	{Name: "ari.workspace.timers.get", Description: "Read a durable workspace timer", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true, OperationKind: daemonOperationKindReadOnly},
	{Name: "ari.workspace.timers.cancel", Description: "Cancel a durable workspace timer owned by the scoped source session", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), OperationKind: daemonOperationKindMutating},
	{Name: "ari.workspace.deliveries.get", Description: "Read a pending workspace event delivery", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true, OperationKind: daemonOperationKindReadOnly},
	{Name: "ari.workspace.deliveries.list_due", Description: "List due pending workspace event deliveries", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true, OperationKind: daemonOperationKindReadOnly},
}

func ariToolScopeFields() []string {
	return []string{"source_run_id", "workspace_id", "profile_id", "profile_name", "tool_name", "within_default_scope"}
}

func ariToolTrustChoices() []string {
	return []string{"trust_once", "trust_always_by_operation_type", "deny"}
}

func (d *Daemon) registerAriToolMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if err := rpc.RegisterMethod(registry, rpc.Method[AriToolListRequest, AriToolListResponse]{
		Name:        "ari.tool.list",
		Description: "List Ari-owned tools available to helpers",
		Handler: func(ctx context.Context, req AriToolListRequest) (AriToolListResponse, error) {
			_ = ctx
			_ = req
			tools := make([]AriToolSchema, len(ariTools))
			copy(tools, ariTools)
			return AriToolListResponse{Tools: tools}, nil
		},
	}); err != nil {
		return fmt.Errorf("register ari.tool.list: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AriToolCallRequest, AriToolCallResponse]{
		Name:        "ari.tool.call",
		Description: "Call an Ari-owned helper tool",
		Handler: func(ctx context.Context, req AriToolCallRequest) (AriToolCallResponse, error) {
			return d.callAriTool(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register ari.tool.call: %w", err)
	}
	return nil
}

func (d *Daemon) callAriTool(ctx context.Context, store *globaldb.Store, req AriToolCallRequest) (AriToolCallResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return AriToolCallResponse{}, ariToolError("missing_tool_name", "tool name is required")
	}
	if strings.TrimSpace(req.Scope.ToolName) == "" {
		req.Scope.ToolName = name
	}
	if req.Scope.ToolName != name {
		return AriToolCallResponse{}, ariToolError("scope_tool_mismatch", "scope tool_name must match requested tool")
	}
	tool, ok := ariToolByName(name)
	if !ok {
		return AriToolCallResponse{}, ariToolError("unknown_tool", "unknown Ari tool")
	}
	if err := validateAriToolScope(req.Scope); err != nil {
		return AriToolCallResponse{}, err
	}
	if _, err := store.GetWorkspace(ctx, req.Scope.WorkspaceID); err != nil {
		return AriToolCallResponse{}, err
	}
	if name == "ari.defaults.set" && !req.Scope.WithinDefaultScope {
		return AriToolCallResponse{}, ariToolError("handoff_required", "defaults writes require an in-scope helper approval")
	}
	if tool.ApprovalRequired {
		if err := validateAndConsumeAriApproval(ctx, store, req); err != nil {
			return AriToolCallResponse{}, err
		}
	}
	switch name {
	case "ari.defaults.get":
		return d.ariDefaultsGet()
	case "ari.defaults.set":
		var response AriToolCallResponse
		_, err := recordDaemonOperation(ctx, store, daemonOperationRecordOptions{OperationType: "ari_defaults_set", OperationKind: daemonOperationKindMutating, Actor: req.Scope.ProfileName, Source: daemonOperationSourceTool, Scope: globaldb.OperationScopeGlobal, RequestSummary: "set Ari defaults from helper tool", TrustDecision: "approved_once", RollbackData: map[string]string{"scope": "ari_owned_config"}, PayloadSnapshot: map[string]string{"tool": name, "workspace_id": req.Scope.WorkspaceID, "request_hash": req.Approval.RequestHash}}, func(ctx context.Context) error {
			_ = ctx
			var err error
			response, err = d.ariDefaultsSet(req.Input)
			return err
		})
		return response, err
	case "ari.profile.draft":
		return ariProfileDraft(req.Input)
	case "ari.profile.save":
		var response AriToolCallResponse
		_, err := recordDaemonOperation(ctx, store, daemonOperationRecordOptions{WorkspaceID: req.Scope.WorkspaceID, OperationType: "ari_profile_save", OperationKind: daemonOperationKindMutating, Actor: req.Scope.ProfileName, Source: daemonOperationSourceTool, Scope: globaldb.OperationScopeWorkspace, RequestSummary: "save Ari helper profile", TrustDecision: "approved_once", RollbackData: map[string]string{"scope": "ari_owned_profile"}, PayloadSnapshot: map[string]string{"tool": name, "workspace_id": req.Scope.WorkspaceID, "request_hash": req.Approval.RequestHash}}, func(ctx context.Context) error {
			var err error
			response, err = ariProfileSave(ctx, store, req.Scope, req.Input)
			return err
		})
		return response, err
	case "ari.self_check":
		return d.ariSelfCheck(ctx, store, req.Scope)
	case "ari.run.explain_latest":
		return ariRunExplainLatest(ctx, store, req.Scope)
	case "ari.session.fanout":
		var response AriToolCallResponse
		requestHash, err := HashAriToolRequest(name, req.Input)
		if err != nil {
			return AriToolCallResponse{}, err
		}
		_, err = recordDaemonOperation(ctx, store, daemonOperationRecordOptions{WorkspaceID: req.Scope.WorkspaceID, OperationType: "ari_session_fanout", OperationKind: daemonOperationKindMutating, Actor: req.Scope.ProfileName, Source: daemonOperationSourceTool, Scope: globaldb.OperationScopeWorkspace, RequestSummary: "launch Ari fanout workers from helper tool", TrustDecision: "scoped_source_session", RollbackData: map[string]string{"scope": "runtime_coordination", "rollback": "not_supported_for_external_worker_runs"}, PayloadSnapshot: map[string]string{"tool": name, "workspace_id": req.Scope.WorkspaceID, "source_run_id": req.Scope.SourceRunID, "request_hash": requestHash}}, func(ctx context.Context) error {
			var err error
			response, err = d.ariSessionFanout(ctx, store, req.Scope, req.Input)
			return err
		})
		return response, err
	case "ari.fanout.status":
		return ariFanoutStatus(ctx, store, req.Scope, req.Input)
	case "ari.inbox.list":
		return ariInboxList(ctx, store, req.Scope, req.Input)
	case "ari.inbox.count":
		return ariInboxCount(ctx, store, req.Scope, req.Input)
	case "ari.inbox.mark_read":
		var response AriToolCallResponse
		requestHash, err := HashAriToolRequest(name, req.Input)
		if err != nil {
			return AriToolCallResponse{}, err
		}
		_, err = recordDaemonOperation(ctx, store, daemonOperationRecordOptions{WorkspaceID: req.Scope.WorkspaceID, OperationType: "ari_inbox_mark_read", OperationKind: daemonOperationKindMutating, Actor: req.Scope.ProfileName, Source: daemonOperationSourceTool, Scope: globaldb.OperationScopeWorkspace, RequestSummary: "mark Ari inbox items read from helper tool", TrustDecision: "scoped_source_session", RollbackData: map[string]string{"scope": "runtime_inbox", "rollback": "not_supported_for_read_lifecycle"}, PayloadSnapshot: map[string]string{"tool": name, "workspace_id": req.Scope.WorkspaceID, "source_run_id": req.Scope.SourceRunID, "request_hash": requestHash}}, func(ctx context.Context) error {
			var err error
			response, err = ariInboxMarkRead(ctx, store, req.Scope, req.Input)
			return err
		})
		return response, err
	case "ari.workspace.events.next":
		return ariWorkspaceEventsNext(ctx, store, req.Scope, req.Input)
	case "ari.workspace.events.ack":
		var response AriToolCallResponse
		requestHash, err := HashAriToolRequest(name, req.Input)
		if err != nil {
			return AriToolCallResponse{}, err
		}
		_, err = recordDaemonOperation(ctx, store, daemonOperationRecordOptions{WorkspaceID: req.Scope.WorkspaceID, OperationType: "ari_workspace_events_ack", OperationKind: daemonOperationKindMutating, Actor: req.Scope.ProfileName, Source: daemonOperationSourceTool, Scope: globaldb.OperationScopeWorkspace, RequestSummary: "ack Ari workspace event subscription from helper tool", TrustDecision: "scoped_source_session", RollbackData: map[string]string{"scope": "workspace_event_subscription", "rollback": "not_supported_for_read_lifecycle"}, PayloadSnapshot: map[string]string{"tool": name, "workspace_id": req.Scope.WorkspaceID, "source_run_id": req.Scope.SourceRunID, "request_hash": requestHash}}, func(ctx context.Context) error {
			var err error
			response, err = ariWorkspaceEventsAck(ctx, store, req.Scope, req.Input)
			return err
		})
		return response, err
	case "ari.workspace.signals.send":
		var response AriToolCallResponse
		requestHash, err := HashAriToolRequest(name, req.Input)
		if err != nil {
			return AriToolCallResponse{}, err
		}
		_, err = recordDaemonOperation(ctx, store, daemonOperationRecordOptions{WorkspaceID: req.Scope.WorkspaceID, OperationType: "ari_workspace_signals_send", OperationKind: daemonOperationKindMutating, Actor: req.Scope.ProfileName, Source: daemonOperationSourceTool, Scope: globaldb.OperationScopeWorkspace, RequestSummary: "send Ari workspace signal from helper tool", TrustDecision: "scoped_source_session", RollbackData: map[string]string{"scope": "workspace_event_history", "rollback": "append_only_signal_not_reverted"}, PayloadSnapshot: map[string]string{"tool": name, "workspace_id": req.Scope.WorkspaceID, "source_run_id": req.Scope.SourceRunID, "request_hash": requestHash}}, func(ctx context.Context) error {
			var err error
			response, err = ariWorkspaceSignalSend(ctx, store, req.Scope, req.Input)
			return err
		})
		return response, err
	case "ari.workspace.timers.create":
		var response AriToolCallResponse
		requestHash, err := HashAriToolRequest(name, req.Input)
		if err != nil {
			return AriToolCallResponse{}, err
		}
		_, err = recordDaemonOperation(ctx, store, daemonOperationRecordOptions{WorkspaceID: req.Scope.WorkspaceID, OperationType: "ari_workspace_timers_create", OperationKind: daemonOperationKindMutating, Actor: req.Scope.ProfileName, Source: daemonOperationSourceTool, Scope: globaldb.OperationScopeWorkspace, RequestSummary: "create Ari workspace timer from helper tool", TrustDecision: "scoped_source_session", RollbackData: map[string]string{"scope": "workspace_timer", "rollback": "cancel_timer_when_scheduled"}, PayloadSnapshot: map[string]string{"tool": name, "workspace_id": req.Scope.WorkspaceID, "source_run_id": req.Scope.SourceRunID, "request_hash": requestHash}}, func(ctx context.Context) error {
			var err error
			response, err = ariWorkspaceTimerCreate(ctx, store, req.Scope, req.Input)
			return err
		})
		return response, err
	case "ari.workspace.timers.get":
		return ariWorkspaceTimerGet(ctx, store, req.Scope, req.Input)
	case "ari.workspace.timers.cancel":
		var response AriToolCallResponse
		requestHash, err := HashAriToolRequest(name, req.Input)
		if err != nil {
			return AriToolCallResponse{}, err
		}
		_, err = recordDaemonOperation(ctx, store, daemonOperationRecordOptions{WorkspaceID: req.Scope.WorkspaceID, OperationType: "ari_workspace_timers_cancel", OperationKind: daemonOperationKindMutating, Actor: req.Scope.ProfileName, Source: daemonOperationSourceTool, Scope: globaldb.OperationScopeWorkspace, RequestSummary: "cancel Ari workspace timer from helper tool", TrustDecision: "scoped_source_session", RollbackData: map[string]string{"scope": "workspace_timer", "rollback": "not_supported_for_timer_cancellation"}, PayloadSnapshot: map[string]string{"tool": name, "workspace_id": req.Scope.WorkspaceID, "source_run_id": req.Scope.SourceRunID, "request_hash": requestHash}}, func(ctx context.Context) error {
			var err error
			response, err = ariWorkspaceTimerCancel(ctx, store, req.Scope, req.Input)
			return err
		})
		return response, err
	case "ari.workspace.deliveries.get":
		return ariWorkspaceDeliveryGet(ctx, store, req.Scope, req.Input)
	case "ari.workspace.deliveries.list_due":
		return ariWorkspaceDeliveriesListDue(ctx, store, req.Scope, req.Input)
	default:
		return AriToolCallResponse{}, ariToolError("unknown_tool", "unknown Ari tool")
	}
}

func ariToolByName(name string) (AriToolSchema, bool) {
	for _, tool := range ariTools {
		if tool.Name == name {
			return tool, true
		}
	}
	return AriToolSchema{}, false
}

func validateAriToolScope(scope AriToolScope) error {
	if strings.TrimSpace(scope.WorkspaceID) == "" || strings.TrimSpace(scope.ToolName) == "" || strings.TrimSpace(scope.ProfileID) == "" || strings.TrimSpace(scope.ProfileName) == "" || strings.TrimSpace(scope.SourceRunID) == "" {
		return ariToolError("missing_scope", "tool scope requires source run, workspace, profile, and tool metadata")
	}
	return nil
}

func validateAndConsumeAriApproval(ctx context.Context, store *globaldb.Store, req AriToolCallRequest) error {
	approval := req.Approval
	if strings.TrimSpace(approval.ApprovalID) == "" || strings.TrimSpace(approval.ApprovedBy) == "" || strings.TrimSpace(approval.ApprovedAt) == "" || strings.TrimSpace(approval.RequestHash) == "" {
		return ariToolError("approval_required", "approval is required for this Ari tool")
	}
	stored, err := loadAriApproval(ctx, store, approval.ApprovalID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return ariToolError("approval_unknown", "approval was not issued by Ari")
		}
		return err
	}
	if stored.Consumed {
		return ariToolError("approval_reused", "approval has already been used")
	}
	if stored.Approval != approval {
		return ariToolError("approval_mismatch", "approval does not match issued marker")
	}
	approvedAt, err := time.Parse(time.RFC3339, approval.ApprovedAt)
	if err != nil {
		return ariToolError("approval_invalid", "approval approved_at must be RFC3339")
	}
	if time.Since(approvedAt) > 10*time.Minute || time.Until(approvedAt) > time.Minute {
		return ariToolError("approval_stale", "approval is stale or from the future")
	}
	if approval.Scope.WorkspaceID != req.Scope.WorkspaceID || approval.Scope.ProfileID != req.Scope.ProfileID || approval.Scope.ProfileName != req.Scope.ProfileName || approval.Scope.ToolName != req.Name || approval.Scope.SourceRunID != req.Scope.SourceRunID {
		return ariToolError("approval_wrong_scope", "approval scope does not match tool call")
	}
	hash, err := HashAriToolRequest(req.Name, req.Input)
	if err != nil {
		return err
	}
	if approval.RequestHash != hash {
		return ariToolError("approval_wrong_hash", "approval request hash does not match tool call")
	}
	oldValue, newValue, err := encodeConsumedAriApproval(stored)
	if err != nil {
		return err
	}
	swapped, err := store.CompareAndSwapMeta(ctx, ariApprovalMetaKey(approval.ApprovalID), oldValue, newValue)
	if err != nil {
		return err
	}
	if !swapped {
		latest, err := loadAriApproval(ctx, store, approval.ApprovalID)
		if err != nil {
			return err
		}
		if latest.Consumed {
			return ariToolError("approval_reused", "approval has already been used")
		}
		return ariToolError("approval_mismatch", "approval changed before it could be consumed")
	}
	return nil
}

func encodeConsumedAriApproval(stored storedAriApproval) (string, string, error) {
	oldValue, err := json.Marshal(stored)
	if err != nil {
		return "", "", err
	}
	stored.Consumed = true
	newValue, err := json.Marshal(stored)
	if err != nil {
		return "", "", err
	}
	return string(oldValue), string(newValue), nil
}

func storeAriApproval(ctx context.Context, store *globaldb.Store, approval storedAriApproval) error {
	encoded, err := json.Marshal(approval)
	if err != nil {
		return err
	}
	return store.SetMeta(ctx, ariApprovalMetaKey(approval.Approval.ApprovalID), string(encoded))
}

func loadAriApproval(ctx context.Context, store *globaldb.Store, approvalID string) (storedAriApproval, error) {
	value, err := store.GetMeta(ctx, ariApprovalMetaKey(approvalID))
	if err != nil {
		return storedAriApproval{}, err
	}
	var approval storedAriApproval
	if err := json.Unmarshal([]byte(value), &approval); err != nil {
		return storedAriApproval{}, err
	}
	return approval, nil
}

func ariApprovalMetaKey(approvalID string) string {
	return "ari.approval." + strings.TrimSpace(approvalID)
}

func HashAriToolRequest(name string, input any) (string, error) {
	canonical, err := canonicalJSON(input)
	if err != nil {
		return "", ariToolError("invalid_request_body", "tool request body is invalid")
	}
	payload, err := json.Marshal(struct {
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}{Name: strings.TrimSpace(name), Input: canonical})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func canonicalJSON(input any) (json.RawMessage, error) {
	if input == nil {
		return json.RawMessage(`{}`), nil
	}
	var value any
	switch typed := input.(type) {
	case json.RawMessage:
		if len(typed) == 0 {
			return json.RawMessage(`{}`), nil
		}
		if err := json.Unmarshal(typed, &value); err != nil {
			return nil, err
		}
	case []byte:
		if err := json.Unmarshal(typed, &value); err != nil {
			return nil, err
		}
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(encoded, &value); err != nil {
			return nil, err
		}
	}
	return json.Marshal(value)
}

func (d *Daemon) ariDefaultsGet() (AriToolCallResponse, error) {
	values, err := readJSONConfig(d.configPath)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	return AriToolCallResponse{Status: "ok", Output: map[string]any{"default_harness": readConfigString(values, "default_harness"), "preferred_model": readConfigString(values, "preferred_model"), "default_invocation_class": readConfigString(values, "default_invocation_class")}}, nil
}

func (d *Daemon) ariDefaultsSet(input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	harness, hasHarness := optionalString(body, "default_harness")
	if hasHarness && harness != "" && !isSupportedHarness(harness) {
		return AriToolCallResponse{}, ariToolError("invalid_default_harness", "default_harness is unsupported")
	}
	model, hasModel := optionalString(body, "preferred_model")
	invocationClass, hasInvocationClass := optionalString(body, "default_invocation_class")
	if hasInvocationClass && invocationClass != "" && invocationClass != string(HarnessInvocationSticky) && invocationClass != string(HarnessInvocationEphemeral) {
		return AriToolCallResponse{}, ariToolError("invalid_default_invocation_class", "default_invocation_class is unsupported")
	}
	updates := map[string]string{}
	if hasHarness {
		updates["default_harness"] = harness
	}
	if hasModel {
		updates["preferred_model"] = model
	}
	if hasInvocationClass {
		updates["default_invocation_class"] = invocationClass
	}
	if err := patchJSONConfigStrings(d.configPath, updates); err != nil {
		return AriToolCallResponse{}, err
	}
	return AriToolCallResponse{Status: "ok", ApplicationStatus: "restart_required", Output: map[string]any{"updated": true}}, nil
}

func ariProfileDraft(input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	name := stringValue(body, "name")
	if name == "" {
		return AriToolCallResponse{}, ariToolError("missing_profile_name", "profile name is required")
	}
	harness := stringValue(body, "harness")
	if harness != "" && !isSupportedHarness(harness) {
		return AriToolCallResponse{}, ariToolError("invalid_profile_harness", "profile harness is unsupported")
	}
	output := map[string]any{"name": name, "harness": harness, "model": stringValue(body, "model"), "prompt": stringValue(body, "prompt"), "invocation_class": stringValue(body, "invocation_class"), "defaults": map[string]any{}}
	return AriToolCallResponse{Status: "draft", Output: output}, nil
}

func ariProfileSave(ctx context.Context, store *globaldb.Store, scope AriToolScope, input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	profile, err := createStoredProfile(ctx, store, ProfileCreateRequest{WorkspaceID: scope.WorkspaceID, Name: stringValue(body, "name"), Harness: stringValue(body, "harness"), Model: stringValue(body, "model"), Prompt: stringValue(body, "prompt"), InvocationClass: HarnessInvocationClass(stringValue(body, "invocation_class")), Defaults: mapValue(body, "defaults")})
	if err != nil {
		return AriToolCallResponse{}, err
	}
	return AriToolCallResponse{Status: "ok", ApplicationStatus: "applied_live", Output: map[string]any{"profile_id": profile.ProfileID, "workspace_id": profile.WorkspaceID, "name": profile.Name, "harness": profile.Harness, "model": profile.Model, "prompt": profile.Prompt, "invocation_class": string(profile.InvocationClass)}}, nil
}

func (d *Daemon) ariSelfCheck(ctx context.Context, store *globaldb.Store, scope AriToolScope) (AriToolCallResponse, error) {
	_, cfgErr := readJSONConfig(d.configPath)
	_, wsErr := store.GetWorkspace(ctx, scope.WorkspaceID)
	return AriToolCallResponse{Status: "ok", Output: map[string]any{"daemon_version": d.version, "config_readable": cfgErr == nil, "workspace_available": wsErr == nil}}, nil
}

func ariRunExplainLatest(ctx context.Context, store *globaldb.Store, scope AriToolScope) (AriToolCallResponse, error) {
	responses, err := store.ListFinalResponses(ctx, scope.WorkspaceID)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	if len(responses) == 0 {
		return AriToolCallResponse{Status: "ok", Output: map[string]any{"summary": "No final response records are available for this workspace yet.", "run_available": false}}, nil
	}
	latest := responses[0]
	return AriToolCallResponse{Status: "ok", Output: map[string]any{"summary": latest.Text, "run_available": true, "run_id": latest.HarnessSessionID, "final_response_id": latest.FinalResponseID}}, nil
}

func (d *Daemon) ariSessionFanout(ctx context.Context, store *globaldb.Store, scope AriToolScope, input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	sourceSessionID := stringValue(body, "source_session_id")
	if sourceSessionID == "" {
		sourceSessionID = strings.TrimSpace(scope.SourceRunID)
	}
	if sourceSessionID != strings.TrimSpace(scope.SourceRunID) {
		return AriToolCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "source_scope_mismatch", "source_session_id": sourceSessionID, "scope_source_run_id": strings.TrimSpace(scope.SourceRunID), "start_invoked": false})
	}
	bodyText := stringValue(body, "body")
	if bodyText == "" {
		return AriToolCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "body", "start_invoked": false})
	}
	targetProfileIDs, err := stringSliceValue(body, "target_profile_ids")
	if err != nil {
		return AriToolCallResponse{}, err
	}
	if len(targetProfileIDs) == 0 {
		return AriToolCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "target_profile_ids", "start_invoked": false})
	}
	seenProfiles := make(map[string]struct{}, len(targetProfileIDs))
	for _, profileID := range targetProfileIDs {
		if _, ok := seenProfiles[profileID]; ok {
			return AriToolCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "duplicate_target_profile", "target_profile_id": profileID, "start_invoked": false})
		}
		seenProfiles[profileID] = struct{}{}
	}
	contextExcerptIDs, err := stringSliceValue(body, "context_excerpt_ids")
	if err != nil {
		return AriToolCallResponse{}, err
	}
	fanoutGroupID := stringValue(body, "fanout_group_id")
	if err := validateAriFanoutCanStart(ctx, store, scope, sourceSessionID, fanoutGroupID, targetProfileIDs, contextExcerptIDs); err != nil {
		return AriToolCallResponse{}, err
	}
	wait, err := ariFanoutWaitFromInput(body)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	fanout, err := d.fanoutSession(ctx, store, AgentMessageSendRequest{WorkspaceID: scope.WorkspaceID, FanoutGroupID: fanoutGroupID, SourceSessionID: sourceSessionID, TargetProfileIDs: targetProfileIDs, Body: bodyText, ContextExcerptIDs: contextExcerptIDs})
	if err != nil {
		return AriToolCallResponse{}, err
	}
	waitStatus, waitTimedOut, members, err := waitForAriFanoutMembers(ctx, store, fanout, wait)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	output := map[string]any{"fanout_group_id": fanout.FanoutGroupID, "workspace_id": scope.WorkspaceID, "source_session_id": sourceSessionID, "members": members, "wait_mode": wait.Mode, "wait_status": waitStatus, "wait_timed_out": waitTimedOut}
	return AriToolCallResponse{Status: "ok", Output: output}, nil
}

type ariFanoutWait struct {
	Mode      string
	TimeoutMS int
}

func ariFanoutWaitFromInput(body map[string]any) (ariFanoutWait, error) {
	wait := ariFanoutWait{Mode: "none"}
	raw, ok := body["wait"]
	if !ok || raw == nil {
		return wait, nil
	}
	waitMap, ok := raw.(map[string]any)
	if !ok {
		return ariFanoutWait{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_wait", "start_invoked": false})
	}
	if modeRaw, ok := waitMap["mode"]; ok && modeRaw != nil {
		modeText, ok := modeRaw.(string)
		if !ok {
			return ariFanoutWait{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_wait_mode", "wait_mode": fmt.Sprint(modeRaw), "start_invoked": false})
		}
		if mode := strings.TrimSpace(modeText); mode != "" {
			wait.Mode = mode
		}
	}
	if wait.Mode != "none" && wait.Mode != "any" && wait.Mode != "all" {
		return ariFanoutWait{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_wait_mode", "wait_mode": wait.Mode, "start_invoked": false})
	}
	if rawTimeout, ok := waitMap["timeout_ms"]; ok && rawTimeout != nil {
		var timeout int
		switch typed := rawTimeout.(type) {
		case float64:
			timeout = int(typed)
		case int:
			timeout = typed
		default:
			return ariFanoutWait{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_wait_timeout", "start_invoked": false})
		}
		if timeout < 0 {
			return ariFanoutWait{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_wait_timeout", "start_invoked": false})
		}
		wait.TimeoutMS = timeout
	}
	if wait.Mode != "none" && wait.TimeoutMS <= 0 {
		return ariFanoutWait{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_wait_timeout", "wait_mode": wait.Mode, "start_invoked": false})
	}
	return wait, nil
}

func waitForAriFanoutMembers(ctx context.Context, store *globaldb.Store, fanout AgentMessageSendResponse, wait ariFanoutWait) (string, bool, []map[string]any, error) {
	if wait.Mode == "none" {
		members := ariFanoutMembersFromResponse(fanout)
		return fanoutWaitStatus(members), false, members, nil
	}
	deadline := time.Time{}
	if wait.TimeoutMS > 0 {
		deadline = time.Now().Add(time.Duration(wait.TimeoutMS) * time.Millisecond)
	}
	for {
		members, err := ariFanoutMembersFromStore(ctx, store, fanout.FanoutGroupID)
		if err != nil {
			return "", false, nil, err
		}
		if fanoutWaitSatisfied(wait.Mode, members) {
			return fanoutWaitStatus(members), false, members, nil
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return "partial", true, members, nil
		}
		select {
		case <-ctx.Done():
			return "", false, nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func ariFanoutMembersFromResponse(fanout AgentMessageSendResponse) []map[string]any {
	members := make([]map[string]any, 0, len(fanout.FanoutMembers))
	for _, member := range fanout.FanoutMembers {
		members = append(members, map[string]any{"fanout_member_id": member.FanoutMemberID, "target_profile_id": member.TargetProfileID, "worker_session_id": member.Session.SessionID, "request_agent_message_id": member.Request.AgentMessageID, "status": member.Session.Status, "request_status": member.Request.Status})
	}
	return members
}

func ariFanoutMembersFromStore(ctx context.Context, store *globaldb.Store, groupID string) ([]map[string]any, error) {
	group, err := store.GetFanoutGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return ariFanoutMembersForGroup(ctx, store, group)
}

func ariFanoutMembersForGroup(ctx context.Context, store *globaldb.Store, group globaldb.FanoutGroup) ([]map[string]any, error) {
	stored, err := fanoutMemberProjectionForGroup(ctx, store, group)
	if err != nil {
		return nil, err
	}
	members := make([]map[string]any, 0, len(stored))
	for _, member := range stored {
		members = append(members, map[string]any{"fanout_member_id": member.FanoutMemberID, "target_profile_id": member.TargetProfileID, "worker_session_id": member.WorkerSessionID, "request_agent_message_id": member.RequestAgentMessageID, "reply_agent_message_id": member.ReplyAgentMessageID, "final_response_id": member.FinalResponseID, "status": member.Status})
	}
	return members, nil
}

func fanoutWaitSatisfied(mode string, members []map[string]any) bool {
	if len(members) == 0 {
		return false
	}
	terminal := 0
	for _, member := range members {
		if isFanoutTerminalStatus(fmt.Sprint(member["status"])) {
			terminal++
		}
	}
	switch mode {
	case "any":
		return terminal > 0
	case "all":
		return terminal == len(members)
	default:
		return true
	}
}

func fanoutWaitStatus(members []map[string]any) string {
	if len(members) == 0 {
		return "running"
	}
	terminal := 0
	statusCounts := map[string]int{}
	for _, member := range members {
		status := fmt.Sprint(member["status"])
		statusCounts[status]++
		if isFanoutTerminalStatus(status) {
			terminal++
		}
	}
	if terminal == len(members) {
		if statusCounts["failed"] > 0 {
			return "failed"
		}
		if statusCounts["timed_out"] > 0 {
			return "timed_out"
		}
		if statusCounts["stopped"] > 0 {
			return "stopped"
		}
		return "completed"
	}
	if terminal > 0 {
		return "partial"
	}
	return "running"
}

func isFanoutTerminalStatus(status string) bool {
	switch status {
	case "completed", "failed", "stopped", "timed_out":
		return true
	default:
		return false
	}
}

func ariFanoutStatus(ctx context.Context, store *globaldb.Store, scope AriToolScope, input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	groupID := stringValue(body, "fanout_group_id")
	if groupID == "" {
		return AriToolCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "fanout_group_id"})
	}
	group, err := store.GetFanoutGroup(ctx, groupID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return AriToolCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_fanout_group", "fanout_group_id": groupID})
		}
		return AriToolCallResponse{}, err
	}
	if err := validateFanoutReadScope(scope, group, stringValue(body, "source_session_id")); err != nil {
		return AriToolCallResponse{}, err
	}
	members, err := ariFanoutMembersForGroup(ctx, store, group)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	return AriToolCallResponse{Status: "ok", Output: map[string]any{"fanout_group_id": group.FanoutGroupID, "workspace_id": group.WorkspaceID, "source_session_id": group.SourceSessionID, "source_agent_id": group.SourceAgentID, "request_agent_message_id": group.RequestAgentMessageID, "status": fanoutWaitStatus(members), "members": members}}, nil
}

func ariInboxList(ctx context.Context, store *globaldb.Store, scope AriToolScope, input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	sourceSessionID, err := scopedAriInboxSourceSessionID(scope, stringValue(body, "source_session_id"))
	if err != nil {
		return AriToolCallResponse{}, err
	}
	items, err := store.ListInboxItems(ctx, scope.WorkspaceID, sourceSessionID)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	unreadOnly := boolValue(body, "unread_only")
	outputItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if unreadOnly && item.Status != "unread" {
			continue
		}
		outputItems = append(outputItems, ariInboxItemOutput(item))
	}
	return AriToolCallResponse{Status: "ok", Output: map[string]any{"workspace_id": scope.WorkspaceID, "source_session_id": sourceSessionID, "items": outputItems}}, nil
}

func ariInboxCount(ctx context.Context, store *globaldb.Store, scope AriToolScope, input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	sourceSessionID, err := scopedAriInboxSourceSessionID(scope, stringValue(body, "source_session_id"))
	if err != nil {
		return AriToolCallResponse{}, err
	}
	counts, err := store.CountInboxItems(ctx, scope.WorkspaceID, sourceSessionID)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	return AriToolCallResponse{Status: "ok", Output: map[string]any{"workspace_id": scope.WorkspaceID, "source_session_id": sourceSessionID, "total_count": int(counts.TotalCount), "unread_count": int(counts.UnreadCount), "read_count": int(counts.ReadCount)}}, nil
}

func ariInboxMarkRead(ctx context.Context, store *globaldb.Store, scope AriToolScope, input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	sourceSessionID, err := scopedAriInboxSourceSessionID(scope, stringValue(body, "source_session_id"))
	if err != nil {
		return AriToolCallResponse{}, err
	}
	inboxItemIDs, err := stringSliceValue(body, "inbox_item_ids")
	if err != nil {
		return AriToolCallResponse{}, err
	}
	if len(inboxItemIDs) == 0 {
		return AriToolCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "inbox_item_ids"})
	}
	marked, err := store.MarkInboxItemsRead(ctx, scope.WorkspaceID, sourceSessionID, inboxItemIDs)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	return AriToolCallResponse{Status: "ok", Output: map[string]any{"workspace_id": scope.WorkspaceID, "source_session_id": sourceSessionID, "marked_count": int(marked)}}, nil
}

// ariToolsEventsNextMaxTimeoutMS bounds how long an agent tool call may hold
// a server-side event wait.
const ariToolsEventsNextMaxTimeoutMS = 60_000

func ariWorkspaceEventsNext(ctx context.Context, store *globaldb.Store, scope AriToolScope, input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	subscription, err := scopedAriWorkspaceEventSubscription(ctx, store, scope, stringValue(body, "subscription_id"))
	if err != nil {
		return AriToolCallResponse{}, err
	}
	limit, err := optionalNonNegativeIntValue(body, "limit")
	if err != nil {
		return AriToolCallResponse{}, err
	}
	minEvents, err := optionalNonNegativeIntValue(body, "min_events")
	if err != nil {
		return AriToolCallResponse{}, err
	}
	timeoutMS, err := optionalNonNegativeIntValue(body, "timeout_ms")
	if err != nil {
		return AriToolCallResponse{}, err
	}
	if timeoutMS > ariToolsEventsNextMaxTimeoutMS {
		return AriToolCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_wait_timeout", "timeout_ms": timeoutMS, "max_timeout_ms": ariToolsEventsNextMaxTimeoutMS})
	}
	if minEvents > 0 && timeoutMS <= 0 {
		return AriToolCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_wait_timeout", "min_events": minEvents})
	}
	response, err := workspaceEventsNext(ctx, store, WorkspaceEventsNextRequest{SubscriptionID: subscription.SubscriptionID, Limit: limit, MinEvents: minEvents, TimeoutMS: timeoutMS})
	if err != nil {
		return AriToolCallResponse{}, err
	}
	events := make([]map[string]any, 0, len(response.Events))
	for _, event := range response.Events {
		events = append(events, ariWorkspaceEventResponseOutput(event))
	}
	output := map[string]any{"subscription_id": subscription.SubscriptionID, "workspace_id": subscription.WorkspaceID, "owner_session_id": subscription.OwnerSessionID, "count": len(events), "events": events}
	if minEvents > 0 {
		output["wait_status"] = response.WaitStatus
		output["wait_timed_out"] = response.WaitTimedOut
	}
	return AriToolCallResponse{Status: "ok", Output: output}, nil
}

func ariWorkspaceEventResponseOutput(event WorkspaceEventResponse) map[string]any {
	return map[string]any{"event_id": event.EventID, "workspace_id": event.WorkspaceID, "sequence": event.Sequence, "event_type": event.EventType, "subject_type": event.SubjectType, "subject_id": event.SubjectID, "producer_type": event.ProducerType, "producer_id": event.ProducerID, "correlation_id": event.CorrelationID, "causation_id": event.CausationID, "payload_json": event.PayloadJSON, "payload_ref_json": event.PayloadRefJSON, "attention_required": event.AttentionRequired, "created_at": event.CreatedAt}
}

func ariWorkspaceEventsAck(ctx context.Context, store *globaldb.Store, scope AriToolScope, input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	subscription, err := scopedAriWorkspaceEventSubscription(ctx, store, scope, stringValue(body, "subscription_id"))
	if err != nil {
		return AriToolCallResponse{}, err
	}
	sequence, ok, err := optionalNonNegativeInt64Value(body, "sequence")
	if err != nil {
		return AriToolCallResponse{}, err
	}
	if !ok {
		return AriToolCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "sequence"})
	}
	if err := store.AckEventSubscription(ctx, subscription.SubscriptionID, sequence); err != nil {
		return AriToolCallResponse{}, workspaceEventRPCError(err)
	}
	acked, err := store.GetEventSubscription(ctx, subscription.SubscriptionID)
	if err != nil {
		return AriToolCallResponse{}, workspaceEventRPCError(err)
	}
	return AriToolCallResponse{Status: "ok", Output: map[string]any{"acked": true, "subscription_id": acked.SubscriptionID, "workspace_id": acked.WorkspaceID, "owner_session_id": acked.OwnerSessionID, "cursor_sequence": acked.CursorSequence, "ack_sequence": acked.AckSequence, "status": acked.Status}}, nil
}

func ariWorkspaceSignalSend(ctx context.Context, store *globaldb.Store, scope AriToolScope, input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	targetType := stringValue(body, "target_type")
	targetID := stringValue(body, "target_id")
	if targetType == "" || targetID == "" {
		missing := "target_type"
		if targetType != "" {
			missing = "target_id"
		}
		return AriToolCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": missing})
	}
	event, err := store.AppendWorkspaceEvent(ctx, globaldb.WorkspaceEvent{EventID: stringValue(body, "event_id"), WorkspaceID: scope.WorkspaceID, EventType: "signal.sent", SubjectType: targetType, SubjectID: targetID, ProducerType: workspaceEventProducerSession, ProducerID: strings.TrimSpace(scope.SourceRunID), CorrelationID: stringValue(body, "correlation_id"), CausationID: stringValue(body, "causation_id"), PayloadJSON: stringValue(body, "payload_json")})
	if err != nil {
		return AriToolCallResponse{}, workspaceEventRPCError(err)
	}
	return AriToolCallResponse{Status: "ok", Output: ariWorkspaceEventOutput(event)}, nil
}

func ariWorkspaceTimerCreate(ctx context.Context, store *globaldb.Store, scope AriToolScope, input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	fireAtRaw := stringValue(body, "fire_at")
	if fireAtRaw == "" {
		return AriToolCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "fire_at"})
	}
	fireAt, err := parseWorkspaceTimerTime(fireAtRaw, "invalid_fire_at")
	if err != nil {
		return AriToolCallResponse{}, err
	}
	timer, err := store.CreateWorkspaceTimer(ctx, globaldb.WorkspaceTimer{TimerID: stringValue(body, "timer_id"), WorkspaceID: scope.WorkspaceID, OwnerSessionID: strings.TrimSpace(scope.SourceRunID), SubscriptionID: stringValue(body, "subscription_id"), SubjectType: stringValue(body, "subject_type"), SubjectID: stringValue(body, "subject_id"), Purpose: stringValue(body, "purpose"), FireAt: fireAt, PayloadJSON: stringValue(body, "payload_json")})
	if err != nil {
		return AriToolCallResponse{}, workspaceTimerRPCError(err)
	}
	return AriToolCallResponse{Status: "ok", Output: ariWorkspaceTimerOutput(timer)}, nil
}

func ariWorkspaceTimerGet(ctx context.Context, store *globaldb.Store, scope AriToolScope, input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	timer, err := scopedAriWorkspaceTimer(ctx, store, scope, stringValue(body, "timer_id"), false)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	return AriToolCallResponse{Status: "ok", Output: ariWorkspaceTimerOutput(timer)}, nil
}

func ariWorkspaceTimerCancel(ctx context.Context, store *globaldb.Store, scope AriToolScope, input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	timer, err := scopedAriWorkspaceTimer(ctx, store, scope, stringValue(body, "timer_id"), true)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	canceled, err := store.CancelWorkspaceTimer(ctx, timer.TimerID)
	if err != nil {
		return AriToolCallResponse{}, workspaceTimerRPCError(err)
	}
	return AriToolCallResponse{Status: "ok", Output: ariWorkspaceTimerOutput(canceled)}, nil
}

func ariWorkspaceDeliveryGet(ctx context.Context, store *globaldb.Store, scope AriToolScope, input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	delivery, err := scopedAriWorkspaceDelivery(ctx, store, scope, stringValue(body, "delivery_id"))
	if err != nil {
		return AriToolCallResponse{}, err
	}
	return AriToolCallResponse{Status: "ok", Output: ariWorkspaceDeliveryOutput(delivery)}, nil
}

func ariWorkspaceDeliveriesListDue(ctx context.Context, store *globaldb.Store, scope AriToolScope, input any) (AriToolCallResponse, error) {
	body, err := inputMap(input)
	if err != nil {
		return AriToolCallResponse{}, err
	}
	now := time.Now().UTC()
	if rawNow := stringValue(body, "now"); rawNow != "" {
		now, err = parseWorkspaceTimerTime(rawNow, "invalid_now")
		if err != nil {
			return AriToolCallResponse{}, err
		}
	}
	limit, err := optionalNonNegativeIntValue(body, "limit")
	if err != nil {
		return AriToolCallResponse{}, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > ariWorkspaceDeliveriesListDueMaxLimit {
		return AriToolCallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_limit", "limit": limit, "max_limit": ariWorkspaceDeliveriesListDueMaxLimit})
	}
	due, err := store.ListDuePendingDeliveriesForScope(ctx, now, strings.TrimSpace(scope.WorkspaceID), strings.TrimSpace(scope.SourceRunID), limit)
	if err != nil {
		return AriToolCallResponse{}, workspaceDeliveryRPCError(err)
	}
	scoped := make([]globaldb.PendingDelivery, 0, limit)
	for _, delivery := range due {
		visible, err := ariPendingDeliveryVisibleToScope(ctx, store, scope, delivery)
		if err != nil {
			return AriToolCallResponse{}, err
		}
		if !visible {
			continue
		}
		scoped = append(scoped, delivery)
		if len(scoped) == limit {
			break
		}
	}
	return AriToolCallResponse{Status: "ok", Output: map[string]any{"workspace_id": strings.TrimSpace(scope.WorkspaceID), "count": len(scoped), "deliveries": ariWorkspaceDeliveryOutputs(scoped)}}, nil
}

func scopedAriWorkspaceEventSubscription(ctx context.Context, store *globaldb.Store, scope AriToolScope, subscriptionID string) (globaldb.EventSubscription, error) {
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" {
		return globaldb.EventSubscription{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "subscription_id"})
	}
	subscription, err := store.GetEventSubscription(ctx, subscriptionID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return globaldb.EventSubscription{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_event_subscription", "subscription_id": subscriptionID})
		}
		return globaldb.EventSubscription{}, err
	}
	if subscription.WorkspaceID != strings.TrimSpace(scope.WorkspaceID) {
		return globaldb.EventSubscription{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "subscription_scope_mismatch", "subscription_id": subscription.SubscriptionID, "workspace_id": strings.TrimSpace(scope.WorkspaceID), "subscription_workspace_id": subscription.WorkspaceID})
	}
	ownerSessionID := strings.TrimSpace(subscription.OwnerSessionID)
	if ownerSessionID != "" && ownerSessionID != strings.TrimSpace(scope.SourceRunID) {
		return globaldb.EventSubscription{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "subscription_scope_mismatch", "subscription_id": subscription.SubscriptionID, "owner_session_id": ownerSessionID, "scope_source_run_id": strings.TrimSpace(scope.SourceRunID)})
	}
	return subscription, nil
}

func ariWorkspaceEventOutput(event globaldb.WorkspaceEvent) map[string]any {
	return map[string]any{"event_id": event.EventID, "workspace_id": event.WorkspaceID, "sequence": event.Sequence, "event_type": event.EventType, "subject_type": event.SubjectType, "subject_id": event.SubjectID, "producer_type": event.ProducerType, "producer_id": event.ProducerID, "correlation_id": event.CorrelationID, "causation_id": event.CausationID, "payload_json": event.PayloadJSON, "payload_ref_json": event.PayloadRefJSON, "attention_required": event.AttentionRequired, "created_at": event.CreatedAt.UTC().Format(time.RFC3339Nano)}
}

const ariWorkspaceDeliveriesListDueMaxLimit = 1000

func scopedAriWorkspaceTimer(ctx context.Context, store *globaldb.Store, scope AriToolScope, timerID string, requireOwner bool) (globaldb.WorkspaceTimer, error) {
	timerID = strings.TrimSpace(timerID)
	if timerID == "" {
		return globaldb.WorkspaceTimer{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "timer_id"})
	}
	timer, err := store.GetWorkspaceTimer(ctx, timerID)
	if err != nil {
		return globaldb.WorkspaceTimer{}, workspaceTimerRPCError(err)
	}
	if timer.WorkspaceID != strings.TrimSpace(scope.WorkspaceID) {
		return globaldb.WorkspaceTimer{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "timer_scope_mismatch", "timer_id": timer.TimerID, "workspace_id": strings.TrimSpace(scope.WorkspaceID), "timer_workspace_id": timer.WorkspaceID})
	}
	ownerSessionID := strings.TrimSpace(timer.OwnerSessionID)
	if requireOwner && ownerSessionID != strings.TrimSpace(scope.SourceRunID) {
		return globaldb.WorkspaceTimer{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "timer_scope_mismatch", "timer_id": timer.TimerID, "owner_session_id": ownerSessionID, "scope_source_run_id": strings.TrimSpace(scope.SourceRunID)})
	}
	return timer, nil
}

func ariWorkspaceTimerOutput(timer globaldb.WorkspaceTimer) map[string]any {
	return map[string]any{"timer_id": timer.TimerID, "workspace_id": timer.WorkspaceID, "owner_session_id": timer.OwnerSessionID, "subscription_id": timer.SubscriptionID, "subject_type": timer.SubjectType, "subject_id": timer.SubjectID, "purpose": timer.Purpose, "status": timer.Status, "fire_at": timer.FireAt.UTC().Format(time.RFC3339Nano), "payload_json": timer.PayloadJSON, "fired_event_id": timer.FiredEventID, "created_at": timer.CreatedAt.UTC().Format(time.RFC3339Nano), "updated_at": timer.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}

func scopedAriWorkspaceDelivery(ctx context.Context, store *globaldb.Store, scope AriToolScope, deliveryID string) (globaldb.PendingDelivery, error) {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		return globaldb.PendingDelivery{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "delivery_id"})
	}
	delivery, err := store.GetPendingDelivery(ctx, deliveryID)
	if err != nil {
		return globaldb.PendingDelivery{}, workspaceDeliveryRPCError(err)
	}
	visible, err := ariPendingDeliveryVisibleToScope(ctx, store, scope, delivery)
	if err != nil {
		return globaldb.PendingDelivery{}, err
	}
	if !visible {
		return globaldb.PendingDelivery{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "delivery_scope_mismatch", "delivery_id": delivery.DeliveryID, "workspace_id": strings.TrimSpace(scope.WorkspaceID), "delivery_workspace_id": delivery.WorkspaceID})
	}
	return delivery, nil
}

func ariPendingDeliveryVisibleToScope(ctx context.Context, store *globaldb.Store, scope AriToolScope, delivery globaldb.PendingDelivery) (bool, error) {
	if delivery.WorkspaceID != strings.TrimSpace(scope.WorkspaceID) {
		return false, nil
	}
	subscriptionID := strings.TrimSpace(delivery.SubscriptionID)
	if subscriptionID == "" {
		return true, nil
	}
	subscription, err := store.GetEventSubscription(ctx, subscriptionID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return true, nil
		}
		return false, err
	}
	ownerSessionID := strings.TrimSpace(subscription.OwnerSessionID)
	return ownerSessionID == "" || ownerSessionID == strings.TrimSpace(scope.SourceRunID), nil
}

func ariWorkspaceDeliveryOutputs(deliveries []globaldb.PendingDelivery) []map[string]any {
	outputs := make([]map[string]any, 0, len(deliveries))
	for _, delivery := range deliveries {
		outputs = append(outputs, ariWorkspaceDeliveryOutput(delivery))
	}
	return outputs
}

func ariWorkspaceDeliveryOutput(delivery globaldb.PendingDelivery) map[string]any {
	output := map[string]any{"delivery_id": delivery.DeliveryID, "workspace_id": delivery.WorkspaceID, "subscription_id": delivery.SubscriptionID, "target_type": delivery.TargetType, "target_id": delivery.TargetID, "delivery_policy_json": delivery.DeliveryPolicyJSON, "event_ids": append([]string(nil), delivery.EventIDs...), "status": delivery.Status, "attempts": delivery.Attempts, "last_error": delivery.LastError, "created_at": delivery.CreatedAt.UTC().Format(time.RFC3339Nano), "updated_at": delivery.UpdatedAt.UTC().Format(time.RFC3339Nano)}
	if delivery.NextAttemptAt != nil {
		output["next_attempt_at"] = delivery.NextAttemptAt.UTC().Format(time.RFC3339Nano)
	}
	if delivery.DeadlineAt != nil {
		output["deadline_at"] = delivery.DeadlineAt.UTC().Format(time.RFC3339Nano)
	}
	if delivery.TerminalAt != nil {
		output["terminal_at"] = delivery.TerminalAt.UTC().Format(time.RFC3339Nano)
	}
	return output
}

func scopedAriInboxSourceSessionID(scope AriToolScope, inputSourceSessionID string) (string, error) {
	sourceSessionID := strings.TrimSpace(inputSourceSessionID)
	if sourceSessionID == "" {
		sourceSessionID = strings.TrimSpace(scope.SourceRunID)
	}
	if sourceSessionID != strings.TrimSpace(scope.SourceRunID) {
		return "", rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "source_scope_mismatch", "source_session_id": sourceSessionID, "scope_source_run_id": strings.TrimSpace(scope.SourceRunID)})
	}
	return sourceSessionID, nil
}

func ariInboxItemOutput(item globaldb.InboxItem) map[string]any {
	return map[string]any{"inbox_item_id": item.InboxItemID, "workspace_id": item.WorkspaceID, "source_session_id": item.SourceSessionID, "workspace_event_id": item.WorkspaceEventID, "event_type": item.EventType, "fanout_group_id": item.FanoutGroupID, "fanout_member_id": item.FanoutMemberID, "worker_session_id": item.WorkerSessionID, "final_response_id": item.FinalResponseID, "kind": item.Kind, "status": item.Status, "attention_required": item.AttentionRequired, "summary": item.Summary, "created_at": formatAriInboxTime(item.CreatedAt), "updated_at": formatAriInboxTime(item.UpdatedAt)}
}

func formatAriInboxTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func validateFanoutReadScope(scope AriToolScope, group globaldb.FanoutGroup, inputSourceSessionID string) error {
	if group.WorkspaceID != strings.TrimSpace(scope.WorkspaceID) || group.SourceSessionID != strings.TrimSpace(scope.SourceRunID) {
		return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "fanout_scope_mismatch", "fanout_group_id": group.FanoutGroupID, "workspace_id": strings.TrimSpace(scope.WorkspaceID), "source_session_id": strings.TrimSpace(scope.SourceRunID), "fanout_workspace_id": group.WorkspaceID, "fanout_source_session_id": group.SourceSessionID})
	}
	if inputSourceSessionID != "" && inputSourceSessionID != strings.TrimSpace(scope.SourceRunID) {
		return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "source_scope_mismatch", "source_session_id": inputSourceSessionID, "scope_source_run_id": strings.TrimSpace(scope.SourceRunID)})
	}
	return nil
}

func validateAriFanoutCanStart(ctx context.Context, store *globaldb.Store, scope AriToolScope, sourceSessionID, fanoutGroupID string, targetProfileIDs, contextExcerptIDs []string) error {
	sourceRun, err := store.GetHarnessSession(ctx, sourceSessionID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_source_session", "source_session_id": sourceSessionID, "workspace_id": scope.WorkspaceID, "start_invoked": false})
		}
		return err
	}
	if sourceRun.WorkspaceID != scope.WorkspaceID {
		return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "source_workspace_mismatch", "source_session_id": sourceSessionID, "source_workspace_id": sourceRun.WorkspaceID, "workspace_id": scope.WorkspaceID, "start_invoked": false})
	}
	if sourceRun.AgentID != strings.TrimSpace(scope.ProfileID) {
		return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "source_profile_mismatch", "source_session_id": sourceSessionID, "source_agent_id": sourceRun.AgentID, "scope_profile_id": strings.TrimSpace(scope.ProfileID), "start_invoked": false})
	}
	if err := requireWorkspaceCanStartRuntime(ctx, store, sourceRun.WorkspaceID); err != nil {
		return err
	}
	if strings.TrimSpace(fanoutGroupID) != "" {
		if _, err := store.GetFanoutGroup(ctx, fanoutGroupID); err == nil {
			return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "fanout_group_exists", "fanout_group_id": strings.TrimSpace(fanoutGroupID), "start_invoked": false})
		} else if !errors.Is(err, globaldb.ErrNotFound) {
			return err
		}
	}
	excerpts := make([]globaldb.ContextExcerpt, 0, len(contextExcerptIDs))
	for _, contextExcerptID := range contextExcerptIDs {
		excerpt, excerptErr := store.GetContextExcerpt(ctx, contextExcerptID)
		if errors.Is(excerptErr, globaldb.ErrNotFound) {
			return rpc.NewHandlerError(rpc.InvalidParams, excerptErr.Error(), map[string]any{"reason": "unknown_context_excerpt", "context_excerpt_id": contextExcerptID, "start_invoked": false})
		}
		if excerptErr != nil {
			return excerptErr
		}
		if excerpt.WorkspaceID != sourceRun.WorkspaceID {
			return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "context_excerpt_mismatch", "context_excerpt_id": contextExcerptID, "start_invoked": false})
		}
		if strings.TrimSpace(excerpt.TargetSessionID) != "" {
			return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "context_excerpt_target_session_mismatch", "context_excerpt_id": contextExcerptID, "target_session_id": excerpt.TargetSessionID, "start_invoked": false})
		}
		excerpts = append(excerpts, excerpt)
	}
	for _, profileID := range targetProfileIDs {
		targetAgent, err := store.GetHarnessSessionConfig(ctx, profileID)
		if errors.Is(err, globaldb.ErrNotFound) {
			resolved, resolveErr := resolveStoredProfile(ctx, store, sourceRun.WorkspaceID, profileID)
			if errors.Is(resolveErr, globaldb.ErrNotFound) {
				return unknownProfileError(profileID)
			}
			if resolveErr != nil {
				return resolveErr
			}
			targetAgent = globaldb.HarnessSessionConfig{AgentID: resolved.ProfileID, WorkspaceID: resolved.WorkspaceID}
		} else if err != nil {
			return err
		}
		if targetAgent.WorkspaceID != sourceRun.WorkspaceID {
			return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "target_workspace_mismatch", "target_agent_id": targetAgent.AgentID, "source_workspace_id": sourceRun.WorkspaceID, "target_workspace_id": targetAgent.WorkspaceID, "start_invoked": false})
		}
		if strings.TrimSpace(fanoutGroupID) != "" {
			workerSessionID := strings.TrimSpace(fanoutGroupID) + "-c" + stableRuntimeAgentIDSegment(profileID) + "-run"
			if _, err := store.GetHarnessSession(ctx, workerSessionID); err == nil {
				return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "fanout_worker_session_exists", "worker_session_id": workerSessionID, "target_profile_id": profileID, "start_invoked": false})
			} else if !errors.Is(err, globaldb.ErrNotFound) {
				return err
			}
		}
		for _, excerpt := range excerpts {
			if excerpt.TargetAgentID != "" && excerpt.TargetAgentID != targetAgent.AgentID {
				return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "context_excerpt_mismatch", "context_excerpt_id": excerpt.ContextExcerptID, "target_profile_id": profileID, "start_invoked": false})
			}
		}
	}
	return nil
}

func readConfigString(values map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(values[key], &value)
	return strings.TrimSpace(value)
}

func inputMap(input any) (map[string]any, error) {
	canonical, err := canonicalJSON(input)
	if err != nil {
		return nil, ariToolError("invalid_request_body", "tool request body is invalid")
	}
	values := map[string]any{}
	if err := json.Unmarshal(canonical, &values); err != nil {
		return nil, ariToolError("invalid_request_body", "tool request body must be an object")
	}
	return values, nil
}

func optionalString(values map[string]any, key string) (string, bool) {
	value, ok := values[key]
	if !ok || value == nil {
		return "", ok
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text), true
	}
	return strings.TrimSpace(fmt.Sprint(value)), true
}

func stringValue(values map[string]any, key string) string {
	value, _ := optionalString(values, key)
	return value
}

func boolValue(values map[string]any, key string) bool {
	value, ok := values[key]
	if !ok || value == nil {
		return false
	}
	if typed, ok := value.(bool); ok {
		return typed
	}
	return strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "true")
}

func optionalNonNegativeIntValue(values map[string]any, key string) (int, error) {
	value, ok, err := optionalNonNegativeInt64Value(values, key)
	if err != nil || !ok {
		return 0, err
	}
	return int(value), nil
}

func optionalNonNegativeInt64Value(values map[string]any, key string) (int64, bool, error) {
	value, ok := values[key]
	if !ok || value == nil {
		return 0, false, nil
	}
	var parsed int64
	switch typed := value.(type) {
	case int:
		parsed = int64(typed)
	case int64:
		parsed = typed
	case float64:
		parsed = int64(typed)
		if float64(parsed) != typed {
			return 0, true, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_integer", "field": key})
		}
	case string:
		var err error
		parsed, err = strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, true, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_integer", "field": key})
		}
	default:
		return 0, true, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_integer", "field": key})
	}
	if parsed < 0 {
		return 0, true, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_integer", "field": key})
	}
	return parsed, true, nil
}

func stringSliceValue(values map[string]any, key string) ([]string, error) {
	value, ok := values[key]
	if !ok || value == nil {
		return nil, nil
	}
	raw, ok := value.([]any)
	if !ok {
		if stringsValue, ok := value.([]string); ok {
			result := make([]string, 0, len(stringsValue))
			for _, text := range stringsValue {
				if trimmed := strings.TrimSpace(text); trimmed != "" {
					result = append(result, trimmed)
				}
			}
			return result, nil
		}
		return nil, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_string_list", "field": key, "start_invoked": false})
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if trimmed := strings.TrimSpace(fmt.Sprint(item)); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result, nil
}

func mapValue(values map[string]any, key string) map[string]any {
	if value, ok := values[key].(map[string]any); ok {
		return value
	}
	return map[string]any{}
}

func ariToolError(reason, message string) error {
	return rpc.NewHandlerError(rpc.InvalidParams, reason+": "+message, map[string]any{"reason": reason})
}
