package cmd

import (
	"context"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/sourcegraph/jsonrpc2"
)

func TestResolveAgentSelectorPrefersExactIDOrNameOverNumericIndex(t *testing.T) {
	originalGet := agentGetRPC
	originalList := agentListRPC
	agentGetRPC = func(context.Context, string, string, string) (daemon.AgentGetResponse, error) {
		return daemon.AgentGetResponse{AgentID: "agt-name-1", WorkspaceID: "ws-1", Status: "running"}, nil
	}
	listCalled := false
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		listCalled = true
		return daemon.AgentListResponse{}, nil
	}
	t.Cleanup(func() {
		agentGetRPC = originalGet
		agentListRPC = originalList
	})

	agentID, err := resolveAgentSelector(context.Background(), "/tmp/daemon.sock", "ws-1", "1")
	if err != nil {
		t.Fatalf("resolveAgentSelector returned error: %v", err)
	}
	if agentID != "agt-name-1" {
		t.Fatalf("agent id = %q, want %q", agentID, "agt-name-1")
	}
	if listCalled {
		t.Fatal("agent list called unexpectedly")
	}
}

func TestResolveAgentSelectorFallsBackToIndexWhenNoExactMatch(t *testing.T) {
	originalGet := agentGetRPC
	originalList := agentListRPC
	agentGetRPC = func(context.Context, string, string, string) (daemon.AgentGetResponse, error) {
		return daemon.AgentGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.AgentNotFound), Message: "agent not found"}
	}
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		return daemon.AgentListResponse{Agents: []daemon.AgentSummary{
			{AgentID: "agt-0", Status: "running"},
			{AgentID: "agt-1", Status: "running"},
		}}, nil
	}
	t.Cleanup(func() {
		agentGetRPC = originalGet
		agentListRPC = originalList
	})

	agentID, err := resolveAgentSelector(context.Background(), "/tmp/daemon.sock", "ws-1", "1")
	if err != nil {
		t.Fatalf("resolveAgentSelector returned error: %v", err)
	}
	if agentID != "agt-1" {
		t.Fatalf("agent id = %q, want %q", agentID, "agt-1")
	}
}

func TestResolveAgentSelectorReturnsNotFoundForUnknownNamedSelector(t *testing.T) {
	originalGet := agentGetRPC
	originalList := agentListRPC
	agentGetRPC = func(context.Context, string, string, string) (daemon.AgentGetResponse, error) {
		return daemon.AgentGetResponse{}, &jsonrpc2.Error{Code: int64(rpc.AgentNotFound), Message: "agent not found"}
	}
	listCalled := false
	agentListRPC = func(context.Context, string, string) (daemon.AgentListResponse, error) {
		listCalled = true
		return daemon.AgentListResponse{}, nil
	}
	t.Cleanup(func() {
		agentGetRPC = originalGet
		agentListRPC = originalList
	})

	_, err := resolveAgentSelector(context.Background(), "/tmp/daemon.sock", "ws-1", "missing-agent")
	if err == nil {
		t.Fatal("resolveAgentSelector returned nil error")
	}
	if err.Error() != "Agent not found" {
		t.Fatalf("resolveAgentSelector error = %q, want %q", err.Error(), "Agent not found")
	}
	if listCalled {
		t.Fatal("agent list called unexpectedly for named selector")
	}
}
