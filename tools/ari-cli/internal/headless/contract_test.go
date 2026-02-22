package headless

import (
	"strings"
	"testing"
)

func TestSupportedCommandsMatrix(t *testing.T) {
	expected := map[string]bool{
		"build":  true,
		"plan":   false,
		"init":   false,
		"ask":    false,
		"review": false,
	}

	if len(SupportedCommands) != len(expected) {
		t.Fatalf("SupportedCommands has %d commands, want %d", len(SupportedCommands), len(expected))
	}

	for command, want := range expected {
		got, ok := SupportedCommands[command]
		if !ok {
			t.Fatalf("SupportedCommands missing command %q", command)
		}
		if got != want {
			t.Fatalf("SupportedCommands[%q] = %v, want %v", command, got, want)
		}
	}
}

func TestIsHeadlessSupported(t *testing.T) {
	tests := []struct {
		command  string
		expected bool
	}{
		{"build", true},
		{"plan", false},
		{"init", false},
		{"ask", false},
		{"review", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := IsHeadlessSupported(tt.command)
			if got != tt.expected {
				t.Errorf("IsHeadlessSupported(%q) = %v, want %v", tt.command, got, tt.expected)
			}
		})
	}
}

func TestHeadlessUnsupportedError(t *testing.T) {
	err := HeadlessUnsupportedError("plan")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "plan") {
		t.Errorf("error message should mention command name, got: %s", msg)
	}
	if !strings.Contains(msg, "does not support --headless") {
		t.Errorf("error message should explain unsupported status, got: %s", msg)
	}
	if !strings.Contains(msg, "interactive workflow") {
		t.Errorf("error message should mention reason, got: %s", msg)
	}
}
