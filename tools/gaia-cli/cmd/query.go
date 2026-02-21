package cmd

import (
	"encoding/json"
	"fmt"

	querypkg "github.com/adavies/opencode-gaia/tools/gaia-cli/internal/query"
	"github.com/spf13/cobra"
)

func newQueryCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query .gaia state for users and GAIA agents",
	}

	cmd.AddCommand(
		newQueryAllCmd(app),
		newQuerySessionsCmd(app),
		newQuerySessionCmd(app),
		newQueryLifecycleCmd(app),
		newQuerySurfacesCmd(app),
	)

	return cmd
}

func newQueryAllCmd(app *App) *cobra.Command {
	var (
		sessionID string
		asJSON    bool
	)

	cmd := &cobra.Command{
		Use:   "all",
		Short: "Return combined .gaia context for users and agents",
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolvedSessionID, err := querypkg.ResolveSessionID(app.RepoRoot, sessionID)
			if err != nil {
				return err
			}

			sessions, err := querypkg.ListSessions(app.RepoRoot)
			if err != nil {
				return err
			}

			runtimeState, _, err := querypkg.ReadSessionState(app.RepoRoot, resolvedSessionID)
			if err != nil {
				return err
			}

			lifecycleState, _, lifecycleErr := querypkg.ReadLifecycleState(app.RepoRoot, resolvedSessionID)
			flowState, _, flowErr := querypkg.ReadFlowState(app.RepoRoot, resolvedSessionID)
			activePlanState, _, activePlanErr := querypkg.ReadActivePlanState(app.RepoRoot, resolvedSessionID)
			surfaceRegistry, surfaceErr := querypkg.ReadSurfaceRegistry(app.RepoRoot)

			payload := map[string]any{
				"session_id": resolvedSessionID,
				"sessions":   sessions,
				"runtime":    runtimeState,
			}

			if lifecycleErr == nil {
				payload["lifecycle"] = lifecycleState
			} else {
				payload["lifecycle_error"] = lifecycleErr.Error()
			}

			if flowErr == nil {
				payload["flow"] = flowState
			} else {
				payload["flow_error"] = flowErr.Error()
			}

			if activePlanErr == nil {
				payload["active_plan"] = activePlanState
			} else {
				payload["active_plan_error"] = activePlanErr.Error()
			}

			if surfaceErr == nil {
				payload["surfaces"] = surfaceRegistry
			} else {
				payload["surfaces_error"] = surfaceErr.Error()
			}

			if asJSON {
				return printJSON(cmd, payload)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Session: %s\n", resolvedSessionID)
			encoded, err := json.MarshalIndent(payload, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session id (default: latest)")
	cmd.Flags().BoolVar(&asJSON, "json", true, "output machine-readable JSON")
	return cmd
}

func newQuerySessionsCmd(app *App) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List runtime sessions from .gaia/runtime",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sessions, err := querypkg.ListSessions(app.RepoRoot)
			if err != nil {
				return err
			}

			if asJSON {
				return printJSON(cmd, sessions)
			}

			if len(sessions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No runtime sessions found")
				return nil
			}

			for _, session := range sessions {
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"%s\tstream=%s\tactive=%d\tcompleted=%d\tblocked=%d\tupdated=%s\n",
					session.SessionID,
					session.CurrentStream,
					session.ActiveCount,
					session.CompletedCount,
					session.BlockedCount,
					session.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
				)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "output machine-readable JSON")
	return cmd
}

func newQuerySessionCmd(app *App) *cobra.Command {
	var (
		sessionID string
		asJSON    bool
	)

	cmd := &cobra.Command{
		Use:   "session",
		Short: "Show one runtime session state from .gaia/runtime/<session>/state.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			state, resolvedSessionID, err := querypkg.ReadSessionState(app.RepoRoot, sessionID)
			if err != nil {
				return err
			}

			activePlan, _, activePlanErr := querypkg.ReadActivePlanState(app.RepoRoot, resolvedSessionID)

			if asJSON {
				payload := map[string]any{
					"session_id": resolvedSessionID,
					"state":      state,
				}

				if activePlanErr == nil {
					payload["active_plan"] = activePlan
				}

				return printJSON(cmd, payload)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Session: %s\n", resolvedSessionID)
			encoded, err := json.MarshalIndent(state, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session id (default: latest)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output machine-readable JSON")
	return cmd
}

func newQueryLifecycleCmd(app *App) *cobra.Command {
	var (
		sessionID string
		asJSON    bool
	)

	cmd := &cobra.Command{
		Use:   "lifecycle",
		Short: "Show lifecycle state from .gaia/lifecycle/<session>.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			state, resolvedSessionID, err := querypkg.ReadLifecycleState(app.RepoRoot, sessionID)
			if err != nil {
				return err
			}

			if asJSON {
				return printJSON(cmd, map[string]any{
					"session_id": resolvedSessionID,
					"lifecycle":  state,
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Session: %s\n", resolvedSessionID)
			encoded, err := json.MarshalIndent(state, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session id (default: latest runtime session)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output machine-readable JSON")
	return cmd
}

func newQuerySurfacesCmd(app *App) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "surfaces",
		Short: "Show surface compatibility registry from .gaia/surfaces/registry.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			registry, err := querypkg.ReadSurfaceRegistry(app.RepoRoot)
			if err != nil {
				return err
			}

			if asJSON {
				return printJSON(cmd, registry)
			}

			encoded, err := json.MarshalIndent(registry, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "output machine-readable JSON")
	return cmd
}

func printJSON(cmd *cobra.Command, payload any) error {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}

	fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
	return nil
}
