package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/spf13/cobra"
)

type apiRunDeps struct {
	configuredDaemonConfig func() (*config.Config, error)
	ensureDaemonRunning    func(context.Context, *config.Config) error
	call                   func(context.Context, string, string, json.RawMessage) (json.RawMessage, error)
}

var apiDeps = apiRunDeps{
	configuredDaemonConfig: configuredDaemonConfig,
	ensureDaemonRunning:    ensureDaemonRunning,
	call: func(ctx context.Context, socketPath, method string, params json.RawMessage) (json.RawMessage, error) {
		rpcClient := client.New(socketPath)
		var response json.RawMessage
		if err := rpcClient.Call(ctx, method, json.RawMessage(params), &response); err != nil {
			return nil, err
		}
		return response, nil
	},
}

func NewAPICmd() *cobra.Command {
	var params string
	cmd := &cobra.Command{
		Use:   "api <method>",
		Short: "Call Ari daemon JSON-RPC methods",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			method := strings.TrimSpace(args[0])
			if method == "" {
				return fmt.Errorf("method is required")
			}
			rawParams := json.RawMessage(`{}`)
			if strings.TrimSpace(params) != "" {
				rawParams = json.RawMessage(params)
			}
			if !json.Valid(rawParams) {
				return fmt.Errorf("--params must be valid JSON")
			}
			cfg, err := apiDeps.configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := apiDeps.ensureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			response, err := apiDeps.call(ctx, cfg.Daemon.SocketPath, method, rawParams)
			if err != nil {
				return err
			}
			pretty, err := json.MarshalIndent(response, "", "  ")
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(pretty))
			return err
		},
	}
	cmd.Flags().StringVar(&params, "params", "", "JSON params object to send")
	return cmd
}
