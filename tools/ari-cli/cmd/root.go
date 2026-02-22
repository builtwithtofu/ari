package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "ari",
		Short: "Ari CLI baseline",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "ari reset baseline: command surface is not implemented yet")
		},
	}

	rootCmd.PersistentFlags().Bool("headless", false, "Emit JSON events to stdout for machine consumption")

	rootCmd.AddCommand(NewInitCmd())
	rootCmd.AddCommand(NewAskCmd())
	rootCmd.AddCommand(NewPlanCmd())
	rootCmd.AddCommand(NewBuildCmd())
	rootCmd.AddCommand(NewReviewCmd())
	rootCmd.AddCommand(NewServeCmd())

	return rootCmd
}

func isHeadless(cmd *cobra.Command) bool {
	headless, _ := cmd.Flags().GetBool("headless")
	return headless
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
