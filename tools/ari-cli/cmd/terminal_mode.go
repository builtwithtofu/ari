package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func isInteractiveTerminal(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}

	input := cmd.InOrStdin()
	inputFile, ok := input.(*os.File)
	if !ok {
		return false
	}

	return term.IsTerminal(int(inputFile.Fd()))
}

func isInteractiveTerminalWithOutput(cmd *cobra.Command) bool {
	if !isInteractiveTerminal(cmd) {
		return false
	}

	output := cmd.OutOrStdout()
	outputFile, ok := output.(*os.File)
	if !ok {
		return false
	}

	return term.IsTerminal(int(outputFile.Fd()))
}
