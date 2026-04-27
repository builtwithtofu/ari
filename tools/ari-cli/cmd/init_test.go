package cmd

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

func TestInitWithHarnessAppliesDaemonOwnedChoice(t *testing.T) {
	restore := replaceInitDeps(t)
	defer restore()

	applied := daemon.InitApplyRequest{}
	initApplyRPC = func(ctx context.Context, socketPath string, req daemon.InitApplyRequest) (daemon.InitApplyResponse, error) {
		_ = ctx
		if socketPath != "test.sock" {
			t.Fatalf("socketPath = %q, want test.sock", socketPath)
		}
		applied = req
		return daemon.InitApplyResponse{Initialized: true, DefaultHarness: req.Harness, DefaultHarnessSet: true}, nil
	}

	out, err := executeRootCommandRaw("init", "--harness", "codex")
	if err != nil {
		t.Fatalf("ari init returned error: %v", err)
	}
	if applied.Harness != "codex" {
		t.Fatalf("applied harness = %q, want codex", applied.Harness)
	}
	if !strings.Contains(out, "Default harness set: codex") {
		t.Fatalf("output missing harness success: %q", out)
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
		return daemon.InitOptionsResponse{Harnesses: []daemon.InitHarnessOption{{Name: "codex", Label: "codex"}}}, nil
	}
	initPromptHarness = func(cmdOut initPromptOutput, options []daemon.InitHarnessOption) (string, error) {
		_ = cmdOut
		if len(options) != 1 || options[0].Name != "codex" {
			t.Fatalf("prompt options = %#v", options)
		}
		return "codex", nil
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
		return daemon.InitOptionsResponse{Harnesses: []daemon.InitHarnessOption{{Name: "claude-code", Label: "claude-code"}, {Name: "codex", Label: "codex"}}}, nil
	}
	initPromptHarness = promptInitHarness

	root := NewRootCmd()
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("2\n"))
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err != nil {
		t.Fatalf("ari init returned error: %v", err)
	}
	if !strings.Contains(out.String(), "Default harness set: codex") {
		t.Fatalf("output missing selected harness: %q", out.String())
	}
}

func replaceInitDeps(t *testing.T) func() {
	t.Helper()
	originalConfigured := initConfiguredDaemonConfig
	originalEnsure := initEnsureDaemonRunning
	originalOptions := initOptionsRPC
	originalApply := initApplyRPC
	originalPrompt := initPromptHarness
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
		return daemon.InitApplyResponse{Initialized: true, DefaultHarness: req.Harness, DefaultHarnessSet: true}, nil
	}
	initPromptHarness = func(cmdOut initPromptOutput, options []daemon.InitHarnessOption) (string, error) {
		_ = cmdOut
		if len(options) == 0 {
			return "", fmt.Errorf("no harness options available")
		}
		return options[0].Name, nil
	}
	return func() {
		initConfiguredDaemonConfig = originalConfigured
		initEnsureDaemonRunning = originalEnsure
		initOptionsRPC = originalOptions
		initApplyRPC = originalApply
		initPromptHarness = originalPrompt
	}
}
