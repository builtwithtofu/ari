package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestIsInteractiveTerminalReturnsFalseForNonFileInput(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("input"))

	if got := isInteractiveTerminal(cmd); got {
		t.Fatal("isInteractiveTerminal = true, want false for non-file input")
	}
}

func TestIsInteractiveTerminalWithOutputReturnsFalseForBufferedOutput(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("input"))
	cmd.SetOut(&bytes.Buffer{})

	if got := isInteractiveTerminalWithOutput(cmd); got {
		t.Fatal("isInteractiveTerminalWithOutput = true, want false for buffered output")
	}
}
