package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
)

func TestAPICallsDaemonMethodWithJSONParams(t *testing.T) {
	originalDeps := apiDeps
	t.Cleanup(func() { apiDeps = originalDeps })

	apiDeps.configuredDaemonConfig = func() (*config.Config, error) {
		return &config.Config{Daemon: config.DaemonConfig{SocketPath: "/tmp/daemon.sock"}}, nil
	}
	apiDeps.ensureDaemonRunning = func(ctx context.Context, cfg *config.Config) error {
		_ = ctx
		_ = cfg
		return nil
	}
	apiDeps.call = func(ctx context.Context, socketPath, method string, params json.RawMessage) (json.RawMessage, error) {
		_ = ctx
		if socketPath != "/tmp/daemon.sock" {
			t.Fatalf("socket path = %q, want configured socket", socketPath)
		}
		if method != "workspace.list" {
			t.Fatalf("method = %q, want workspace.list", method)
		}
		if string(params) != `{"include_closed":false}` {
			t.Fatalf("params = %s, want include_closed false", params)
		}
		return json.RawMessage(`{"workspaces":[{"workspace_id":"ws-1","name":"alpha"}]}`), nil
	}

	out, err := executeRootCommandRaw("api", "workspace.list", "--params", `{"include_closed":false}`)
	if err != nil {
		t.Fatalf("execute api returned error: %v", err)
	}
	if !strings.Contains(out, `"workspace_id": "ws-1"`) || !strings.Contains(out, `"name": "alpha"`) {
		t.Fatalf("api output = %q, want pretty JSON response", out)
	}
}
