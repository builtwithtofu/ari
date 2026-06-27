package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
)

type codexExecutorOptions struct {
	Executable      string
	Cwd             string
	AuthHomeRoot    string
	AuthProjection  HarnessAuthProjectionPlan
	StartTransport  codexTransportStarter
	RunDelivery     codexCommandRunner
	RunAuthCommand  codexAuthCommandRunner
	NotificationCap int
}

type (
	codexTransportStarter  func(context.Context, codexExecutorOptions) (codexTransport, error)
	codexCommandRunner     func(context.Context, codexExecutorOptions, string) (commandRunResult, error)
	codexAuthCommandRunner func(context.Context, codexExecutorOptions, []string) (commandRunResult, error)
)

type codexTransport interface {
	Call(context.Context, string, any, any) error
	Notify(context.Context, string, any) error
	Notifications() <-chan codexNotification
	PID() int
	ProcessSample(context.Context) *ProcessMetricsSample
	Close() error
}

type CodexExecutor struct {
	adapterLifecycle[codexExecutorOptions]
	options codexExecutorOptions
}

func NewCodexExecutor(cwd string) *CodexExecutor {
	return newCodexExecutor(codexExecutorOptions{Executable: harnessExecutable("codex", EnvCodexExecutable), Cwd: cwd, StartTransport: startCodexAppServerTransport})
}

func NewCodexExecutorForTest(options codexExecutorOptions) *CodexExecutor {
	return newCodexExecutor(options)
}

func newCodexExecutor(options codexExecutorOptions) *CodexExecutor {
	if strings.TrimSpace(options.Executable) == "" {
		options.Executable = "codex"
	}
	if options.StartTransport == nil {
		options.StartTransport = startCodexAppServerTransport
	}
	if options.RunDelivery == nil {
		options.RunDelivery = runCodexAppServerDeliveryCommand
	}
	if options.RunAuthCommand == nil {
		options.RunAuthCommand = runCodexAuthCommand
	}
	if options.NotificationCap <= 0 {
		options.NotificationCap = 64
	}
	return &CodexExecutor{adapterLifecycle: newAdapterLifecycle[codexExecutorOptions](HarnessNameCodex), options: options}
}

func safeAuthSlotPathComponent(value string) string {
	value = strings.TrimSpace(value)
	var out strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			out.WriteRune(r)
		default:
			out.WriteByte('-')
		}
	}
	return strings.Trim(out.String(), "-._")
}

func (e *CodexExecutor) AuthStatus(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	if ctx == nil {
		return HarnessAuthStatus{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return HarnessAuthStatus{}, fmt.Errorf("executor is required")
	}
	options := e.options
	projection, err := ResolveNativeAuthSlotProjection(options.AuthProjection, NativeAuthSlotProjectionRequest{Harness: HarnessNameCodex, AuthSlotID: slot.AuthSlotID, EnvKey: "CODEX_HOME", Root: options.AuthHomeRoot, RiskLabels: []string{"provider_owned", "native_config_root_isolation"}})
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	options.AuthProjection = projection
	result, err := options.RunAuthCommand(ctx, options, []string{"login", "status"})
	if err == nil && result.ExitCode != nil && *result.ExitCode == 0 {
		return HarnessAuthStatus{Harness: HarnessNameCodex, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthAuthenticated, AriSecretStorage: HarnessAriSecretStorageNone}, nil
	}
	if unavailable := (*HarnessUnavailableError)(nil); errors.As(err, &unavailable) {
		return HarnessAuthStatus{}, err
	}
	return NewHarnessAuthRequired(HarnessNameCodex, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "device_code", SecretOwnedBy: HarnessNameCodex}), nil
}

func runCodexAuthCommand(ctx context.Context, options codexExecutorOptions, args []string) (commandRunResult, error) {
	path, executable, err := resolveHarnessExecutable(HarnessNameCodex, options.Executable, "codex")
	if err != nil {
		return commandRunResult{}, err
	}
	if len(args) == 2 && args[0] == "login" && args[1] == "--device-auth" {
		return runCodexDeviceAuthCommand(ctx, path, executable, options)
	}
	return harnessCommand{
		harness:                HarnessNameCodex,
		path:                   path,
		executable:             executable,
		args:                   args,
		cwd:                    options.Cwd,
		projection:             options.AuthProjection,
		startFailedUnavailable: true,
		keepResultOnWaitErr:    true,
	}.run(ctx)
}

func runCodexDeviceAuthCommand(ctx context.Context, path, executable string, options codexExecutorOptions) (commandRunResult, error) {
	cmd := exec.Command(path, "login", "--device-auth")
	cmd.Dir = strings.TrimSpace(options.Cwd)
	cmd.Env = commandEnvWithProjection(options.AuthProjection)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return commandRunResult{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return commandRunResult{}, err
	}
	if err := cmd.Start(); err != nil {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "start_failed", Executable: executable, Probe: executable + " login --device-auth", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: true}
	}
	sample := sampleLinuxProcessMetrics(ctx, HarnessSession{PID: cmd.Process.Pid})
	outputCh := make(chan string, 32)
	readPipe := func(reader io.Reader) {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			outputCh <- scanner.Text() + "\n"
		}
	}
	go readPipe(stdout)
	go readPipe(stderr)
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	var output strings.Builder
	for {
		select {
		case line := <-outputCh:
			output.WriteString(line)
			flow := codexAuthFlowFromOutput([]byte(output.String()))
			if flow.VerificationURL != "" && flow.UserCode != "" {
				go func() {
					for {
						select {
						case <-outputCh:
						case <-waitCh:
							return
						}
					}
				}()
				return commandRunResult{Output: []byte(output.String()), ProcessSample: &sample}, nil
			}
		case err := <-waitCh:
			exitCode := -1
			if cmd.ProcessState != nil {
				exitCode = cmd.ProcessState.ExitCode()
			}
			return commandRunResult{Output: []byte(output.String()), ProcessSample: &sample, ExitCode: &exitCode}, err
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return commandRunResult{Output: []byte(output.String()), ProcessSample: &sample}, ctx.Err()
		}
	}
}

func codexAuthFlowFromOutput(output []byte) HarnessAuthRemediation {
	text := string(output)
	verificationURL := firstRegexpGroup(text, `(https?://[^\s]+)`)
	userCode := firstRegexpGroup(text, `(?i)(?:code|user code)[:\s]+([A-Z0-9-]{4,})`)
	flowID := firstRegexpGroup(text, `(?i)(?:flow|login)[-_ ]?id[:\s]+([A-Za-z0-9_.:-]+)`)
	return HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, FlowID: flowID, Method: "device_code", VerificationURL: verificationURL, UserCode: userCode, SecretOwnedBy: HarnessNameCodex}
}

func firstRegexpGroup(text, pattern string) string {
	matches := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func (e *CodexExecutor) AuthStart(ctx context.Context, slot HarnessAuthSlot, method string) (HarnessAuthStatus, error) {
	if ctx == nil {
		return HarnessAuthStatus{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return HarnessAuthStatus{}, fmt.Errorf("executor is required")
	}
	options := e.options
	projection, err := ResolveNativeAuthSlotProjection(options.AuthProjection, NativeAuthSlotProjectionRequest{Harness: HarnessNameCodex, AuthSlotID: slot.AuthSlotID, EnvKey: "CODEX_HOME", Root: options.AuthHomeRoot, RiskLabels: []string{"provider_owned", "native_config_root_isolation"}})
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	options.AuthProjection = projection
	method = strings.TrimSpace(method)
	if method == "" {
		method = "device_code"
	}
	if method == "api_key" {
		return NewHarnessAuthRequired(HarnessNameCodex, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "api_key_provider_setup", SecretOwnedBy: HarnessNameCodex}), nil
	}
	if method == "browser" {
		return NewHarnessAuthRequired(HarnessNameCodex, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "client_provider_login", SecretOwnedBy: HarnessNameCodex}), nil
	}
	if method != "device_code" {
		return HarnessAuthStatus{}, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "auth_method_unsupported", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}
	args := []string{"login", "--device-auth"}
	result, err := options.RunAuthCommand(ctx, options, args)
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	flow := codexAuthFlowFromOutput(result.Output)
	flow.Method = method
	if flow.SecretOwnedBy == "" {
		flow.SecretOwnedBy = HarnessNameCodex
	}
	return HarnessAuthStatus{Harness: HarnessNameCodex, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthInProgress, Remediation: &flow, AriSecretStorage: HarnessAriSecretStorageNone}, nil
}

func (e *CodexExecutor) AuthLogout(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	if ctx == nil {
		return HarnessAuthStatus{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return HarnessAuthStatus{}, fmt.Errorf("executor is required")
	}
	status, err := e.AuthStatus(ctx, slot)
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	if status.Status != HarnessAuthAuthenticated {
		status.Status = HarnessAuthRequired
		return status, nil
	}
	options := e.options
	projection, err := ResolveNativeAuthSlotProjection(options.AuthProjection, NativeAuthSlotProjectionRequest{Harness: HarnessNameCodex, AuthSlotID: slot.AuthSlotID, EnvKey: "CODEX_HOME", Root: options.AuthHomeRoot, RiskLabels: []string{"provider_owned", "native_config_root_isolation"}})
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	options.AuthProjection = projection
	result, err := options.RunAuthCommand(ctx, options, []string{"logout"})
	if result.ExitCode != nil && *result.ExitCode != 0 {
		return HarnessAuthStatus{}, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "auth_logout_failed", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: true}
	}
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	return NewHarnessAuthRequired(HarnessNameCodex, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "device_code", SecretOwnedBy: HarnessNameCodex}), nil
}

func (e *CodexExecutor) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{
		Name:                    HarnessNameCodex,
		DisplayName:             "Codex",
		Capabilities:            sharedHarnessRuntimeCapabilities(),
		ObservationCapabilities: []HarnessObservationCapability{HarnessObservationEventStream},
		DeliveryCapabilities:    []HarnessDeliveryCapability{HarnessDeliveryVisiblePromptTurn},
		InvocationModes:         []HarnessInvocationMode{HarnessInvocationModeServer},
		Auth: HarnessAuthDescriptor{
			StatusCheck:        HarnessAuthSupportSupported,
			Login:              HarnessAuthSupportSupported,
			LoginMethods:       []string{"browser", "device_code", "api_key"},
			Logout:             HarnessAuthSupportSupported,
			NamedSlotStatus:    HarnessAuthSupportSupported,
			NamedSlotExecution: HarnessAuthSupportSupported,
			SlotScope:          "codex_home",
			CredentialOwner:    HarnessCredentialOwnerProvider,
			RiskLabels:         []string{"provider_owned", "native_config_root_isolation"},
			Caveats:            []string{"codex_named_slots_use_per_slot_codex_home"},
		},
	}
}

func (e *CodexExecutor) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
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
	for _, option := range req.Options {
		switch typed := option.(type) {
		case invocationModeOption:
			if typed.mode != HarnessInvocationModeServer {
				return ExecutorRun{}, &HarnessValidationError{Message: fmt.Sprintf("invocation mode %q is not supported by harness %s", typed.mode, HarnessNameCodex), Field: "invocation_mode"}
			}
		default:
			return ExecutorRun{}, fmt.Errorf("unsupported codex option %T", option)
		}
	}
	options := e.options
	options.AuthProjection = req.AuthProjection
	var err error
	projection, err := ResolveNativeAuthSlotProjection(options.AuthProjection, NativeAuthSlotProjectionRequest{Harness: HarnessNameCodex, AuthSlotID: req.AuthSlotID, EnvKey: "CODEX_HOME", Root: options.AuthHomeRoot, RiskLabels: []string{"provider_owned", "native_config_root_isolation"}})
	if err != nil {
		return ExecutorRun{}, err
	}
	options.AuthProjection = projection
	transport, err := options.StartTransport(ctx, options)
	if err != nil {
		return ExecutorRun{}, err
	}
	defer func() { _ = transport.Close() }()

	if err := transport.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "ari", "title": "Ari", "version": "0.1.0"}}, &codexInitializeResult{}); err != nil {
		return ExecutorRun{}, fmt.Errorf("initialize codex app-server: %w", err)
	}
	if err := transport.Notify(ctx, "initialized", nil); err != nil {
		return ExecutorRun{}, fmt.Errorf("acknowledge codex initialization: %w", err)
	}
	var thread codexThreadStartResult
	threadParams := map[string]any{"model": strings.TrimSpace(req.Model), "cwd": strings.TrimSpace(options.Cwd), "approvalPolicy": "never", "sandbox": "workspaceWrite", "experimentalRawEvents": false, "persistExtendedHistory": true}
	if instructions := strings.TrimSpace(req.Prompt); instructions != "" {
		threadParams["baseInstructions"] = instructions
	}
	if err := transport.Call(ctx, "thread/start", threadParams, &thread); err != nil {
		return ExecutorRun{}, fmt.Errorf("start codex thread: %w", err)
	}
	threadID := strings.TrimSpace(thread.Thread.ID)
	if threadID == "" {
		return ExecutorRun{}, fmt.Errorf("codex thread id is required")
	}
	var turn codexTurnStartResult
	if err := transport.Call(ctx, "turn/start", map[string]any{"threadId": threadID, "input": []map[string]string{{"type": "text", "text": contextPacketPrompt(req)}}}, &turn); err != nil {
		return ExecutorRun{}, fmt.Errorf("start codex turn: %w", err)
	}
	turnID := strings.TrimSpace(turn.Turn.ID)
	items, err := collectCodexTimelineItems(ctx, transport.Notifications(), workspaceID, threadID, turnID)
	if err != nil {
		return ExecutorRun{}, err
	}
	e.storeRun(threadID, items, options)
	providerRunID := turnID
	if providerRunID == "" {
		providerRunID = threadID
	}
	run := ExecutorRun{RunID: threadID, SessionID: threadID, Executor: HarnessNameCodex, ProviderSessionID: threadID, ProviderRunID: providerRunID, ProviderThreadID: threadID, PID: transport.PID(), ProcessSample: transport.ProcessSample(ctx), CapabilityNames: harnessCapabilitiesToStrings(e.Descriptor().Capabilities), Persistence: HarnessSessionPersistent, ResumeMode: HarnessResumeJSONRPC}
	if cursor, err := json.Marshal(map[string]string{"thread_id": threadID}); err == nil {
		run.ResumeCursor = cursor
	}
	return run, nil
}

func (e *CodexExecutor) AttemptWorkspaceDelivery(ctx context.Context, attempt WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error) {
	if ctx == nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("executor is required")
	}
	if strings.TrimSpace(attempt.Delivery.TargetID) == "" || strings.TrimSpace(attempt.Delivery.DeliveryID) == "" || len(attempt.Delivery.EventIDs) == 0 {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("delivery target thread id, delivery id, and event ids are required")
	}
	request, err := codexWorkspaceDeliveryAppServerRequest(attempt)
	if err != nil {
		return WorkspaceDeliveryAttemptResult{}, err
	}
	deliveryOptions := e.options
	if threadID := strings.TrimSpace(attempt.Delivery.TargetID); threadID != "" {
		if options, ok := e.deliveryOption(threadID); ok {
			deliveryOptions = options
		}
	}
	commandResult, commandErr := deliveryOptions.RunDelivery(ctx, deliveryOptions, request)
	deliveryResult, parseErr := parseCodexAppServerDeliveryOutput(commandResult.Output)
	if parseErr == nil {
		return deliveryResult, nil
	}
	if commandErr != nil {
		return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: commandErr.Error()}, commandErr
	}
	return WorkspaceDeliveryAttemptResult{}, parseErr
}

type codexInitializeResult struct{}

type codexThreadStartResult struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
}

type codexTurnStartResult struct {
	Turn struct {
		ID string `json:"id"`
	} `json:"turn"`
}

func codexWorkspaceDeliveryAppServerRequest(attempt WorkspaceDeliveryAttempt) (string, error) {
	threadID := strings.TrimSpace(attempt.Delivery.TargetID)
	if threadID == "" {
		return "", fmt.Errorf("delivery target thread id is required")
	}
	messages := []map[string]any{
		{
			"id":     1,
			"method": "initialize",
			"params": map[string]any{"clientInfo": map[string]string{"name": "ari", "title": "Ari", "version": "0.1.0"}},
		},
		{"method": "initialized"},
		{
			"id":     2,
			"method": "turn/start",
			"params": map[string]any{
				"threadId": threadID,
				"input":    []map[string]string{{"type": "text", "text": codexWorkspaceDeliveryTurn(attempt)}},
			},
		},
	}
	var out strings.Builder
	for _, message := range messages {
		encoded, err := json.Marshal(message)
		if err != nil {
			return "", fmt.Errorf("encode codex delivery app-server request: %w", err)
		}
		out.Write(encoded)
		out.WriteByte('\n')
	}
	return out.String(), nil
}

func codexWorkspaceDeliveryTurn(attempt WorkspaceDeliveryAttempt) string {
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

func parseCodexAppServerDeliveryOutput(output []byte) (WorkspaceDeliveryAttemptResult, error) {
	scanner := bufio.NewScanner(strings.NewReader(strings.TrimSpace(string(output))))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	admitted := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var message codexRPCMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			continue
		}
		if message.Error != nil {
			lastError := strings.TrimSpace(message.Error.Message)
			if lastError == "" {
				lastError = "codex app-server delivery failed"
			}
			return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptFailed, LastError: lastError}, nil
		}
		if message.ID != nil && len(message.Result) > 0 {
			admitted = true
			continue
		}
		if strings.TrimSpace(message.Method) == "turn/completed" {
			return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("scan codex app-server delivery output: %w", err)
	}
	if admitted {
		return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: "codex app-server delivery admitted without completion signal"}, nil
	}
	return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("codex app-server delivery output did not include a terminal event")
}

func collectCodexTimelineItems(ctx context.Context, notifications <-chan codexNotification, workspaceID, threadID, turnID string) ([]TimelineItem, error) {
	items := []TimelineItem{{ID: threadID + ":started", WorkspaceID: workspaceID, RunID: threadID, SourceKind: "executor", SourceID: threadID, Kind: "lifecycle", Status: "running", Sequence: 1, Text: "codex thread started", Metadata: map[string]any{"provider_thread_id": threadID}}}
	sequence := 2
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case notification, ok := <-notifications:
			if !ok {
				return nil, fmt.Errorf("codex notification stream ended before turn completed")
			}
			newItems, completed := codexTimelineItemsFromNotification(notification, workspaceID, threadID, turnID, sequence)
			items = append(items, newItems...)
			sequence += len(newItems)
			if completed {
				return items, nil
			}
		}
	}
}

func codexTimelineItemsFromNotification(notification codexNotification, workspaceID, threadID, turnID string, sequence int) ([]TimelineItem, bool) {
	switch notification.Method {
	case "item/completed":
		var params struct {
			Item struct {
				ID   string `json:"id"`
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		_ = json.Unmarshal(notification.Params, &params)
		text := strings.TrimSpace(params.Item.Text)
		if text == "" {
			return nil, false
		}
		providerItemID := strings.TrimSpace(params.Item.ID)
		id := providerItemID
		if id == "" {
			id = fmt.Sprintf("item-%d", sequence)
		}
		return []TimelineItem{{ID: fmt.Sprintf("%s:%s", threadID, id), WorkspaceID: workspaceID, RunID: threadID, SourceKind: "executor", SourceID: threadID, Kind: "agent_text", Status: "completed", Sequence: sequence, Text: text, Metadata: map[string]any{"provider_item_id": providerItemID, "provider_kind": strings.TrimSpace(params.Item.Type), "provider_turn_id": turnID}}}, false
	case "thread/tokenUsage/updated":
		var params struct {
			TokenUsage struct {
				Last struct {
					InputTokens  int64 `json:"inputTokens"`
					OutputTokens int64 `json:"outputTokens"`
				} `json:"last"`
			} `json:"tokenUsage"`
		}
		_ = json.Unmarshal(notification.Params, &params)
		return []TimelineItem{{ID: fmt.Sprintf("%s:token-usage-%d", threadID, sequence), WorkspaceID: workspaceID, RunID: threadID, SourceKind: "executor", SourceID: threadID, Kind: "telemetry", Status: "completed", Sequence: sequence, Text: "codex token usage updated", Metadata: map[string]any{"input_tokens": fmt.Sprintf("%d", params.TokenUsage.Last.InputTokens), "output_tokens": fmt.Sprintf("%d", params.TokenUsage.Last.OutputTokens)}}}, false
	case "turn/completed":
		return []TimelineItem{{ID: threadID + ":completed", WorkspaceID: workspaceID, RunID: threadID, SourceKind: "executor", SourceID: threadID, Kind: "lifecycle", Status: "completed", Sequence: sequence, Text: "codex turn completed"}}, true
	case "error":
		return []TimelineItem{{ID: fmt.Sprintf("%s:error-%d", threadID, sequence), WorkspaceID: workspaceID, RunID: threadID, SourceKind: "executor", SourceID: threadID, Kind: "lifecycle", Status: "failed", Sequence: sequence, Text: string(notification.Params)}}, true
	default:
		return nil, false
	}
}

func startCodexAppServerTransport(ctx context.Context, options codexExecutorOptions) (codexTransport, error) {
	path, executable, err := resolveHarnessExecutable(HarnessNameCodex, options.Executable, "codex")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, "app-server", "--listen", "stdio://")
	cmd.Dir = strings.TrimSpace(options.Cwd)
	cmd.Env = commandEnvWithProjection(options.AuthProjection)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open codex stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open codex stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open codex stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "start_failed", Executable: executable, Probe: executable + " app-server --listen stdio://", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: true}
	}
	transport := newCodexStdioTransport(cmd, stdin, stdout, stderr, options.NotificationCap)
	return transport, nil
}

func runCodexAppServerDeliveryCommand(ctx context.Context, options codexExecutorOptions, request string) (commandRunResult, error) {
	path, executable, err := resolveHarnessExecutable(HarnessNameCodex, options.Executable, "codex")
	if err != nil {
		return commandRunResult{}, err
	}
	return harnessCommand{
		harness:                HarnessNameCodex,
		path:                   path,
		executable:             executable,
		args:                   []string{"app-server"},
		cwd:                    options.Cwd,
		projection:             options.AuthProjection,
		stdin:                  &request,
		startFailedUnavailable: true,
		waitErrWrap:            "run codex app-server delivery",
		keepResultOnWaitErr:    true,
	}.run(ctx)
}

// codexNotification and codexRPCMessage are the codex-facing names for the
// shared JSONL RPC transport types.
type (
	codexNotification = harnessRPCNotification
	codexRPCMessage   = harnessRPCMessage
)

func newCodexStdioTransport(cmd *exec.Cmd, stdin io.WriteCloser, stdout io.Reader, stderr io.Reader, notificationCap int) *jsonlRPCTransport {
	return newJSONLRPCTransport(HarnessNameCodex, cmd, stdin, stdout, stderr, notificationCap, codexNotificationIsTerminal)
}

func codexNotificationIsTerminal(notification harnessRPCNotification) bool {
	switch strings.TrimSpace(notification.Method) {
	case "turn/completed", "error":
		return true
	default:
		return false
	}
}
