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
	"sync"
	"sync/atomic"
	"time"
)

type codexExecutorOptions struct {
	Executable      string
	Cwd             string
	StartTransport  codexTransportStarter
	RunAuthCommand  codexAuthCommandRunner
	NotificationCap int
}

type (
	codexTransportStarter  func(context.Context, codexExecutorOptions) (codexTransport, error)
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

type codexNotification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type CodexExecutor struct {
	options codexExecutorOptions
	mu      sync.Mutex
	runs    map[string][]TimelineItem
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
	if options.RunAuthCommand == nil {
		options.RunAuthCommand = runCodexAuthCommand
	}
	if options.NotificationCap <= 0 {
		options.NotificationCap = 64
	}
	return &CodexExecutor{options: options, runs: map[string][]TimelineItem{}}
}

func (e *CodexExecutor) AuthStatus(ctx context.Context, slot HarnessAuthSlot) (HarnessAuthStatus, error) {
	if ctx == nil {
		return HarnessAuthStatus{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return HarnessAuthStatus{}, fmt.Errorf("executor is required")
	}
	if !authSlotIsDefaultForHarness(HarnessNameCodex, slot.AuthSlotID) {
		return HarnessAuthStatus{}, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "auth_slot_selection_unsupported", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
	}
	result, err := e.options.RunAuthCommand(ctx, e.options, []string{"login", "status"})
	if err == nil && result.ExitCode != nil && *result.ExitCode == 0 {
		return HarnessAuthStatus{Harness: HarnessNameCodex, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthAuthenticated, AriSecretStorage: HarnessAriSecretStorageNone}, nil
	}
	if unavailable := (*HarnessUnavailableError)(nil); errors.As(err, &unavailable) {
		return HarnessAuthStatus{}, err
	}
	return NewHarnessAuthRequired(HarnessNameCodex, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "device_code", SecretOwnedBy: HarnessNameCodex}), nil
}

func runCodexAuthCommand(ctx context.Context, options codexExecutorOptions, args []string) (commandRunResult, error) {
	executable := strings.TrimSpace(options.Executable)
	if executable == "" {
		executable = "codex"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "missing_executable", Executable: executable, Probe: executable + " --version", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
	}
	if len(args) == 2 && args[0] == "login" && args[1] == "--device-auth" {
		return runCodexDeviceAuthCommand(ctx, path, executable, options)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = strings.TrimSpace(options.Cwd)
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "start_failed", Executable: executable, Probe: executable + " " + strings.Join(args, " "), RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: true}
	}
	sample := sampleLinuxProcessMetrics(ctx, AgentSession{PID: cmd.Process.Pid})
	err = cmd.Wait()
	exitCode := cmd.ProcessState.ExitCode()
	return commandRunResult{Output: []byte(output.String()), ProcessSample: &sample, ExitCode: &exitCode}, err
}

func runCodexDeviceAuthCommand(ctx context.Context, path, executable string, options codexExecutorOptions) (commandRunResult, error) {
	cmd := exec.Command(path, "login", "--device-auth")
	cmd.Dir = strings.TrimSpace(options.Cwd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return commandRunResult{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return commandRunResult{}, err
	}
	if err := cmd.Start(); err != nil {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "start_failed", Executable: executable, Probe: executable + " login --device-auth", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: true}
	}
	sample := sampleLinuxProcessMetrics(ctx, AgentSession{PID: cmd.Process.Pid})
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
	if !authSlotIsDefaultForHarness(HarnessNameCodex, slot.AuthSlotID) {
		return HarnessAuthStatus{}, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "auth_slot_selection_unsupported", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
	}
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
		return HarnessAuthStatus{}, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "auth_method_unsupported", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
	}
	args := []string{"login", "--device-auth"}
	result, err := e.options.RunAuthCommand(ctx, e.options, args)
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
	if !authSlotIsDefaultForHarness(HarnessNameCodex, slot.AuthSlotID) {
		return HarnessAuthStatus{}, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "auth_slot_selection_unsupported", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
	}
	status, err := e.AuthStatus(ctx, slot)
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	if status.Status != HarnessAuthAuthenticated {
		status.Status = HarnessAuthRequired
		return status, nil
	}
	result, err := e.options.RunAuthCommand(ctx, e.options, []string{"logout"})
	if err != nil {
		return HarnessAuthStatus{}, err
	}
	if result.ExitCode != nil && *result.ExitCode != 0 {
		return HarnessAuthStatus{}, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "auth_logout_failed", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: true}
	}
	return NewHarnessAuthRequired(HarnessNameCodex, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "device_code", SecretOwnedBy: HarnessNameCodex}), nil
}

func (e *CodexExecutor) Descriptor() HarnessAdapterDescriptor {
	return HarnessAdapterDescriptor{Name: HarnessNameCodex, Capabilities: []HarnessCapability{HarnessCapabilityAgentSessionFromContext, HarnessCapabilityContextPacket, HarnessCapabilityTimelineItems, HarnessCapabilityFinalResponse, HarnessCapabilityMeasuredTokenTelemetry}}
}

func (e *CodexExecutor) Start(ctx context.Context, req ExecutorStartRequest) (ExecutorRun, error) {
	if ctx == nil {
		return ExecutorRun{}, fmt.Errorf("context is required")
	}
	if e == nil {
		return ExecutorRun{}, fmt.Errorf("executor is required")
	}
	if !authSlotIsDefaultForHarness(HarnessNameCodex, req.AuthSlotID) {
		return ExecutorRun{}, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "auth_slot_selection_unsupported", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return ExecutorRun{}, fmt.Errorf("workspace id is required")
	}
	transport, err := e.options.StartTransport(ctx, e.options)
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
	if err := transport.Call(ctx, "thread/start", map[string]any{"model": strings.TrimSpace(req.Model), "cwd": strings.TrimSpace(e.options.Cwd), "approvalPolicy": "never", "sandbox": "workspaceWrite", "experimentalRawEvents": false, "persistExtendedHistory": true}, &thread); err != nil {
		return ExecutorRun{}, fmt.Errorf("start codex thread: %w", err)
	}
	threadID := strings.TrimSpace(thread.Thread.ID)
	if threadID == "" {
		return ExecutorRun{}, fmt.Errorf("codex thread id is required")
	}
	var turn codexTurnStartResult
	if err := transport.Call(ctx, "turn/start", map[string]any{"threadId": threadID, "input": []map[string]string{{"type": "text", "text": codexPromptFromRequest(req)}}}, &turn); err != nil {
		return ExecutorRun{}, fmt.Errorf("start codex turn: %w", err)
	}
	turnID := strings.TrimSpace(turn.Turn.ID)
	items, err := collectCodexTimelineItems(ctx, transport.Notifications(), workspaceID, threadID, turnID)
	if err != nil {
		return ExecutorRun{}, err
	}
	e.mu.Lock()
	e.runs[threadID] = items
	e.mu.Unlock()
	providerRunID := turnID
	if providerRunID == "" {
		providerRunID = threadID
	}
	return ExecutorRun{RunID: threadID, SessionID: threadID, Executor: HarnessNameCodex, ProviderSessionID: threadID, ProviderRunID: providerRunID, PID: transport.PID(), ProcessSample: transport.ProcessSample(ctx), CapabilityNames: harnessCapabilitiesToStrings(e.Descriptor().Capabilities)}, nil
}

func (e *CodexExecutor) Items(ctx context.Context, runID string) ([]TimelineItem, error) {
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

func (e *CodexExecutor) Stop(ctx context.Context, runID string) error {
	_ = ctx
	_ = runID
	return nil
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

func codexPromptFromRequest(req ExecutorStartRequest) string {
	parts := make([]string, 0, 2)
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		parts = append(parts, prompt)
	}
	parts = append(parts, strings.TrimSpace(req.ContextPacket))
	return strings.Join(parts, "\n\n")
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
	executable := strings.TrimSpace(options.Executable)
	if executable == "" {
		executable = "codex"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return nil, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "missing_executable", Executable: executable, Probe: executable + " --version", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: false}
	}
	cmd := exec.CommandContext(ctx, path, "app-server", "--listen", "stdio://")
	cmd.Dir = strings.TrimSpace(options.Cwd)
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
		return nil, &HarnessUnavailableError{Harness: HarnessNameCodex, Reason: "start_failed", Executable: executable, Probe: executable + " app-server --listen stdio://", RequiredCapability: HarnessCapabilityAgentSessionFromContext, StartInvoked: true}
	}
	transport := newCodexStdioTransport(cmd, stdin, stdout, stderr, options.NotificationCap)
	return transport, nil
}

type codexStdioTransport struct {
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	notifications chan codexNotification
	responses     map[int64]chan codexRPCMessage
	mu            sync.Mutex
	nextID        int64
	closed        chan struct{}
	closeOnce     sync.Once
}

type codexRPCMessage struct {
	ID     *int64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *codexRPCError  `json:"error,omitempty"`
}

type codexRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func newCodexStdioTransport(cmd *exec.Cmd, stdin io.WriteCloser, stdout io.Reader, stderr io.Reader, notificationCap int) *codexStdioTransport {
	transport := &codexStdioTransport{cmd: cmd, stdin: stdin, notifications: make(chan codexNotification, notificationCap), responses: make(map[int64]chan codexRPCMessage), closed: make(chan struct{})}
	go transport.readMessages(stdout)
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	return transport
}

func (t *codexStdioTransport) PID() int {
	if t == nil || t.cmd == nil || t.cmd.Process == nil {
		return 0
	}
	return t.cmd.Process.Pid
}

func (t *codexStdioTransport) ProcessSample(ctx context.Context) *ProcessMetricsSample {
	pid := t.PID()
	if pid <= 0 {
		return nil
	}
	sample := sampleLinuxProcessMetrics(ctx, AgentSession{PID: pid})
	return &sample
}

func (t *codexStdioTransport) Call(ctx context.Context, method string, params any, result any) error {
	id := atomic.AddInt64(&t.nextID, 1)
	responseCh := make(chan codexRPCMessage, 1)
	t.mu.Lock()
	t.responses[id] = responseCh
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		delete(t.responses, id)
		t.mu.Unlock()
	}()
	message := map[string]any{"id": id, "method": method}
	if params != nil {
		message["params"] = params
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode codex %s request: %w", method, err)
	}
	if _, err := t.stdin.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write codex %s request: %w", method, err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.closed:
		select {
		case response := <-responseCh:
			return decodeCodexCallResponse(method, response, result)
		default:
		}
		return fmt.Errorf("codex app-server closed before %s response", method)
	case response := <-responseCh:
		return decodeCodexCallResponse(method, response, result)
	}
}

func decodeCodexCallResponse(method string, response codexRPCMessage, result any) error {
	if response.Error != nil {
		return fmt.Errorf("codex %s error %d: %s", method, response.Error.Code, response.Error.Message)
	}
	if result == nil || len(response.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return fmt.Errorf("decode codex %s response: %w", method, err)
	}
	return nil
}

func (t *codexStdioTransport) Notify(ctx context.Context, method string, params any) error {
	message := map[string]any{"method": method}
	if params != nil {
		message["params"] = params
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode codex %s notification: %w", method, err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.closed:
		return fmt.Errorf("codex app-server closed before %s notification", method)
	default:
	}
	if _, err := t.stdin.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write codex %s notification: %w", method, err)
	}
	return nil
}

func (t *codexStdioTransport) Notifications() <-chan codexNotification {
	return t.notifications
}

func (t *codexStdioTransport) Close() error {
	var err error
	t.closeOnce.Do(func() {
		_ = t.stdin.Close()
		if t.cmd != nil && t.cmd.Process != nil {
			err = t.cmd.Process.Kill()
		}
		if t.cmd != nil {
			_, _ = waitWithTimeout(t.cmd, 2*time.Second)
		}
		close(t.closed)
	})
	return err
}

func waitWithTimeout(cmd *exec.Cmd, timeout time.Duration) (struct{}, error) {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return struct{}{}, err
	case <-time.After(timeout):
		return struct{}{}, fmt.Errorf("process did not exit within %s", timeout)
	}
}

func (t *codexStdioTransport) readMessages(stdout io.Reader) {
	defer func() {
		close(t.notifications)
		t.closeOnce.Do(func() { close(t.closed) })
	}()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var message codexRPCMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			continue
		}
		if message.ID != nil {
			t.mu.Lock()
			responseCh := t.responses[*message.ID]
			t.mu.Unlock()
			if responseCh != nil {
				responseCh <- message
			}
			continue
		}
		if strings.TrimSpace(message.Method) != "" {
			t.deliverNotification(codexNotification{Method: message.Method, Params: message.Params})
		}
	}
}

func (t *codexStdioTransport) deliverNotification(notification codexNotification) {
	select {
	case t.notifications <- notification:
		return
	default:
	}
	if !codexNotificationIsTerminal(notification) {
		return
	}
	select {
	case <-t.notifications:
	default:
	}
	select {
	case t.notifications <- notification:
	default:
	}
}

func codexNotificationIsTerminal(notification codexNotification) bool {
	switch strings.TrimSpace(notification.Method) {
	case "turn/completed", "error":
		return true
	default:
		return false
	}
}
