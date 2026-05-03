package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	telemetryEnsureDaemonRunning = ensureDaemonRunning
	telemetryRollupRPC           = func(ctx context.Context, socketPath string, req daemon.TelemetryRollupRequest) (daemon.TelemetryRollupResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.TelemetryRollupResponse
		if err := rpcClient.Call(ctx, "telemetry.rollup", req, &response); err != nil {
			return daemon.TelemetryRollupResponse{}, err
		}
		return response, nil
	}
)

func NewTelemetryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "telemetry", Short: "Read local agent telemetry", Hidden: true}
	cmd.AddCommand(newTelemetryRollupCmd())
	return cmd
}

func newTelemetryRollupCmd() *cobra.Command {
	var workspaceID string
	cmd := &cobra.Command{
		Use:   "rollup --workspace-id <workspace-id>",
		Short: "Roll up agent run telemetry for a workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			if workspaceID == "" {
				return userFacingError{message: "Provide --workspace-id"}
			}
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := telemetryEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			resp, err := telemetryRollupRPC(ctx, cfg.Daemon.SocketPath, daemon.TelemetryRollupRequest{WorkspaceID: workspaceID})
			if err != nil {
				return err
			}
			for _, rollup := range resp.Rollups {
				if err := printTelemetryRollup(cmd, rollup); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace-id", "", "Workspace id to roll up telemetry for")
	return cmd
}

func printTelemetryRollup(cmd *cobra.Command, rollup daemon.TelemetryRollup) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "telemetry\tprofile=%s\tharness=%s\tmodel=%s\tinvocation_class=%s\truns=%d\tcompleted=%d\tfailed=%d\n", rollup.Group.Profile, rollup.Group.Harness, rollup.Group.Model, rollup.Group.InvocationClass, rollup.Runs, rollup.Completed, rollup.Failed); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "input_tokens=%s\toutput_tokens=%s\testimated_cost=%s\tduration_ms=%s\texit_code=%s\n", formatTelemetryKnownInt64(rollup.InputTokens), formatTelemetryKnownInt64(rollup.OutputTokens), formatTelemetryKnownInt64(rollup.EstimatedCost), formatTelemetryKnownInt64(rollup.DurationMS), formatTelemetryKnownInt64(rollup.Process.ExitCode)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "process_owned=%t\tpid=%s\tcpu_time_ms=%s\tmemory_rss_bytes_peak=%s\tchild_processes_peak=%s\n", rollup.Process.OwnedByAri, formatTelemetryKnownInt64(rollup.Process.PID), formatTelemetryKnownInt64(rollup.Process.CPUTimeMS), formatTelemetryKnownInt64(rollup.Process.MemoryRSSBytesPeak), formatTelemetryKnownInt64(rollup.Process.ChildProcessesPeak)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "orphan_state=%s\tports=%s\n", rollup.Process.OrphanState, formatTelemetryPorts(rollup.Process.Ports)); err != nil {
		return err
	}
	return nil
}

func formatTelemetryKnownInt64(value daemon.TelemetryKnownInt64) string {
	if !value.Known || value.Value == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", *value.Value)
}

func formatTelemetryPorts(ports []daemon.ProcessPortObservation) string {
	if len(ports) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(ports))
	for _, port := range ports {
		parts = append(parts, fmt.Sprintf("%s/%d/%s", port.Protocol, port.Port, port.Confidence))
	}
	return strings.Join(parts, ",")
}
