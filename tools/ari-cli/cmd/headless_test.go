package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/headless"
	planpkg "github.com/builtwithtofu/ari/tools/ari-cli/internal/plan"
	"github.com/spf13/cobra"
)

func TestBuildCmd_Headless_StdoutIsJSONLOnly(t *testing.T) {
	t.Setenv(buildProviderEnv, buildProviderSimulator)

	projectDir, worldPath := setupTestWorld(t)
	writeTestPlan(t, worldPath, &planpkg.Plan{
		PlanID:    "plan-headless-success",
		Goal:      "Headless success",
		Status:    planpkg.PlanStatusApproved,
		CreatedAt: nowRFC3339(),
		UpdatedAt: nowRFC3339(),
		Steps: []planpkg.Step{
			{
				StepID:      "step-1",
				Type:        planpkg.StepTypeHumanInput,
				Description: "Ask for confirmation",
				Status:      planpkg.StepStatusApproved,
				Payload: map[string]any{
					"prompt": "Proceed?",
				},
			},
		},
	})

	restore := chdirForTest(t, filepath.Join(projectDir, "nested", "dir"))
	defer restore()

	stdout, stderrText := captureProcessOutput(t, func() {
		command := NewBuildCmd()
		enableHeadlessFlag(t, command)
		command.SilenceUsage = true
		if err := command.Flags().Set("headless", "true"); err != nil {
			t.Fatalf("set headless flag: %v", err)
		}
		if !isHeadless(command) {
			t.Fatal("expected headless mode enabled")
		}
		command.SetArgs([]string{"--plan", "plan-headless-success"})

		err := command.Execute()
		if err != nil {
			t.Fatalf("expected command success, got error: %v", err)
		}
	})

	if !strings.Contains(stderrText, "Executing plan: plan-headless-success") {
		t.Fatalf("expected diagnostics on stderr, got:\n%s", stderrText)
	}
	if !strings.Contains(stderrText, "Status: success") {
		t.Fatalf("expected success status on stderr, got:\n%s", stderrText)
	}

	assertJSONLLines(t, stdout)
	if strings.Contains(stdout, "Executing plan") || strings.Contains(stdout, "Status: success") {
		t.Fatalf("stdout should contain only JSONL events, got:\n%s", stdout)
	}
}

func TestBuildCmd_Headless_FailureExitAndDiagnostics(t *testing.T) {
	t.Setenv(buildProviderEnv, buildProviderSimulator)

	projectDir, worldPath := setupTestWorld(t)
	writeTestPlan(t, worldPath, &planpkg.Plan{
		PlanID:    "plan-headless-fail",
		Goal:      "Headless fail",
		Status:    planpkg.PlanStatusApproved,
		CreatedAt: nowRFC3339(),
		UpdatedAt: nowRFC3339(),
		Steps: []planpkg.Step{
			{
				StepID:      "step-1",
				Type:        planpkg.StepTypeToolCall,
				Description: "Call unknown tool",
				Status:      planpkg.StepStatusApproved,
				Payload: map[string]any{
					"tool": "unknown_tool",
				},
			},
		},
	})

	restore := chdirForTest(t, projectDir)
	defer restore()

	stdout, stderrText := captureProcessOutput(t, func() {
		command := NewBuildCmd()
		enableHeadlessFlag(t, command)
		command.SilenceUsage = true
		if err := command.Flags().Set("headless", "true"); err != nil {
			t.Fatalf("set headless flag: %v", err)
		}
		if !isHeadless(command) {
			t.Fatal("expected headless mode enabled")
		}
		command.SetArgs([]string{"--plan", "plan-headless-fail"})

		err := command.Execute()
		if err == nil {
			t.Fatal("expected execution failure")
		}
	})

	assertJSONLLines(t, stdout)
	if strings.Contains(stdout, "Execution failed") || strings.Contains(stdout, "Status: failed") {
		t.Fatalf("stdout should not contain human diagnostics, got:\n%s", stdout)
	}

	if !strings.Contains(stderrText, "Execution failed") {
		t.Fatalf("expected failure diagnostics on stderr, got:\n%s", stderrText)
	}
	if !strings.Contains(stderrText, "Status: failed") {
		t.Fatalf("expected failed status on stderr, got:\n%s", stderrText)
	}
}

func TestPlanCmd_HeadlessUnsupported(t *testing.T) {
	command := NewPlanCmd()
	enableHeadlessFlag(t, command)
	command.SilenceUsage = true
	if err := command.Flags().Set("headless", "true"); err != nil {
		t.Fatalf("set headless flag: %v", err)
	}
	command.SetArgs([]string{"goal"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected error for unsupported command")
	}

	msg := err.Error()
	if !strings.Contains(msg, "plan") {
		t.Errorf("error should mention command name, got: %s", msg)
	}
	if !strings.Contains(msg, "does not support --headless") {
		t.Errorf("error should explain unsupported status, got: %s", msg)
	}
}

func TestBuildCmd_NonHeadless_Unchanged(t *testing.T) {
	t.Setenv(buildProviderEnv, buildProviderSimulator)

	projectDir, worldPath := setupTestWorld(t)
	writeTestPlan(t, worldPath, &planpkg.Plan{
		PlanID:    "plan-non-headless",
		Goal:      "Non-headless behavior",
		Status:    planpkg.PlanStatusApproved,
		CreatedAt: nowRFC3339(),
		UpdatedAt: nowRFC3339(),
		Steps: []planpkg.Step{
			{
				StepID:      "step-1",
				Type:        planpkg.StepTypeHumanInput,
				Description: "Ask for confirmation",
				Status:      planpkg.StepStatusApproved,
				Payload: map[string]any{
					"prompt": "Proceed?",
				},
			},
		},
	})

	restore := chdirForTest(t, projectDir)
	defer restore()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	root := NewRootCmd()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"build", "--plan", "plan-non-headless"})

	err := root.Execute()
	if err != nil {
		t.Fatalf("expected command success, got error: %v", err)
	}

	stdoutText := stdout.String()
	if !strings.Contains(stdoutText, "Executing plan: plan-non-headless") {
		t.Fatalf("expected human progress output on stdout, got:\n%s", stdoutText)
	}
	if !strings.Contains(stdoutText, "Status: success") {
		t.Fatalf("expected success status on stdout, got:\n%s", stdoutText)
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected stderr to be empty in non-headless mode, got:\n%s", stderr.String())
	}

	if !headless.IsHeadlessSupported("build") {
		t.Fatal("build command should support headless mode")
	}
}

func captureProcessOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()

	originalStdout := os.Stdout
	originalStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}

	os.Stdout = stdoutW
	os.Stderr = stderrW
	defer func() {
		os.Stdout = originalStdout
		os.Stderr = originalStderr
	}()

	fn()

	if err := stdoutW.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	if err := stderrW.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}

	stdoutData, err := io.ReadAll(stdoutR)
	if err != nil {
		_ = stdoutR.Close()
		t.Fatalf("read captured stdout: %v", err)
	}
	stderrData, err := io.ReadAll(stderrR)
	if err != nil {
		_ = stdoutR.Close()
		_ = stderrR.Close()
		t.Fatalf("read captured stderr: %v", err)
	}
	if err := stdoutR.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	if err := stderrR.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}

	return string(stdoutData), string(stderrData)
}

func assertJSONLLines(t *testing.T, output string) {
	t.Helper()

	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		t.Fatal("expected JSONL output on stdout, got empty output")
	}

	lines := strings.Split(trimmed, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			t.Fatalf("stdout line %d is empty; expected JSON object", i+1)
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("stdout line %d is not valid JSON: %v\nline: %s", i+1, err, line)
		}

		if _, ok := event["type"]; !ok {
			t.Fatalf("stdout line %d missing event type: %s", i+1, line)
		}
	}
}

func enableHeadlessFlag(t *testing.T, command *cobra.Command) {
	t.Helper()

	if command.Flags().Lookup("headless") != nil {
		return
	}

	command.Flags().Bool("headless", false, "Emit JSON events to stdout for machine consumption")
}
