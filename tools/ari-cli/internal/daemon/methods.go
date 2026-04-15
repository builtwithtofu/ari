package daemon

import (
	"context"
	"fmt"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type StatusRequest struct{}

type StatusResponse struct {
	Version       string `json:"version"`
	PID           int    `json:"pid"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	SocketPath    string `json:"socket_path"`
	DatabasePath  string `json:"database_path"`
	DatabaseState string `json:"database_state"`
	ConfigPath    string `json:"config_path"`
	ConfigSource  string `json:"config_source"`
}

type StopRequest struct{}

type StopResponse struct {
	Status string `json:"status"`
}

func (d *Daemon) registerMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
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

	if err := d.registerWorkspaceMethods(registry, store); err != nil {
		return err
	}

	if err := d.registerWorkspaceProjectionMethods(registry, store); err != nil {
		return err
	}

	if err := d.registerWorkspaceTimelineMethods(registry, store); err != nil {
		return err
	}

	if err := d.registerContextMethods(registry, store); err != nil {
		return err
	}

	if err := d.registerCommandMethods(registry, store); err != nil {
		return err
	}

	if err := d.registerAgentMethods(registry, store); err != nil {
		return err
	}

	if err := d.registerExecutorMethods(registry, store); err != nil {
		return err
	}

	if err := d.registerAttachMethods(registry, store); err != nil {
		return err
	}

	return nil
}
