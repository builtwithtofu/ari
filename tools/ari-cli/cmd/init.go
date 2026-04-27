package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

type initPromptOutput interface {
	InOrStdin() io.Reader
	OutOrStdout() io.Writer
}

var (
	initConfiguredDaemonConfig = configuredDaemonConfig
	initEnsureDaemonRunning    = ensureDaemonRunning
	initOptionsRPC             = func(ctx context.Context, socketPath string) (daemon.InitOptionsResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.InitOptionsResponse
		if err := rpcClient.Call(ctx, "init.options", daemon.InitOptionsRequest{}, &response); err != nil {
			return daemon.InitOptionsResponse{}, err
		}
		return response, nil
	}
	initApplyRPC = func(ctx context.Context, socketPath string, req daemon.InitApplyRequest) (daemon.InitApplyResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.InitApplyResponse
		if err := rpcClient.Call(ctx, "init.apply", req, &response); err != nil {
			return daemon.InitApplyResponse{}, err
		}
		return response, nil
	}
	initPromptHarness = promptInitHarness
)

func NewInitCmd() *cobra.Command {
	var harness string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Ari onboarding defaults",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			cfg, err := initConfiguredDaemonConfig()
			if err != nil {
				return err
			}
			if err := initEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			selected := strings.TrimSpace(harness)
			if selected == "" {
				options, err := initOptionsRPC(ctx, cfg.Daemon.SocketPath)
				if err != nil {
					return err
				}
				selected, err = initPromptHarness(cmd, options.Harnesses)
				if err != nil {
					return err
				}
			}

			response, err := initApplyRPC(ctx, cfg.Daemon.SocketPath, daemon.InitApplyRequest{Harness: selected})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Default harness set: %s\n", response.DefaultHarness); err != nil {
				return err
			}
			if response.SystemWorkspaceReady {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "System workspace ready: system"); err != nil {
					return err
				}
			}
			if response.SystemHelperReady {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "System helper ready: helper"); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&harness, "harness", "", "Default Ari harness")
	return cmd
}

func promptInitHarness(cmd initPromptOutput, options []daemon.InitHarnessOption) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no harness options available")
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Choose your default harness:"); err != nil {
		return "", err
	}
	for index, option := range options {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s\n", index+1, option.Label); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), "Harness: "); err != nil {
		return "", err
	}
	var choice int
	if _, err := fmt.Fscan(cmd.InOrStdin(), &choice); err != nil {
		return "", fmt.Errorf("read harness choice: %w", err)
	}
	if choice < 1 || choice > len(options) {
		return "", fmt.Errorf("harness choice must be between 1 and %d", len(options))
	}
	return options[choice-1].Name, nil
}
