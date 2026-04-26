package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

func resolveAgentSelector(ctx context.Context, socketPath, workspaceID, selector string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("agent selector: context is required")
	}
	if strings.TrimSpace(socketPath) == "" {
		return "", fmt.Errorf("agent selector: socket path is required")
	}
	if strings.TrimSpace(workspaceID) == "" {
		return "", userFacingError{message: "Workspace not found"}
	}

	selector = strings.TrimSpace(selector)
	if selector == "" {
		selector = "0"
	}

	index, err := strconv.Atoi(selector)
	if err != nil {
		if details, err := agentGetRPC(ctx, socketPath, workspaceID, selector); err == nil {
			if !strings.EqualFold(strings.TrimSpace(details.Status), "running") {
				return "", userFacingError{message: "Agent is not running"}
			}
			return strings.TrimSpace(details.AgentID), nil
		} else if !isAgentNotFoundRPCError(err) {
			return "", mapAgentRPCError(err)
		}
		return "", userFacingError{message: "Agent not found"}
	}
	if index < 0 {
		return "", userFacingError{message: "Agent index must be 0 or greater"}
	}

	list, err := agentListRPC(ctx, socketPath, workspaceID)
	if err != nil {
		return "", mapAgentRPCError(err)
	}

	running := make([]daemon.AgentSummary, 0, len(list.Agents))
	for _, item := range list.Agents {
		if strings.TrimSpace(item.AgentID) == "" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(item.Status), "running") {
			continue
		}
		running = append(running, item)
	}

	if len(running) == 0 {
		return "", userFacingError{message: "No running agents found; start one with `ari agent spawn`"}
	}
	if index >= len(running) {
		return "", userFacingError{message: fmt.Sprintf("Agent index %d is out of range (0-%d)", index, len(running)-1)}
	}

	return strings.TrimSpace(running[index].AgentID), nil
}
