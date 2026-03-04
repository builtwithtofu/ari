package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

func NewDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage Ari daemon",
	}

	cmd.AddCommand(newDaemonStartCmd())
	cmd.AddCommand(newDaemonStopCmd())
	cmd.AddCommand(newDaemonStatusCmd())

	return cmd
}

func newDaemonStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start Ari daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}

			runningDaemon := daemon.New(cfg.Daemon.SocketPath, "")

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Ari daemon starting (PID %d, socket %s)\n", os.Getpid(), cfg.Daemon.SocketPath); err != nil {
				return err
			}

			runCtx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			return runningDaemon.Start(runCtx)
		},
	}
}

func newDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop Ari daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}

			rpcClient := client.New(cfg.Daemon.SocketPath)
			stopCtx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			var response daemon.StopResponse
			if err := rpcClient.Call(stopCtx, "daemon.stop", daemon.StopRequest{}, &response); err != nil {
				if isDaemonUnavailable(err) {
					if _, outErr := fmt.Fprintln(cmd.OutOrStdout(), "Daemon is not running"); outErr != nil {
						return outErr
					}
					return nil
				}
				return err
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Daemon stopping"); err != nil {
				return err
			}
			return nil
		},
	}
}

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Ari daemon status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}

			rpcClient := client.New(cfg.Daemon.SocketPath)
			statusCtx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			var response daemon.StatusResponse
			if err := rpcClient.Call(statusCtx, "daemon.status", daemon.StatusRequest{}, &response); err != nil {
				if isDaemonUnavailable(err) {
					if _, outErr := fmt.Fprintln(cmd.OutOrStdout(), "Daemon is not running"); outErr != nil {
						return outErr
					}
					return nil
				}
				return err
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Daemon: running"); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Version: %s\n", response.Version); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "PID: %d\n", response.PID); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Uptime: %ds\n", response.UptimeSeconds); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Socket: %s\n", response.SocketPath); err != nil {
				return err
			}

			return nil
		},
	}
}

func isDaemonUnavailable(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) {
		return true
	}

	text := strings.ToLower(err.Error())
	return strings.Contains(text, "no such file or directory") ||
		strings.Contains(text, "connection refused") ||
		strings.Contains(text, "connect: no such file")
}

func configuredDaemonConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	if cfg.Daemon.SocketPath == "" {
		return nil, fmt.Errorf("daemon socket path is required")
	}

	return cfg, nil
}
