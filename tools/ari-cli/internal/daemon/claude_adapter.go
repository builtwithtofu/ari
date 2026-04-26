package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

type claudeExecutorOptions struct {
	Executable string
	Cwd        string
	Model      string
	RunCommand claudeCommandRunner
}

type commandRunResult struct {
	Output        []byte
	ProcessSample *ProcessMetricsSample
	ExitCode      *int
}

type claudeCommandRunner func(context.Context, claudeExecutorOptions, string) (commandRunResult, error)

type ClaudeExecutor struct {
	options claudeExecutorOptions
	mu      sync.Mutex
	runs    map[string][]TimelineItem
}

func NewClaudeExecutor(cwd string) *ClaudeExecutor {
	return newClaudeExecutor(claudeExecutorOptions{Executable: "claude", Cwd: cwd, RunCommand: runClaudeCommand})
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
	return &ClaudeExecutor{options: options, runs: map[string][]TimelineItem{}}
}

func (e *ClaudeExecutor) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{Name: HarnessNameClaude, Capabilities: []HarnessCapability{HarnessCapabilityAgentRunFromContext, HarnessCapabilityContextPacket, HarnessCapabilityTimelineItems, HarnessCapabilityFinalResponse, HarnessCapabilityMeasuredTokenTelemetry}}
}

func (e *ClaudeExecutor) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
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
	return ExecutorRun{RunID: sessionID, Executor: HarnessNameClaude, ProviderRunID: sessionID, ExitCode: commandResult.ExitCode, ProcessSample: commandResult.ProcessSample, CapabilityNames: harnessCapabilitiesToStrings(e.Descriptor().Capabilities)}, nil
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
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameClaude, Reason: "missing_executable", Executable: executable, Probe: executable + " --version", RequiredCapability: HarnessCapabilityAgentRunFromContext, StartInvoked: false}
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
	sample := sampleLinuxProcessMetrics(ctx, AgentRun{PID: cmd.Process.Pid})
	err = cmd.Wait()
	exitCode := cmd.ProcessState.ExitCode()
	if err != nil {
		return commandRunResult{}, fmt.Errorf("run claude headless: %w: %s", err, strings.TrimSpace(output.String()))
	}
	return commandRunResult{Output: []byte(output.String()), ProcessSample: &sample, ExitCode: &exitCode}, nil
}
