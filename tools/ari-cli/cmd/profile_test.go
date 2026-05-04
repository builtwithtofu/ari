package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
