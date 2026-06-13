package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// GrokExecutor integrates the xAI Grok CLI. Runs are headless
// (`grok -p --output-format streaming-json`); grok assigns the provider
// session id, which is captured from the terminal `end` event, and later
// turns or deliveries reattach with `grok -r <session-id>` (the `-s` flag is
// TUI-only and ignored headless, per grok 0.2.x). Auth state lives in
// GROK_HOME, so named slots project a per-slot home like codex; headless
// output carries no token usage, so the adapter does not declare measured
// token telemetry.
type grokExecutorOptions struct {
	Executable     string
	Cwd            string
	Model          string
	SystemPrompt   string
	InvocationMode HarnessInvocationMode
	AuthProjection HarnessAuthProjectionPlan
	AuthHomeRoot   string
	RunCommand     grokCommandRunner
	RunAuthCommand grokCommandRunner
	LookupEnv      func(string) string
}

type grokCommandRunner func(context.Context, grokExecutorOptions, []string) (commandRunResult, error)

type GrokExecutor struct {
	options         grokExecutorOptions
	mu              sync.Mutex
	runs            map[string][]TimelineItem
	deliveryOptions map[string]grokExecutorOptions
}

func NewGrokExecutor(cwd string) *GrokExecutor {
	return newGrokExecutor(grokExecutorOptions{Executable: harnessExecutable("grok", EnvGrokExecutable), Cwd: cwd, RunCommand: runGrokCommand})
}

func NewGrokExecutorForTest(options grokExecutorOptions) *GrokExecutor {
	return newGrokExecutor(options)
}

func newGrokExecutor(options grokExecutorOptions) *GrokExecutor {
	if strings.TrimSpace(options.Executable) == "" {
		options.Executable = "grok"
	}
	if options.InvocationMode == "" {
		options.InvocationMode = HarnessInvocationModeHeadless
	}
	if options.RunCommand == nil {
		options.RunCommand = runGrokCommand
	}
	if options.RunAuthCommand == nil {
		options.RunAuthCommand = runGrokCommand
	}
	if options.LookupEnv == nil {
		options.LookupEnv = os.Getenv
	}
	return &GrokExecutor{options: options, runs: map[string][]TimelineItem{}, deliveryOptions: map[string]grokExecutorOptions{}}
}

func (options grokExecutorOptions) withGrokAuthSlotProjection(authSlotID string) (grokExecutorOptions, error) {
	authSlotID = strings.TrimSpace(authSlotID)
	if authSlotIsDefaultForHarness(HarnessNameGrok, authSlotID) {
		return options, nil
	}
	if options.AuthProjection.Kind == HarnessAuthProjectionConfigRoot && strings.TrimSpace(options.AuthProjection.Env["GROK_HOME"]) != "" {
		return options, nil
	}
	home, err := harnessAuthSlotHome(HarnessNameGrok, authSlotID, options.AuthHomeRoot)
	if err != nil {
		return grokExecutorOptions{}, err
	}
	options.AuthProjection = HarnessAuthProjectionPlan{Owner: HarnessAuthProjectionOwnerNative, Kind: HarnessAuthProjectionConfigRoot, Env: map[string]string{"GROK_HOME": home}, RiskLabels: []string{"provider_owned", "native_config_root_isolation"}}
	return options, nil
}

// grokAuthHome resolves the GROK_HOME directory auth state is read from:
// the projected per-slot home when present, else the default `~/.grok`.
func (options grokExecutorOptions) grokAuthHome() string {
	if home := strings.TrimSpace(options.AuthProjection.Env["GROK_HOME"]); home != "" {
		return home
	}
	if home := strings.TrimSpace(options.LookupEnv("GROK_HOME")); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(userHome, ".grok")
}

func (e *GrokExecutor) AuthStatus(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	if ctx == nil {
		return HarnessAuthStatus{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return HarnessAuthStatus{}, fmt.Errorf("executor is required")
	}
	options, err := e.options.withGrokAuthSlotProjection(slot.AuthSlotID)
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	authenticated := false
	if home := options.grokAuthHome(); home != "" {
		if _, err := os.Stat(filepath.Join(home, "auth.json")); err == nil {
			authenticated = true
		}
	}
	if !authenticated && authSlotIsDefaultForHarness(HarnessNameGrok, slot.AuthSlotID) && grokProviderAPIKeyPresent(options) {
		authenticated = true
	}
	if authenticated {
		return HarnessAuthStatus{Harness: HarnessNameGrok, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthAuthenticated, AriSecretStorage: HarnessAriSecretStorageNone}, nil
	}
	return NewHarnessAuthRequired(HarnessNameGrok, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "device_code", SecretOwnedBy: HarnessNameGrok}), nil
}

func grokProviderAPIKeyPresent(options grokExecutorOptions) bool {
	for _, key := range []string{"GROK_CODE_XAI_API_KEY", "XAI_API_KEY"} {
		if strings.TrimSpace(options.AuthProjection.Env[key]) != "" || strings.TrimSpace(options.LookupEnv(key)) != "" {
			return true
		}
	}
	return false
}

func (e *GrokExecutor) AuthStart(ctx context.Context, slot HarnessAuthSlot, method string) (HarnessAuthStatus, error) {
	if ctx == nil {
		return HarnessAuthStatus{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return HarnessAuthStatus{}, fmt.Errorf("executor is required")
	}
	options, err := e.options.withGrokAuthSlotProjection(slot.AuthSlotID)
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	method = strings.TrimSpace(method)
	if method == "" {
		method = "device_code"
	}
	switch method {
	case "api_key":
		return NewHarnessAuthRequired(HarnessNameGrok, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "api_key_provider_setup", SecretOwnedBy: HarnessNameGrok}), nil
	case "browser":
		return NewHarnessAuthRequired(HarnessNameGrok, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "client_provider_login", SecretOwnedBy: HarnessNameGrok}), nil
	case "device_code":
	default:
		return HarnessAuthStatus{}, &HarnessUnavailableError{Harness: HarnessNameGrok, Reason: "auth_method_unsupported", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}
	result, err := options.RunAuthCommand(ctx, options, []string{"login", "--device-auth"})
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	flow := codexAuthFlowFromOutput(result.Output)
	flow.Method = method
	flow.SecretOwnedBy = HarnessNameGrok
	return HarnessAuthStatus{Harness: HarnessNameGrok, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthInProgress, Remediation: &flow, AriSecretStorage: HarnessAriSecretStorageNone}, nil
}

func (e *GrokExecutor) AuthLogout(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	if ctx == nil {
		return HarnessAuthStatus{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return HarnessAuthStatus{}, fmt.Errorf("executor is required")
	}
	options, err := e.options.withGrokAuthSlotProjection(slot.AuthSlotID)
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	result, err := options.RunAuthCommand(ctx, options, []string{"logout"})
	if result.ExitCode != nil && *result.ExitCode != 0 {
		return HarnessAuthStatus{}, &HarnessUnavailableError{Harness: HarnessNameGrok, Reason: "auth_logout_failed", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: true}
	}
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	return NewHarnessAuthRequired(HarnessNameGrok, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "device_code", SecretOwnedBy: HarnessNameGrok}), nil
}

// grokRuntimeCapabilities is the shared runtime contract minus measured token
// telemetry: grok's headless output formats carry no usage counters.
func grokRuntimeCapabilities() []HarnessCapability {
	return []HarnessCapability{
		HarnessCapabilityHarnessSessionFromContext,
		HarnessCapabilityContextPacket,
		HarnessCapabilityTimelineItems,
		HarnessCapabilityFinalResponse,
	}
}

func (e *GrokExecutor) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{
		Name:                    HarnessNameGrok,
		DisplayName:             "grok",
		Capabilities:            grokRuntimeCapabilities(),
		ObservationCapabilities: []HarnessObservationCapability{HarnessObservationEventStream},
		DeliveryCapabilities:    []HarnessDeliveryCapability{HarnessDeliveryVisiblePromptTurn},
		InvocationModes:         []HarnessInvocationMode{HarnessInvocationModeHeadless},
		Auth: HarnessAuthDescriptor{
			StatusCheck:        HarnessAuthSupportPartial,
			Login:              HarnessAuthSupportSupported,
			LoginMethods:       []string{"browser", "device_code", "api_key"},
			Logout:             HarnessAuthSupportSupported,
			NamedSlotStatus:    HarnessAuthSupportSupported,
			NamedSlotExecution: HarnessAuthSupportSupported,
			SlotScope:          "grok_home",
			CredentialOwner:    HarnessCredentialOwnerProvider,
			RiskLabels:         []string{"provider_owned", "native_config_root_isolation"},
			Caveats:            []string{"grok_named_slots_use_per_slot_grok_home", "auth_json_presence_status_only", "no_headless_token_usage"},
		},
	}
}

func (e *GrokExecutor) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	if ctx == nil {
		return ExecutorRun{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return ExecutorRun{}, fmt.Errorf("executor is required")
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return ExecutorRun{}, fmt.Errorf("workspace id is required")
	}
	options := e.options
	for _, option := range req.Options {
		switch typed := option.(type) {
		case invocationModeOption:
			options.InvocationMode = typed.mode
		default:
			return ExecutorRun{}, fmt.Errorf("unsupported grok option %T", option)
		}
	}
	if !harnessInvocationModesContain(e.Descriptor().InvocationModes, options.InvocationMode) {
		return ExecutorRun{}, &HarnessValidationError{Message: fmt.Sprintf("invocation mode %q is not supported by harness %s", options.InvocationMode, HarnessNameGrok), Field: "invocation_mode"}
	}
	if strings.TrimSpace(req.Model) != "" {
		options.Model = strings.TrimSpace(req.Model)
	}
	options.SystemPrompt = strings.TrimSpace(req.Prompt)
	options.AuthProjection = req.AuthProjection
	options, err := options.withGrokAuthSlotProjection(req.AuthSlotID)
	if err != nil {
		return ExecutorRun{}, err
	}
	commandResult, err := options.RunCommand(ctx, options, grokStartArgs(options, contextPacketPrompt(req)))
	if err != nil {
		return ExecutorRun{}, err
	}
	parsed, err := parseGrokStreamingEvents(commandResult.Output)
	if err != nil {
		return ExecutorRun{}, err
	}
	if parsed.ErrorMessage != "" && strings.TrimSpace(parsed.SessionID) == "" {
		return ExecutorRun{}, fmt.Errorf("grok run failed: %s", parsed.ErrorMessage)
	}
	sessionID := strings.TrimSpace(parsed.SessionID)
	if sessionID == "" {
		return ExecutorRun{}, fmt.Errorf("grok session id is required")
	}
	items := grokTimelineItemsFromEvents(workspaceID, sessionID, parsed)
	e.mu.Lock()
	e.runs[sessionID] = items
	e.deliveryOptions[sessionID] = options
	e.mu.Unlock()
	run := ExecutorRun{RunID: sessionID, SessionID: sessionID, Executor: HarnessNameGrok, ProviderSessionID: sessionID, ProviderRunID: strings.TrimSpace(parsed.RequestID), ExitCode: commandResult.ExitCode, ProcessSample: commandResult.ProcessSample, CapabilityNames: harnessCapabilitiesToStrings(e.Descriptor().Capabilities), Persistence: HarnessSessionPersistent, ResumeMode: HarnessResumeCLIFlag}
	if cursor, err := json.Marshal(map[string]string{"session_id": sessionID}); err == nil {
		run.ResumeCursor = cursor
	}
	return run, nil
}

func (e *GrokExecutor) Items(ctx context.Context, runID string) ([]TimelineItem, error) {
	_ = ctx
	if e == nil {
		return nil, fmt.Errorf("executor is required")
	}
	e.mu.Lock()
	items, ok := e.runs[strings.TrimSpace(runID)]
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("run %q not found", runID)
	}
	return append([]TimelineItem(nil), items...), nil
}

func (e *GrokExecutor) Stop(ctx context.Context, runID string) error {
	_ = ctx
	_ = runID
	return nil
}

func (e *GrokExecutor) AttemptWorkspaceDelivery(ctx context.Context, attempt WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error) {
	if ctx == nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("executor is required")
	}
	sessionID := strings.TrimSpace(attempt.Delivery.TargetID)
	if sessionID == "" || strings.TrimSpace(attempt.Delivery.DeliveryID) == "" || len(attempt.Delivery.EventIDs) == 0 {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("delivery target session, delivery id, and event ids are required")
	}
	deliveryOptions := e.options
	e.mu.Lock()
	if options, ok := e.deliveryOptions[sessionID]; ok {
		deliveryOptions = options
	}
	e.mu.Unlock()
	args := []string{"-r", sessionID, "-p", grokWorkspaceDeliveryTurn(attempt), "--output-format", "streaming-json", "--no-auto-update"}
	commandResult, commandErr := deliveryOptions.RunCommand(ctx, deliveryOptions, args)
	deliveryResult, parseErr := parseGrokDeliveryOutput(commandResult.Output)
	if commandErr != nil {
		if parseErr == nil && deliveryResult.Status == WorkspaceDeliveryAttemptFailed {
			return deliveryResult, nil
		}
		return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: commandErr.Error()}, commandErr
	}
	if parseErr == nil {
		return deliveryResult, nil
	}
	return WorkspaceDeliveryAttemptResult{}, parseErr
}

func grokWorkspaceDeliveryTurn(attempt WorkspaceDeliveryAttempt) string {
	payload := struct {
		Kind           string `json:"kind"`
		WorkspaceID    string `json:"workspace_id"`
		SubscriptionID string `json:"subscription_id"`
		EventCount     int    `json:"event_count"`
	}{
		Kind:           "ari.workspace_delivery",
		WorkspaceID:    strings.TrimSpace(attempt.Delivery.WorkspaceID),
		SubscriptionID: strings.TrimSpace(attempt.Delivery.SubscriptionID),
		EventCount:     len(attempt.Delivery.EventIDs),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("Ari workspace delivery %s", strings.TrimSpace(attempt.Delivery.DeliveryID))
	}
	return string(encoded)
}

func parseGrokDeliveryOutput(output []byte) (WorkspaceDeliveryAttemptResult, error) {
	parsed, err := parseGrokStreamingEvents(output)
	if err != nil {
		return WorkspaceDeliveryAttemptResult{}, err
	}
	if parsed.ErrorMessage != "" {
		return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptFailed, LastError: parsed.ErrorMessage}, nil
	}
	if parsed.Completed {
		return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}, nil
	}
	return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: "grok delivery output ended without a terminal end event"}, nil
}

// grokParsedEvents aggregates grok's streaming-json NDJSON: text chunks, the
// terminal end event (stopReason + sessionId), and error events. Unknown
// types (thought, auto_compact_*, max_turns_reached) are skipped.
type grokParsedEvents struct {
	Text         string
	SessionID    string
	RequestID    string
	StopReason   string
	Completed    bool
	ErrorMessage string
}

func parseGrokStreamingEvents(output []byte) (grokParsedEvents, error) {
	var parsed grokParsedEvents
	scanner := bufio.NewScanner(bytes.NewReader(bytes.TrimSpace(output)))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	sawEvent := false
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event struct {
			Type       string `json:"type"`
			Data       string `json:"data"`
			Text       string `json:"text"`
			Message    string `json:"message"`
			StopReason string `json:"stopReason"`
			SessionID  string `json:"sessionId"`
			RequestID  string `json:"requestId"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		sawEvent = true
		switch event.Type {
		case "text":
			parsed.Text += event.Data
		case "end":
			parsed.Completed = true
			parsed.StopReason = strings.TrimSpace(event.StopReason)
			parsed.SessionID = strings.TrimSpace(event.SessionID)
			parsed.RequestID = strings.TrimSpace(event.RequestID)
		case "error":
			message := strings.TrimSpace(event.Message)
			if message == "" {
				message = "grok reported an error"
			}
			parsed.ErrorMessage = message
		case "":
			// json output format: a single object with text/sessionId fields.
			if strings.TrimSpace(event.SessionID) != "" {
				parsed.SessionID = strings.TrimSpace(event.SessionID)
				parsed.RequestID = strings.TrimSpace(event.RequestID)
				parsed.StopReason = strings.TrimSpace(event.StopReason)
				parsed.Text = event.Text
				parsed.Completed = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return grokParsedEvents{}, fmt.Errorf("read grok events: %w", err)
	}
	if !sawEvent {
		return grokParsedEvents{}, fmt.Errorf("grok output did not include any events")
	}
	if parsed.ErrorMessage == "" && !parsed.Completed {
		return grokParsedEvents{}, fmt.Errorf("grok output ended without terminal end event")
	}
	return parsed, nil
}

func grokTimelineItemsFromEvents(workspaceID, sessionID string, parsed grokParsedEvents) []TimelineItem {
	metadata := map[string]any{"provider_session_id": sessionID}
	if requestID := strings.TrimSpace(parsed.RequestID); requestID != "" {
		metadata["provider_request_id"] = requestID
	}
	items := []TimelineItem{{ID: sessionID + ":started", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "lifecycle", Status: "running", Sequence: 1, Text: "grok run started", Metadata: metadata}}
	sequence := 2
	if text := strings.TrimSpace(parsed.Text); text != "" {
		items = append(items, TimelineItem{ID: sessionID + ":result", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "agent_text", Status: "completed", Sequence: sequence, Text: text})
		sequence++
	}
	if parsed.ErrorMessage != "" {
		items = append(items, TimelineItem{ID: sessionID + ":error", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "lifecycle", Status: "failed", Sequence: sequence, Text: parsed.ErrorMessage})
		return items
	}
	items = append(items, TimelineItem{ID: sessionID + ":completed", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "lifecycle", Status: "completed", Sequence: sequence, Text: "grok run completed"})
	return items
}

func grokStartArgs(options grokExecutorOptions, prompt string) []string {
	args := []string{"-p", prompt, "--output-format", "streaming-json", "--no-auto-update"}
	if model := strings.TrimSpace(options.Model); model != "" {
		args = append(args, "-m", model)
	}
	if systemPrompt := strings.TrimSpace(options.SystemPrompt); systemPrompt != "" {
		// --rules appends to the system prompt, preserving grok's native
		// tool/system behavior (additive channel, like claude
		// --append-system-prompt).
		args = append(args, "--rules", systemPrompt)
	}
	return args
}

func runGrokCommand(ctx context.Context, options grokExecutorOptions, args []string) (commandRunResult, error) {
	path, executable, err := resolveHarnessExecutable(HarnessNameGrok, options.Executable, "grok")
	if err != nil {
		return commandRunResult{}, err
	}
	return harnessCommand{
		harness:                HarnessNameGrok,
		path:                   path,
		executable:             executable,
		args:                   args,
		cwd:                    options.Cwd,
		projection:             options.AuthProjection,
		startFailedUnavailable: true,
		waitErrWrap:            "run grok",
		keepResultOnWaitErr:    true,
	}.run(ctx)
}
