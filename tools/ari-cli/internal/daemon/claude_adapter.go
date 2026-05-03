package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

type claudeExecutorOptions struct {
	Executable     string
	Cwd            string
	Model          string
	RunCommand     claudeCommandRunner
	RunAuthCommand claudeAuthCommandRunner
}

type commandRunResult struct {
	Output        []byte
	ProcessSample *ProcessMetricsSample
	ExitCode      *int
}

type (
	claudeCommandRunner     func(context.Context, claudeExecutorOptions, string) (commandRunResult, error)
	claudeAuthCommandRunner func(context.Context, claudeExecutorOptions, []string) (commandRunResult, error)
)

type ClaudeExecutor struct {
	options claudeExecutorOptions
	mu      sync.Mutex
	runs    map[string][]TimelineItem
}

func NewClaudeExecutor(cwd string) *ClaudeExecutor {
	return newClaudeExecutor(claudeExecutorOptions{Executable: harnessExecutable("claude", EnvClaudeExecutable), Cwd: cwd, RunCommand: runClaudeCommand})
}

func NewClaudeExecutorForTest(options claudeExecutorOptions) *ClaudeExecutor {
	return newClaudeExecutor(options)
}

func newClaudeExecutor(options claudeExecutorOptions) *ClaudeExecutor {
	if strings.TrimSpace(options.Executable) == "" {
		options.Executable = "claude"
	}
	if options.RunCommand == nil {
		options.RunCommand = runClaudeCommand
	}
	if options.RunAuthCommand == nil {
		options.RunAuthCommand = runClaudeAuthCommand
	}
	return &ClaudeExecutor{options: options, runs: map[string][]TimelineItem{}}
}

func (e *ClaudeExecutor) AuthStatus(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	if ctx == nil {
		return HarnessAuthStatus{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return HarnessAuthStatus{}, fmt.Errorf("executor is required")
	}
	if !authSlotIsDefaultForHarness(HarnessNameClaude, slot.AuthSlotID) {
		return NewHarnessAuthRequired(HarnessNameClaude, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "provider_owned_slot_binding", SecretOwnedBy: HarnessNameClaude}), nil
	}
	result, err := e.options.RunAuthCommand(ctx, e.options, []string{"auth", "status", "--json"})
	if err != nil {
		var unavailable *HarnessUnavailableError
		if errors.As(err, &unavailable) {
			return HarnessAuthStatus{}, err
		}
	}
	if result.ExitCode != nil && *result.ExitCode == 0 && claudeAuthOutputAuthenticated(result.Output) {
		return HarnessAuthStatus{Harness: HarnessNameClaude, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthAuthenticated, AriSecretStorage: HarnessAriSecretStorageNone}, nil
	}
	return NewHarnessAuthRequired(HarnessNameClaude, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "provider_config", SecretOwnedBy: HarnessNameClaude}), nil
}

func (e *ClaudeExecutor) AuthLogout(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	if ctx == nil {
		return HarnessAuthStatus{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return HarnessAuthStatus{}, fmt.Errorf("executor is required")
	}
	if !authSlotIsDefaultForHarness(HarnessNameClaude, slot.AuthSlotID) {
		return HarnessAuthStatus{}, &HarnessUnavailableError{Harness: HarnessNameClaude, Reason: "auth_slot_selection_unsupported", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
	}
	result, err := e.options.RunAuthCommand(ctx, e.options, []string{"auth", "logout"})
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	if result.ExitCode != nil && *result.ExitCode != 0 {
		return HarnessAuthStatus{}, &HarnessUnavailableError{Harness: HarnessNameClaude, Reason: "auth_logout_failed", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: true}
	}
	return NewHarnessAuthRequired(HarnessNameClaude, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "provider_config", SecretOwnedBy: HarnessNameClaude}), nil
}

func (e *ClaudeExecutor) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{Name: HarnessNameClaude, Capabilities: []HarnessCapability{HarnessCapabilityAgentSessionFromContext, HarnessCapabilityContextPacket, HarnessCapabilityTimelineItems, HarnessCapabilityFinalResponse, HarnessCapabilityMeasuredTokenTelemetry}}
}

func (e *ClaudeExecutor) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	if ctx == nil {
		return ExecutorRun{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return ExecutorRun{}, fmt.Errorf("executor is required")
	}
	if !authSlotIsDefaultForHarness(HarnessNameClaude, req.AuthSlotID) {
		return ExecutorRun{}, &HarnessUnavailableError{Harness: HarnessNameClaude, Reason: "auth_slot_selection_unsupported", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return ExecutorRun{}, fmt.Errorf("workspace id is required")
	}
	options := e.options
	if strings.TrimSpace(req.Model) != "" {
		options.Model = strings.TrimSpace(req.Model)
	}
	commandResult, err := options.RunCommand(ctx, options, claudePromptFromRequest(req))
	if err != nil {
		return ExecutorRun{}, err
	}
	result, err := parseClaudeJSONResult(commandResult.Output)
	if err != nil {
		return ExecutorRun{}, err
	}
	sessionID := strings.TrimSpace(result.SessionID)
	if sessionID == "" {
		return ExecutorRun{}, fmt.Errorf("claude session id is required")
	}
	items := claudeTimelineItemsFromResult(workspaceID, sessionID, result)
	e.mu.Lock()
	e.runs[sessionID] = items
	e.mu.Unlock()
	return ExecutorRun{RunID: sessionID, SessionID: sessionID, Executor: HarnessNameClaude, ProviderSessionID: sessionID, ProviderRunID: sessionID, ExitCode: commandResult.ExitCode, ProcessSample: commandResult.ProcessSample, CapabilityNames: harnessCapabilitiesToStrings(e.Descriptor().Capabilities)}, nil
}

func (e *ClaudeExecutor) Items(ctx context.Context, runID string) ([]TimelineItem, error) {
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

func (e *ClaudeExecutor) Stop(ctx context.Context, runID string) error {
	_ = ctx
	_ = runID
	return nil
}

type claudeJSONResult struct {
	Result       string `json:"result"`
	SessionID    string `json:"session_id"`
	TotalCostUSD any    `json:"total_cost_usd,omitempty"`
	Usage        struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

func parseClaudeJSONResult(output []byte) (claudeJSONResult, error) {
	var result claudeJSONResult
	if err := json.Unmarshal(bytes.TrimSpace(output), &result); err != nil {
		return claudeJSONResult{}, fmt.Errorf("decode claude json result: %w", err)
	}
	return result, nil
}

func claudeTimelineItemsFromResult(workspaceID, sessionID string, result claudeJSONResult) []TimelineItem {
	items := []TimelineItem{{ID: sessionID + ":started", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "lifecycle", Status: "running", Sequence: 1, Text: "claude run started", Metadata: map[string]any{"provider_session_id": sessionID}}}
	sequence := 2
	if text := strings.TrimSpace(result.Result); text != "" {
		items = append(items, TimelineItem{ID: sessionID + ":result", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "agent_text", Status: "completed", Sequence: sequence, Text: text})
		sequence++
	}
	if result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0 {
		items = append(items, TimelineItem{ID: sessionID + ":usage", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "telemetry", Status: "completed", Sequence: sequence, Text: "claude usage updated", Metadata: map[string]any{"input_tokens": fmt.Sprintf("%d", result.Usage.InputTokens), "output_tokens": fmt.Sprintf("%d", result.Usage.OutputTokens)}})
		sequence++
	}
	items = append(items, TimelineItem{ID: sessionID + ":completed", WorkspaceID: workspaceID, RunID: sessionID, SourceKind: "executor", SourceID: sessionID, Kind: "lifecycle", Status: "completed", Sequence: sequence, Text: "claude run completed"})
	return items
}

func claudePromptFromRequest(req ExecutorStartRequest) string {
	parts := make([]string, 0, 2)
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		parts = append(parts, prompt)
	}
	parts = append(parts, strings.TrimSpace(req.ContextPacket))
	return strings.Join(parts, "\n\n")
}

func claudeArgs(options claudeExecutorOptions) []string {
	args := []string{"--bare", "-p", "-", "--output-format", "json"}
	if model := strings.TrimSpace(options.Model); model != "" {
		args = append(args, "--model", model)
	}
	return args
}

func runClaudeCommand(ctx context.Context, options claudeExecutorOptions, prompt string) (commandRunResult, error) {
	executable := strings.TrimSpace(options.Executable)
	if executable == "" {
		executable = "claude"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameClaude, Reason: "missing_executable", Executable: executable, Probe: executable + " --version", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
	}
	cmd := exec.CommandContext(ctx, path, claudeArgs(options)...)
	cmd.Dir = strings.TrimSpace(options.Cwd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return commandRunResult{}, err
	}
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return commandRunResult{}, err
	}
	_, _ = io.WriteString(stdin, prompt)
	_ = stdin.Close()
	sample := sampleLinuxProcessMetrics(ctx, AgentSession{PID: cmd.Process.Pid})
	err = cmd.Wait()
	exitCode := cmd.ProcessState.ExitCode()
	if err != nil {
		return commandRunResult{}, fmt.Errorf("run claude headless: %w: %s", err, strings.TrimSpace(output.String()))
	}
	return commandRunResult{Output: []byte(output.String()), ProcessSample: &sample, ExitCode: &exitCode}, nil
}

func runClaudeAuthCommand(ctx context.Context, options claudeExecutorOptions, args []string) (commandRunResult, error) {
	executable := strings.TrimSpace(options.Executable)
	if executable == "" {
		executable = "claude"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameClaude, Reason: "missing_executable", Executable: executable, Probe: executable + " --version", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = strings.TrimSpace(options.Cwd)
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameClaude, Reason: "start_failed", Executable: executable, Probe: executable + " " + strings.Join(args, " "), RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: true}
	}
	sample := sampleLinuxProcessMetrics(ctx, AgentSession{PID: cmd.Process.Pid})
	err = cmd.Wait()
	exitCode := cmd.ProcessState.ExitCode()
	return commandRunResult{Output: []byte(output.String()), ProcessSample: &sample, ExitCode: &exitCode}, err
}

func claudeAuthOutputAuthenticated(output []byte) bool {
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 {
		return false
	}
	var status map[string]any
	if err := json.Unmarshal(trimmed, &status); err != nil {
		return !bytes.Contains(bytes.ToLower(trimmed), []byte("not authenticated"))
	}
	for _, key := range []string{"authenticated", "logged_in", "loggedIn", "valid"} {
		if value, ok := status[key].(bool); ok && value {
			return true
		}
	}
	if value, ok := status["status"].(string); ok {
		return strings.EqualFold(strings.TrimSpace(value), "authenticated")
	}
	return false
}
