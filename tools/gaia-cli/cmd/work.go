package cmd

import (
	"errors"
	"fmt"

	"github.com/adavies/opencode-gaia/tools/gaia-cli/internal/lifecycle"
	"github.com/spf13/cobra"
)

func newWorkCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "work",
		Short: "Resume and manage in-progress GAIA work",
	}

	cmd.AddCommand(newWorkContinueCmd(app))
	return cmd
}

func newWorkContinueCmd(app *App) *cobra.Command {
	var sessionID string

	cmd := &cobra.Command{
		Use:   "continue",
		Short: "Resume executing work from persisted lifecycle state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if sessionID == "" {
				return errors.New("--session is required")
			}

			policy, err := lifecycle.ContinueWork(app.RepoRoot, sessionID)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "State: %s\n", policy.State)
			fmt.Fprintf(cmd.OutOrStdout(), "Session: %s\n", policy.SessionID)
			fmt.Fprintf(cmd.OutOrStdout(), "Next command: %s\n", policy.NextCommand)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session id")
	return cmd
}
