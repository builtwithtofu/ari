package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

var runClaudeSessionCommand = func(ctx context.Context, cwd string, args []string) ([]byte, error) {
	executable := claudeCommandName()
	path, err := exec.LookPath(executable)
	if err != nil {
		return nil, &HarnessUnavailableError{Harness: HarnessNameClaude, Reason: "missing_executable", Executable: executable, Probe: executable + " --version", RequiredCapability: HarnessCapabilityTimelineItems, StartInvoked: false}
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = strings.TrimSpace(cwd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("run claude %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func claudeSessionLogs(ctx context.Context, store *globaldb.Store, req ClaudeSessionLogsRequest) (ClaudeSessionLogsResponse, error) {
	session, providerID, err := claudeBackgroundSessionRef(ctx, store, req.SessionID)
	if err != nil {
		return ClaudeSessionLogsResponse{}, err
	}
	command := claudeLogsCommand(providerID)
	output, err := runClaudeSessionCommand(ctx, "", command[1:])
	if err != nil {
		return ClaudeSessionLogsResponse{}, err
	}
	return ClaudeSessionLogsResponse{SessionID: session.SessionID, ProviderSessionID: providerID, Command: command, Output: strings.TrimSpace(string(output))}, nil
}

func claudeSessionAttach(ctx context.Context, store *globaldb.Store, req ClaudeSessionAttachRequest) (ClaudeSessionAttachResponse, error) {
	session, providerID, err := claudeBackgroundSessionRef(ctx, store, req.SessionID)
	if err != nil {
		return ClaudeSessionAttachResponse{}, err
	}
	return ClaudeSessionAttachResponse{SessionID: session.SessionID, ProviderSessionID: providerID, Command: claudeAttachCommand(providerID)}, nil
}

func claudeBackgroundSessionRef(ctx context.Context, store *globaldb.Store, sessionID string) (globaldb.AgentSession, string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return globaldb.AgentSession{}, "", rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", map[string]any{"reason": "missing_session_id"})
	}
	session, err := store.GetAgentSession(ctx, sessionID)
	if err != nil {
		return globaldb.AgentSession{}, "", mapWorkspaceStoreError(err, sessionID)
	}
	if strings.TrimSpace(session.Harness) != HarnessNameClaude {
		return globaldb.AgentSession{}, "", rpc.NewHandlerError(rpc.InvalidParams, "session is not a Claude session", map[string]any{"reason": "not_claude_session", "session_id": sessionID})
	}
	invocationMode, _ := agentSessionModeFromProviderMetadata(session.ProviderMetadataJSON)
	if invocationMode != "" && invocationMode != string(HarnessInvocationModeBackground) {
		return globaldb.AgentSession{}, "", rpc.NewHandlerError(rpc.InvalidParams, "session is not a Claude background session", map[string]any{"reason": "not_claude_background_session", "session_id": sessionID})
	}
	providerID := strings.TrimSpace(session.ProviderSessionID)
	if providerID == "" {
		providerID = strings.TrimSpace(session.ProviderRunID)
	}
	if providerID == "" {
		return globaldb.AgentSession{}, "", rpc.NewHandlerError(rpc.InvalidParams, "Claude provider session id is missing", map[string]any{"reason": "missing_provider_session_id", "session_id": sessionID})
	}
	return session, providerID, nil
}

func claudeLogsCommand(providerSessionID string) []string {
	return []string{claudeCommandName(), "logs", strings.TrimSpace(providerSessionID)}
}

func claudeAttachCommand(providerSessionID string) []string {
	return []string{claudeCommandName(), "attach", strings.TrimSpace(providerSessionID)}
}

func claudeCommandName() string {
	executable := harnessExecutable("claude", EnvClaudeExecutable)
	if strings.TrimSpace(executable) == "" {
		return "claude"
	}
	return executable
}
