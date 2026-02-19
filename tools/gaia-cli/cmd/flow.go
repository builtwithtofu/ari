package cmd

import (
	"errors"
	"fmt"

	flowpkg "github.com/adavies/opencode-gaia/tools/gaia-cli/internal/flow"
	"github.com/adavies/opencode-gaia/tools/gaia-cli/internal/lifecycle"
	"github.com/spf13/cobra"
)

func newFlowCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flow",
		Short: "Run deterministic GAIA flow lifecycle commands",
	}

	cmd.AddCommand(
		newFlowStartCmd(app),
		newFlowIterateCmd(app),
		newFlowExecuteCmd(app),
		newFlowContinueCmd(app),
	)

	return cmd
}

func newFlowStartCmd(app *App) *cobra.Command {
	var (
		sessionID string
		streamID  string
		mode      string
		risk      string
		scope     string
		asJSON    bool
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start or refresh a deterministic planning flow",
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := flowpkg.Start(app.RepoRoot, flowpkg.StartInput{
				SessionID: sessionID,
				StreamID:  streamID,
				Mode:      lifecycle.CollaborationMode(mode),
				Risk:      lifecycle.RiskLevel(risk),
				Scope:     scope,
			})
			if err != nil {
				return err
			}

			if asJSON {
				return printJSON(cmd, result)
			}

			printFlowResult(cmd, result)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session id (default: date-based)")
	cmd.Flags().StringVar(&streamID, "stream", "default", "stream id")
	cmd.Flags().StringVar(&mode, "mode", string(lifecycle.ModeSupervised), "collaboration mode (supervised|checkpoint|agentic)")
	cmd.Flags().StringVar(&risk, "risk", string(lifecycle.RiskLow), "risk level (low|medium|high)")
	cmd.Flags().StringVar(&scope, "scope", "", "current planning scope")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output machine-readable JSON")

	return cmd
}

func newFlowIterateCmd(app *App) *cobra.Command {
	var (
		sessionID string
		scope     string
		note      string
		asJSON    bool
	)

	cmd := &cobra.Command{
		Use:   "iterate",
		Short: "Advance one deterministic planning loop iteration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := flowpkg.Iterate(app.RepoRoot, flowpkg.IterateInput{
				SessionID: sessionID,
				Scope:     scope,
				Note:      note,
			})
			if err != nil {
				return err
			}

			if asJSON {
				return printJSON(cmd, result)
			}

			printFlowResult(cmd, result)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session id")
	cmd.Flags().StringVar(&scope, "scope", "", "updated planning scope (falls back to current scope)")
	cmd.Flags().StringVar(&note, "note", "", "operator feedback note for this planning loop")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output machine-readable JSON")
	_ = cmd.MarkFlagRequired("session")

	return cmd
}

func newFlowExecuteCmd(app *App) *cobra.Command {
	var (
		sessionID string
		approve   bool
		asJSON    bool
	)

	cmd := &cobra.Command{
		Use:   "execute",
		Short: "Enter execution for the active deterministic flow",
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := flowpkg.Execute(app.RepoRoot, flowpkg.ExecuteInput{
				SessionID: sessionID,
				Approve:   approve,
			})
			if err != nil && !errors.Is(err, lifecycle.ErrApprovalRequired) {
				return err
			}

			if asJSON {
				return printJSON(cmd, map[string]any{
					"result":            result,
					"approval_required": errors.Is(err, lifecycle.ErrApprovalRequired),
				})
			}

			printFlowResult(cmd, result)
			if errors.Is(err, lifecycle.ErrApprovalRequired) {
				fmt.Fprintln(cmd.OutOrStdout(), "Approval required for medium/high risk execution")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session id")
	cmd.Flags().BoolVar(&approve, "approve", false, "explicitly approve medium/high risk execution")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output machine-readable JSON")
	_ = cmd.MarkFlagRequired("session")
	return cmd
}

func newFlowContinueCmd(app *App) *cobra.Command {
	var (
		sessionID string
		asJSON    bool
	)

	cmd := &cobra.Command{
		Use:   "continue",
		Short: "Resume execution from persisted deterministic flow state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := flowpkg.Continue(app.RepoRoot, sessionID)
			if err != nil {
				return err
			}

			if asJSON {
				return printJSON(cmd, result)
			}

			printFlowResult(cmd, result)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session id")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output machine-readable JSON")
	_ = cmd.MarkFlagRequired("session")
	return cmd
}

func printFlowResult(cmd *cobra.Command, result flowpkg.TransitionResult) {
	fmt.Fprintf(cmd.OutOrStdout(), "State: %s\n", result.Policy.State)
	fmt.Fprintf(cmd.OutOrStdout(), "Session: %s\n", result.Policy.SessionID)
	fmt.Fprintf(cmd.OutOrStdout(), "Stream: %s\n", result.Policy.StreamID)
	fmt.Fprintf(cmd.OutOrStdout(), "Iteration: %d\n", result.Flow.Iteration)
	fmt.Fprintf(cmd.OutOrStdout(), "Next command: %s\n", result.NextCommand)
	fmt.Fprintf(cmd.OutOrStdout(), "Lifecycle: %s\n", result.Flow.LifecyclePath)
	fmt.Fprintf(cmd.OutOrStdout(), "Flow state: %s\n", result.Flow.FlowPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Flow events: %s\n", result.Flow.EventLogPath)
}
