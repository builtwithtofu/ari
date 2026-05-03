package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

type opencodeExecutorOptions struct {
	Executable     string
	Cwd            string
	Model          string
	RunCommand     opencodeCommandRunner
	RunAuthCommand opencodeAuthCommandRunner
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
		return HarnessAuthStatus{}, &HarnessUnavailableError{Harness: HarnessNameOpenCode, Reason: "auth_logout_failed", RequiredCapability: HarnessCapabilityAgentRunFromContext, StartInvoked: true}
	}
	return NewHarnessAuthRequired(HarnessNameOpenCode, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "provider_login", SecretOwnedBy: HarnessNameOpenCode}), nil
}

func (e *OpenCodeExecutor) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{Name: HarnessNameOpenCode, Capabilities: []HarnessCapability{HarnessCapabilityAgentRunFromContext, HarnessCapabilityContextPacket, HarnessCapabilityTimelineItems, HarnessCapabilityFinalResponse, HarnessCapabilityMeasuredTokenTelemetry}}
}

func (e *OpenCodeExecutor) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	if ctx == nil {
		return ExecutorRun{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return ExecutorRun{}, fmt.Errorf("executor is required")
	}
	if !authSlotIsDefaultForHarness(HarnessNameOpenCode, req.AuthSlotID) {
		return ExecutorRun{}, &HarnessUnavailableError{Harness: HarnessNameOpenCode, Reason: "auth_slot_selection_unsupported", RequiredCapability: HarnessCapabilityAgentRunFromContext, StartInvoked: false}
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return ExecutorRun{}, fmt.Errorf("workspace id is required")
	}
	options := e.options
	if strings.TrimSpace(req.Model) != "" {
		options.Model = strings.TrimSpace(req.Model)
	}
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
	return ExecutorRun{RunID: parsed.SessionID, Executor: HarnessNameOpenCode, ProviderRunID: parsed.SessionID, ExitCode: commandResult.ExitCode, ProcessSample: commandResult.ProcessSample, CapabilityNames: harnessCapabilitiesToStrings(e.Descriptor().Capabilities)}, nil
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
	parts := make([]string, 0, 2)
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		parts = append(parts, prompt)
	}
	parts = append(parts, strings.TrimSpace(req.ContextPacket))
	return strings.Join(parts, "\n\n")
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
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameOpenCode, Reason: "missing_executable", Executable: executable, Probe: executable + " --version", RequiredCapability: HarnessCapabilityAgentRunFromContext, StartInvoked: false}
	}
	cmd := exec.CommandContext(ctx, path, opencodeArgs(options, prompt)...)
	cmd.Dir = strings.TrimSpace(options.Cwd)
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return commandRunResult{}, err
	}
	sample := sampleLinuxProcessMetrics(ctx, AgentRun{PID: cmd.Process.Pid})
	err = cmd.Wait()
	exitCode := cmd.ProcessState.ExitCode()
	if err != nil {
		return commandRunResult{}, fmt.Errorf("run opencode json: %w: %s", err, strings.TrimSpace(output.String()))
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
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameOpenCode, Reason: "missing_executable", Executable: executable, Probe: executable + " --version", RequiredCapability: HarnessCapabilityAgentRunFromContext, StartInvoked: false}
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = strings.TrimSpace(options.Cwd)
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameOpenCode, Reason: "start_failed", Executable: executable, Probe: executable + " " + strings.Join(args, " "), RequiredCapability: HarnessCapabilityAgentRunFromContext, StartInvoked: true}
	}
	sample := sampleLinuxProcessMetrics(ctx, AgentRun{PID: cmd.Process.Pid})
	err = cmd.Wait()
	exitCode := cmd.ProcessState.ExitCode()
	return commandRunResult{Output: []byte(output.String()), ProcessSample: &sample, ExitCode: &exitCode}, err
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
