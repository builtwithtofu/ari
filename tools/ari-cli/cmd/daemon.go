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

var (
	daemonPIDCheck = daemon.CheckPIDFile
	daemonStopRPC  = func(ctx context.Context, socketPath string) error {
		rpcClient := client.New(socketPath)
		var response daemon.StopResponse
		return rpcClient.Call(ctx, "daemon.stop", daemon.StopRequest{}, &response)
	}
	daemonStatusRPC = func(ctx context.Context, socketPath string) (daemon.StatusResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.StatusResponse
		if err := rpcClient.Call(ctx, "daemon.status", daemon.StatusRequest{}, &response); err != nil {
			return daemon.StatusResponse{}, err
		}
		return response, nil
	}
	daemonSignalProcess = func(pid int, sig syscall.Signal) error {
		return syscall.Kill(pid, sig)
	}
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
			pidPath := cfg.Daemon.PIDPath

			existingPID, running, err := checkRunningDaemon(cmd.Context(), cfg.Daemon.SocketPath, pidPath)
			if err != nil {
				return err
			}
			if running {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Daemon is already running (PID %d).\nHint: Run `ari daemon status` or `ari daemon stop`.\n", existingPID); err != nil {
					return err
				}
				return nil
			}

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(sigCh)

			runningDaemon := daemon.NewWithSignalChannel(cfg.Daemon.SocketPath, cfg.Daemon.DBPath, pidPath, configPath, configSource, "", sigCh)

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Ari daemon starting (PID %d, socket %s)\n", os.Getpid(), cfg.Daemon.SocketPath); err != nil {
				return err
			}

			return runningDaemon.Start(cmd.Context())
		},
	}
}

func checkRunningDaemon(ctx context.Context, socketPath, pidPath string) (int, bool, error) {
	existingPID, running, err := daemonPIDCheck(pidPath)
	if err != nil {
		return 0, false, err
	}
	if !running {
		return 0, false, nil
	}

	statusCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()

	status, err := daemonStatusRPC(statusCtx, socketPath)
	if err == nil {
		if status.PID == existingPID {
			return existingPID, true, nil
		}
		// PID file is stale but the socket proves a daemon is serving.
		if removeErr := daemon.RemovePIDFile(pidPath); removeErr != nil {
			return 0, false, removeErr
		}
		return status.PID, true, nil
	}

	if isDaemonUnavailable(err) {
		return existingPID, true, nil
	}

	if isTimeoutError(err) {
		return existingPID, true, nil
	}

	if isPermissionDenied(err) {
		return 0, false, socketPermissionError(socketPath)
	}

	return 0, false, err
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

			stopCtx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			if err := daemonStopRPC(stopCtx, cfg.Daemon.SocketPath); err != nil {
				if isPermissionDenied(err) {
					return socketPermissionError(cfg.Daemon.SocketPath)
				}

				if isTimeoutError(err) {
					stoppedBySignal, fallbackErr := fallbackStopByPID()
					if fallbackErr != nil {
						return timeoutError()
					}
					if !stoppedBySignal {
						return timeoutError()
					}

					if _, outErr := fmt.Fprintln(cmd.OutOrStdout(), "Daemon stopping"); outErr != nil {
						return outErr
					}
					return nil
				}

				stoppedBySignal, fallbackErr := fallbackStopByPID()
				if fallbackErr != nil {
					if isDaemonUnavailable(fallbackErr) {
						if _, outErr := fmt.Fprintln(cmd.OutOrStdout(), notRunningMessage()); outErr != nil {
							return outErr
						}
						return nil
					}
					return fallbackErr
				}
				if !stoppedBySignal {
					if _, outErr := fmt.Fprintln(cmd.OutOrStdout(), notRunningMessage()); outErr != nil {
						return outErr
					}
					return nil
				}

				if _, outErr := fmt.Fprintln(cmd.OutOrStdout(), "Daemon stopping"); outErr != nil {
					return outErr
				}
				return nil
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Daemon stopping"); err != nil {
				return err
			}
			return nil
		},
	}
}

func fallbackStopByPID() (bool, error) {
	cfg, err := configuredDaemonConfig()
	if err != nil {
		return false, err
	}

	pidPath := cfg.Daemon.PIDPath
	pid, running, err := daemonPIDCheck(pidPath)
	if err != nil {
		return false, err
	}
	if !running {
		return false, nil
	}

	statusCtx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()

	status, err := daemonStatusRPC(statusCtx, cfg.Daemon.SocketPath)
	if err != nil {
		if isDaemonUnavailable(err) {
			if removeErr := daemon.RemovePIDFile(pidPath); removeErr != nil {
				return false, removeErr
			}
			return false, nil
		}
		if isTimeoutError(err) {
			// Keep fallback behavior for unresponsive daemon.
		} else {
			return false, err
		}
	}

	if err == nil && status.PID != pid {
		if removeErr := daemon.RemovePIDFile(pidPath); removeErr != nil {
			return false, removeErr
		}
		return false, nil
	}

	if err := daemonSignalProcess(pid, syscall.SIGTERM); err != nil {
		return false, err
	}

	return true, nil
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

			statusCtx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			response, err := daemonStatusRPC(statusCtx, cfg.Daemon.SocketPath)
			if err != nil {
				if isDaemonUnavailable(err) {
					if _, outErr := fmt.Fprintln(cmd.OutOrStdout(), notRunningMessage()); outErr != nil {
						return outErr
					}
					return nil
				}
				if isPermissionDenied(err) {
					return socketPermissionError(cfg.Daemon.SocketPath)
				}
				if isTimeoutError(err) {
					return timeoutError()
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

func isPermissionDenied(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EACCES) {
		return true
	}

	return strings.Contains(strings.ToLower(err.Error()), "permission denied")
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	return strings.Contains(strings.ToLower(err.Error()), "timeout")
}

func notRunningMessage() string {
	return "Daemon is not running.\nHint: Start it with `ari daemon start`."
}

func socketPermissionError(socketPath string) error {
	return userFacingError{message: fmt.Sprintf("Permission denied: %s.\nHint: Check socket file permissions and ownership.", socketPath)}
}

func timeoutError() error {
	return userFacingError{message: "Daemon did not respond (timeout).\nHint: Try `ari daemon stop` or check the process."}
}

type userFacingError struct {
	message string
}

func (e userFacingError) Error() string {
	return e.message
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

	if os.Getenv("ARI_DAEMON_SOCKET_PATH") != "" || os.Getenv("ARI_DAEMON_DB_PATH") != "" || os.Getenv("ARI_DAEMON_PID_PATH") != "" {
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
