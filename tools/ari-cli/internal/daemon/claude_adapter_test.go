package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestClaudeExecutorMapsJSONResult(t *testing.T) {
	runner := &fakeClaudeRunner{output: []byte(`{"result":"Done","session_id":"550e8400-e29b-41d4-a716-446655440000","usage":{"input_tokens":12,"output_tokens":34},"total_cost_usd":0.0123}`)}
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	run, items, err := StartExecutorRun(context.Background(), executor, packet, Profile{Name: "reviewer", Harness: HarnessNameClaude, Model: "opus", Prompt: "Review it", InvocationClass: HarnessInvocationSticky, Defaults: map[string]any{"invocation_mode": "headless"}})
	if err != nil {
		t.Fatalf("StartExecutorRun returned error: %v", err)
	}
	if run.Executor != HarnessNameClaude || run.ProviderRunID != "550e8400-e29b-41d4-a716-446655440000" || run.HarnessSessionID == run.ProviderRunID || !isULID(run.HarnessSessionID) {
		t.Fatalf("run = %#v, want Ari run id with Claude provider session", run)
	}
	if len(items) != 4 {
		t.Fatalf("items len = %d, want lifecycle/text/telemetry/completed items: %#v", len(items), items)
	}
	if items[1].Kind != "agent_text" || items[1].Text != "Done" {
		t.Fatalf("message item = %#v, want Claude result text", items[1])
	}
	if items[2].Kind != "telemetry" || items[2].Metadata["input_tokens"] != "12" || items[2].Metadata["output_tokens"] != "34" {
		t.Fatalf("telemetry item = %#v, want Claude usage metadata", items[2])
	}
	if got := strings.Join(runner.args, " "); !strings.Contains(got, "--bare") || !strings.Contains(got, "--output-format json") || !strings.Contains(got, "--model opus") {
		t.Fatalf("claude args = %q, want bare json model invocation", got)
	}
	if strings.Contains(runner.prompt, "Review it") || !strings.Contains(runner.prompt, "ctx_123") {
		t.Fatalf("claude prompt = %q, want context packet without profile behavior", runner.prompt)
	}
}

func TestClaudeExecutorMapsProfilePromptToReplacementSystemPrompt(t *testing.T) {
	runner := &fakeClaudeRunner{output: []byte(`{"result":"Done","session_id":"550e8400-e29b-41d4-a716-446655440000"}`)}
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, _, err := StartExecutorRun(context.Background(), executor, packet, Profile{Name: "reviewer", Harness: HarnessNameClaude, Model: "opus", Prompt: "Act as the reviewer", InvocationClass: HarnessInvocationSticky, Defaults: map[string]any{"invocation_mode": "headless"}})
	if err != nil {
		t.Fatalf("StartExecutorRun returned error: %v", err)
	}

	args := strings.Join(runner.args, " ")
	if !strings.Contains(args, "--system-prompt Act as the reviewer") {
		t.Fatalf("claude args = %q, want replacement --system-prompt with profile behavior", args)
	}
	if strings.Contains(args, "--append-system-prompt") {
		t.Fatalf("claude args = %q, must not append profile behavior by default", args)
	}
	if strings.Contains(runner.prompt, "Act as the reviewer") {
		t.Fatalf("claude stdin prompt = %q, must keep profile behavior out of visible task/context payload", runner.prompt)
	}
	if !strings.Contains(runner.prompt, "ctx_123") {
		t.Fatalf("claude stdin prompt = %q, want context packet visible in user payload", runner.prompt)
	}
}

func TestClaudeExecutorUsesRequestPromptAsReplacementSystemPrompt(t *testing.T) {
	runner := &fakeClaudeRunner{output: []byte(`{"result":"Done","session_id":"550e8400-e29b-41d4-a716-446655440000"}`)}
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: runner.Run})

	_, err := executor.Start(context.Background(), ExecutorStartRequest{
		WorkspaceID:   "ws-1",
		Model:         "opus",
		Prompt:        "Session-specific behavior",
		ContextPacket: `{"context_packet_id":"ctx_123","task":"visible task"}`,
		Options:       []HarnessOption{ClaudeWithInvocationMode(HarnessInvocationModeHeadless)},
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	args := strings.Join(runner.args, " ")
	if !strings.Contains(args, "--system-prompt Session-specific behavior") {
		t.Fatalf("claude args = %q, want request prompt mapped to replacement --system-prompt", args)
	}
	if strings.Contains(args, "--append-system-prompt") {
		t.Fatalf("claude args = %q, must not append request prompt by default", args)
	}
	if strings.Contains(runner.prompt, "Session-specific behavior") {
		t.Fatalf("claude stdin prompt = %q, must keep request prompt out of visible task/context payload", runner.prompt)
	}
	if !strings.Contains(runner.prompt, "visible task") {
		t.Fatalf("claude stdin prompt = %q, want context packet visible in user payload", runner.prompt)
	}
}

func TestClaudeTemporaryInvocationDefaultsToBackground(t *testing.T) {
	runner := &fakeClaudeRunner{output: []byte(`Started background session 550e8400-e29b-41d4-a716-446655440000`)}
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: runner.Run})

	run, err := executor.Start(context.Background(), ExecutorStartRequest{WorkspaceID: "ws-1", InvocationClass: HarnessInvocationEphemeral, ContextPacket: `{"context_packet_id":"ctx_123"}`})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	args := strings.Join(runner.args, " ")
	if !stringSliceContains(runner.args, "--bg") || stringSliceContains(runner.args, "-p") || run.ProviderSessionID == "" {
		t.Fatalf("run = %#v args = %q, want ephemeral background invocation", run, args)
	}
}

func TestClaudeBackgroundInvocationOmitsEmptyPromptArgument(t *testing.T) {
	runner := &fakeClaudeRunner{output: []byte(`backgrounded · 7c5dcf5d`)}
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: runner.Run})

	_, err := executor.Start(context.Background(), ExecutorStartRequest{WorkspaceID: "ws-1", Options: []HarnessOption{ClaudeWithInvocationMode(HarnessInvocationModeBackground)}})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	for _, arg := range runner.args {
		if arg == "" {
			t.Fatalf("claude args = %#v, want no empty positional prompt", runner.args)
		}
	}
}

func TestClaudeExecutorUsesTypedBackgroundOption(t *testing.T) {
	runner := &fakeClaudeRunner{output: []byte(`Started background session 550e8400-e29b-41d4-a716-446655440000`)}
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: runner.Run})

	run, err := executor.Start(context.Background(), ExecutorStartRequest{
		WorkspaceID:   "ws-1",
		Model:         "opus",
		Prompt:        "Act as the reviewer",
		ContextPacket: `{"context_packet_id":"ctx_123","task":"visible task"}`,
		Options:       []HarnessOption{ClaudeWithInvocationMode(HarnessInvocationModeBackground)},
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if run.ProviderSessionID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("run = %#v, want parsed background session id", run)
	}
	items, err := executor.Items(context.Background(), run.ProviderSessionID)
	if err != nil {
		t.Fatalf("Items returned error: %v", err)
	}
	if got := executorRunStatusFromItems(items); got != "running" {
		t.Fatalf("background status = %q from %#v, want running", got, items)
	}
	if response := harnessFinalResponseFromItems(HarnessSession{}, executor.Descriptor(), items); response != nil {
		t.Fatalf("background final response = %#v, want nil launch notice response", response)
	}

	args := strings.Join(runner.args, " ")
	if !stringSliceContains(runner.args, "--bg") || stringSliceContains(runner.args, "-p") || stringSliceContains(runner.args, "--bare") {
		t.Fatalf("claude args = %q, want background invocation without headless flags", args)
	}
	if !strings.Contains(args, "--append-system-prompt Act as the reviewer") {
		t.Fatalf("claude args = %q, want appended profile behavior for background mode", args)
	}
	if strings.Contains(args, "--system-prompt") {
		t.Fatalf("claude args = %q, must not replace system prompt in background mode", args)
	}
	if !strings.Contains(runner.prompt, "visible task") || strings.Contains(runner.prompt, "Act as the reviewer") {
		t.Fatalf("claude prompt = %q, want visible context without profile behavior", runner.prompt)
	}
}

func TestClaudeBackgroundSessionIDUsesLaunchOutputShape(t *testing.T) {
	output := []byte(`debug correlation 11111111-1111-1111-1111-111111111111
backgrounded · 7c5dcf5d
  claude attach 7c5dcf5d`)
	if got := claudeBackgroundSessionIDFromOutput(output); got != "7c5dcf5d" {
		t.Fatalf("session id = %q, want background session id", got)
	}
}

func TestDecodeStoredDefaultsRejectsMalformedJSON(t *testing.T) {
	_, err := decodeStoredDefaults(`{`)
	if err == nil || !strings.Contains(err.Error(), "decode profile defaults") {
		t.Fatalf("error = %v, want malformed defaults error", err)
	}
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestClaudeOptionsFromSettingsUseNormalizedAndNativeInvocationModes(t *testing.T) {
	options, err := claudeOptionsFromSettings(nil)
	if err != nil {
		t.Fatalf("claudeOptionsFromSettings returned error: %v", err)
	}
	if len(options) != 0 {
		t.Fatalf("options = %#v, want no explicit option when settings omit invocation_mode", options)
	}

	options, err = claudeOptionsFromSettings(map[string]any{"invocation_mode": "background"})
	if err != nil {
		t.Fatalf("claudeOptionsFromSettings returned error: %v", err)
	}
	resolved := claudeExecutorOptions{}
	for _, option := range options {
		option.(claudeOption).apply(&resolved)
	}
	if resolved.InvocationMode != HarnessInvocationModeBackground {
		t.Fatalf("resolved mode = %q, want background", resolved.InvocationMode)
	}

	options, err = claudeOptionsFromSettings(map[string]any{"invocation_mode": "background", "claude": map[string]any{"invocation_mode": "headless"}})
	if err != nil {
		t.Fatalf("claudeOptionsFromSettings returned error: %v", err)
	}
	resolved = claudeExecutorOptions{InvocationMode: HarnessInvocationModeBackground}
	for _, option := range options {
		option.(claudeOption).apply(&resolved)
	}
	if resolved.InvocationMode != HarnessInvocationModeHeadless {
		t.Fatalf("resolved mode = %q, want native Claude override to headless", resolved.InvocationMode)
	}
}

func TestClaudeOptionsFromSettingsRejectUnsupportedInvocationMode(t *testing.T) {
	_, err := claudeOptionsFromSettings(map[string]any{"invocation_mode": "telepathy"})
	if err == nil || !strings.Contains(err.Error(), "unsupported invocation_mode") {
		t.Fatalf("error = %v, want unsupported invocation mode", err)
	}
}

func TestClaudeOptionsFromSettingsRejectMalformedSettings(t *testing.T) {
	_, err := claudeOptionsFromSettings(map[string]any{"invocation_mode": 123})
	if err == nil || !strings.Contains(err.Error(), "invocation_mode must be a string") {
		t.Fatalf("error = %v, want non-string invocation mode error", err)
	}

	_, err = claudeOptionsFromSettings(map[string]any{"claude": "background"})
	if err == nil || !strings.Contains(err.Error(), "claude must be an object") {
		t.Fatalf("error = %v, want malformed native settings error", err)
	}
}

func TestClaudeExecutorRejectsForeignTypedOptions(t *testing.T) {
	runner := &fakeClaudeRunner{output: []byte(`{"result":"Done","session_id":"550e8400-e29b-41d4-a716-446655440000"}`)}
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: runner.Run})

	_, err := executor.Start(context.Background(), ExecutorStartRequest{WorkspaceID: "ws-1", Options: []HarnessOption{foreignHarnessOption{}}})
	if err == nil || !strings.Contains(err.Error(), "unsupported claude option") {
		t.Fatalf("error = %v, want unsupported claude option", err)
	}
	if len(runner.args) > 0 {
		t.Fatalf("runner args = %#v, want no command invocation", runner.args)
	}
}

func TestStartExecutorRunTranslatesProfileSettingsToTypedClaudeOptions(t *testing.T) {
	runner := &fakeClaudeRunner{output: []byte(`Started background session 550e8400-e29b-41d4-a716-446655440000`)}
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, _, err := StartExecutorRun(context.Background(), executor, packet, Profile{Name: "reviewer", Harness: HarnessNameClaude, Prompt: "Review", Defaults: map[string]any{"invocation_mode": "background"}})
	if err != nil {
		t.Fatalf("StartExecutorRun returned error: %v", err)
	}

	args := strings.Join(runner.args, " ")
	if !strings.Contains(args, "--bg") || !strings.Contains(args, "--append-system-prompt Review") {
		t.Fatalf("claude args = %q, want typed background option from profile settings", args)
	}
}

func TestStartExecutorRunDefaultsClaudeProfileToBackgroundLifecycle(t *testing.T) {
	runner := &fakeClaudeRunner{output: []byte(`Started background session 550e8400-e29b-41d4-a716-446655440000`)}
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	run, items, err := StartExecutorRun(context.Background(), executor, packet, Profile{Name: "reviewer", Harness: HarnessNameClaude, Prompt: "Review"})
	if err != nil {
		t.Fatalf("StartExecutorRun returned error: %v", err)
	}
	if run.ProviderSessionID != "550e8400-e29b-41d4-a716-446655440000" || run.Status != "running" || run.InvocationMode != string(HarnessInvocationModeBackground) || run.UsageBucket != "subscription" {
		t.Fatalf("run = %#v, want running Claude background session", run)
	}
	if len(items) != 1 || items[0].Status != "running" || items[0].Metadata["invocation_mode"] != string(HarnessInvocationModeBackground) || items[0].Metadata["usage_bucket"] != "subscription" {
		t.Fatalf("items = %#v, want running background lifecycle with subscription metadata", items)
	}
	args := strings.Join(runner.args, " ")
	if !stringSliceContains(runner.args, "--bg") || !strings.Contains(args, "--append-system-prompt Review") {
		t.Fatalf("claude args = %q, want default background invocation", args)
	}
}

func TestStartExecutorRunPreservesExplicitHeadlessSettings(t *testing.T) {
	runner := &fakeClaudeRunner{output: []byte(`{"result":"Done","session_id":"550e8400-e29b-41d4-a716-446655440000"}`)}
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	run, _, err := StartExecutorRun(context.Background(), executor, packet, Profile{Name: "reviewer", Harness: HarnessNameClaude, Prompt: "Review", Defaults: map[string]any{"invocation_mode": "headless"}})
	if err != nil {
		t.Fatalf("StartExecutorRun returned error: %v", err)
	}
	if run.InvocationMode != string(HarnessInvocationModeHeadless) || run.UsageBucket != "agent_sdk_credit" {
		t.Fatalf("run = %#v, want explicit headless/API-credit mode", run)
	}

	args := strings.Join(runner.args, " ")
	if !stringSliceContains(runner.args, "--bare") || !stringSliceContains(runner.args, "-p") || !stringSliceContains(runner.args, "-") || !strings.Contains(args, "--output-format json") {
		t.Fatalf("claude args = %q, want explicit headless invocation", args)
	}
	if strings.Contains(args, "--bg") || !strings.Contains(args, "--system-prompt Review") || strings.Contains(args, "--append-system-prompt") {
		t.Fatalf("claude args = %q, want replacement prompt and no background flags", args)
	}
}

func TestStartExecutorRunTemporaryClaudeProfileDefaultsToBackground(t *testing.T) {
	runner := &fakeClaudeRunner{output: []byte(`Started background session 550e8400-e29b-41d4-a716-446655440000`)}
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: runner.Run})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	run, _, err := StartExecutorRun(context.Background(), executor, packet, Profile{Name: "reviewer", Harness: HarnessNameClaude, Prompt: "Review", InvocationClass: HarnessInvocationEphemeral})
	if err != nil {
		t.Fatalf("StartExecutorRun returned error: %v", err)
	}
	args := strings.Join(runner.args, " ")
	if run.InvocationMode != string(HarnessInvocationModeBackground) || !stringSliceContains(runner.args, "--bg") || stringSliceContains(runner.args, "-p") {
		t.Fatalf("run = %#v args = %q, want ephemeral profile background mode", run, args)
	}
}

func TestHarnessSessionDefaultsCanOverrideInvocationModeWithoutDuplicatingProfile(t *testing.T) {
	profile := Profile{Name: "reviewer", Harness: HarnessNameClaude, Prompt: "Review", Defaults: map[string]any{"invocation_mode": "headless"}}
	profile = applyHarnessSessionDefaults(profile, HarnessSessionDefaults{Settings: map[string]any{"invocation_mode": "background"}})

	options, err := harnessOptionsFromProfile(profile)
	if err != nil {
		t.Fatalf("harnessOptionsFromProfile returned error: %v", err)
	}
	resolved := claudeExecutorOptions{}
	for _, option := range options {
		option.(claudeOption).apply(&resolved)
	}
	if resolved.InvocationMode != HarnessInvocationModeBackground || profile.Prompt != "Review" {
		t.Fatalf("profile = %#v resolved = %#v, want same profile prompt with per-run background mode", profile, resolved)
	}
}

func TestHarnessSessionResponseFromStoreExposesProviderModeMetadata(t *testing.T) {
	session := agentSessionResponseFromStore(globaldb.HarnessSession{
		SessionID:            "ari-session",
		WorkspaceID:          "ws-1",
		Harness:              HarnessNameClaude,
		ProviderSessionID:    "550e8400-e29b-41d4-a716-446655440000",
		Status:               "running",
		ProviderMetadataJSON: `{"invocation_mode":"background","usage_bucket":"subscription"}`,
	})

	if session.ProviderSessionID != "550e8400-e29b-41d4-a716-446655440000" || session.InvocationMode != "background" || session.UsageBucket != "subscription" {
		t.Fatalf("session = %#v, want show/list response with provider id and mode metadata", session)
	}
}

func TestClaudeSessionLogsAndAttachUsePersistedProviderID(t *testing.T) {
	t.Setenv(EnvClaudeExecutable, "/opt/agents/claude")
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	ctx := context.Background()
	seedSessionWithPrimaryFolder(t, store, "ws-1", "/repo")
	if err := store.EnsureHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "agent-1", Name: "claude", Harness: HarnessNameClaude}); err != nil {
		t.Fatalf("EnsureHarnessSessionConfig returned error: %v", err)
	}
	if err := store.CreateHarnessSession(ctx, globaldb.HarnessSession{SessionID: "ari-session", WorkspaceID: "ws-1", AgentID: "agent-1", Harness: HarnessNameClaude, Status: "running", Usage: globaldb.HarnessSessionUsageSticky, ProviderSessionID: "550e8400-e29b-41d4-a716-446655440000", CWD: "/repo", ProviderMetadataJSON: `{"invocation_mode":"background","usage_bucket":"subscription"}`}); err != nil {
		t.Fatalf("CreateHarnessSession returned error: %v", err)
	}
	originalRunner := runClaudeSessionCommand
	t.Cleanup(func() { runClaudeSessionCommand = originalRunner })
	runClaudeSessionCommand = func(ctx context.Context, cwd string, args []string) ([]byte, error) {
		_ = ctx
		if cwd != "" || strings.Join(args, " ") != "logs 550e8400-e29b-41d4-a716-446655440000" {
			t.Fatalf("cwd=%q args=%q, want Claude logs invocation", cwd, strings.Join(args, " "))
		}
		return []byte("background log line\n"), nil
	}

	logs := callMethod[ClaudeSessionLogsResponse](t, registry, "session.claude.logs", ClaudeSessionLogsRequest{SessionID: "ari-session"})
	if logs.ProviderSessionID != "550e8400-e29b-41d4-a716-446655440000" || strings.Join(logs.Command, " ") != "/opt/agents/claude logs 550e8400-e29b-41d4-a716-446655440000" || logs.Output != "background log line" {
		t.Fatalf("logs = %#v, want native logs command and output", logs)
	}
	attach := callMethod[ClaudeSessionAttachResponse](t, registry, "session.claude.attach", ClaudeSessionAttachRequest{SessionID: "ari-session"})
	if attach.ProviderSessionID != logs.ProviderSessionID || strings.Join(attach.Command, " ") != "/opt/agents/claude attach 550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("attach = %#v, want native attach command", attach)
	}
}

func TestClaudeSessionLogsAllowPreMetadataBackgroundSession(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	ctx := context.Background()
	seedSessionWithPrimaryFolder(t, store, "ws-1", "/repo")
	if err := store.EnsureHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "agent-1", Name: "claude", Harness: HarnessNameClaude}); err != nil {
		t.Fatalf("EnsureHarnessSessionConfig returned error: %v", err)
	}
	if err := store.CreateHarnessSession(ctx, globaldb.HarnessSession{SessionID: "ari-session", WorkspaceID: "ws-1", AgentID: "agent-1", Harness: HarnessNameClaude, Status: "running", Usage: globaldb.HarnessSessionUsageSticky, ProviderSessionID: "7c5dcf5d", CWD: "/repo", ProviderMetadataJSON: `{}`}); err != nil {
		t.Fatalf("CreateHarnessSession returned error: %v", err)
	}
	originalRunner := runClaudeSessionCommand
	t.Cleanup(func() { runClaudeSessionCommand = originalRunner })
	runClaudeSessionCommand = func(ctx context.Context, cwd string, args []string) ([]byte, error) {
		_ = ctx
		if strings.Join(args, " ") != "logs 7c5dcf5d" {
			t.Fatalf("args=%q, want legacy Claude logs invocation", strings.Join(args, " "))
		}
		return []byte("legacy log line\n"), nil
	}

	logs := callMethod[ClaudeSessionLogsResponse](t, registry, "session.claude.logs", ClaudeSessionLogsRequest{SessionID: "ari-session"})
	if logs.ProviderSessionID != "7c5dcf5d" || logs.Output != "legacy log line" {
		t.Fatalf("logs = %#v, want legacy provider id logs", logs)
	}
}

func TestClaudeExecutorReportsMissingExecutableBeforeStart(t *testing.T) {
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "missing-claude", Cwd: "/repo", RunCommand: func(ctx context.Context, opts claudeExecutorOptions, prompt string) (commandRunResult, error) {
		return commandRunResult{}, &HarnessUnavailableError{Harness: HarnessNameClaude, Reason: "missing_executable", Executable: opts.Executable, Probe: opts.Executable + " --version", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, _, err := StartExecutorRun(context.Background(), executor, packet, Profile{Harness: HarnessNameClaude, Defaults: map[string]any{"invocation_mode": "headless"}})
	unavailable := &HarnessUnavailableError{}
	if !errors.As(err, &unavailable) {
		t.Fatalf("error = %T %[1]v, want HarnessUnavailableError", err)
	}
	if unavailable.StartInvoked || unavailable.Executable != "missing-claude" || unavailable.RequiredCapability != HarnessCapabilityHarnessSessionFromContext {
		t.Fatalf("unavailable = %#v, want pre-start missing executable", unavailable)
	}
}

func TestClaudeAuthStatusNormalizesProviderOwnedReadiness(t *testing.T) {
	exitCode := 0
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts claudeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		if strings.Join(args, " ") != "auth status --json" {
			t.Fatalf("args = %q, want auth status --json", strings.Join(args, " "))
		}
		return commandRunResult{Output: []byte(`{"authenticated":true}`), ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "claude-default", Harness: HarnessNameClaude})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthAuthenticated || status.AuthSlotID != "claude-default" || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want authenticated Claude slot without Ari secrets", status)
	}
}

func TestClaudeAuthStatusReturnsProviderConfigRemediation(t *testing.T) {
	exitCode := 1
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts claudeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		_ = args
		return commandRunResult{Output: []byte(`{"authenticated":false}`), ExitCode: &exitCode}, errors.New("not authenticated")
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "claude-default", Harness: HarnessNameClaude})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired || status.Remediation == nil || status.Remediation.SecretOwnedBy != HarnessNameClaude || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want provider-owned remediation", status)
	}
}

func TestClaudeAuthStatusTreatsEmptyOutputAsAuthRequired(t *testing.T) {
	exitCode := 0
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts claudeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		_ = args
		return commandRunResult{Output: nil, ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthStatus(context.Background(), HarnessAuthSlot{AuthSlotID: "claude-default", Harness: HarnessNameClaude})
	if err != nil {
		t.Fatalf("AuthStatus returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want auth_required for empty provider output", status)
	}
}

func TestClaudeAuthLogoutRunsProviderLogout(t *testing.T) {
	exitCode := 0
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunAuthCommand: func(ctx context.Context, opts claudeExecutorOptions, args []string) (commandRunResult, error) {
		_ = ctx
		_ = opts
		if strings.Join(args, " ") != "auth logout" {
			t.Fatalf("args = %q, want auth logout", strings.Join(args, " "))
		}
		return commandRunResult{ExitCode: &exitCode}, nil
	}})

	status, err := executor.AuthLogout(context.Background(), HarnessAuthSlot{AuthSlotID: "claude-default", Harness: HarnessNameClaude})
	if err != nil {
		t.Fatalf("AuthLogout returned error: %v", err)
	}
	if status.Status != HarnessAuthRequired || status.AriSecretStorage != HarnessAriSecretStorageNone {
		t.Fatalf("status = %#v, want auth_required after provider logout", status)
	}
}

func TestClaudeExecutorRejectsMissingSessionID(t *testing.T) {
	executor := NewClaudeExecutorForTest(claudeExecutorOptions{Executable: "claude", Cwd: "/repo", RunCommand: func(ctx context.Context, opts claudeExecutorOptions, prompt string) (commandRunResult, error) {
		return commandRunResult{Output: []byte(`{"result":"Done"}`)}, nil
	}})
	packet := ContextPacket{ID: "ctx_123", WorkspaceID: "ws-1", TaskID: "task-1", PacketHash: "sha256:abc"}

	_, _, err := StartExecutorRun(context.Background(), executor, packet, Profile{Harness: HarnessNameClaude, Defaults: map[string]any{"invocation_mode": "headless"}})
	if err == nil || !strings.Contains(err.Error(), "claude session id is required") {
		t.Fatalf("StartExecutorRun error = %v, want missing session id error", err)
	}
}

type fakeClaudeRunner struct {
	output []byte
	args   []string
	prompt string
}

type foreignHarnessOption struct{}

func (foreignHarnessOption) harnessOption() {}

func (r *fakeClaudeRunner) Run(ctx context.Context, opts claudeExecutorOptions, prompt string) (commandRunResult, error) {
	_ = ctx
	r.args = claudeArgs(opts)
	if opts.InvocationMode == HarnessInvocationModeBackground {
		if trimmed := strings.TrimSpace(prompt); trimmed != "" {
			r.args = append(r.args, trimmed)
		}
	}
	r.prompt = prompt
	return commandRunResult{Output: append([]byte(nil), r.output...)}, nil
}
