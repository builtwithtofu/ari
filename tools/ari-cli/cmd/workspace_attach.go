package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newWorkspaceAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach",
		Short: "Attach to the workspace runtime",
		Args:  cobra.NoArgs,
		RunE:  runWorkspaceAttachEntrypoint,
	}
}

func runWorkspaceAttachEntrypoint(cmd *cobra.Command, _ []string) error {
	if cmd == nil {
		return fmt.Errorf("workspace attach: command is required")
	}

	_, err := fmt.Fprintln(cmd.OutOrStdout(), "Workspace attach is not implemented yet; run `ari workspace set <id-or-name>` then `ari agent attach <id-or-name>` for now.")
	return err
}
