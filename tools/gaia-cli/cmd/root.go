package cmd

import (
	"fmt"
	"os"

	"github.com/adavies/opencode-gaia/tools/gaia-cli/internal/reporoot"
	"github.com/spf13/cobra"
)

type App struct {
	RepoRoot string
}

func NewRootCmd() *cobra.Command {
	app := &App{}

	cmd := &cobra.Command{
		Use:     "ari",
		Aliases: []string{"gaia", "ariadne"},
		Short:   "Ariadne protocol CLI for Project GAIA state",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := reporoot.Resolve(app.RepoRoot)
			if err != nil {
				return err
			}

			app.RepoRoot = resolved
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&app.RepoRoot, "repo-root", "", "repository root path")

	cmd.AddCommand(
		newFlowCmd(app),
		newQueryCmd(app),
		newStatusCmd(app),
		newSandboxCmd(app),
		newPlanCmd(app),
		newWorkCmd(app),
	)

	return cmd
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
