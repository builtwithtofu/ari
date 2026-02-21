package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/adavies/opencode-gaia/tools/gaia-cli/internal/lifecycle"
	statuspkg "github.com/adavies/opencode-gaia/tools/gaia-cli/internal/status"
	"github.com/spf13/cobra"
)

func newStatusCmd(app *App) *cobra.Command {
	var sessionID string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show GAIA runtime status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeSummary, err := statuspkg.LoadRuntimeSummary(app.RepoRoot, sessionID)
			if err != nil {
				return err
			}

			lifecycleSummary, lifecycleErr := lifecycle.LoadPolicy(app.RepoRoot, runtimeSummary.SessionID)

			if asJSON {
				payload := map[string]any{
					"runtime": runtimeSummary,
				}
				if lifecycleErr == nil {
					payload["lifecycle"] = lifecycleSummary
				}

				encoded, err := json.MarshalIndent(payload, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Session: %s\n", runtimeSummary.SessionID)
			fmt.Fprintf(cmd.OutOrStdout(), "Current stream: %s\n", runtimeSummary.CurrentStreamID)
			fmt.Fprintf(cmd.OutOrStdout(), "Active work units: %d\n", runtimeSummary.ActiveCount)
			fmt.Fprintf(cmd.OutOrStdout(), "Completed work units: %d\n", runtimeSummary.CompletedCount)
			fmt.Fprintf(cmd.OutOrStdout(), "Blocked work units: %d\n", runtimeSummary.BlockedCount)
			if runtimeSummary.ActivePlan != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Current work unit: %s\n", runtimeSummary.ActivePlan.WorkUnit)
				fmt.Fprintf(cmd.OutOrStdout(), "Current risk: %s\n", runtimeSummary.ActivePlan.RiskLevel)
				fmt.Fprintf(cmd.OutOrStdout(), "Current status: %s\n", runtimeSummary.ActivePlan.Status)
			}

			if lifecycleErr == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Lifecycle state: %s\n", lifecycleSummary.State)
				fmt.Fprintf(cmd.OutOrStdout(), "Next command: %s\n", lifecycleSummary.NextCommand)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session id (default: latest runtime state)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output machine-readable JSON")

	return cmd
}
