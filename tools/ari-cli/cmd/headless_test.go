package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/headless"
	planpkg "github.com/builtwithtofu/ari/tools/ari-cli/internal/plan"
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

	stdout := captureProcessStdout(t, func() {
		root := NewRootCmd()
		stderr := &bytes.Buffer{}
		root.SetErr(stderr)
		root.SetArgs([]string{"build", "--plan", "plan-headless-success", "--headless"})

		err := root.Execute()
		if err != nil {
			t.Fatalf("expected command success, got error: %v", err)
		}

		stderrText := stderr.String()
		if !strings.Contains(stderrText, "Executing plan: plan-headless-success") {
			t.Fatalf("expected diagnostics on stderr, got:\n%s", stderrText)
		}
		if !strings.Contains(stderrText, "Status: success") {
			t.Fatalf("expected success status on stderr, got:\n%s", stderrText)
		}
	})

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

	var stderr bytes.Buffer
	stdout := captureProcessStdout(t, func() {
		root := NewRootCmd()
		root.SetErr(&stderr)
		root.SetArgs([]string{"build", "--plan", "plan-headless-fail", "--headless"})

		err := root.Execute()
		if err == nil {
			t.Fatal("expected execution failure")
		}
	})

	assertJSONLLines(t, stdout)
	if strings.Contains(stdout, "Execution failed") || strings.Contains(stdout, "Status: failed") {
		t.Fatalf("stdout should not contain human diagnostics, got:\n%s", stdout)
	}

	stderrText := stderr.String()
	if !strings.Contains(stderrText, "Execution failed") {
		t.Fatalf("expected failure diagnostics on stderr, got:\n%s", stderrText)
	}
	if !strings.Contains(stderrText, "Status: failed") {
		t.Fatalf("expected failed status on stderr, got:\n%s", stderrText)
	}
}

func TestPlanCmd_HeadlessUnsupported(t *testing.T) {
	root := NewRootCmd()
	root.SetArgs([]string{"plan", "goal", "--headless"})

	err := root.Execute()
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

func captureProcessStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}

	os.Stdout = w
	defer func() {
		os.Stdout = originalStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}

	data, err := os.ReadFile("/proc/self/fd/" + fdString(r.Fd()))
	if err == nil {
		_ = r.Close()
		return string(data)
	}

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(r); err != nil {
		_ = r.Close()
		t.Fatalf("read captured stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}

	return buf.String()
}

func fdString(fd uintptr) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(strings.Trim(strings.ReplaceAll(strings.TrimSpace(strings.TrimSpace(strings.TrimSpace(strings.TrimSpace(" "))), " ", "")), "", "")), "", "")), "", ""), ""))
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
