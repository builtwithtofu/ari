package cmd

import (
	"errors"
	"fmt"

	"github.com/adavies/opencode-gaia/tools/gaia-cli/internal/lifecycle"
	"github.com/spf13/cobra"
)

func newPlanCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Manage deterministic GAIA planning lifecycle",
	}

	cmd.AddCommand(
		newPlanStartCmd(app),
		newPlanExecuteCmd(app),
	)

	return cmd
}

func newPlanStartCmd(app *App) *cobra.Command {
	var (
		sessionID string
		streamID  string
		mode      string
		risk      string
		scope     string
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start or refresh a planning session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			policy, err := lifecycle.StartPlan(app.RepoRoot, lifecycle.StartPlanInput{
				SessionID: sessionID,
				StreamID:  streamID,
				Mode:      lifecycle.CollaborationMode(mode),
				Risk:      lifecycle.RiskLevel(risk),
				Scope:     scope,
			})
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "State: %s\n", policy.State)
			fmt.Fprintf(cmd.OutOrStdout(), "Session: %s\n", policy.SessionID)
			fmt.Fprintf(cmd.OutOrStdout(), "Stream: %s\n", policy.StreamID)
			fmt.Fprintf(cmd.OutOrStdout(), "Next command: %s\n", policy.NextCommand)
			fmt.Fprintf(cmd.OutOrStdout(), "Saved: %s\n", policy.Path)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session id (default: date-based)")
	cmd.Flags().StringVar(&streamID, "stream", "default", "stream id")
	cmd.Flags().StringVar(&mode, "mode", string(lifecycle.ModeSupervised), "collaboration mode (supervised|checkpoint|agentic)")
	cmd.Flags().StringVar(&risk, "risk", string(lifecycle.RiskLow), "risk level (low|medium|high)")
	cmd.Flags().StringVar(&scope, "scope", "", "current planning scope")

	return cmd
}

func newPlanExecuteCmd(app *App) *cobra.Command {
	var (
		sessionID string
		approve   bool
	)

	cmd := &cobra.Command{
		Use:   "execute",
		Short: "Gate and enter execution for a ready plan",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if sessionID == "" {
				return errors.New("--session is required")
			}

			policy, err := lifecycle.ExecutePlan(app.RepoRoot, sessionID, approve)
			if err != nil {
				if errors.Is(err, lifecycle.ErrApprovalRequired) {
					fmt.Fprintf(cmd.OutOrStdout(), "State: %s\n", policy.State)
					fmt.Fprintf(cmd.OutOrStdout(), "Approval required for risk level %s\n", policy.Risk)
					fmt.Fprintf(cmd.OutOrStdout(), "Next command: %s\n", policy.NextCommand)
					return nil
				}

				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "State: %s\n", policy.State)
			fmt.Fprintf(cmd.OutOrStdout(), "Session: %s\n", policy.SessionID)
			fmt.Fprintf(cmd.OutOrStdout(), "Next command: %s\n", policy.NextCommand)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session id")
	cmd.Flags().BoolVar(&approve, "approve", false, "explicitly approve medium/high risk execution")
	return cmd
}
