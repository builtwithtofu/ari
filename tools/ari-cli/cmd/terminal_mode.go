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
