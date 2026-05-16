package cmd

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

func TestInitWithHarnessModelRootAppliesDaemonOwnedChoice(t *testing.T) {
	restore := replaceInitDeps(t)
	defer restore()

	applied := daemon.InitApplyRequest{}
	initApplyRPC = func(ctx context.Context, socketPath string, req daemon.InitApplyRequest) (daemon.InitApplyResponse, error) {
		_ = ctx
		if socketPath != "test.sock" {
			t.Fatalf("socketPath = %q, want test.sock", socketPath)
		}
		applied = req
		return daemon.InitApplyResponse{Initialized: true, DefaultHarness: req.Harness, PreferredModel: req.Model, DefaultRoot: req.Root, DefaultHarnessSet: true}, nil
	}

	out, err := executeRootCommandRaw("init", "--harness", "codex", "--model", "gpt-5.5", "--root", "~/Projects")
	if err != nil {
		t.Fatalf("ari init returned error: %v", err)
	}
	if applied.Harness != "codex" {
		t.Fatalf("applied harness = %q, want codex", applied.Harness)
	}
	if applied.Model != "gpt-5.5" || applied.Root != "~/Projects" {
		t.Fatalf("applied model/root = %q/%q, want gpt-5.5/~/Projects", applied.Model, applied.Root)
	}
	if !strings.Contains(out, "Default harness set: codex") {
		t.Fatalf("output missing harness success: %q", out)
	}
	if !strings.Contains(out, "Welcome to Ari.") || !strings.Contains(out, "ari workspace setup") {
		t.Fatalf("output missing welcome/example signals: %q", out)
	}
}

func TestInitInvalidHarnessComesFromDaemonAndWritesNothingInCLI(t *testing.T) {
	restore := replaceInitDeps(t)
	defer restore()

	initApplyRPC = func(ctx context.Context, socketPath string, req daemon.InitApplyRequest) (daemon.InitApplyResponse, error) {
		_ = ctx
		_ = socketPath
		_ = req
		return daemon.InitApplyResponse{}, fmt.Errorf("init apply: harness must be one of claude-code, codex, opencode")
	}

	_, err := executeRootCommandRaw("init", "--harness", "bad")
	if err == nil {
		t.Fatal("ari init returned nil error")
	}
	if !strings.Contains(err.Error(), "harness must be one of") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestInitInteractiveUsesOptionsAndPromptSeam(t *testing.T) {
	restore := replaceInitDeps(t)
	defer restore()

	initOptionsRPC = func(ctx context.Context, socketPath string) (daemon.InitOptionsResponse, error) {
		_ = ctx
		_ = socketPath
		return daemon.InitOptionsResponse{Harnesses: []daemon.InitHarnessOption{{Name: "codex", Label: "codex"}}, Models: []daemon.InitModelOption{{Name: "", Label: "Manual/default model"}}, Roots: []daemon.InitRootOption{{Path: "~/", Label: "~/"}}}, nil
	}
	initPromptSelection = func(cmdOut initPromptOutput, options daemon.InitOptionsResponse, selected initSelection) (initSelection, error) {
		_ = cmdOut
		_ = selected
		if len(options.Harnesses) != 1 || options.Harnesses[0].Name != "codex" || len(options.Roots) != 1 || options.Roots[0].Path != "~/" {
			t.Fatalf("prompt options = %#v", options)
		}
		return initSelection{Harness: "codex", Model: "gpt-5.5", Root: "~/"}, nil
	}

	out, err := executeRootCommandRaw("init")
	if err != nil {
		t.Fatalf("ari init returned error: %v", err)
	}
	if !strings.Contains(out, "Default harness set: codex") {
		t.Fatalf("output missing harness success: %q", out)
	}
}

func TestInitInteractiveDefaultPromptReadsChoice(t *testing.T) {
	restore := replaceInitDeps(t)
	defer restore()

	initOptionsRPC = func(ctx context.Context, socketPath string) (daemon.InitOptionsResponse, error) {
		_ = ctx
		_ = socketPath
		return daemon.InitOptionsResponse{Harnesses: []daemon.InitHarnessOption{{Name: "claude-code", Label: "claude-code"}, {Name: "codex", Label: "codex"}}, Models: []daemon.InitModelOption{{Name: "", Label: "Manual/default model"}}, Roots: []daemon.InitRootOption{{Path: "~/", Label: "~/"}}}, nil
	}
	initPromptHarness = promptInitHarness
	initPromptSelection = promptInitSelection

	root := NewRootCmd()
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("2\ngpt-5.5\n~/Projects\n"))
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("ari init returned error: %v", err)
	}
	if !strings.Contains(out.String(), "Default harness set: codex") {
		t.Fatalf("output missing selected harness: %q", out.String())
	}
}

func TestInitInteractiveDefaultRootAcceptsEnter(t *testing.T) {
	root := NewRootCmd()
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	got, err := promptInitRootWithScanner(root, bufio.NewScanner(strings.NewReader("\n")), []daemon.InitRootOption{{Path: "~/", Label: "~/"}})
	if err != nil {
		t.Fatalf("prompt root returned error: %v", err)
	}
	if got != "~/" {
		t.Fatalf("root = %q, want ~/", got)
	}
	if !strings.Contains(out.String(), "~/") {
		t.Fatalf("root prompt did not show ~/ default: %q", out.String())
	}
}

func TestInitInteractivePromptRejectsNonNumericChoiceClearly(t *testing.T) {
	root := NewRootCmd()
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("wat\n"))
	_, err := promptInitHarness(root, []daemon.InitHarnessOption{{Name: "codex", Label: "codex"}})
	if err == nil || !strings.Contains(err.Error(), "must be a number") {
		t.Fatalf("prompt error = %v, want numeric guidance", err)
	}
}

func TestInitInteractiveStartsApplyTimeoutAfterPrompt(t *testing.T) {
	restore := replaceInitDeps(t)
	defer restore()

	initOptionsRPC = func(ctx context.Context, socketPath string) (daemon.InitOptionsResponse, error) {
		_ = ctx
		_ = socketPath
		return daemon.InitOptionsResponse{Harnesses: []daemon.InitHarnessOption{{Name: "codex", Label: "codex"}}, Models: []daemon.InitModelOption{{Name: "", Label: "Manual/default model"}}, Roots: []daemon.InitRootOption{{Path: "~/", Label: "~/"}}}, nil
	}
	initPromptSelection = func(cmdOut initPromptOutput, options daemon.InitOptionsResponse, selected initSelection) (initSelection, error) {
		_ = cmdOut
		_ = options
		_ = selected
		time.Sleep(6 * time.Second)
		return initSelection{Harness: "codex", Root: "~/"}, nil
	}
	initApplyRPC = func(ctx context.Context, socketPath string, req daemon.InitApplyRequest) (daemon.InitApplyResponse, error) {
		_ = socketPath
		_ = req
		select {
		case <-ctx.Done():
			return daemon.InitApplyResponse{}, ctx.Err()
		default:
		}
		return daemon.InitApplyResponse{Initialized: true, DefaultHarness: "codex", DefaultHarnessSet: true}, nil
	}

	if _, err := executeRootCommandRaw("init"); err != nil {
		t.Fatalf("ari init returned error: %v", err)
	}
}

func TestInitWithFlagsPrintsHelperTrustExplanation(t *testing.T) {
	restore := replaceInitDeps(t)
	defer restore()

	initApplyRPC = func(ctx context.Context, socketPath string, req daemon.InitApplyRequest) (daemon.InitApplyResponse, error) {
		_ = ctx
		_ = socketPath
		return daemon.InitApplyResponse{Initialized: true, DefaultHarness: req.Harness, DefaultHarnessSet: true}, nil
	}

	out, err := executeRootCommandRaw("init", "--harness", "codex", "--model", "gpt-5.5", "--root", "~/Projects")
	if err != nil {
		t.Fatalf("ari init returned error: %v", err)
	}
	assertHelperTrustSignals(t, out)
}

func TestInitInteractivePrintsHelperTrustExplanation(t *testing.T) {
	restore := replaceInitDeps(t)
	defer restore()

	initOptionsRPC = func(ctx context.Context, socketPath string) (daemon.InitOptionsResponse, error) {
		_ = ctx
		_ = socketPath
		return daemon.InitOptionsResponse{
			Harnesses: []daemon.InitHarnessOption{{Name: "codex", Label: "codex"}},
			Models:    []daemon.InitModelOption{{Name: "", Label: "Manual/default model"}},
			Roots:     []daemon.InitRootOption{{Path: "~/", Label: "~/"}},
		}, nil
	}
	initPromptSelection = func(cmdOut initPromptOutput, options daemon.InitOptionsResponse, selected initSelection) (initSelection, error) {
		_ = cmdOut
		_ = options
		return initSelection{Harness: "codex", Model: "gpt-5.5", Root: "~/"}, nil
	}

	out, err := executeRootCommandRaw("init")
	if err != nil {
		t.Fatalf("ari init returned error: %v", err)
	}
	assertHelperTrustSignals(t, out)
}

func TestWriteHelperTrustExplanationContainsAllSignals(t *testing.T) {
	var buf strings.Builder
	writeHelperTrustExplanation(&buf)
	out := buf.String()
	assertHelperTrustSignals(t, out)
}

func assertHelperTrustSignals(t *testing.T, out string) {
	t.Helper()
	for _, signal := range []string{
		HelperTrustSignalReadOnly,
		HelperTrustSignalMutating,
		HelperTrustSignalChoices,
	} {
		if !strings.Contains(out, signal) {
			t.Errorf("output missing trust signal %q\nfull output:\n%s", signal, out)
		}
	}
}

func replaceInitDeps(t *testing.T) func() {
	t.Helper()
	originalConfigured := initConfiguredDaemonConfig
	originalEnsure := initEnsureDaemonRunning
	originalOptions := initOptionsRPC
	originalApply := initApplyRPC
	originalPrompt := initPromptHarness
	originalPromptSelection := initPromptSelection
	initConfiguredDaemonConfig = func() (*config.Config, error) {
		return &config.Config{Daemon: config.DaemonConfig{SocketPath: "test.sock", DBPath: "test.db", PIDPath: "test.pid"}, LogLevel: "info", VCSPreference: "auto"}, nil
	}
	initEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	initOptionsRPC = func(ctx context.Context, socketPath string) (daemon.InitOptionsResponse, error) {
		_ = ctx
		_ = socketPath
		return daemon.InitOptionsResponse{}, nil
	}
	initApplyRPC = func(ctx context.Context, socketPath string, req daemon.InitApplyRequest) (daemon.InitApplyResponse, error) {
		_ = ctx
		_ = socketPath
		return daemon.InitApplyResponse{Initialized: true, DefaultHarness: req.Harness, PreferredModel: req.Model, DefaultRoot: req.Root, DefaultHarnessSet: true}, nil
	}
	initPromptHarness = func(cmdOut initPromptOutput, options []daemon.InitHarnessOption) (string, error) {
		_ = cmdOut
		if len(options) == 0 {
			return "", fmt.Errorf("no harness options available")
		}
		return options[0].Name, nil
	}
	initPromptSelection = func(cmdOut initPromptOutput, options daemon.InitOptionsResponse, selected initSelection) (initSelection, error) {
		_ = cmdOut
		_ = options
		if strings.TrimSpace(selected.Harness) == "" {
			selected.Harness = "codex"
		}
		if strings.TrimSpace(selected.Root) == "" {
			selected.Root = "~/"
		}
		return selected, nil
	}
	return func() {
		initConfiguredDaemonConfig = originalConfigured
		initEnsureDaemonRunning = originalEnsure
		initOptionsRPC = originalOptions
		initApplyRPC = originalApply
		initPromptHarness = originalPrompt
		initPromptSelection = originalPromptSelection
	}
}
