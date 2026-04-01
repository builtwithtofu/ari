package daemon

import (
	"context"
	"fmt"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type StatusRequest struct{}

type StatusResponse struct {
	Version       string `json:"version"`
	PID           int    `json:"pid"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	SocketPath    string `json:"socket_path"`
}

type StopRequest struct{}

type StopResponse struct {
	Status string `json:"status"`
}

func (d *Daemon) registerMethods(registry *rpc.MethodRegistry) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[StatusRequest, StatusResponse]{
		Name:        "daemon.status",
		Description: "Report daemon status",
		Handler: func(ctx context.Context, req StatusRequest) (StatusResponse, error) {
			_ = ctx
			_ = req
			return d.status(), nil
		},
	}); err != nil {
		return fmt.Errorf("register daemon.status: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[StopRequest, StopResponse]{
		Name:        "daemon.stop",
		Description: "Stop running daemon",
		Handler: func(ctx context.Context, req StopRequest) (StopResponse, error) {
			_ = ctx
			_ = req
			d.Stop()
			return StopResponse{Status: "stopping"}, nil
		},
	}); err != nil {
		return fmt.Errorf("register daemon.stop: %w", err)
	}

	return nil
}
