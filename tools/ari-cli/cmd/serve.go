package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/spf13/cobra"
)

func NewServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start Ari JSON-RPC server",
		Long:  "Start the Ari server for headless operation via JSON-RPC",
		RunE: func(cmd *cobra.Command, args []string) error {
			stdio, _ := cmd.Flags().GetBool("stdio")
			if !stdio {
				return fmt.Errorf("--stdio is required (HTTP mode not yet implemented)")
			}

			return runStdioServer(cmd.Context())
		},
	}

	cmd.Flags().Bool("stdio", false, "Run server over stdin/stdout")

	return cmd
}

func runStdioServer(ctx context.Context) error {
	registry := rpc.NewMethodRegistry()
	server := rpc.NewServer(registry)
	transport := rpc.NewStdioTransport(server)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	return transport.Run(ctx)
}
