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
	aritool "github.com/builtwithtofu/ari/tools/ari-cli/internal/tool"
)

func readOnlyAriToolSchema(name, description string) aritool.Schema {
	return aritool.ReadOnlySchema(name, description, daemonOperationKindReadOnly)
}

func mutatingAriToolSchema(name, description string, approvalRequired bool) aritool.Schema {
	return aritool.MutatingSchema(name, description, daemonOperationKindMutating, approvalRequired)
}

func ariToolRegistry() []aritool.Definition[*Daemon] {
	return []aritool.Definition[*Daemon]{
		{
			Schema: readOnlyAriToolSchema("ari.defaults.get", "Read Ari default harness, model, and invocation settings"),
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return d.ariDefaultsGet()
			},
		},
		{
			Schema:               mutatingAriToolSchema("ari.defaults.set", "Update Ari defaults after scoped approval", true),
			RequiresDefaultScope: true,
			Operation:            &aritool.OperationDef{Type: "ari_defaults_set", Scope: globaldb.OperationScopeGlobal, RequestSummary: "set Ari defaults from helper tool", TrustDecision: aritool.TrustApprovedOnce, RollbackData: map[string]string{"scope": "ari_owned_config"}},
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return d.ariDefaultsSet(input)
			},
		},
		{
			Schema: readOnlyAriToolSchema("ari.profile.draft", "Draft a profile spec without persisting it"),
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariProfileDraft(input)
			},
		},
		{
			Schema:    mutatingAriToolSchema("ari.profile.save", "Persist an approved profile spec", true),
			Operation: &aritool.OperationDef{Type: "ari_profile_save", Scope: globaldb.OperationScopeWorkspace, RequestSummary: "save Ari helper profile", TrustDecision: aritool.TrustApprovedOnce, RollbackData: map[string]string{"scope": "ari_owned_profile"}},
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariProfileSave(ctx, store, scope, input)
			},
		},
		{
			Schema: readOnlyAriToolSchema("ari.self_check", "Read Ari daemon, config, workspace, profile, and harness health"),
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return d.ariSelfCheck(ctx, store, scope)
			},
		},
		{
			Schema: readOnlyAriToolSchema("ari.run.explain_latest", "Summarize the latest available Ari run evidence"),
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariRunExplainLatest(ctx, store, scope)
			},
		},
		{
			Schema:    mutatingAriToolSchema("ari.session.fanout", "Launch one or more ephemeral worker profiles from a scoped sticky source session", false),
			Operation: &aritool.OperationDef{Type: "ari_session_fanout", Scope: globaldb.OperationScopeWorkspace, RequestSummary: "launch Ari fanout workers from helper tool", TrustDecision: aritool.TrustScopedSourceSession, RollbackData: map[string]string{"scope": "runtime_coordination", "rollback": "not_supported_for_external_worker_runs"}},
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return d.ariSessionFanout(ctx, store, scope, input)
			},
		},
		{
			Schema: readOnlyAriToolSchema("ari.fanout.status", "Read durable fanout group and member status"),
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariFanoutStatus(ctx, store, scope, input)
			},
		},
		{
			Schema: readOnlyAriToolSchema("ari.inbox.list", "List durable sticky-session inbox items"),
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariInboxList(ctx, store, scope, input)
			},
		},
		{
			Schema: readOnlyAriToolSchema("ari.inbox.count", "Count durable sticky-session inbox items by read state"),
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariInboxCount(ctx, store, scope, input)
			},
		},
		{
			Schema:    mutatingAriToolSchema("ari.inbox.mark_read", "Mark durable sticky-session inbox items read", false),
			Operation: &aritool.OperationDef{Type: "ari_inbox_mark_read", Scope: globaldb.OperationScopeWorkspace, RequestSummary: "mark Ari inbox items read from helper tool", TrustDecision: aritool.TrustScopedSourceSession, RollbackData: map[string]string{"scope": "runtime_inbox", "rollback": "not_supported_for_read_lifecycle"}},
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariInboxMarkRead(ctx, store, scope, input)
			},
		},
		{
			Schema: readOnlyAriToolSchema("ari.workspace.events.next", "Read unread events from a durable workspace event subscription, optionally blocking until min_events arrive within timeout_ms (max 60000)"),
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariWorkspaceEventsNext(ctx, store, scope, input)
			},
		},
		{
			Schema:    mutatingAriToolSchema("ari.workspace.events.ack", "Advance a durable workspace event subscription cursor", false),
			Operation: &aritool.OperationDef{Type: "ari_workspace_events_ack", Scope: globaldb.OperationScopeWorkspace, RequestSummary: "ack Ari workspace event subscription from helper tool", TrustDecision: aritool.TrustScopedSourceSession, RollbackData: map[string]string{"scope": "workspace_event_subscription", "rollback": "not_supported_for_read_lifecycle"}},
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariWorkspaceEventsAck(ctx, store, scope, input)
			},
		},
		{
			Schema:    mutatingAriToolSchema("ari.workspace.signals.send", "Send a workspace-scoped signal event from the scoped source session", false),
			Operation: &aritool.OperationDef{Type: "ari_workspace_signals_send", Scope: globaldb.OperationScopeWorkspace, RequestSummary: "send Ari workspace signal from helper tool", TrustDecision: aritool.TrustScopedSourceSession, RollbackData: map[string]string{"scope": "workspace_event_history", "rollback": "append_only_signal_not_reverted"}},
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariWorkspaceSignalSend(ctx, store, scope, input)
			},
		},
		{
			Schema:    mutatingAriToolSchema("ari.workspace.timers.create", "Create a durable workspace timer owned by the scoped source session", false),
			Operation: &aritool.OperationDef{Type: "ari_workspace_timers_create", Scope: globaldb.OperationScopeWorkspace, RequestSummary: "create Ari workspace timer from helper tool", TrustDecision: aritool.TrustScopedSourceSession, RollbackData: map[string]string{"scope": "workspace_timer", "rollback": "cancel_timer_when_scheduled"}},
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariWorkspaceTimerCreate(ctx, store, scope, input)
			},
		},
		{
			Schema: readOnlyAriToolSchema("ari.workspace.timers.get", "Read a durable workspace timer"),
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariWorkspaceTimerGet(ctx, store, scope, input)
			},
		},
		{
			Schema:    mutatingAriToolSchema("ari.workspace.timers.cancel", "Cancel a durable workspace timer owned by the scoped source session", false),
			Operation: &aritool.OperationDef{Type: "ari_workspace_timers_cancel", Scope: globaldb.OperationScopeWorkspace, RequestSummary: "cancel Ari workspace timer from helper tool", TrustDecision: aritool.TrustScopedSourceSession, RollbackData: map[string]string{"scope": "workspace_timer", "rollback": "not_supported_for_timer_cancellation"}},
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariWorkspaceTimerCancel(ctx, store, scope, input)
			},
		},
		{
			Schema: readOnlyAriToolSchema("ari.workspace.deliveries.get", "Read a pending workspace event delivery"),
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariWorkspaceDeliveryGet(ctx, store, scope, input)
			},
		},
		{
			Schema: readOnlyAriToolSchema("ari.workspace.deliveries.list_due", "List due pending workspace event deliveries"),
			Handler: func(ctx context.Context, d *Daemon, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
				return ariWorkspaceDeliveriesListDue(ctx, store, scope, input)
			},
		},
	}
}

func validateAriToolRegistry() error {
	return aritool.ValidateDefinitions(ariToolRegistry())
}

func (d *Daemon) registerAriToolMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	toolCatalog, err := aritool.NewCatalog(ariToolRegistry())
	if err != nil {
		return err
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[aritool.ListRequest, aritool.ListResponse]{
		Name:        "ari.tool.list",
		Description: "List Ari-owned tools available to helpers",
		Handler: func(ctx context.Context, req aritool.ListRequest) (aritool.ListResponse, error) {
			_ = ctx
			_ = req
			return aritool.ListResponse{Tools: toolCatalog.Schemas()}, nil
		},
	}); err != nil {
		return fmt.Errorf("register ari.tool.list: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[aritool.CallRequest, aritool.CallResponse]{
		Name:        "ari.tool.call",
		Description: "Call an Ari-owned helper tool",
		Handler: func(ctx context.Context, req aritool.CallRequest) (aritool.CallResponse, error) {
			return d.callAriTool(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register ari.tool.call: %w", err)
	}
	return nil
}

func (d *Daemon) callAriTool(ctx context.Context, store *globaldb.Store, req aritool.CallRequest) (aritool.CallResponse, error) {
	toolCatalog, err := aritool.NewCatalog(ariToolRegistry())
	if err != nil {
		return aritool.CallResponse{}, err
	}
	return toolCatalog.Call(ctx, d, store, req, aritool.CallOptions{OperationSource: daemonOperationSourceTool, OperationRunner: func(ctx context.Context, record aritool.OperationRecord, fn func(context.Context) error) error {
		return d.recordAriToolOperation(ctx, store, record, fn)
	}})
}

func (d *Daemon) recordAriToolOperation(ctx context.Context, store *globaldb.Store, record aritool.OperationRecord, fn func(context.Context) error) error {
	options := daemonOperationRecordOptions{WorkspaceID: record.WorkspaceID, OperationType: record.OperationType, OperationKind: record.OperationKind, Actor: record.Actor, Source: record.Source, Scope: record.Scope, RequestSummary: record.RequestSummary, TrustDecision: record.TrustDecision, RollbackData: record.RollbackData, PayloadSnapshot: record.PayloadSnapshot}
	_, err := recordDaemonOperation(ctx, store, options, fn)
	return err
}

func (d *Daemon) ariDefaultsGet() (aritool.CallResponse, error) {
	values, err := readJSONConfig(d.configPath)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	return aritool.CallResponse{Status: "ok", Output: map[string]any{"default_harness": readConfigString(values, "default_harness"), "preferred_model": readConfigString(values, "preferred_model"), "default_invocation_class": readConfigString(values, "default_invocation_class")}}, nil
}

func (d *Daemon) ariDefaultsSet(input any) (aritool.CallResponse, error) {
	body, err := aritool.InputMap(input)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	harness, hasHarness := aritool.OptionalStringPresent(body, "default_harness")
	if hasHarness && harness != "" && !isSupportedHarness(harness) {
		return aritool.CallResponse{}, ariToolError("invalid_default_harness", "default_harness is unsupported")
	}
	model, hasModel := aritool.OptionalStringPresent(body, "preferred_model")
	invocationClass, hasInvocationClass := aritool.OptionalStringPresent(body, "default_invocation_class")
	if hasInvocationClass && invocationClass != "" && invocationClass != string(HarnessInvocationSticky) && invocationClass != string(HarnessInvocationEphemeral) {
		return aritool.CallResponse{}, ariToolError("invalid_default_invocation_class", "default_invocation_class is unsupported")
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
		return aritool.CallResponse{}, err
	}
	return aritool.CallResponse{Status: "ok", ApplicationStatus: "restart_required", Output: map[string]any{"updated": true}}, nil
}

func ariProfileDraft(input any) (aritool.CallResponse, error) {
	body, err := aritool.InputMap(input)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	name := aritool.StringValueOrEmpty(body, "name")
	if name == "" {
		return aritool.CallResponse{}, ariToolError("missing_profile_name", "profile name is required")
	}
	harness := aritool.StringValueOrEmpty(body, "harness")
	if harness != "" && !isSupportedHarness(harness) {
		return aritool.CallResponse{}, ariToolError("invalid_profile_harness", "profile harness is unsupported")
	}
	output := map[string]any{"name": name, "harness": harness, "model": aritool.StringValueOrEmpty(body, "model"), "prompt": aritool.StringValueOrEmpty(body, "prompt"), "invocation_class": aritool.StringValueOrEmpty(body, "invocation_class"), "defaults": map[string]any{}}
	return aritool.CallResponse{Status: "draft", Output: output}, nil
}

func ariProfileSave(ctx context.Context, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
	body, err := aritool.InputMap(input)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	profile, err := createStoredProfile(ctx, store, ProfileCreateRequest{WorkspaceID: scope.WorkspaceID, Name: aritool.StringValueOrEmpty(body, "name"), Harness: aritool.StringValueOrEmpty(body, "harness"), Model: aritool.StringValueOrEmpty(body, "model"), Prompt: aritool.StringValueOrEmpty(body, "prompt"), InvocationClass: HarnessInvocationClass(aritool.StringValueOrEmpty(body, "invocation_class")), Defaults: aritool.MapValue(body, "defaults")})
	if err != nil {
		return aritool.CallResponse{}, err
	}
	return aritool.CallResponse{Status: "ok", ApplicationStatus: "applied_live", Output: map[string]any{"profile_id": profile.ProfileID, "workspace_id": profile.WorkspaceID, "name": profile.Name, "harness": profile.Harness, "model": profile.Model, "prompt": profile.Prompt, "invocation_class": string(profile.InvocationClass)}}, nil
}

func (d *Daemon) ariSelfCheck(ctx context.Context, store *globaldb.Store, scope aritool.Scope) (aritool.CallResponse, error) {
	_, cfgErr := readJSONConfig(d.configPath)
	_, wsErr := store.GetWorkspace(ctx, scope.WorkspaceID)
	return aritool.CallResponse{Status: "ok", Output: map[string]any{"daemon_version": d.version, "config_readable": cfgErr == nil, "workspace_available": wsErr == nil}}, nil
}

func ariRunExplainLatest(ctx context.Context, store *globaldb.Store, scope aritool.Scope) (aritool.CallResponse, error) {
	responses, err := store.ListFinalResponses(ctx, scope.WorkspaceID)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	if len(responses) == 0 {
		return aritool.CallResponse{Status: "ok", Output: map[string]any{"summary": "No final response records are available for this workspace yet.", "run_available": false}}, nil
	}
	latest := responses[0]
	return aritool.CallResponse{Status: "ok", Output: map[string]any{"summary": latest.Text, "run_available": true, "run_id": latest.HarnessSessionID, "final_response_id": latest.FinalResponseID}}, nil
}

func (d *Daemon) ariSessionFanout(ctx context.Context, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
	body, err := aritool.InputMap(input)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	sourceSessionID := aritool.StringValueOrEmpty(body, "source_session_id")
	if sourceSessionID == "" {
		sourceSessionID = strings.TrimSpace(scope.SourceRunID)
	}
	if sourceSessionID != strings.TrimSpace(scope.SourceRunID) {
		return aritool.CallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "source_scope_mismatch", "source_session_id": sourceSessionID, "scope_source_run_id": strings.TrimSpace(scope.SourceRunID), "start_invoked": false})
	}
	bodyText := aritool.StringValueOrEmpty(body, "body")
	if bodyText == "" {
		return aritool.CallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "body", "start_invoked": false})
	}
	targetProfileIDs, err := aritool.OptionalStringSliceValue(body, "target_profile_ids")
	if err != nil {
		return aritool.CallResponse{}, err
	}
	if len(targetProfileIDs) == 0 {
		return aritool.CallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "target_profile_ids", "start_invoked": false})
	}
	seenProfiles := make(map[string]struct{}, len(targetProfileIDs))
	for _, profileID := range targetProfileIDs {
		if _, ok := seenProfiles[profileID]; ok {
			return aritool.CallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "duplicate_target_profile", "target_profile_id": profileID, "start_invoked": false})
		}
		seenProfiles[profileID] = struct{}{}
	}
	contextExcerptIDs, err := aritool.OptionalStringSliceValue(body, "context_excerpt_ids")
	if err != nil {
		return aritool.CallResponse{}, err
	}
	fanoutGroupID := aritool.StringValueOrEmpty(body, "fanout_group_id")
	if err := validateAriFanoutCanStart(ctx, store, scope, sourceSessionID, fanoutGroupID, targetProfileIDs, contextExcerptIDs); err != nil {
		return aritool.CallResponse{}, err
	}
	wait, err := ariFanoutWaitFromInput(body)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	fanout, err := d.fanoutSession(ctx, store, AgentMessageSendRequest{WorkspaceID: scope.WorkspaceID, FanoutGroupID: fanoutGroupID, SourceSessionID: sourceSessionID, TargetProfileIDs: targetProfileIDs, Body: bodyText, ContextExcerptIDs: contextExcerptIDs})
	if err != nil {
		return aritool.CallResponse{}, err
	}
	waitStatus, waitTimedOut, members, err := waitForAriFanoutMembers(ctx, store, fanout, wait)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	output := map[string]any{"fanout_group_id": fanout.FanoutGroupID, "workspace_id": scope.WorkspaceID, "source_session_id": sourceSessionID, "members": members, "wait_mode": wait.Mode, "wait_status": waitStatus, "wait_timed_out": waitTimedOut}
	return aritool.CallResponse{Status: "ok", Output: output}, nil
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
	group, err := store.GetFanoutGroup(ctx, fanout.FanoutGroupID)
	if err != nil {
		return "", false, nil, err
	}
	workerSessionIDs := ariFanoutWorkerSessionIDs(fanout)
	if len(workerSessionIDs) == 0 {
		members := ariFanoutMembersFromResponse(fanout)
		return fanoutWaitStatus(members), false, members, nil
	}
	filterJSON, conditionJSON, err := ariFanoutWaitSubscriptionJSON(wait.Mode, fanout.FanoutGroupID, workerSessionIDs)
	if err != nil {
		return "", false, nil, err
	}
	result, err := store.WaitEventSubscriptionCondition(ctx, globaldb.EventSubscription{WorkspaceID: group.WorkspaceID, OwnerSessionID: group.SourceSessionID, FilterJSON: filterJSON, CompletionConditionJSON: conditionJSON}, globaldb.EventSubscriptionWaitOptions{Limit: len(workerSessionIDs), Timeout: time.Duration(wait.TimeoutMS) * time.Millisecond})
	if err != nil {
		return "", false, nil, err
	}
	members, err := ariFanoutMembersForGroup(ctx, store, group)
	if err != nil {
		return "", false, nil, err
	}
	if result.Completion.TimedOut {
		return "partial", true, members, nil
	}
	return fanoutWaitStatus(members), false, members, nil
}

func ariFanoutWorkerSessionIDs(fanout AgentMessageSendResponse) []string {
	workerSessionIDs := make([]string, 0, len(fanout.FanoutMembers))
	for _, member := range fanout.FanoutMembers {
		if sessionID := strings.TrimSpace(member.Session.SessionID); sessionID != "" {
			workerSessionIDs = append(workerSessionIDs, sessionID)
		}
	}
	return workerSessionIDs
}

func ariFanoutWaitSubscriptionJSON(mode, fanoutGroupID string, workerSessionIDs []string) (string, string, error) {
	terminalEventTypes := []string{workspaceEventWorkerCompleted, workspaceEventWorkerFailed, workspaceEventWorkerStopped}
	filter, err := json.Marshal(globaldb.EventSubscriptionFilter{EventTypes: terminalEventTypes, SubjectIDs: workerSessionIDs, CorrelationIDs: []string{strings.TrimSpace(fanoutGroupID)}})
	if err != nil {
		return "", "", err
	}
	condition, err := json.Marshal(globaldb.EventSubscriptionCompletionCondition{Mode: mode, SubjectIDs: workerSessionIDs, TerminalEventTypes: terminalEventTypes})
	if err != nil {
		return "", "", err
	}
	return string(filter), string(condition), nil
}

func ariFanoutMembersFromResponse(fanout AgentMessageSendResponse) []map[string]any {
	members := make([]map[string]any, 0, len(fanout.FanoutMembers))
	for _, member := range fanout.FanoutMembers {
		members = append(members, map[string]any{"fanout_member_id": member.FanoutMemberID, "target_profile_id": member.TargetProfileID, "worker_session_id": member.Session.SessionID, "request_agent_message_id": member.Request.AgentMessageID, "status": member.Session.Status, "request_status": member.Request.Status})
	}
	return members
}

func ariFanoutMembersForGroup(ctx context.Context, store *globaldb.Store, group globaldb.FanoutGroup) ([]map[string]any, error) {
	stored, err := store.ListFanoutMembers(ctx, group.FanoutGroupID)
	if err != nil {
		return nil, err
	}
	members := make([]map[string]any, 0, len(stored))
	for _, member := range stored {
		members = append(members, map[string]any{"fanout_member_id": member.FanoutMemberID, "target_profile_id": member.TargetProfileID, "worker_session_id": member.WorkerSessionID, "request_agent_message_id": member.RequestAgentMessageID, "reply_agent_message_id": member.ReplyAgentMessageID, "final_response_id": member.FinalResponseID, "status": member.Status})
	}
	return members, nil
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

func ariFanoutStatus(ctx context.Context, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
	body, err := aritool.InputMap(input)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	groupID := aritool.StringValueOrEmpty(body, "fanout_group_id")
	if groupID == "" {
		return aritool.CallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "fanout_group_id"})
	}
	group, err := store.GetFanoutGroup(ctx, groupID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return aritool.CallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), map[string]any{"reason": "unknown_fanout_group", "fanout_group_id": groupID})
		}
		return aritool.CallResponse{}, err
	}
	if err := aritool.ValidateFanoutReadScope(scope, group, aritool.StringValueOrEmpty(body, "source_session_id")); err != nil {
		return aritool.CallResponse{}, err
	}
	members, err := ariFanoutMembersForGroup(ctx, store, group)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	return aritool.CallResponse{Status: "ok", Output: map[string]any{"fanout_group_id": group.FanoutGroupID, "workspace_id": group.WorkspaceID, "source_session_id": group.SourceSessionID, "source_agent_id": group.SourceAgentID, "request_agent_message_id": group.RequestAgentMessageID, "status": fanoutWaitStatus(members), "members": members}}, nil
}

func ariInboxList(ctx context.Context, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
	body, err := aritool.InputMap(input)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	sourceSessionID, err := aritool.ScopedInboxSourceSessionID(scope, aritool.StringValueOrEmpty(body, "source_session_id"))
	if err != nil {
		return aritool.CallResponse{}, err
	}
	items, err := store.ListInboxItems(ctx, scope.WorkspaceID, sourceSessionID)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	unreadOnly := aritool.BoolValueOrFalse(body, "unread_only")
	outputItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if unreadOnly && item.Status != "unread" {
			continue
		}
		outputItems = append(outputItems, ariInboxItemOutput(item))
	}
	return aritool.CallResponse{Status: "ok", Output: map[string]any{"workspace_id": scope.WorkspaceID, "source_session_id": sourceSessionID, "items": outputItems}}, nil
}

func ariInboxCount(ctx context.Context, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
	body, err := aritool.InputMap(input)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	sourceSessionID, err := aritool.ScopedInboxSourceSessionID(scope, aritool.StringValueOrEmpty(body, "source_session_id"))
	if err != nil {
		return aritool.CallResponse{}, err
	}
	counts, err := store.CountInboxItems(ctx, scope.WorkspaceID, sourceSessionID)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	return aritool.CallResponse{Status: "ok", Output: map[string]any{"workspace_id": scope.WorkspaceID, "source_session_id": sourceSessionID, "total_count": int(counts.TotalCount), "unread_count": int(counts.UnreadCount), "read_count": int(counts.ReadCount)}}, nil
}

func ariInboxMarkRead(ctx context.Context, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
	body, err := aritool.InputMap(input)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	sourceSessionID, err := aritool.ScopedInboxSourceSessionID(scope, aritool.StringValueOrEmpty(body, "source_session_id"))
	if err != nil {
		return aritool.CallResponse{}, err
	}
	inboxItemIDs, err := aritool.OptionalStringSliceValue(body, "inbox_item_ids")
	if err != nil {
		return aritool.CallResponse{}, err
	}
	if len(inboxItemIDs) == 0 {
		return aritool.CallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_required_fields", "missing_field": "inbox_item_ids"})
	}
	marked, err := store.MarkInboxItemsRead(ctx, scope.WorkspaceID, sourceSessionID, inboxItemIDs)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	return aritool.CallResponse{Status: "ok", Output: map[string]any{"workspace_id": scope.WorkspaceID, "source_session_id": sourceSessionID, "marked_count": int(marked)}}, nil
}

// ariToolsEventsNextMaxTimeoutMS bounds how long an agent tool call may hold
// a server-side event wait.
const ariToolsEventsNextMaxTimeoutMS = 60_000

func ariWorkspaceEventsNext(ctx context.Context, store *globaldb.Store, scope aritool.Scope, input any) (aritool.CallResponse, error) {
	body, err := aritool.InputMap(input)
	if err != nil {
		return aritool.CallResponse{}, err
	}
	subscription, err := aritool.ScopedEventSubscription(ctx, store, scope, aritool.StringValueOrEmpty(body, "subscription_id"))
	if err != nil {
		return aritool.CallResponse{}, err
	}
	limit, err := aritool.OptionalNonNegativeInt(body, "limit")
	if err != nil {
		return aritool.CallResponse{}, err
	}
	minEvents, err := aritool.OptionalNonNegativeInt(body, "min_events")
	if err != nil {
		return aritool.CallResponse{}, err
	}
	timeoutMS, err := aritool.OptionalNonNegativeInt(body, "timeout_ms")
	if err != nil {
		return aritool.CallResponse{}, err
	}
	if timeoutMS > ariToolsEventsNextMaxTimeoutMS {
		return aritool.CallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "invalid_wait_timeout", "timeout_ms": timeoutMS, "max_timeout_ms": ariToolsEventsNextMaxTimeoutMS})
	}
	if minEvents > 0 && timeoutMS <= 0 {
		return aritool.CallResponse{}, rpc.NewHandlerError(rpc.InvalidParams, globaldb.ErrInvalidInput.Error(), map[string]any{"reason": "missing_wait_timeout", "min_events": minEvents})
	}
	response, err := workspaceEventsNext(ctx, store, WorkspaceEventsNextRequest{SubscriptionID: subscription.SubscriptionID, Limit: limit, MinEvents: minEvents, TimeoutMS: timeoutMS})
	if err != nil {
		return aritool.CallResponse{}, err
	}
	events := make([]map[string]any, 0, len(response.Events))
	for _, event := range response.Events {
		events = append(events, ariWorkspaceEventResponseOutput(event))
	}
	output := map[string]any{"subscription_id": subscription.SubscriptionID, "workspace_id": subscription.WorkspaceID, "owner_session_id": subscription.OwnerSessionID, "count": len(events), "events": events}
	if response.WaitStatus != "" {
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
	if err := validateWorkspaceSignalTarget(ctx, store, scope.WorkspaceID, targetType, targetID); err != nil {
		return AriToolCallResponse{}, err
	}
	event, err := store.AppendWorkspaceEvent(ctx, globaldb.WorkspaceEvent{EventID: stringValue(body, "event_id"), WorkspaceID: scope.WorkspaceID, EventType: globaldb.WorkspaceEventSignalSent, SubjectType: targetType, SubjectID: targetID, ProducerType: workspaceEventProducerSession, ProducerID: strings.TrimSpace(scope.SourceRunID), CorrelationID: stringValue(body, "correlation_id"), CausationID: stringValue(body, "causation_id"), PayloadJSON: stringValue(body, "payload_json"), AttentionRequired: true})
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
	timer, err := store.CreateWorkspaceTimer(ctx, globaldb.WorkspaceTimer{TimerID: stringValue(body, "timer_id"), WorkspaceID: scope.WorkspaceID, OwnerSessionID: strings.TrimSpace(scope.SourceRunID), TargetSubscriptionID: stringValue(body, "target_subscription_id"), SubjectType: stringValue(body, "subject_type"), SubjectID: stringValue(body, "subject_id"), Purpose: stringValue(body, "purpose"), FireAt: fireAt, PayloadJSON: stringValue(body, "payload_json")})
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
	timer, err := scopedAriWorkspaceTimer(ctx, store, scope, stringValue(body, "timer_id"), true)
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
	timer, err := scopedAriWorkspaceTimer(ctx, store, scope, stringValue(body, "timer_id"), false)
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
	return aritool.CallResponse{Status: "ok", Output: map[string]any{"workspace_id": strings.TrimSpace(scope.WorkspaceID), "count": len(scoped), "deliveries": ariWorkspaceDeliveryOutputs(scoped)}}, nil
}

func ariWorkspaceEventOutput(event globaldb.WorkspaceEvent) map[string]any {
	return map[string]any{"event_id": event.EventID, "workspace_id": event.WorkspaceID, "sequence": event.Sequence, "event_type": event.EventType, "subject_type": event.SubjectType, "subject_id": event.SubjectID, "producer_type": event.ProducerType, "producer_id": event.ProducerID, "correlation_id": event.CorrelationID, "causation_id": event.CausationID, "payload_json": event.PayloadJSON, "payload_ref_json": event.PayloadRefJSON, "attention_required": event.AttentionRequired, "created_at": event.CreatedAt.UTC().Format(time.RFC3339Nano)}
}

const ariWorkspaceDeliveriesListDueMaxLimit = 1000

func ariWorkspaceTimerOutput(timer globaldb.WorkspaceTimer) map[string]any {
	return map[string]any{"timer_id": timer.TimerID, "workspace_id": timer.WorkspaceID, "owner_session_id": timer.OwnerSessionID, "target_subscription_id": timer.TargetSubscriptionID, "subject_type": timer.SubjectType, "subject_id": timer.SubjectID, "purpose": timer.Purpose, "status": timer.Status, "fire_at": timer.FireAt.UTC().Format(time.RFC3339Nano), "payload_json": timer.PayloadJSON, "fired_event_id": timer.FiredEventID, "created_at": timer.CreatedAt.UTC().Format(time.RFC3339Nano), "updated_at": timer.UpdatedAt.UTC().Format(time.RFC3339Nano)}
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

func ariInboxItemOutput(item globaldb.InboxItem) map[string]any {
	return map[string]any{"inbox_item_id": item.InboxItemID, "workspace_id": item.WorkspaceID, "source_session_id": item.SourceSessionID, "workspace_event_id": item.WorkspaceEventID, "event_type": item.EventType, "fanout_group_id": item.FanoutGroupID, "fanout_member_id": item.FanoutMemberID, "worker_session_id": item.WorkerSessionID, "final_response_id": item.FinalResponseID, "kind": item.Kind, "status": item.Status, "attention_required": item.AttentionRequired, "summary": item.Summary, "created_at": formatAriInboxTime(item.CreatedAt), "updated_at": formatAriInboxTime(item.UpdatedAt)}
}

func formatAriInboxTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func validateAriFanoutCanStart(ctx context.Context, store *globaldb.Store, scope aritool.Scope, sourceSessionID, fanoutGroupID string, targetProfileIDs, contextExcerptIDs []string) error {
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

func ariToolError(reason, message string) error {
	return rpc.NewHandlerError(rpc.InvalidParams, reason+": "+message, map[string]any{"reason": reason})
}
