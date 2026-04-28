package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	{Name: "ari.defaults.get", Description: "Read Ari default harness, model, and invocation settings", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true},
	{Name: "ari.defaults.set", Description: "Update Ari defaults after scoped approval", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ApprovalRequired: true},
	{Name: "ari.profile.draft", Description: "Draft a profile spec without persisting it", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true},
	{Name: "ari.profile.save", Description: "Persist an approved profile spec", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ApprovalRequired: true},
	{Name: "ari.self_check", Description: "Read Ari daemon, config, workspace, profile, and harness health", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true},
	{Name: "ari.run.explain_latest", Description: "Summarize the latest available Ari run evidence", ScopeRequired: true, RequiredScopeFields: ariToolScopeFields(), ReadOnly: true},
}

func ariToolScopeFields() []string {
	return []string{"source_run_id", "workspace_id", "profile_id", "profile_name", "tool_name", "within_default_scope"}
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
	if _, err := store.GetSession(ctx, req.Scope.WorkspaceID); err != nil {
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
		return d.ariDefaultsSet(req.Input)
	case "ari.profile.draft":
		return ariProfileDraft(req.Input)
	case "ari.profile.save":
		return ariProfileSave(ctx, store, req.Scope, req.Input)
	case "ari.self_check":
		return d.ariSelfCheck(ctx, store, req.Scope)
	case "ari.run.explain_latest":
		return ariRunExplainLatest(ctx, store, req.Scope)
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
	if hasInvocationClass && invocationClass != "" && invocationClass != string(HarnessInvocationAgent) && invocationClass != string(HarnessInvocationTemporary) {
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
	name := strings.TrimSpace(fmt.Sprint(body["name"]))
	if name == "" {
		return AriToolCallResponse{}, ariToolError("missing_profile_name", "profile name is required")
	}
	harness := strings.TrimSpace(fmt.Sprint(body["harness"]))
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
	profile, err := createStoredAgentProfile(ctx, store, AgentProfileCreateRequest{WorkspaceID: scope.WorkspaceID, Name: stringValue(body, "name"), Harness: stringValue(body, "harness"), Model: stringValue(body, "model"), Prompt: stringValue(body, "prompt"), InvocationClass: HarnessInvocationClass(stringValue(body, "invocation_class")), Defaults: mapValue(body, "defaults")})
	if err != nil {
		return AriToolCallResponse{}, err
	}
	return AriToolCallResponse{Status: "ok", ApplicationStatus: "applied_live", Output: map[string]any{"profile_id": profile.ProfileID, "workspace_id": profile.WorkspaceID, "name": profile.Name, "harness": profile.Harness, "model": profile.Model, "prompt": profile.Prompt, "invocation_class": string(profile.InvocationClass)}}, nil
}

func (d *Daemon) ariSelfCheck(ctx context.Context, store *globaldb.Store, scope AriToolScope) (AriToolCallResponse, error) {
	_, cfgErr := readJSONConfig(d.configPath)
	_, wsErr := store.GetSession(ctx, scope.WorkspaceID)
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
	return AriToolCallResponse{Status: "ok", Output: map[string]any{"summary": latest.Text, "run_available": true, "run_id": latest.RunID, "final_response_id": latest.FinalResponseID}}, nil
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

func mapValue(values map[string]any, key string) map[string]any {
	if value, ok := values[key].(map[string]any); ok {
		return value
	}
	return map[string]any{}
}

func ariToolError(reason, message string) error {
	return rpc.NewHandlerError(rpc.InvalidParams, reason+": "+message, map[string]any{"reason": reason})
}
