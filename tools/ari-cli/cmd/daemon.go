package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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
			cfg, configPath, configSource, err := configuredDaemonConfigWithSource()
			if err != nil {
				return err
			}

			runningDaemon := daemon.New(cfg.Daemon.SocketPath, cfg.Daemon.DBPath, configPath, configSource, "")

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
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Database: %s (%s)\n", response.DatabasePath, response.DatabaseState); err != nil {
				return err
			}
			configPath := response.ConfigPath
			if configPath == "" {
				configPath = "(none)"
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Config: %s\n", configPath); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Config Source: %s\n", response.ConfigSource); err != nil {
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
	cfg, _, _, err := configuredDaemonConfigWithSource()
	return cfg, err
}

func configuredDaemonConfigWithSource() (*config.Config, string, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", "", err
	}

	if cfg == nil {
		return nil, "", "", fmt.Errorf("config is required")
	}

	configPath, err := daemonConfigPath()
	if err != nil {
		return nil, "", "", err
	}

	if os.Getenv("ARI_DAEMON_SOCKET_PATH") != "" || os.Getenv("ARI_DAEMON_DB_PATH") != "" {
		if _, err := os.Stat(configPath); err == nil {
			return cfg, configPath, "environment", nil
		}
		return cfg, "", "environment", nil
	}

	if _, err := os.Stat(configPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, "defaults", "defaults", nil
		}
		return nil, "", "", fmt.Errorf("stat config path: %w", err)
	}

	return cfg, configPath, "file", nil
}

func daemonConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".ari", "config.json"), nil
}
