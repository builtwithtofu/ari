package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

func TestProfileDefaultsRejectsInvalidInvocationClassBeforeWritingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".ari")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	original := `{"log_level":"info","vcs_preference":"auto","default_harness":"codex"}`
	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	_, err := executeRootCommand("profile", "defaults", "--invocation-class", "invalid")
	if err == nil || !strings.Contains(err.Error(), "default_invocation_class") {
		t.Fatalf("profile defaults error = %v, want validation error", err)
	}
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if _, ok := parsed["default_invocation_class"]; ok {
		t.Fatalf("config = %s, must not persist invalid invocation class", string(body))
	}
	if parsed["default_harness"] != "codex" {
		t.Fatalf("default_harness = %v, want preserved codex", parsed["default_harness"])
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
	t.Setenv("HOME", t.TempDir())
	promptPath := filepath.Join(t.TempDir(), "profile-prompt.md")
	if err := os.WriteFile(promptPath, []byte("profile behavior from file\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	originalEnsure := profileEnsureDaemonRunning
	originalCreate := profileCreateRPC
	profileEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	profileCreateRPC = func(_ context.Context, _ string, req daemon.AgentProfileCreateRequest) (daemon.AgentProfileResponse, error) {
		if req.Prompt != "profile behavior from file\n" {
			t.Fatalf("profile.create prompt = %q, want prompt-file contents", req.Prompt)
		}
		return daemon.AgentProfileResponse{Name: req.Name, Harness: req.Harness, Prompt: req.Prompt}, nil
	}
	t.Cleanup(func() {
		profileEnsureDaemonRunning = originalEnsure
		profileCreateRPC = originalCreate
	})

	if _, err := executeRootCommand("profile", "create", "reviewer", "--harness", "codex", "--prompt-file", promptPath); err != nil {
		t.Fatalf("profile create prompt-file returned error: %v", err)
	}
	_, err := executeRootCommand("profile", "create", "reviewer", "--harness", "codex", "--prompt", "inline", "--prompt-file", promptPath)
	if err == nil || !strings.Contains(err.Error(), "Use either --prompt or --prompt-file, not both") {
		t.Fatalf("profile create conflict error = %v, want prompt/prompt-file conflict", err)
	}
}
