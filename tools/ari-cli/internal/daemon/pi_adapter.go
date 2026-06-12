package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// PiExecutor integrates the pi coding agent (pi.dev). Headless calls run
// `pi -p --mode json`; sticky calls speak pi's --mode rpc line protocol with
// one process per turn, like the codex app-server shape: session continuity
// lives in pi's session files addressed by an ari-chosen --session value, so a
// later turn or delivery reattaches by re-invoking with the same id.
type piExecutorOptions struct {
	Executable     string
	Cwd            string
	Model          string
	SystemPrompt   string
	SessionDir     string
	InvocationMode HarnessInvocationMode
	AuthProjection HarnessAuthProjectionPlan
	RunCommand     piCommandRunner
	LookupEnv      func(string) string
}

type piCommandRunner func(context.Context, piExecutorOptions, []string, string) (commandRunResult, error)

type PiExecutor struct {
	options         piExecutorOptions
	mu              sync.Mutex
	runs            map[string][]TimelineItem
	deliveryOptions map[string]piExecutorOptions
}

func NewPiExecutor(cwd string) *PiExecutor {
	return newPiExecutor(piExecutorOptions{Executable: harnessExecutable("pi", EnvPiExecutable), Cwd: cwd, RunCommand: runPiCommand})
}

func NewPiExecutorForTest(options piExecutorOptions) *PiExecutor {
	return newPiExecutor(options)
}

func newPiExecutor(options piExecutorOptions) *PiExecutor {
	if strings.TrimSpace(options.Executable) == "" {
		options.Executable = "pi"
	}
	if options.InvocationMode == "" {
		options.InvocationMode = HarnessInvocationModeServer
	}
	if options.RunCommand == nil {
		options.RunCommand = runPiCommand
	}
	if options.LookupEnv == nil {
		options.LookupEnv = os.Getenv
	}
	return &PiExecutor{options: options, runs: map[string][]TimelineItem{}, deliveryOptions: map[string]piExecutorOptions{}}
}

// piProviderKeyEnvVars are the provider API key variables pi consumes. Auth
// status is a presence probe over these (projection first, then process env);
// pi has no provider-side status or logout command.
var piProviderKeyEnvVars = []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GOOGLE_API_KEY", "GEMINI_API_KEY", "XAI_API_KEY", "GROQ_API_KEY", "MISTRAL_API_KEY", "OPENROUTER_API_KEY"}

func (options piExecutorOptions) withPiAuthSlotProjection(authSlotID string) (piExecutorOptions, error) {
	authSlotID = strings.TrimSpace(authSlotID)
	if authSlotIsDefaultForHarness(HarnessNamePi, authSlotID) {
		return options, nil
	}
	if !piEnvProjectionReady(options.AuthProjection) {
		return piExecutorOptions{}, &HarnessUnavailableError{Harness: HarnessNamePi, Reason: "auth_slot_projection_required", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}
	return options, nil
}

// piEnvProjectionReady reports whether a named slot carries an ari-owned env
// projection of provider keys produced by the daemon secrets boundary.
func piEnvProjectionReady(projection HarnessAuthProjectionPlan) bool {
	if projection.Owner != HarnessAuthProjectionOwnerAri || projection.Kind != HarnessAuthProjectionEnv {
		return false
	}
	for _, key := range piProviderKeyEnvVars {
		if strings.TrimSpace(projection.Env[key]) != "" {
			return true
		}
	}
	return false
}

func (e *PiExecutor) AuthStatus(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	if ctx == nil {
		return HarnessAuthStatus{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return HarnessAuthStatus{}, fmt.Errorf("executor is required")
	}
	for _, key := range piProviderKeyEnvVars {
		if strings.TrimSpace(e.options.AuthProjection.Env[key]) != "" || strings.TrimSpace(e.options.LookupEnv(key)) != "" {
			return HarnessAuthStatus{Harness: HarnessNamePi, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthAuthenticated, AriSecretStorage: HarnessAriSecretStorageNone}, nil
		}
	}
	return NewHarnessAuthRequired(HarnessNamePi, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "provider_env_key", SecretOwnedBy: HarnessNamePi}), nil
}

func (e *PiExecutor) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{
		Name:                    HarnessNamePi,
		DisplayName:             "pi",
		Capabilities:            sharedHarnessRuntimeCapabilities(),
		ObservationCapabilities: []HarnessObservationCapability{HarnessObservationEventStream},
		DeliveryCapabilities:    []HarnessDeliveryCapability{HarnessDeliveryVisiblePromptTurn},
		InvocationModes:         []HarnessInvocationMode{HarnessInvocationModeHeadless, HarnessInvocationModeServer},
		Auth: HarnessAuthDescriptor{
			StatusCheck:        HarnessAuthSupportPartial,
			Login:              HarnessAuthSupportPartial,
			LoginMethods:       []string{"provider_env_key", "pi_interactive"},
			Logout:             HarnessAuthSupportUnsupported,
			NamedSlotStatus:    HarnessAuthSupportPartial,
			NamedSlotExecution: HarnessAuthSupportSupported,
			SlotScope:          "ari_env_keys",
			CredentialOwner:    HarnessCredentialOwnerProvider,
			RiskLabels:         []string{"provider_owned", "ari_projected_env_keys", "env_projection_downgrade_risk"},
			Caveats:            []string{"env_key_presence_status_only", "named_execution_requires_ari_secret_grant", "no_provider_logout"},
		},
	}
}

func (e *PiExecutor) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
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
			return ExecutorRun{}, fmt.Errorf("unsupported pi option %T", option)
		}
	}
	if !harnessInvocationModesContain(e.Descriptor().InvocationModes, options.InvocationMode) {
		return ExecutorRun{}, &HarnessValidationError{Message: fmt.Sprintf("invocation mode %q is not supported by harness %s", options.InvocationMode, HarnessNamePi), Field: "invocation_mode"}
	}
	if strings.TrimSpace(req.Model) != "" {
		options.Model = strings.TrimSpace(req.Model)
	}
	options.SystemPrompt = strings.TrimSpace(req.Prompt)
	options.AuthProjection = req.AuthProjection
	options, err := options.withPiAuthSlotProjection(req.AuthSlotID)
	if err != nil {
		return ExecutorRun{}, err
	}
	sessionID := strings.TrimSpace(req.RunID)
	if sessionID == "" {
		return ExecutorRun{}, fmt.Errorf("pi session id is required")
	}
	commandResult, err := options.RunCommand(ctx, options, piStartArgs(options, sessionID), piStartInput(options, req))
	if err != nil {
		return ExecutorRun{}, err
	}
	parsed, err := parsePiEvents(commandResult.Output)
	if err != nil {
		return ExecutorRun{}, err
	}
	items := piTimelineItemsFromEvents(workspaceID, sessionID, options.InvocationMode, parsed)
	e.mu.Lock()
	e.runs[sessionID] = items
	e.deliveryOptions[sessionID] = options
	e.mu.Unlock()
	resumeMode := HarnessResumeCLIFlag
	if options.InvocationMode == HarnessInvocationModeServer {
		resumeMode = HarnessResumeJSONRPC
	}
	run := ExecutorRun{RunID: sessionID, SessionID: sessionID, Executor: HarnessNamePi, ProviderSessionID: sessionID, ProviderRunID: sessionID, ExitCode: commandResult.ExitCode, ProcessSample: commandResult.ProcessSample, CapabilityNames: harnessCapabilitiesToStrings(e.Descriptor().Capabilities), Persistence: HarnessSessionPersistent, ResumeMode: resumeMode}
	if cursor, err := json.Marshal(map[string]string{"session_id": sessionID}); err == nil {
		run.ResumeCursor = cursor
	}
	return run, nil
}

func (e *PiExecutor) Items(ctx context.Context, runID string) ([]TimelineItem, error) {
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

func (e *PiExecutor) Stop(ctx context.Context, runID string) error {
	_ = ctx
	_ = runID
	return nil
}

func (e *PiExecutor) AttemptWorkspaceDelivery(ctx context.Context, attempt WorkspaceDeliveryAttempt) (WorkspaceDeliveryAttemptResult, error) {
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
	command, err := json.Marshal(map[string]string{"type": "prompt", "id": "ari-delivery", "message": piWorkspaceDeliveryTurn(attempt)})
	if err != nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("encode pi delivery prompt: %w", err)
	}
	commandResult, commandErr := deliveryOptions.RunCommand(ctx, deliveryOptions, piRPCArgs(deliveryOptions, sessionID), string(command)+"\n")
	deliveryResult, parseErr := parsePiDeliveryOutput(commandResult.Output)
	if parseErr == nil {
		return deliveryResult, nil
	}
	if commandErr != nil {
		return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: commandErr.Error()}, commandErr
	}
	return WorkspaceDeliveryAttemptResult{}, parseErr
}

func piWorkspaceDeliveryTurn(attempt WorkspaceDeliveryAttempt) string {
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

func parsePiDeliveryOutput(output []byte) (WorkspaceDeliveryAttemptResult, error) {
	admitted := false
	scanner := bufio.NewScanner(bytes.NewReader(bytes.TrimSpace(output)))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event piEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		switch event.Type {
		case "response":
			if event.Command == "prompt" {
				if !event.Success {
					lastError := strings.TrimSpace(event.Error)
					if lastError == "" {
						lastError = "pi rpc delivery prompt rejected"
					}
					return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptFailed, LastError: lastError}, nil
				}
				admitted = true
			}
		case "agent_end":
			return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptCompleted}, nil
		case "error":
			lastError := strings.TrimSpace(event.Message)
			if lastError == "" {
				lastError = "pi rpc delivery failed"
			}
			return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptFailed, LastError: lastError}, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("scan pi delivery output: %w", err)
	}
	if admitted {
		return WorkspaceDeliveryAttemptResult{Status: WorkspaceDeliveryAttemptRetry, LastError: "pi delivery admitted without agent_end"}, nil
	}
	return WorkspaceDeliveryAttemptResult{}, fmt.Errorf("pi delivery output did not include a terminal event")
}

// piEvent is the subset of pi's NDJSON stream used for delivery parsing.
// Unknown event types (extension UI requests, deltas, queue updates) are
// ignored so extension-heavy pi setups do not break parsing.
type piEvent struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

type piAssistantMessage struct {
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage *struct {
		Input  int64 `json:"input"`
		Output int64 `json:"output"`
	} `json:"usage"`
	StopReason string `json:"stopReason"`
}

type piParsedEvents struct {
	Text         string
	InputTokens  int64
	OutputTokens int64
	HasUsage     bool
	ToolNames    []string
	Completed    bool
	ErrorMessage string
}

func parsePiEvents(output []byte) (piParsedEvents, error) {
	var parsed piParsedEvents
	scanner := bufio.NewScanner(bytes.NewReader(bytes.TrimSpace(output)))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	sawEvent := false
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var raw struct {
			Type     string          `json:"type"`
			Message  json.RawMessage `json:"message"`
			ToolName string          `json:"toolName"`
			Error    string          `json:"error"`
			Msg      string          `json:"msg"`
			Text     string          `json:"text"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		sawEvent = true
		switch raw.Type {
		case "message_end", "turn_end":
			var message piAssistantMessage
			if len(raw.Message) > 0 && json.Unmarshal(raw.Message, &message) == nil {
				if message.Role == "assistant" {
					for _, content := range message.Content {
						if content.Type == "text" && strings.TrimSpace(content.Text) != "" {
							if parsed.Text != "" {
								parsed.Text += "\n"
							}
							parsed.Text += content.Text
						}
					}
					if message.Usage != nil {
						parsed.InputTokens = message.Usage.Input
						parsed.OutputTokens = message.Usage.Output
						parsed.HasUsage = true
					}
				}
			}
		case "tool_execution_end":
			if strings.TrimSpace(raw.ToolName) != "" {
				parsed.ToolNames = append(parsed.ToolNames, strings.TrimSpace(raw.ToolName))
			}
		case "agent_end":
			parsed.Completed = true
		case "error", "extension_error":
			message := strings.TrimSpace(raw.Error)
			if message == "" {
				// pi error events may carry the description as a string
				// "message" field; message_end reuses the same key for an
				// object, so decode it only here.
				var text string
				if json.Unmarshal(raw.Message, &text) == nil {
					message = strings.TrimSpace(text)
				}
			}
			if message == "" {
				message = strings.TrimSpace(raw.Msg)
			}
			if message == "" {
				message = strings.TrimSpace(raw.Text)
			}
			if message == "" {
				message = "pi reported an error"
			}
			parsed.ErrorMessage = message
		}
	}
	if err := scanner.Err(); err != nil {
		return piParsedEvents{}, fmt.Errorf("read pi events: %w", err)
	}
	if !sawEvent {
		return piParsedEvents{}, fmt.Errorf("pi output did not include any events")
	}
	if parsed.ErrorMessage == "" && !parsed.Completed {
		return piParsedEvents{}, fmt.Errorf("pi output ended without terminal agent_end event")
	}
	return parsed, nil
}

func piTimelineItemsFromEvents(workspaceID, sessionID string, mode HarnessInvocationMode, parsed piParsedEvents) []TimelineItem {
	metadata := map[string]any{"provider_session_id": sessionID, "invocation_mode": string(mode)}
	items := []TimelineItem{{ID: sessionID + ":started", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "lifecycle", Status: "running", Sequence: 1, Text: "pi run started", Metadata: metadata}}
	sequence := 2
	for _, toolName := range parsed.ToolNames {
		items = append(items, TimelineItem{ID: fmt.Sprintf("%s:tool-%d", sessionID, sequence), WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "tool", Status: "completed", Sequence: sequence, Text: toolName})
		sequence++
	}
	if text := strings.TrimSpace(parsed.Text); text != "" {
		items = append(items, TimelineItem{ID: sessionID + ":result", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "agent_text", Status: "completed", Sequence: sequence, Text: text})
		sequence++
	}
	if parsed.HasUsage {
		items = append(items, TimelineItem{ID: sessionID + ":usage", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "telemetry", Status: "completed", Sequence: sequence, Text: "pi usage updated", Metadata: map[string]any{"input_tokens": fmt.Sprintf("%d", parsed.InputTokens), "output_tokens": fmt.Sprintf("%d", parsed.OutputTokens)}})
		sequence++
	}
	if parsed.ErrorMessage != "" {
		items = append(items, TimelineItem{ID: sessionID + ":error", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "lifecycle", Status: "failed", Sequence: sequence, Text: parsed.ErrorMessage})
		return items
	}
	items = append(items, TimelineItem{ID: sessionID + ":completed", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "lifecycle", Status: "completed", Sequence: sequence, Text: "pi run completed"})
	return items
}

func piStartArgs(options piExecutorOptions, sessionID string) []string {
	if options.InvocationMode == HarnessInvocationModeServer {
		return piRPCArgs(options, sessionID)
	}
	args := []string{"-p", "--mode", "json", "--session", sessionID}
	args = append(args, piCommonArgs(options)...)
	return args
}

func piRPCArgs(options piExecutorOptions, sessionID string) []string {
	args := []string{"--mode", "rpc", "--session", sessionID}
	return append(args, piCommonArgs(options)...)
}

func piCommonArgs(options piExecutorOptions) []string {
	args := []string(nil)
	if dir := strings.TrimSpace(options.SessionDir); dir != "" {
		args = append(args, "--session-dir", dir)
	}
	if model := strings.TrimSpace(options.Model); model != "" {
		args = append(args, "--model", model)
	}
	if systemPrompt := strings.TrimSpace(options.SystemPrompt); systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}
	return args
}

// piStartInput renders the process input for a start turn: the context packet
// as a positional message in headless mode, or a prompt command line in RPC
// mode.
func piStartInput(options piExecutorOptions, req ExecutorStartRequest) string {
	packet := strings.TrimSpace(req.ContextPacket)
	if options.InvocationMode != HarnessInvocationModeServer {
		return packet
	}
	command, err := json.Marshal(map[string]string{"type": "prompt", "id": "ari-start", "message": packet})
	if err != nil {
		return packet
	}
	return string(command) + "\n"
}

func runPiCommand(ctx context.Context, options piExecutorOptions, args []string, input string) (commandRunResult, error) {
	executable := strings.TrimSpace(options.Executable)
	if executable == "" {
		executable = "pi"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNamePi, Reason: "missing_executable", Executable: executable, Probe: executable + " --version", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}
	rpcMode := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--mode" && args[i+1] == "rpc" {
			rpcMode = true
		}
	}
	if !rpcMode {
		if trimmed := strings.TrimSpace(input); trimmed != "" {
			args = append(args, trimmed)
		}
		input = ""
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = strings.TrimSpace(options.Cwd)
	cmd.Env = commandEnvWithProjection(options.AuthProjection)
	var stdin io.WriteCloser
	if rpcMode {
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return commandRunResult{}, err
		}
	}
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNamePi, Reason: "start_failed", Executable: executable, Probe: executable + " " + strings.Join(args, " "), RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: true}
	}
	if stdin != nil {
		_, _ = io.WriteString(stdin, input)
		_ = stdin.Close()
	}
	sample := sampleLinuxProcessMetrics(ctx, HarnessSession{PID: cmd.Process.Pid})
	err = cmd.Wait()
	exitCode := cmd.ProcessState.ExitCode()
	result := commandRunResult{Output: []byte(output.String()), ProcessSample: &sample, ExitCode: &exitCode}
	if err != nil {
		return result, fmt.Errorf("run pi: %w", err)
	}
	return result, nil
}
