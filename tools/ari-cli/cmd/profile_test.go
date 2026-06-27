package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

func TestProfileDefaultsCommandIsRemovedFromCuratedCLI(t *testing.T) {
	profile := NewProfileCmd()
	defaults, _, err := profile.Find([]string{"defaults"})
	if err == nil {
		t.Fatalf("profile defaults resolved to %q; want command removed", defaults.CommandPath())
	}

	out, err := executeRootCommand("profile", "--help")
	if err != nil {
		t.Fatalf("profile help returned error: %v", err)
	}
	if strings.Contains(out, "defaults") {
		t.Fatalf("profile help = %q, want defaults command removed", out)
	}
}

func TestProfileHelpUsesProfileTerminology(t *testing.T) {
	out, err := executeRootCommand("profile", "--help")
	if err != nil {
		t.Fatalf("profile help returned error: %v", err)
	}
	if strings.Contains(out, "agent profile") || strings.Contains(out, "agent profiles") {
		t.Fatalf("profile help = %q, want profile terminology without agent-profile wording", out)
	}
	if !strings.Contains(out, "Manage Ari profiles") {
		t.Fatalf("profile help = %q, want profile help summary", out)
	}
}

func TestProfileCreatePromptFileSuppliesPromptAndConflictsWithPromptFlag(t *testing.T) {
	h := newCommandHarness(t)
	promptPath := filepath.Join(t.TempDir(), "profile-prompt.md")
	if err := os.WriteFile(promptPath, []byte("profile behavior from file\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	swapTestValue(t, &profileEnsureDaemonRunning, func(context.Context, *config.Config) error { return nil })
	swapTestValue(t, &profileCreateRPC, func(_ context.Context, _ string, req daemon.ProfileCreateRequest) (daemon.ProfileResponse, error) {
		if req.Prompt != "profile behavior from file\n" {
			t.Fatalf("profile.create prompt = %q, want prompt-file contents", req.Prompt)
		}
		return daemon.ProfileResponse{Name: req.Name, Harness: req.Harness, Prompt: req.Prompt}, nil
	})

	if _, err := h.execute("profile", "create", "reviewer", "--harness", "codex", "--prompt-file", promptPath); err != nil {
		t.Fatalf("profile create prompt-file returned error: %v", err)
	}
	_, err := h.execute("profile", "create", "reviewer", "--harness", "codex", "--prompt", "inline", "--prompt-file", promptPath)
	if err == nil || !strings.Contains(err.Error(), "Use either --prompt or --prompt-file, not both") {
		t.Fatalf("profile create conflict error = %v, want prompt/prompt-file conflict", err)
	}
}
