package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/provider"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/world"
	"github.com/spf13/cobra"
)

func TestPlanCommandApproveSavesPlan(t *testing.T) {
	tempDir := t.TempDir()
	worldPath := filepath.Join(tempDir, ".ari", "world.db")
	db, err := world.Initialize(worldPath)
	if err != nil {
		t.Fatalf("initialize world: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close world db: %v", err)
	}

	t.Chdir(tempDir)

	input := strings.Join([]string{
		"answer 1",
		"answer 2",
		"answer 3",
		"answer 4",
		"answer 5",
		"approve",
	}, "\n") + "\n"

	var output bytes.Buffer
	cmd := NewPlanCmd()
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"fix"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute plan command: %v", err)
	}

	stdout := output.String()
	if !strings.Contains(stdout, "[1/4] Research phase...") {
		t.Fatalf("expected research phase output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "[2/4] Question phase...") {
		t.Fatalf("expected question phase output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "[3/4] Refinement phase...") {
		t.Fatalf("expected refinement phase output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "Plan approved and saved.") {
		t.Fatalf("expected approved output, got: %q", stdout)
	}

	readDB, err := sql.Open("sqlite", worldPath)
	if err != nil {
		t.Fatalf("open world db: %v", err)
	}
	t.Cleanup(func() {
		_ = readDB.Close()
	})

	queries := world.New(readDB)
	plans, err := queries.ListPlans(context.Background())
	if err != nil {
		t.Fatalf("list plans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("plan count = %d, want 1", len(plans))
	}
	if plans[0].Status != "approved" {
		t.Fatalf("plan status = %q, want %q", plans[0].Status, "approved")
	}
}

func TestRunPlanWorkflowResearchMoreThenApprove(t *testing.T) {
	tempDir := t.TempDir()
	worldPath := filepath.Join(tempDir, ".ari", "world.db")
	db, err := world.Initialize(worldPath)
	if err != nil {
		t.Fatalf("initialize world: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	queries := world.New(db)
	providerClient := provider.NewSimulator(provider.PlanningScenario(nil))

	input := strings.Join([]string{
		"a1",
		"a2",
		"a3",
		"a4",
		"a5",
		"research more",
		"approve",
	}, "\n") + "\n"

	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	err = runPlanWorkflow(context.Background(), cmd, "fix", queries, providerClient)
	if err != nil {
		t.Fatalf("run plan workflow: %v", err)
	}

	stdout := output.String()
	if !strings.Contains(stdout, "Running additional research...") {
		t.Fatalf("expected additional research output, got: %q", stdout)
	}
	if !strings.Contains(stdout, "Plan approved and saved.") {
		t.Fatalf("expected approved output, got: %q", stdout)
	}

	plans, err := queries.ListPlans(context.Background())
	if err != nil {
		t.Fatalf("list plans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("plan count = %d, want 1", len(plans))
	}
}

func TestRootIncludesPlanCommand(t *testing.T) {
	root := NewRootCmd()
	if _, _, err := root.Find([]string{"plan"}); err != nil {
		t.Fatalf("find plan command: %v", err)
	}
}
