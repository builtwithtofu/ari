package cmd

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
)

func TestWorkspaceMatchesWorkspaceUsesDaemonResolver(t *testing.T) {
	originalResolve := workspaceResolveRPC
	var gotReq daemon.WorkspaceResolveRequest
	workspaceResolveRPC = func(_ context.Context, socketPath string, req daemon.WorkspaceResolveRequest) (daemon.WorkspaceResolveResponse, error) {
		if socketPath != "/tmp/daemon.sock" {
			t.Fatalf("socket path = %q, want /tmp/daemon.sock", socketPath)
		}
		gotReq = req
		return daemon.WorkspaceResolveResponse{Workspace: daemon.WorkspaceGetResponse{WorkspaceID: "ws-1"}}, nil
	}
	t.Cleanup(func() { workspaceResolveRPC = originalResolve })

	matches, err := workspaceMatchesWorkspace(context.Background(), "/tmp/daemon.sock", "/workspace/repo", daemon.WorkspaceGetResponse{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("workspaceMatchesWorkspace returned error: %v", err)
	}
	if !matches {
		t.Fatal("workspaceMatchesWorkspace = false, want true")
	}
	if gotReq.Identifier != "" || gotReq.CWD != "/workspace/repo" {
		t.Fatalf("resolver request = %#v, want cwd-only request", gotReq)
	}
}

func TestWorkspaceMatchesWorkspaceUsesStructuredNoMatchReason(t *testing.T) {
	originalResolve := workspaceResolveRPC
	data := json.RawMessage(`{"reason":"no_match"}`)
	workspaceResolveRPC = func(context.Context, string, daemon.WorkspaceResolveRequest) (daemon.WorkspaceResolveResponse, error) {
		return daemon.WorkspaceResolveResponse{}, &jsonrpc2.Error{Code: int64(rpc.InvalidParams), Message: "resolver text changed", Data: &data}
	}
	t.Cleanup(func() { workspaceResolveRPC = originalResolve })

	matches, err := workspaceMatchesWorkspace(context.Background(), "/tmp/daemon.sock", "/tmp", daemon.WorkspaceGetResponse{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("workspaceMatchesWorkspace returned error: %v", err)
	}
	if matches {
		t.Fatal("workspaceMatchesWorkspace = true, want false for structured no_match")
	}
}

func TestResolveWorkspaceFromCWDMapsRPCInvalidParams(t *testing.T) {
	originalResolve := workspaceResolveRPC
	workspaceResolveRPC = func(context.Context, string, daemon.WorkspaceResolveRequest) (daemon.WorkspaceResolveResponse, error) {
		return daemon.WorkspaceResolveResponse{}, &jsonrpc2.Error{Code: int64(rpc.InvalidParams), Message: "bad workspace list"}
	}
	t.Cleanup(func() { workspaceResolveRPC = originalResolve })

	_, err := resolveWorkspaceFromCWD(context.Background(), "/tmp/daemon.sock", "/tmp")
	if err == nil {
		t.Fatal("resolveWorkspaceFromCWD returned nil error")
	}
	if err.Error() != "bad workspace list" {
		t.Fatalf("resolveWorkspaceFromCWD error = %q, want %q", err.Error(), "bad workspace list")
	}
}
