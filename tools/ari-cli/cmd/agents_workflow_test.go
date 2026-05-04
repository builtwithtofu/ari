package cmd

import (
	"strings"
	"testing"
)

func TestSessionHelpUsesSessionMessageTerminology(t *testing.T) {
	for _, args := range [][]string{{"session", "message", "send", "--help"}, {"session", "call", "--help"}} {
		out, err := executeRootCommand(args...)
		if err != nil {
			t.Fatalf("%v returned error: %v", args, err)
		}
		if strings.Contains(out, "Message excerpt") || strings.Contains(out, "message excerpt") || strings.Contains(out, "Excerpt id") || strings.Contains(out, "agent message") || strings.Contains(out, "workspace agent") {
			t.Fatalf("%v output = %q, want session/message terminology without legacy agent wording", args, out)
		}
	}
}

func TestSessionHelpDoesNotExposeLegacyAgentsCommand(t *testing.T) {
	out, err := executeRootCommand("session", "--help")
	if err != nil {
		t.Fatalf("session --help returned error: %v", err)
	}
	if strings.Contains(out, "agents") || strings.Contains(out, "workspace agent") {
		t.Fatalf("session help = %q, want no legacy agents terminology", out)
	}
}
