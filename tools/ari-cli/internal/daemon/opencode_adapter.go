package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

type opencodeExecutorOptions struct {
	Executable        string
	Cwd               string
	Model             string
	AuthProjection    HarnessAuthProjectionPlan
	DeliveryServerURL string
	RunCommand        opencodeCommandRunner
	RunAuthCommand    opencodeAuthCommandRunner
}

type (
	opencodeCommandRunner     func(context.Context, opencodeExecutorOptions, string) (commandRunResult, error)
	opencodeAuthCommandRunner func(context.Context, opencodeExecutorOptions, []string) (commandRunResult, error)
)

type OpenCodeExecutor struct {
	options opencodeExecutorOptions
	mu      sync.Mutex
	runs    map[string][]TimelineItem
}

func NewOpenCodeExecutor(cwd string) *OpenCodeExecutor {
	return newOpenCodeExecutor(opencodeExecutorOptions{Executable: harnessExecutable("opencode", EnvOpenCodeExecutable), Cwd: cwd, RunCommand: runOpenCodeCommand})
}

func NewOpenCodeExecutorForTest(options opencodeExecutorOptions) *OpenCodeExecutor {
	return newOpenCodeExecutor(options)
}

func newOpenCodeExecutor(options opencodeExecutorOptions) *OpenCodeExecutor {
	if strings.TrimSpace(options.Executable) == "" {
		options.Executable = "opencode"
	}
	if options.RunCommand == nil {
		options.RunCommand = runOpenCodeCommand
	}
	if options.RunAuthCommand == nil {
		options.RunAuthCommand = runOpenCodeAuthCommand
	}
	return &OpenCodeExecutor{options: options, runs: map[string][]TimelineItem{}}
}

func (e *OpenCodeExecutor) AuthStatus(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	if ctx == nil {
		return HarnessAuthStatus{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return HarnessAuthStatus{}, fmt.Errorf("executor is required")
	}
	result, err := e.options.RunAuthCommand(ctx, e.options, []string{"auth", "list"})
	if err != nil {
		var unavailable *HarnessUnavailableError
		if errors.As(err, &unavailable) {
			return HarnessAuthStatus{}, err
		}
	}
	if result.ExitCode != nil && *result.ExitCode == 0 && opencodeAuthOutputReady(result.Output, slot) {
		return HarnessAuthStatus{Harness: HarnessNameOpenCode, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthAuthenticated, AriSecretStorage: HarnessAriSecretStorageNone}, nil
	}
	return NewHarnessAuthRequired(HarnessNameOpenCode, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "provider_login", SecretOwnedBy: HarnessNameOpenCode}), nil
}

func (e *OpenCodeExecutor) AuthLogout(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	if ctx == nil {
		return HarnessAuthStatus{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return HarnessAuthStatus{}, fmt.Errorf("executor is required")
	}
	args := []string{"auth", "logout"}
	if provider := opencodeAuthSlotHint(slot); provider != "" {
		args = append(args, "--provider", provider)
	}
	result, err := e.options.RunAuthCommand(ctx, e.options, args)
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	if result.ExitCode != nil && *result.ExitCode != 0 {
		return HarnessAuthStatus{}, &HarnessUnavailableError{Harness: HarnessNameOpenCode, Reason: "auth_logout_failed", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: true}
	}
	return NewHarnessAuthRequired(HarnessNameOpenCode, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "provider_login", SecretOwnedBy: HarnessNameOpenCode}), nil
}

func (e *OpenCodeExecutor) Descriptor() HarnessAdapterDescriptor {
	deliveryCapabilities := []HarnessDeliveryCapability{}
	if e != nil && strings.TrimSpace(e.options.DeliveryServerURL) != "" {
		deliveryCapabilities = []HarnessDeliveryCapability{HarnessDeliveryVisiblePromptTurn}
	}
	return HarnessAdapterDescriptor{
		Name:                    HarnessNameOpenCode,
		Capabilities:            sharedHarnessRuntimeCapabilities(),
		ObservationCapabilities: []HarnessObservationCapability{HarnessObservationUnsupported},
		DeliveryCapabilities:    deliveryCapabilities,
		Auth: HarnessAuthDescriptor{
			StatusCheck:        HarnessAuthSupportSupported,
			Login:              HarnessAuthSupportPartial,
			LoginMethods:       []string{"opencode_interactive"},
			Logout:             HarnessAuthSupportSupported,
			NamedSlotStatus:    HarnessAuthSupportPartial,
			NamedSlotExecution: HarnessAuthSupportSupported,
			SlotScope:          "ari_auth_content",
			CredentialOwner:    HarnessCredentialOwnerProvider,
			RiskLabels:         []string{"provider_owned", "provider_hint_matching", "ari_projected_auth_content", "env_projection_downgrade_risk"},
			Caveats:            []string{"provider_hint_status", "provider_methods_discovery_is_optional", "named_execution_requires_ari_secret_grant"},
		},
	}
}

func (e *OpenCodeExecutor) AuthProviderMethods(ctx context.Context) (HarnessAuthProviderMethodsResponse, error) {
	if ctx == nil {
		return HarnessAuthProviderMethodsResponse{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return HarnessAuthProviderMethodsResponse{}, fmt.Errorf("executor is required")
	}
	return fetchOpenCodeAuthProviderMethods(ctx, e.options)
}

func (e *OpenCodeExecutor) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	if ctx == nil {
		return ExecutorRun{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return ExecutorRun{}, fmt.Errorf("executor is required")
	}
	if !authSlotIsDefaultForHarness(HarnessNameOpenCode, req.AuthSlotID) && !openCodeAuthContentProjectionReady(req.AuthProjection) {
		return ExecutorRun{}, &HarnessUnavailableError{Harness: HarnessNameOpenCode, Reason: "auth_slot_projection_required", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return ExecutorRun{}, fmt.Errorf("workspace id is required")
	}
	options := e.options
	if strings.TrimSpace(req.Model) != "" {
		options.Model = strings.TrimSpace(req.Model)
	}
	options.AuthProjection = req.AuthProjection
	prompt := opencodePromptFromRequest(req)
	commandResult, err := options.RunCommand(ctx, options, prompt)
	if err != nil {
		return ExecutorRun{}, err
	}
	parsed, err := parseOpenCodeEvents(commandResult.Output)
	if err != nil {
		return ExecutorRun{}, err
	}
	if strings.TrimSpace(parsed.SessionID) == "" {
		return ExecutorRun{}, fmt.Errorf("opencode session id is required")
	}
	items := opencodeTimelineItemsFromEvents(workspaceID, parsed)
	e.mu.Lock()
	e.runs[parsed.SessionID] = items
	e.mu.Unlock()
	return ExecutorRun{RunID: parsed.SessionID, SessionID: parsed.SessionID, Executor: HarnessNameOpenCode, ProviderSessionID: parsed.SessionID, ProviderRunID: parsed.SessionID, ExitCode: commandResult.ExitCode, ProcessSample: commandResult.ProcessSample, CapabilityNames: harnessCapabilitiesToStrings(e.Descriptor().Capabilities)}, nil
}

func openCodeAuthContentProjectionReady(projection HarnessAuthProjectionPlan) bool {
	return projection.Owner == HarnessAuthProjectionOwnerAri && projection.Kind == HarnessAuthProjectionAuthContent && strings.TrimSpace(projection.Env["OPENCODE_AUTH_CONTENT"]) != ""
}

func (e *OpenCodeExecutor) Items(ctx context.Context, runID string) ([]TimelineItem, error) {
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

func (e *OpenCodeExecutor) Stop(ctx context.Context, runID string) error {
	_ = ctx
	_ = runID
	return nil
}

func (e *OpenCodeExecutor) AttemptWorkspaceDelivery(ctx context.Context, attempt WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error) {
	if ctx == nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("executor is required")
	}
	serverURL := strings.TrimRight(strings.TrimSpace(e.options.DeliveryServerURL), "/")
	if serverURL == "" {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("opencode delivery server url is required")
	}
	sessionID := strings.TrimSpace(attempt.Delivery.TargetID)
	if sessionID == "" || strings.TrimSpace(attempt.Delivery.DeliveryID) == "" || len(attempt.Delivery.EventIDs) == 0 {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("delivery target session, delivery id, and event ids are required")
	}
	prompt, err := postOpenCodeDeliveryPrompt(ctx, serverURL, sessionID, attempt)
	if err != nil {
		return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: err.Error()}, err
	}
	result, err := fetchOpenCodeDeliveryCompletion(ctx, serverURL, sessionID, prompt.PromptID)
	if err != nil {
		return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: err.Error()}, err
	}
	return result, nil
}

type openCodeDeliveryPromptResponse struct {
	SessionID string `json:"session_id"`
	PromptID  string `json:"prompt_id"`
	Status    string `json:"status"`
}

func postOpenCodeDeliveryPrompt(ctx context.Context, serverURL, sessionID string, attempt WorkspaceDeliveryAttempt) (openCodeDeliveryPromptResponse, error) {
	body, err := json.Marshal(map[string]string{"text": opencodeWorkspaceDeliveryText(attempt), "delivery": "queue", "idempotency_key": strings.TrimSpace(attempt.Delivery.DeliveryID)})
	if err != nil {
		return openCodeDeliveryPromptResponse{}, fmt.Errorf("encode opencode delivery prompt: %w", err)
	}
	endpoint := serverURL + "/api/session/" + url.PathEscape(sessionID) + "/prompt"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return openCodeDeliveryPromptResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return openCodeDeliveryPromptResponse{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return openCodeDeliveryPromptResponse{}, fmt.Errorf("opencode prompt delivery returned HTTP %d", response.StatusCode)
	}
	var prompt openCodeDeliveryPromptResponse
	if err := json.NewDecoder(response.Body).Decode(&prompt); err != nil {
		return openCodeDeliveryPromptResponse{}, fmt.Errorf("decode opencode prompt delivery response: %w", err)
	}
	if strings.TrimSpace(prompt.PromptID) == "" {
		return openCodeDeliveryPromptResponse{}, fmt.Errorf("opencode prompt delivery response missing prompt id")
	}
	return prompt, nil
}

func opencodeWorkspaceDeliveryText(attempt WorkspaceDeliveryAttempt) string {
	payload := struct {
		Kind           string   `json:"kind"`
		DeliveryID     string   `json:"delivery_id"`
		WorkspaceID    string   `json:"workspace_id"`
		SubscriptionID string   `json:"subscription_id"`
		EventIDs       []string `json:"event_ids"`
	}{
		Kind:           "ari.workspace_delivery",
		DeliveryID:     strings.TrimSpace(attempt.Delivery.DeliveryID),
		WorkspaceID:    strings.TrimSpace(attempt.Delivery.WorkspaceID),
		SubscriptionID: strings.TrimSpace(attempt.Delivery.SubscriptionID),
		EventIDs:       append([]string(nil), attempt.Delivery.EventIDs...),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("Ari workspace delivery %s", strings.TrimSpace(attempt.Delivery.DeliveryID))
	}
	return string(encoded)
}

func fetchOpenCodeDeliveryCompletion(ctx context.Context, serverURL, sessionID, promptID string) (WorkspaceDeliveryAttemptResult, error) {
	endpoint := serverURL + "/api/session/" + url.PathEscape(sessionID) + "/events"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return WorkspaceDeliveryAttemptResult{}, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return WorkspaceDeliveryAttemptResult{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("opencode delivery events returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Events []struct {
			Type      string `json:"type"`
			SessionID string `json:"session_id"`
			PromptID  string `json:"prompt_id"`
		} `json:"events"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("decode opencode delivery events: %w", err)
	}
	for _, event := range payload.Events {
		if strings.TrimSpace(event.PromptID) != strings.TrimSpace(promptID) {
			continue
		}
		switch strings.TrimSpace(event.Type) {
		case "prompt.completed", "session.idle":
			return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}, nil
		}
	}
	return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: "opencode prompt delivery admitted without completion event"}, nil
}

type opencodeParsedEvents struct {
	SessionID    string
	Text         string
	InputTokens  int64
	OutputTokens int64
}

type opencodeEvent struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

func parseOpenCodeEvents(output []byte) (opencodeParsedEvents, error) {
	var parsed opencodeParsedEvents
	scanner := bufio.NewScanner(bytes.NewReader(bytes.TrimSpace(output)))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event opencodeEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return opencodeParsedEvents{}, fmt.Errorf("decode opencode event: %w", err)
		}
		applyOpenCodeEvent(&parsed, event)
	}
	if err := scanner.Err(); err != nil {
		return opencodeParsedEvents{}, fmt.Errorf("read opencode events: %w", err)
	}
	return parsed, nil
}

func applyOpenCodeEvent(parsed *opencodeParsedEvents, event opencodeEvent) {
	switch event.Type {
	case "session.status":
		var props struct {
			SessionID string `json:"sessionID"`
		}
		_ = json.Unmarshal(event.Properties, &props)
		if strings.TrimSpace(props.SessionID) != "" {
			parsed.SessionID = strings.TrimSpace(props.SessionID)
		}
	case "message.updated":
		var props struct {
			Info struct {
				SessionID string `json:"sessionID"`
				Tokens    struct {
					Input  int64 `json:"input"`
					Output int64 `json:"output"`
				} `json:"tokens"`
			} `json:"info"`
		}
		_ = json.Unmarshal(event.Properties, &props)
		if strings.TrimSpace(props.Info.SessionID) != "" {
			parsed.SessionID = strings.TrimSpace(props.Info.SessionID)
		}
		parsed.InputTokens = props.Info.Tokens.Input
		parsed.OutputTokens = props.Info.Tokens.Output
	case "message.part.updated":
		var props struct {
			Part struct {
				SessionID string `json:"sessionID"`
				Type      string `json:"type"`
				Text      string `json:"text"`
			} `json:"part"`
		}
		_ = json.Unmarshal(event.Properties, &props)
		if strings.TrimSpace(props.Part.SessionID) != "" {
			parsed.SessionID = strings.TrimSpace(props.Part.SessionID)
		}
		if props.Part.Type == "text" && strings.TrimSpace(props.Part.Text) != "" {
			parsed.Text += props.Part.Text
		}
	}
}

func opencodeTimelineItemsFromEvents(workspaceID string, parsed opencodeParsedEvents) []TimelineItem {
	sessionID := strings.TrimSpace(parsed.SessionID)
	items := []TimelineItem{{ID: sessionID + ":started", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "lifecycle", Status: "running", Sequence: 1, Text: "opencode session started", Metadata: map[string]any{"provider_session_id": sessionID}}}
	sequence := 2
	if text := strings.TrimSpace(parsed.Text); text != "" {
		items = append(items, TimelineItem{ID: sessionID + ":result", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "agent_text", Status: "completed", Sequence: sequence, Text: text})
		sequence++
	}
	if parsed.InputTokens > 0 || parsed.OutputTokens > 0 {
		items = append(items, TimelineItem{ID: sessionID + ":usage", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "telemetry", Status: "completed", Sequence: sequence, Text: "opencode usage updated", Metadata: map[string]any{"input_tokens": fmt.Sprintf("%d", parsed.InputTokens), "output_tokens": fmt.Sprintf("%d", parsed.OutputTokens)}})
		sequence++
	}
	items = append(items, TimelineItem{ID: sessionID + ":completed", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "lifecycle", Status: "completed", Sequence: sequence, Text: "opencode session completed"})
	return items
}

func opencodePromptFromRequest(req ExecutorStartRequest) string {
	prompt := strings.TrimSpace(req.Prompt)
	contextPacket := strings.TrimSpace(req.ContextPacket)
	if prompt == "" {
		return contextPacket
	}
	if contextPacket == "" {
		return prompt
	}
	return prompt + "\n\n" + contextPacket
}

func opencodeArgs(options opencodeExecutorOptions, prompt string) []string {
	args := []string{"run", "--format", "json"}
	if model := strings.TrimSpace(options.Model); model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, prompt)
	return args
}

func runOpenCodeCommand(ctx context.Context, options opencodeExecutorOptions, prompt string) (commandRunResult, error) {
	executable := strings.TrimSpace(options.Executable)
	if executable == "" {
		executable = "opencode"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameOpenCode, Reason: "missing_executable", Executable: executable, Probe: executable + " --version", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}
	cmd := exec.CommandContext(ctx, path, opencodeArgs(options, prompt)...)
	cmd.Dir = strings.TrimSpace(options.Cwd)
	cmd.Env = commandEnvWithProjection(options.AuthProjection)
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return commandRunResult{}, err
	}
	sample := sampleLinuxProcessMetrics(ctx, HarnessSession{PID: cmd.Process.Pid})
	err = cmd.Wait()
	exitCode := cmd.ProcessState.ExitCode()
	if err != nil {
		return commandRunResult{}, fmt.Errorf("run opencode json: %w", err)
	}
	return commandRunResult{Output: []byte(output.String()), ProcessSample: &sample, ExitCode: &exitCode}, nil
}

func runOpenCodeAuthCommand(ctx context.Context, options opencodeExecutorOptions, args []string) (commandRunResult, error) {
	executable := strings.TrimSpace(options.Executable)
	if executable == "" {
		executable = "opencode"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameOpenCode, Reason: "missing_executable", Executable: executable, Probe: executable + " --version", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = strings.TrimSpace(options.Cwd)
	cmd.Env = commandEnvWithProjection(options.AuthProjection)
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameOpenCode, Reason: "start_failed", Executable: executable, Probe: executable + " " + strings.Join(args, " "), RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: true}
	}
	sample := sampleLinuxProcessMetrics(ctx, HarnessSession{PID: cmd.Process.Pid})
	err = cmd.Wait()
	exitCode := cmd.ProcessState.ExitCode()
	return commandRunResult{Output: []byte(output.String()), ProcessSample: &sample, ExitCode: &exitCode}, err
}

func fetchOpenCodeAuthProviderMethods(ctx context.Context, options opencodeExecutorOptions) (HarnessAuthProviderMethodsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	executable := strings.TrimSpace(options.Executable)
	if executable == "" {
		executable = "opencode"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return HarnessAuthProviderMethodsResponse{}, &HarnessUnavailableError{Harness: HarnessNameOpenCode, Reason: "missing_executable", Executable: executable, Probe: executable + " --version", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}
	command := exec.CommandContext(ctx, path, "serve", "--port", "0", "--hostname", "127.0.0.1")
	command.Dir = strings.TrimSpace(options.Cwd)
	command.Env = commandEnvWithProjection(options.AuthProjection)
	pipeReader, pipeWriter := io.Pipe()
	command.Stdout = pipeWriter
	command.Stderr = pipeWriter
	if err := command.Start(); err != nil {
		_ = pipeWriter.Close()
		_ = pipeReader.Close()
		return HarnessAuthProviderMethodsResponse{}, err
	}
	defer func() {
		_ = command.Process.Kill()
		_ = pipeReader.Close()
		_ = pipeWriter.Close()
		_ = command.Wait()
	}()
	serverURL, err := readOpenCodeServerURL(ctx, pipeReader)
	if err != nil {
		return HarnessAuthProviderMethodsResponse{}, err
	}
	connected, err := fetchOpenCodeConnectedProviders(ctx, serverURL)
	if err != nil {
		return HarnessAuthProviderMethodsResponse{}, err
	}
	methods, err := fetchOpenCodeAvailableAuthMethods(ctx, serverURL)
	if err != nil {
		return HarnessAuthProviderMethodsResponse{}, err
	}
	return HarnessAuthProviderMethodsResponse{Status: "ok", Connected: connected, Providers: methods}, nil
}

func fetchOpenCodeAvailableAuthMethods(ctx context.Context, serverURL string) (map[string][]HarnessAuthMethodInfo, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/provider/auth", nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opencode provider auth returned HTTP %d", response.StatusCode)
	}
	var methods map[string][]HarnessAuthMethodInfo
	if err := json.NewDecoder(response.Body).Decode(&methods); err != nil {
		return nil, err
	}
	return methods, nil
}

func fetchOpenCodeConnectedProviders(ctx context.Context, serverURL string) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/provider", nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opencode provider list returned HTTP %d", response.StatusCode)
	}
	var result struct {
		Connected []string `json:"connected"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, err
	}
	slices.Sort(result.Connected)
	return result.Connected, nil
}

func readOpenCodeServerURL(ctx context.Context, reader io.Reader) (string, error) {
	lines := make(chan string, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-done:
				return
			}
		}
		close(lines)
	}()
	urlPattern := regexp.MustCompile(`http://127\.0\.0\.1:[0-9]+`)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return "", fmt.Errorf("opencode server exited before printing URL")
			}
			if match := urlPattern.FindString(line); match != "" {
				return match, nil
			}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func opencodeAuthOutputReady(output []byte, slot HarnessAuthSlot) bool {
	text := strings.ToLower(string(output))
	providerHint := opencodeAuthSlotHint(slot)
	if providerHint == "" && strings.TrimSpace(slot.AuthSlotID) == "opencode-default" {
		if strings.Contains(text, "not authenticated") || strings.Contains(text, "no auth") {
			return false
		}
		return strings.TrimSpace(text) != ""
	}
	if providerHint == "" {
		return false
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, providerHint) {
			return !strings.Contains(line, "not authenticated") && !strings.Contains(line, "no auth")
		}
	}
	return false
}

func opencodeAuthSlotHint(slot HarnessAuthSlot) string {
	for _, value := range []string{slot.ProviderLabel, slot.Label, slot.AuthSlotID} {
		value = strings.ToLower(strings.TrimSpace(value))
		for _, prefix := range []string{"opencode-", "opencode ", "open code "} {
			value = strings.TrimPrefix(value, prefix)
		}
		if value != "" && value != "default" {
			return value
		}
	}
	return ""
}
