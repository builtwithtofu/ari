package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootIsInteractiveTerminal = func(cmd *cobra.Command) bool {
	return isInteractiveTerminal(cmd)
}

var rootRunInteractive = func(cmd *cobra.Command, _ []string) error {
	if cmd == nil {
		return fmt.Errorf("root command is required")
	}

	return cmd.Help()
}

var rootRunNonInteractive = func(cmd *cobra.Command, _ []string) error {
	if cmd == nil {
		return fmt.Errorf("root command is required")
	}

	return cmd.Help()
}

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "ari",
		Short: "Ari daemon CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			if rootIsInteractiveTerminal(cmd) {
				return rootRunInteractive(cmd, args)
			}
			return rootRunNonInteractive(cmd, args)
		},
	}

	rootCmd.AddCommand(NewDaemonCmd())
	rootCmd.AddCommand(NewWorkspaceCmd())
	rootCmd.AddCommand(NewCommandCmd())
	rootCmd.AddCommand(NewAgentCmd())

	return rootCmd
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
