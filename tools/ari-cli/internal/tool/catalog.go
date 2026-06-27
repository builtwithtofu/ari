package tool

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

type ListRequest struct{}

type ListResponse struct {
	Tools []Schema `json:"tools"`
}

type Schema struct {
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	ScopeRequired       bool     `json:"scope_required"`
	RequiredScopeFields []string `json:"required_scope_fields"`
	ApprovalRequired    bool     `json:"approval_required"`
	ReadOnly            bool     `json:"read_only"`
	OperationKind       string   `json:"operation_kind"`
	TrustChoices        []string `json:"trust_choices"`
}

type CallRequest struct {
	Name     string   `json:"name"`
	Scope    Scope    `json:"scope"`
	Input    any      `json:"input,omitempty"`
	Approval Approval `json:"approval,omitempty"`
}

type Scope struct {
	SourceRunID        string `json:"source_run_id"`
	WorkspaceID        string `json:"workspace_id"`
	ProfileID          string `json:"profile_id"`
	ProfileName        string `json:"profile_name"`
	ToolName           string `json:"tool_name"`
	WithinDefaultScope bool   `json:"within_default_scope"`
}

type Approval struct {
	ApprovalID  string        `json:"approval_id"`
	ApprovedBy  string        `json:"approved_by"`
	ApprovedAt  string        `json:"approved_at"`
	Scope       ApprovalScope `json:"scope"`
	RequestHash string        `json:"request_hash"`
}

type ApprovalScope struct {
	WorkspaceID string `json:"workspace_id"`
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name"`
	ToolName    string `json:"tool_name"`
	SourceRunID string `json:"source_run_id"`
}

type CallResponse struct {
	Status            string         `json:"status"`
	ApplicationStatus string         `json:"application_status,omitempty"`
	Output            map[string]any `json:"output,omitempty"`
}

type StoredApproval struct {
	Approval Approval `json:"approval"`
	Consumed bool     `json:"consumed"`
}

type Handler[D any] func(ctx context.Context, deps D, store *globaldb.Store, scope Scope, input any) (CallResponse, error)

type OperationDef struct {
	Type           string
	Scope          string
	RequestSummary string
	TrustDecision  string
	RollbackData   map[string]string
}

type Definition[D any] struct {
	Schema Schema
	// RequiresDefaultScope rejects calls outside an in-scope helper.
	RequiresDefaultScope bool
	// Operation is nil for calls that do not record a daemon operation.
	Operation *OperationDef
	Handler   Handler[D]
}

type OperationRecord struct {
	WorkspaceID     string
	OperationType   string
	OperationKind   string
	Actor           string
	Source          string
	Scope           string
	RequestSummary  string
	TrustDecision   string
	RollbackData    map[string]string
	PayloadSnapshot map[string]string
}

type OperationRunner func(ctx context.Context, record OperationRecord, fn func(context.Context) error) error

type CallOptions struct {
	OperationSource string
	OperationRunner OperationRunner
}

type Catalog[D any] struct {
	definitions []Definition[D]
}

const (
	TrustApprovedOnce        = "approved_once"
	TrustScopedSourceSession = "scoped_source_session"
)

func ReadOnlySchema(name, description, operationKind string) Schema {
	return Schema{Name: name, Description: description, ScopeRequired: true, RequiredScopeFields: ScopeFields(), ReadOnly: true, OperationKind: operationKind}
}

func MutatingSchema(name, description, operationKind string, approvalRequired bool) Schema {
	schema := Schema{Name: name, Description: description, ScopeRequired: true, RequiredScopeFields: ScopeFields(), OperationKind: operationKind}
	if approvalRequired {
		schema.ApprovalRequired = true
		schema.TrustChoices = TrustChoices()
	}
	return schema
}

func ScopeFields() []string {
	return []string{"source_run_id", "workspace_id", "profile_id", "profile_name", "tool_name", "within_default_scope"}
}

func TrustChoices() []string {
	return []string{"trust_once", "trust_always_by_operation_type", "deny"}
}

func NewCatalog[D any](definitions []Definition[D]) (Catalog[D], error) {
	if err := ValidateDefinitions(definitions); err != nil {
		return Catalog[D]{}, err
	}
	copied := append([]Definition[D](nil), definitions...)
	return Catalog[D]{definitions: copied}, nil
}

func (c Catalog[D]) Schemas() []Schema {
	tools := make([]Schema, 0, len(c.definitions))
	for _, definition := range c.definitions {
		tools = append(tools, definition.Schema)
	}
	return tools
}

func (c Catalog[D]) Definitions() []Definition[D] {
	return append([]Definition[D](nil), c.definitions...)
}

func (c Catalog[D]) Call(ctx context.Context, deps D, store *globaldb.Store, req CallRequest, opts CallOptions) (CallResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return CallResponse{}, NewError("missing_tool_name", "tool name is required")
	}
	if strings.TrimSpace(req.Scope.ToolName) == "" {
		req.Scope.ToolName = name
	}
	if req.Scope.ToolName != name {
		return CallResponse{}, NewError("scope_tool_mismatch", "scope tool_name must match requested tool")
	}
	definition, ok := Lookup(c.definitions, name)
	if !ok {
		return CallResponse{}, NewError("unknown_tool", "unknown Ari tool")
	}
	if err := ValidateScope(req.Scope); err != nil {
		return CallResponse{}, err
	}
	if _, err := store.GetWorkspace(ctx, req.Scope.WorkspaceID); err != nil {
		return CallResponse{}, err
	}
	if definition.RequiresDefaultScope && !req.Scope.WithinDefaultScope {
		return CallResponse{}, NewError("handoff_required", "defaults writes require an in-scope helper approval")
	}
	if definition.Schema.ApprovalRequired {
		if err := ValidateAndConsumeApproval(ctx, store, req); err != nil {
			return CallResponse{}, err
		}
	}
	if definition.Operation == nil {
		return definition.Handler(ctx, deps, store, req.Scope, req.Input)
	}
	return callWithOperation(ctx, deps, store, req, definition, opts)
}

func callWithOperation[D any](ctx context.Context, deps D, store *globaldb.Store, req CallRequest, definition Definition[D], opts CallOptions) (CallResponse, error) {
	if opts.OperationRunner == nil {
		return CallResponse{}, fmt.Errorf("operation runner is required for mutating Ari tool %s", definition.Schema.Name)
	}
	operation := definition.Operation
	requestHash := req.Approval.RequestHash
	payload := map[string]string{"tool": req.Name, "workspace_id": req.Scope.WorkspaceID}
	if operation.TrustDecision == TrustScopedSourceSession {
		var err error
		requestHash, err = HashRequest(req.Name, req.Input)
		if err != nil {
			return CallResponse{}, err
		}
		payload["source_run_id"] = req.Scope.SourceRunID
	}
	payload["request_hash"] = requestHash
	record := OperationRecord{OperationType: operation.Type, OperationKind: definition.Schema.OperationKind, Actor: req.Scope.ProfileName, Source: opts.OperationSource, Scope: operation.Scope, RequestSummary: operation.RequestSummary, TrustDecision: operation.TrustDecision, RollbackData: operation.RollbackData, PayloadSnapshot: payload}
	if operation.Scope == globaldb.OperationScopeWorkspace {
		record.WorkspaceID = req.Scope.WorkspaceID
	}
	var response CallResponse
	err := opts.OperationRunner(ctx, record, func(ctx context.Context) error {
		var err error
		response, err = definition.Handler(ctx, deps, store, req.Scope, req.Input)
		return err
	})
	return response, err
}

func ValidateDefinitions[D any](definitions []Definition[D]) error {
	for _, definition := range definitions {
		if definition.Operation == nil {
			continue
		}
		switch definition.Operation.TrustDecision {
		case TrustApprovedOnce:
			if !definition.Schema.ApprovalRequired {
				return fmt.Errorf("tool %s uses approved-once operation trust without approval requirement", definition.Schema.Name)
			}
		case TrustScopedSourceSession:
			if definition.Schema.ApprovalRequired {
				return fmt.Errorf("tool %s requires approval but uses scoped-source-session trust", definition.Schema.Name)
			}
		default:
			return fmt.Errorf("tool %s operation trust decision %q is not recognized", definition.Schema.Name, definition.Operation.TrustDecision)
		}
	}
	return nil
}

func Lookup[D any](definitions []Definition[D], name string) (Definition[D], bool) {
	name = strings.TrimSpace(name)
	for _, definition := range definitions {
		if definition.Schema.Name == name {
			return definition, true
		}
	}
	return Definition[D]{}, false
}

func ValidateScope(scope Scope) error {
	if strings.TrimSpace(scope.SourceRunID) == "" || strings.TrimSpace(scope.WorkspaceID) == "" || strings.TrimSpace(scope.ProfileID) == "" || strings.TrimSpace(scope.ProfileName) == "" || strings.TrimSpace(scope.ToolName) == "" {
		return NewError("missing_scope", "Ari tool calls require source_run_id, workspace_id, profile_id, profile_name, and tool_name scope fields")
	}
	return nil
}

func ValidateAndConsumeApproval(ctx context.Context, store *globaldb.Store, req CallRequest) error {
	approval := req.Approval
	approvalID := strings.TrimSpace(approval.ApprovalID)
	if approvalID == "" || strings.TrimSpace(approval.ApprovedBy) == "" || strings.TrimSpace(approval.ApprovedAt) == "" || strings.TrimSpace(approval.RequestHash) == "" {
		return inputError("approval_required", map[string]any{"tool_name": req.Name})
	}
	stored, err := LoadApproval(ctx, store, approvalID)
	if err != nil {
		return err
	}
	if stored.Consumed {
		return inputError("approval_reused", map[string]any{"approval_id": approvalID})
	}
	if stored.Approval != approval {
		return inputError("approval_mismatch", map[string]any{"approval_id": approvalID})
	}
	approvedAt, err := time.Parse(time.RFC3339, approval.ApprovedAt)
	if err != nil {
		return inputError("approval_invalid", map[string]any{"approval_id": approvalID})
	}
	if time.Since(approvedAt) > 10*time.Minute || time.Until(approvedAt) > time.Minute {
		return inputError("approval_stale", map[string]any{"approval_id": approvalID})
	}
	if approval.Scope.WorkspaceID != req.Scope.WorkspaceID || approval.Scope.ProfileID != req.Scope.ProfileID || approval.Scope.ProfileName != req.Scope.ProfileName || approval.Scope.ToolName != req.Name || approval.Scope.SourceRunID != req.Scope.SourceRunID {
		return inputError("approval_wrong_scope", map[string]any{"approval_id": approvalID})
	}
	expectedHash, err := HashRequest(req.Name, req.Input)
	if err != nil {
		return err
	}
	if approval.RequestHash != expectedHash {
		return inputError("approval_wrong_hash", map[string]any{"approval_id": approvalID})
	}
	oldValue, newValue, err := encodeConsumedApproval(stored)
	if err != nil {
		return err
	}
	swapped, err := store.CompareAndSwapMeta(ctx, approvalMetaKey(approvalID), oldValue, newValue)
	if err != nil {
		return fmt.Errorf("consume Ari approval: %w", err)
	}
	if !swapped {
		latest, err := LoadApproval(ctx, store, approvalID)
		if err != nil {
			return err
		}
		if latest.Consumed {
			return inputError("approval_reused", map[string]any{"approval_id": approvalID})
		}
		return inputError("approval_mismatch", map[string]any{"approval_id": approvalID})
	}
	return nil
}

func StoreApproval(ctx context.Context, store *globaldb.Store, stored StoredApproval) error {
	if strings.TrimSpace(stored.Approval.ApprovalID) == "" {
		return fmt.Errorf("%w: approval id is required", globaldb.ErrInvalidInput)
	}
	value, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("encode Ari approval: %w", err)
	}
	return store.SetMeta(ctx, approvalMetaKey(stored.Approval.ApprovalID), string(value))
}

func LoadApproval(ctx context.Context, store *globaldb.Store, approvalID string) (StoredApproval, error) {
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return StoredApproval{}, inputError("approval_required", nil)
	}
	value, err := store.GetMeta(ctx, approvalMetaKey(approvalID))
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return StoredApproval{}, inputError("approval_unknown", map[string]any{"approval_id": approvalID})
		}
		return StoredApproval{}, err
	}
	var stored StoredApproval
	if err := json.Unmarshal([]byte(value), &stored); err != nil {
		return StoredApproval{}, fmt.Errorf("decode Ari approval %q: %w", approvalID, err)
	}
	return stored, nil
}

func encodeConsumedApproval(stored StoredApproval) (string, string, error) {
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

func approvalMetaKey(approvalID string) string {
	return "ari.approval." + strings.TrimSpace(approvalID)
}

func HashRequest(name string, input any) (string, error) {
	canonical, err := canonicalJSON(input)
	if err != nil {
		return "", NewError("invalid_request_body", "tool request body is invalid")
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
	raw, ok := input.(json.RawMessage)
	if ok {
		var normalized any
		if len(raw) == 0 {
			return json.RawMessage(`{}`), nil
		}
		if err := json.Unmarshal(raw, &normalized); err != nil {
			return nil, err
		}
		return json.Marshal(normalized)
	}
	if raw, ok := input.([]byte); ok {
		var normalized any
		if err := json.Unmarshal(raw, &normalized); err != nil {
			return nil, err
		}
		return json.Marshal(normalized)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func ScopedEventSubscription(ctx context.Context, store *globaldb.Store, scope Scope, subscriptionID string) (globaldb.EventSubscription, error) {
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" {
		return globaldb.EventSubscription{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "subscription_id"})
	}
	subscription, err := store.GetEventSubscription(ctx, subscriptionID)
	if err != nil {
		return globaldb.EventSubscription{}, err
	}
	if subscription.WorkspaceID != strings.TrimSpace(scope.WorkspaceID) {
		return globaldb.EventSubscription{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "subscription_scope_mismatch", "subscription_id": subscriptionID, "workspace_id": scope.WorkspaceID, "subscription_workspace_id": subscription.WorkspaceID})
	}
	owner := strings.TrimSpace(subscription.OwnerSessionID)
	if owner != "" && owner != strings.TrimSpace(scope.SourceRunID) {
		return globaldb.EventSubscription{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "subscription_scope_mismatch", "subscription_id": subscriptionID, "owner_session_id": owner, "scope_source_run_id": scope.SourceRunID})
	}
	return subscription, nil
}

func ScopedWorkspaceTimer(ctx context.Context, store *globaldb.Store, scope Scope, timerID string, allowBlankOwner bool) (globaldb.WorkspaceTimer, error) {
	timerID = strings.TrimSpace(timerID)
	if timerID == "" {
		return globaldb.WorkspaceTimer{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "timer_id"})
	}
	timer, err := store.GetWorkspaceTimer(ctx, timerID)
	if err != nil {
		return globaldb.WorkspaceTimer{}, err
	}
	if timer.WorkspaceID != strings.TrimSpace(scope.WorkspaceID) {
		return globaldb.WorkspaceTimer{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "timer_scope_mismatch", "timer_id": timerID, "workspace_id": scope.WorkspaceID, "timer_workspace_id": timer.WorkspaceID})
	}
	owner := strings.TrimSpace(timer.OwnerSessionID)
	if owner == "" && allowBlankOwner {
		return timer, nil
	}
	if owner != strings.TrimSpace(scope.SourceRunID) {
		return globaldb.WorkspaceTimer{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "timer_scope_mismatch", "timer_id": timerID, "owner_session_id": owner, "scope_source_run_id": scope.SourceRunID})
	}
	return timer, nil
}

func ScopedWorkspaceDelivery(ctx context.Context, store *globaldb.Store, scope Scope, deliveryID string) (globaldb.PendingDelivery, error) {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		return globaldb.PendingDelivery{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "delivery_id"})
	}
	delivery, err := store.GetPendingDelivery(ctx, deliveryID)
	if err != nil {
		return globaldb.PendingDelivery{}, err
	}
	if delivery.WorkspaceID != strings.TrimSpace(scope.WorkspaceID) {
		return globaldb.PendingDelivery{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "delivery_scope_mismatch", "delivery_id": deliveryID, "workspace_id": scope.WorkspaceID, "delivery_workspace_id": delivery.WorkspaceID})
	}
	visible, err := PendingDeliveryVisibleToScope(ctx, store, scope, delivery)
	if err != nil {
		return globaldb.PendingDelivery{}, err
	}
	if !visible {
		return globaldb.PendingDelivery{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "delivery_scope_mismatch", "delivery_id": deliveryID, "scope_source_run_id": scope.SourceRunID})
	}
	return delivery, nil
}

func PendingDeliveryVisibleToScope(ctx context.Context, store *globaldb.Store, scope Scope, delivery globaldb.PendingDelivery) (bool, error) {
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
	owner := strings.TrimSpace(subscription.OwnerSessionID)
	return owner == "" || owner == strings.TrimSpace(scope.SourceRunID), nil
}

func ScopedInboxSourceSessionID(scope Scope, inputSourceSessionID string) (string, error) {
	inputSourceSessionID = strings.TrimSpace(inputSourceSessionID)
	if inputSourceSessionID == "" {
		return strings.TrimSpace(scope.SourceRunID), nil
	}
	if inputSourceSessionID != strings.TrimSpace(scope.SourceRunID) {
		return "", rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "source_scope_mismatch", "source_session_id": inputSourceSessionID, "scope_source_run_id": scope.SourceRunID})
	}
	return inputSourceSessionID, nil
}

func ValidateFanoutReadScope(scope Scope, group globaldb.FanoutGroup, inputSourceSessionID string) error {
	if group.WorkspaceID != strings.TrimSpace(scope.WorkspaceID) {
		return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "fanout_scope_mismatch", "fanout_group_id": group.FanoutGroupID, "workspace_id": scope.WorkspaceID, "fanout_workspace_id": group.WorkspaceID})
	}
	if group.SourceSessionID != strings.TrimSpace(scope.SourceRunID) {
		return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "fanout_scope_mismatch", "fanout_group_id": group.FanoutGroupID, "source_session_id": group.SourceSessionID, "scope_source_run_id": scope.SourceRunID})
	}
	if inputSourceSessionID != "" && inputSourceSessionID != group.SourceSessionID {
		return rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "source_scope_mismatch", "source_session_id": inputSourceSessionID, "scope_source_run_id": scope.SourceRunID})
	}
	return nil
}

func InputMap(input any) (map[string]any, error) {
	if input == nil {
		return map[string]any{}, nil
	}
	if body, ok := input.(map[string]any); ok {
		return body, nil
	}
	if raw, ok := input.(json.RawMessage); ok {
		var body map[string]any
		if len(raw) == 0 {
			return map[string]any{}, nil
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			return nil, err
		}
		if body == nil {
			body = map[string]any{}
		}
		return body, nil
	}
	return nil, NewError("invalid_input", "tool input must be an object")
}

func OptionalString(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func OptionalStringPresent(values map[string]any, key string) (string, bool) {
	value, ok := values[key]
	if !ok || value == nil {
		return "", ok
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text), true
	}
	return strings.TrimSpace(fmt.Sprint(value)), true
}

func StringValueOrEmpty(values map[string]any, key string) string {
	value, _ := OptionalStringPresent(values, key)
	return value
}

func StringValue(values map[string]any, key string) (string, error) {
	value := OptionalString(values, key)
	if value == "" {
		return "", rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": key})
	}
	return value, nil
}

func BoolValue(values map[string]any, key string) (bool, bool) {
	value, ok := values[key]
	if !ok {
		return false, false
	}
	boolValue, ok := value.(bool)
	return boolValue, ok
}

func BoolValueOrFalse(values map[string]any, key string) bool {
	value, ok := values[key]
	if !ok || value == nil {
		return false
	}
	if typed, ok := value.(bool); ok {
		return typed
	}
	return strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "true")
}

func OptionalNonNegativeInt(values map[string]any, key string) (int, error) {
	value, ok, err := OptionalNonNegativeInt64Value(values, key)
	if err != nil || !ok {
		return 0, err
	}
	return int(value), nil
}

func OptionalNonNegativeIntValue(values map[string]any, key string) (int, bool, error) {
	value, ok := values[key]
	if !ok || value == nil {
		return 0, false, nil
	}
	var number int
	switch typed := value.(type) {
	case float64:
		if typed < 0 || typed != float64(int(typed)) {
			return 0, false, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_number", "field": key})
		}
		number = int(typed)
	case int:
		if typed < 0 {
			return 0, false, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_number", "field": key})
		}
		number = typed
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil || parsed < 0 {
			return 0, false, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_number", "field": key})
		}
		number = parsed
	default:
		return 0, false, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_number", "field": key})
	}
	return number, true, nil
}

func OptionalNonNegativeInt64Value(values map[string]any, key string) (int64, bool, error) {
	value, ok := values[key]
	if !ok || value == nil {
		return 0, false, nil
	}
	invalid := func() (int64, bool, error) {
		return 0, true, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_integer", "field": key})
	}
	switch typed := value.(type) {
	case float64:
		parsed := int64(typed)
		if typed < 0 || float64(parsed) != typed {
			return invalid()
		}
		return parsed, true, nil
	case int64:
		if typed < 0 {
			return invalid()
		}
		return typed, true, nil
	case int:
		if typed < 0 {
			return invalid()
		}
		return int64(typed), true, nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil || parsed < 0 {
			return invalid()
		}
		return parsed, true, nil
	default:
		return invalid()
	}
}

func StringSliceValue(values map[string]any, key string) ([]string, error) {
	value, ok := values[key]
	if !ok || value == nil {
		return nil, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": key})
	}
	items, ok := value.([]any)
	if !ok {
		return nil, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_list", "field": key})
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_list", "field": key})
		}
		out = append(out, strings.TrimSpace(text))
	}
	return out, nil
}

func OptionalStringSliceValue(values map[string]any, key string) ([]string, error) {
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

func MapValue(values map[string]any, key string) map[string]any {
	value, ok := values[key]
	if !ok || value == nil {
		return map[string]any{}
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func NewError(reason, message string) error {
	return inputError(reason, map[string]any{"message": message})
}

func inputError(reason string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	data["reason"] = reason
	return rpc.NewHandlerError(rpc.InvalidParams, reason, data)
}
