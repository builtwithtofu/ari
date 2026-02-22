package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	planpkg "github.com/builtwithtofu/ari/tools/ari-cli/internal/plan"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/world"
)

func TestBuildCmd_RequiresPlanFlag(t *testing.T) {
	command := NewBuildCmd()
	command.SetArgs([]string{})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected missing required flag error")
	}
	if !strings.Contains(err.Error(), "required flag") {
		t.Fatalf("expected required flag error, got: %v", err)
	}
}

func TestBuildCmd_ExecutesPlanAndShowsProgress(t *testing.T) {
	t.Setenv(buildProviderEnv, buildProviderSimulator)

	projectDir, worldPath := setupTestWorld(t)
	writeTestPlan(t, worldPath, &planpkg.Plan{
		PlanID:    "plan-success",
		Goal:      "Execute successful plan",
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
			{
				StepID:      "step-2",
				Type:        planpkg.StepTypeToolCall,
				Description: "Capture user response",
				DependsOn:   []string{"step-1"},
				Status:      planpkg.StepStatusApproved,
				Payload: map[string]any{
					"tool": "ask_user",
					"arguments": map[string]any{
						"prompt": "Provide answer",
					},
				},
			},
		},
	})

	restore := chdirForTest(t, filepath.Join(projectDir, "nested", "dir"))
	defer restore()

	output := &bytes.Buffer{}
	command := NewBuildCmd()
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"--plan", "plan-success"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected command success, got error: %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "Executing step 1/2: Ask for confirmation") {
		t.Fatalf("expected progress output for step start, got:\n%s", got)
	}
	if !strings.Contains(got, "[ok] Completed") {
		t.Fatalf("expected completion output, got:\n%s", got)
	}
	if !strings.Contains(got, "Status: success") {
		t.Fatalf("expected success status, got:\n%s", got)
	}
}

func TestBuildCmd_LoadPlanError(t *testing.T) {
	t.Setenv(buildProviderEnv, buildProviderSimulator)

	projectDir, _ := setupTestWorld(t)
	restore := chdirForTest(t, projectDir)
	defer restore()

	command := NewBuildCmd()
	command.SetArgs([]string{"--plan", "missing-plan"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected load plan error")
	}
	if !strings.Contains(err.Error(), "load plan") {
		t.Fatalf("expected load plan error, got: %v", err)
	}
}

func TestBuildCmd_ExecutionFailurePrintsStatus(t *testing.T) {
	t.Setenv(buildProviderEnv, buildProviderSimulator)

	projectDir, worldPath := setupTestWorld(t)
	writeTestPlan(t, worldPath, &planpkg.Plan{
		PlanID:    "plan-fail",
		Goal:      "Fail execution",
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

	output := &bytes.Buffer{}
	command := NewBuildCmd()
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"--plan", "plan-fail"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected execution failure")
	}

	got := output.String()
	if !strings.Contains(got, "Execution failed") {
		t.Fatalf("expected failure summary in output, got:\n%s", got)
	}
	if !strings.Contains(got, "Status: failed") {
		t.Fatalf("expected failed status in output, got:\n%s", got)
	}
}

func setupTestWorld(t *testing.T) (string, string) {
	t.Helper()

	projectDir := t.TempDir()
	ariDir := filepath.Join(projectDir, ".ari")
	worldPath := filepath.Join(ariDir, "world.db")

	db, err := world.Initialize(worldPath)
	if err != nil {
		t.Fatalf("initialize world db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close initialized db: %v", err)
	}

	return projectDir, worldPath
}

func writeTestPlan(t *testing.T, worldPath string, p *planpkg.Plan) {
	t.Helper()

	db, err := world.Initialize(worldPath)
	if err != nil {
		t.Fatalf("open world db: %v", err)
	}
	defer db.Close()

	content, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}

	queries := world.New(db)
	_, err = queries.CreatePlan(context.Background(), world.CreatePlanParams{
		ID:        p.PlanID,
		Title:     p.Goal,
		Status:    string(p.Status),
		Content:   string(content),
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
}

func chdirForTest(t *testing.T, dir string) func() {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create test cwd: %v", err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("change cwd: %v", err)
	}

	return func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
