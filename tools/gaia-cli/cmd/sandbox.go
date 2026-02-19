package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/adavies/opencode-gaia/tools/gaia-cli/internal/sandbox"
	"github.com/spf13/cobra"
)

func newSandboxCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Navigate and launch GAIA sandbox contexts",
	}

	cmd.AddCommand(
		newSandboxListCmd(app),
		newSandboxTuiCmd(app),
		newSandboxWebCmd(app),
	)
	return cmd
}

func newSandboxListCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List seeded sandbox workspaces",
		RunE: func(cmd *cobra.Command, _ []string) error {
			workspaces, err := sandbox.ListWorkspaces(app.RepoRoot)
			if err != nil {
				return err
			}

			if len(workspaces) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No sandbox workspaces found")
				fmt.Fprintln(cmd.OutOrStdout(), "Next command: gaia sandbox tui \"feature-x\"")
				return nil
			}

			for _, item := range workspaces {
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"%s\t%s\t%s\n",
					item.Name,
					item.Updated.Format(time.RFC3339),
					item.Path,
				)
			}

			return nil
		},
	}
}

func newSandboxTuiCmd(app *App) *cobra.Command {
	var model string

	cmd := &cobra.Command{
		Use:   "tui [label]",
		Short: "Launch sandboxed OpenCode TUI with seeded scenarios",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			label := ""
			if len(args) > 0 {
				label = args[0]
			}

			harnessArgs := []string{"manual-tui"}
			if label != "" {
				harnessArgs = append(harnessArgs, label)
			}
			if model != "" {
				harnessArgs = append(harnessArgs, "--model", model)
			}

			return sandbox.RunHarness(app.RepoRoot, harnessArgs)
		},
	}

	cmd.Flags().StringVar(&model, "model", "", "model override for top-level and GAIA subagents")
	return cmd
}

func newSandboxWebCmd(app *App) *cobra.Command {
	var (
		model string
		port  int
	)

	cmd := &cobra.Command{
		Use:   "web [label]",
		Short: "Launch sandboxed OpenCode serve mode with seeded scenarios",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			label := ""
			if len(args) > 0 {
				label = args[0]
			}

			if port <= 0 {
				return errors.New("--port must be positive")
			}

			harnessArgs := []string{"manual-web"}
			if label != "" {
				harnessArgs = append(harnessArgs, label)
			}
			if model != "" {
				harnessArgs = append(harnessArgs, "--model", model)
			}

			harnessArgs = append(harnessArgs, "--port", fmt.Sprintf("%d", port))
			return sandbox.RunHarness(app.RepoRoot, harnessArgs)
		},
	}

	cmd.Flags().StringVar(&model, "model", "", "model override for top-level and GAIA subagents")
	cmd.Flags().IntVar(&port, "port", 4096, "serve port")
	return cmd
}
