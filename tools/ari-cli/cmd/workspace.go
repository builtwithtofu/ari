package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/vcs"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/spf13/cobra"
)

var (
	workspaceEnsureDaemonRunning         = ensureDaemonRunning
	workspaceSwitchIsInteractiveTerminal = func(cmd *cobra.Command) bool {
		return isInteractiveTerminal(cmd)
	}
	workspaceCreateRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceCreateRequest) (daemon.WorkspaceCreateResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.WorkspaceCreateResponse
		if err := rpcClient.Call(ctx, "workspace.create", req, &response); err != nil {
			return daemon.WorkspaceCreateResponse{}, err
		}
		return response, nil
	}
	workspaceListRPC = func(ctx context.Context, socketPath string) (daemon.WorkspaceListResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.WorkspaceListResponse
		if err := rpcClient.Call(ctx, "workspace.list", daemon.WorkspaceListRequest{}, &response); err != nil {
			return daemon.WorkspaceListResponse{}, err
		}
		return response, nil
	}
	workspaceGetRPC = func(ctx context.Context, socketPath, sessionID string) (daemon.WorkspaceGetResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.WorkspaceGetResponse
		if err := rpcClient.Call(ctx, "workspace.get", daemon.WorkspaceGetRequest{WorkspaceID: sessionID}, &response); err != nil {
			return daemon.WorkspaceGetResponse{}, err
		}
		return response, nil
	}
	workspaceCloseRPC = func(ctx context.Context, socketPath, sessionID string) (daemon.WorkspaceCloseResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.WorkspaceCloseResponse
		if err := rpcClient.Call(ctx, "workspace.close", daemon.WorkspaceCloseRequest{WorkspaceID: sessionID}, &response); err != nil {
			return daemon.WorkspaceCloseResponse{}, err
		}
		return response, nil
	}
	workspaceSuspendRPC = func(ctx context.Context, socketPath, sessionID string) (daemon.WorkspaceSuspendResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.WorkspaceSuspendResponse
		if err := rpcClient.Call(ctx, "workspace.suspend", daemon.WorkspaceSuspendRequest{WorkspaceID: sessionID}, &response); err != nil {
			return daemon.WorkspaceSuspendResponse{}, err
		}
		return response, nil
	}
	workspaceResumeRPC = func(ctx context.Context, socketPath, sessionID string) (daemon.WorkspaceResumeResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.WorkspaceResumeResponse
		if err := rpcClient.Call(ctx, "workspace.resume", daemon.WorkspaceResumeRequest{WorkspaceID: sessionID}, &response); err != nil {
			return daemon.WorkspaceResumeResponse{}, err
		}
		return response, nil
	}
	workspaceAddFolderRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceAddFolderRequest) (daemon.WorkspaceAddFolderResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.WorkspaceAddFolderResponse
		if err := rpcClient.Call(ctx, "workspace.add_folder", req, &response); err != nil {
			return daemon.WorkspaceAddFolderResponse{}, err
		}
		return response, nil
	}
	workspaceRemoveFolderRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceRemoveFolderRequest) error {
		rpcClient := client.New(socketPath)
		var response daemon.WorkspaceRemoveFolderResponse
		return rpcClient.Call(ctx, "workspace.remove_folder", req, &response)
	}
)

func NewWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "workspace", Short: "Manage Ari workspaces"}
	cmd.AddCommand(newWorkspaceCreateCmd())
	cmd.AddCommand(newWorkspaceListCmd())
	cmd.AddCommand(newWorkspaceShowCmd())
	cmd.AddCommand(newWorkspaceCloseCmd())
	cmd.AddCommand(newWorkspaceSuspendCmd())
	cmd.AddCommand(newWorkspaceResumeCmd())
	cmd.AddCommand(newWorkspaceSetCmd())
	cmd.AddCommand(newWorkspaceCurrentCmd())
	cmd.AddCommand(newWorkspaceSwitchCmd())
	cmd.AddCommand(newWorkspaceClearCmd())
	cmd.AddCommand(newWorkspaceFolderCmd())
	cmd.AddCommand(newWorkspaceAttachCmd())
	return cmd
}

func newWorkspaceSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <id-or-name>",
		Short: "Set active workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			lookupCtx, lookupCancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer lookupCancel()

			sessionID, err := resolveSessionIdentifier(lookupCtx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}
			if err := writeAndReportActiveSession(cmd, sessionID); err != nil {
				return err
			}
			return resumeWorkspaceAgentConversation(cmd, cfg, sessionID)
		},
	}
}

func newWorkspaceCurrentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show active workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sessionID, err := config.ReadActiveWorkspace()
			if err != nil {
				return err
			}
			if strings.TrimSpace(sessionID) == "" {
				return userFacingError{message: "No active workspace is set"}
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Current workspace: %s\n", sessionID)
			return err
		},
	}
}

func newWorkspaceClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Clear active workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := config.WriteActiveWorkspace(""); err != nil {
				return err
			}
			if strings.TrimSpace(os.Getenv("ARI_ACTIVE_WORKSPACE")) != "" {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "Cleared persisted active workspace; ARI_ACTIVE_WORKSPACE still overrides it in this shell")
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Cleared active workspace")
			return err
		},
	}
}

func newWorkspaceSwitchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "switch",
		Short: "Switch active workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			if !workspaceSwitchIsInteractiveTerminal(cmd) {
				return userFacingError{message: "workspace switch requires an interactive terminal; use workspace set <id-or-name>"}
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			response, err := workspaceListRPC(ctx, cfg.Daemon.SocketPath)
			if err != nil {
				return mapSessionRPCError(err)
			}

			available := make([]daemon.WorkspaceSummary, 0, len(response.Workspaces))
			for _, session := range response.Workspaces {
				if strings.EqualFold(strings.TrimSpace(session.Status), "closed") {
					continue
				}
				available = append(available, session)
			}

			if len(available) == 0 {
				return userFacingError{message: "No open workspaces available; create one with `ari workspace create <name>`"}
			}

			selected := daemon.WorkspaceSummary{}
			if len(available) == 1 {
				selected = available[0]
			} else {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Select workspace:"); err != nil {
					return err
				}
				for index, session := range available {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %d) %s (%s)\n", index+1, session.Name, session.WorkspaceID); err != nil {
						return err
					}
				}
				if _, err := fmt.Fprint(cmd.OutOrStdout(), "Enter selection number: "); err != nil {
					return err
				}

				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil {
					return userFacingError{message: "Unable to read workspace selection"}
				}
				selection, err := strconv.Atoi(strings.TrimSpace(line))
				if err != nil || selection < 1 || selection > len(available) {
					return userFacingError{message: "Invalid workspace selection"}
				}
				selected = available[selection-1]
			}

			if err := writeAndReportActiveSession(cmd, selected.WorkspaceID); err != nil {
				return err
			}
			return resumeWorkspaceAgentConversation(cmd, cfg, selected.WorkspaceID)
		},
	}
}

func writeAndReportActiveSession(cmd *cobra.Command, sessionID string) error {
	if cmd == nil {
		return fmt.Errorf("active workspace write: command is required")
	}
	if strings.TrimSpace(sessionID) == "" {
		return userFacingError{message: "Workspace identifier is required"}
	}
	if err := config.WriteActiveWorkspace(sessionID); err != nil {
		return err
	}
	if strings.TrimSpace(os.Getenv("ARI_ACTIVE_WORKSPACE")) != "" {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Persisted active workspace set: %s; ARI_ACTIVE_WORKSPACE still overrides it in this shell\n", sessionID)
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Active workspace set: %s\n", sessionID)
	return err
}

func resumeWorkspaceAgentConversation(cmd *cobra.Command, cfg *config.Config, sessionID string) error {
	if cmd == nil {
		return fmt.Errorf("resume workspace agent: command is required")
	}
	if cfg == nil {
		return fmt.Errorf("resume workspace agent: config is required")
	}
	if sessionID = strings.TrimSpace(sessionID); sessionID == "" {
		return fmt.Errorf("resume workspace agent: workspace id is required")
	}

	lookupCtx, lookupCancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer lookupCancel()

	agents, err := agentListRPC(lookupCtx, cfg.Daemon.SocketPath, sessionID)
	if err != nil {
		_, writeErr := fmt.Fprintf(cmd.OutOrStdout(), "Warning: active workspace set but resume lookup failed: %v\n", mapAgentRPCError(err))
		return writeErr
	}

	for _, summary := range agents.Agents {
		getCtx, getCancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		details, err := agentGetRPC(getCtx, cfg.Daemon.SocketPath, sessionID, summary.AgentID)
		getCancel()
		if err != nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(details.Status), "running") {
			continue
		}
		harness := strings.TrimSpace(details.Harness)
		resumableID := strings.TrimSpace(details.HarnessResumableID)
		resumableFlag := strings.TrimSpace(daemon.ResumableFlagForHarness(harness))
		if harness == "" || resumableID == "" || resumableFlag == "" {
			continue
		}

		spawnCtx, spawnCancel := context.WithTimeout(cmd.Context(), 5*time.Second)
		spawnResp, err := agentSpawnRPC(spawnCtx, cfg.Daemon.SocketPath, daemon.AgentSpawnRequest{
			WorkspaceID: sessionID,
			Harness:     harness,
			Args:        []string{resumableFlag, resumableID},
		})
		spawnCancel()
		if err != nil {
			_, writeErr := fmt.Fprintf(cmd.OutOrStdout(), "Warning: active workspace set but resume failed: %v\n", mapAgentRPCError(err))
			return writeErr
		}

		_, writeErr := fmt.Fprintf(cmd.OutOrStdout(), "Resumed agent conversation: %s (%s)\n", spawnResp.AgentID, spawnResp.Status)
		return writeErr
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), "No resumable agent history found for workspace")
	return err
}

func newWorkspaceCreateCmd() *cobra.Command {
	var folder string
	var cleanup string
	var vcsPreference string
	var harness string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			if strings.TrimSpace(folder) == "" {
				folder = defaultSessionFolder(cwd)
			}
			folderPath, err := absolutizeInputPath(cwd, folder)
			if err != nil {
				return err
			}
			if strings.TrimSpace(cleanup) == "" {
				cleanup = "manual"
			}
			if strings.TrimSpace(vcsPreference) == "" {
				vcsPreference = cfg.VCSPreference
			}

			createCtx, createCancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer createCancel()

			response, err := workspaceCreateRPC(createCtx, cfg.Daemon.SocketPath, daemon.WorkspaceCreateRequest{
				Name:          args[0],
				Folder:        folderPath,
				OriginRoot:    cwd,
				CleanupPolicy: cleanup,
				VCSPreference: vcsPreference,
			})
			if err != nil {
				return mapSessionRPCError(err)
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Workspace created: %s (%s)\n", response.Name, response.WorkspaceID); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  Folder: %s (%s)\n", response.Folder, response.VCSType); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  Origin: %s\n", response.OriginRoot); err != nil {
				return err
			}

			autoHarness := strings.TrimSpace(harness)
			if autoHarness == "" {
				autoHarness = strings.TrimSpace(cfg.DefaultHarness)
			}
			if autoHarness == "" {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Warning: workspace created but default agent did not start: no default harness configured; set `default_harness` or pass --harness"); err != nil {
					return err
				}
				return nil
			}

			spawnCtx, spawnCancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer spawnCancel()

			agentResp, err := agentSpawnRPC(spawnCtx, cfg.Daemon.SocketPath, daemon.AgentSpawnRequest{
				WorkspaceID: response.WorkspaceID,
				Harness:     autoHarness,
			})
			if err != nil {
				if _, warnErr := fmt.Fprintf(cmd.OutOrStdout(), "Warning: workspace created but default agent did not start: %v\n", mapAgentRPCError(err)); warnErr != nil {
					return warnErr
				}
				return nil
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Agent started: %s (%s)\n", agentResp.AgentID, agentResp.Status); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&folder, "folder", "", "Initial workspace folder (defaults to CWD)")
	cmd.Flags().StringVar(&cleanup, "cleanup", "manual", "Cleanup policy: manual|on_close")
	cmd.Flags().StringVar(&vcsPreference, "vcs-preference", "", "VCS preference: auto|jj|git (defaults to global config)")
	cmd.Flags().StringVar(&harness, "harness", "", "Harness for auto-started agent: "+strings.Join(daemon.SupportedHarnesses(), "|"))

	return cmd
}

func newWorkspaceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List workspaces",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			response, err := workspaceListRPC(ctx, cfg.Daemon.SocketPath)
			if err != nil {
				return mapSessionRPCError(err)
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "ID       NAME          STATUS     FOLDERS  CREATED"); err != nil {
				return err
			}
			for _, session := range response.Workspaces {
				shortID := session.WorkspaceID
				if len(shortID) > 8 {
					shortID = shortID[:8]
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%-8s %-13s %-10s %-7d %s\n", shortID, session.Name, session.Status, session.FolderCount, session.CreatedAt); err != nil {
					return err
				}
			}

			return nil
		},
	}
}

func newWorkspaceShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id-or-name>",
		Short: "Show workspace details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := resolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			response, err := workspaceGetRPC(ctx, cfg.Daemon.SocketPath, sessionID)
			if err != nil {
				return mapSessionRPCError(err)
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s (%s)\n", response.Name, response.WorkspaceID); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", response.Status); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Origin: %s\n", response.OriginRoot); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Cleanup: %s\n", response.CleanupPolicy); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Folders:"); err != nil {
				return err
			}
			for _, folder := range response.Folders {
				primary := ""
				if folder.IsPrimary {
					primary = ", primary"
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %s (%s%s)\n", folder.Path, folder.VCSType, primary); err != nil {
					return err
				}
			}

			return nil
		},
	}
}

func newWorkspaceCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close <id-or-name>",
		Short: "Close workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := resolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			resp, err := workspaceCloseRPC(ctx, cfg.Daemon.SocketPath, sessionID)
			if err != nil {
				return mapSessionRPCError(err)
			}

			activeSession, err := config.ReadPersistedActiveWorkspace()
			if err != nil {
				return err
			}
			if strings.TrimSpace(activeSession) == sessionID {
				if err := config.WriteActiveWorkspace(""); err != nil {
					return err
				}
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Workspace close: %s\n", resp.Status)
			return err
		},
	}
}

func newWorkspaceSuspendCmd() *cobra.Command {
	return newWorkspaceStatusCommand("suspend", "Suspend workspace", func(ctx context.Context, socketPath, sessionID string) (string, error) {
		resp, err := workspaceSuspendRPC(ctx, socketPath, sessionID)
		if err != nil {
			return "", err
		}
		return resp.Status, nil
	})
}

func newWorkspaceResumeCmd() *cobra.Command {
	return newWorkspaceStatusCommand("resume", "Resume workspace", func(ctx context.Context, socketPath, sessionID string) (string, error) {
		resp, err := workspaceResumeRPC(ctx, socketPath, sessionID)
		if err != nil {
			return "", err
		}
		return resp.Status, nil
	})
}

func newWorkspaceStatusCommand(use, short string, call func(context.Context, string, string) (string, error)) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <id-or-name>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := resolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			status, err := call(ctx, cfg.Daemon.SocketPath, sessionID)
			if err != nil {
				return mapSessionRPCError(err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Workspace %s: %s\n", use, status)
			return err
		},
	}
}

func newWorkspaceFolderCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "folder", Short: "Manage workspace folders"}

	cmd.AddCommand(&cobra.Command{
		Use:   "add <id-or-name> <path>",
		Short: "Add folder to workspace",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := resolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			folderPath, err := absolutizeInputPath(cwd, args[1])
			if err != nil {
				return err
			}

			response, err := workspaceAddFolderRPC(ctx, cfg.Daemon.SocketPath, daemon.WorkspaceAddFolderRequest{WorkspaceID: sessionID, FolderPath: folderPath})
			if err != nil {
				return mapSessionRPCError(err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Added folder: %s (%s)\n", response.FolderPath, response.VCSType)
			return err
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove <id-or-name> <path>",
		Short: "Remove folder from workspace",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := workspaceEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := resolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			folderPath, err := absolutizeInputPath(cwd, args[1])
			if err != nil {
				return err
			}

			if err := workspaceRemoveFolderRPC(ctx, cfg.Daemon.SocketPath, daemon.WorkspaceRemoveFolderRequest{WorkspaceID: sessionID, FolderPath: folderPath}); err != nil {
				return mapSessionRPCError(err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Removed folder: %s\n", folderPath)
			return err
		},
	})

	return cmd
}

func resolveSessionIdentifier(ctx context.Context, socketPath, idOrName string) (string, error) {
	target, err := resolveSessionTarget(ctx, socketPath, idOrName)
	if err != nil {
		return "", err
	}
	return target.WorkspaceID, nil
}

type resolvedSessionTarget struct {
	WorkspaceID string
	Session     *daemon.WorkspaceGetResponse
}

func resolveSessionTarget(ctx context.Context, socketPath, idOrName string) (resolvedSessionTarget, error) {
	idOrName = strings.TrimSpace(idOrName)
	if idOrName == "" {
		return resolvedSessionTarget{}, userFacingError{message: "Workspace identifier is required"}
	}

	if session, err := workspaceGetRPC(ctx, socketPath, idOrName); err == nil {
		workspaceID := strings.TrimSpace(session.WorkspaceID)
		if workspaceID == idOrName {
			resolved := session
			return resolvedSessionTarget{WorkspaceID: session.WorkspaceID, Session: &resolved}, nil
		}
	} else if !isSessionNotFoundError(err) {
		return resolvedSessionTarget{}, mapSessionRPCError(err)
	}

	list, err := workspaceListRPC(ctx, socketPath)
	if err != nil {
		return resolvedSessionTarget{}, mapSessionRPCError(err)
	}

	exactIDMatches := make([]daemon.WorkspaceSummary, 0)
	nameMatches := make([]daemon.WorkspaceSummary, 0)
	prefixMatches := make([]string, 0)
	for _, session := range list.Workspaces {
		if session.WorkspaceID == idOrName {
			exactIDMatches = append(exactIDMatches, session)
		}
		if session.Name == idOrName {
			nameMatches = append(nameMatches, session)
		}
		if strings.HasPrefix(session.WorkspaceID, idOrName) {
			prefixMatches = append(prefixMatches, session.WorkspaceID)
		}
	}
	if len(exactIDMatches) == 1 {
		session, err := workspaceGetRPC(ctx, socketPath, exactIDMatches[0].WorkspaceID)
		if err != nil {
			return resolvedSessionTarget{}, mapSessionRPCError(err)
		}
		resolved := session
		return resolvedSessionTarget{WorkspaceID: session.WorkspaceID, Session: &resolved}, nil
	}
	if len(exactIDMatches) > 1 {
		return resolvedSessionTarget{}, userFacingError{message: "Workspace ID prefix is ambiguous"}
	}
	if len(nameMatches) == 1 {
		session, err := workspaceGetRPC(ctx, socketPath, nameMatches[0].WorkspaceID)
		if err != nil {
			if isSessionNotFoundError(err) {
				return resolvedSessionTarget{}, userFacingError{message: "Workspace not found"}
			}
			return resolvedSessionTarget{}, mapSessionRPCError(err)
		}
		resolved := session
		return resolvedSessionTarget{WorkspaceID: session.WorkspaceID, Session: &resolved}, nil
	}
	if len(nameMatches) > 1 {
		workspaceID, err := resolveNameCollisionByCWD(ctx, socketPath, nameMatches)
		if err != nil {
			return resolvedSessionTarget{}, err
		}
		return resolvedSessionTarget{WorkspaceID: workspaceID}, nil
	}
	if len(prefixMatches) == 1 {
		return resolvedSessionTarget{WorkspaceID: prefixMatches[0]}, nil
	}
	if len(prefixMatches) > 1 {
		return resolvedSessionTarget{}, userFacingError{message: "Workspace ID prefix is ambiguous"}
	}

	return resolvedSessionTarget{}, userFacingError{message: "Workspace not found"}
}

func resolveNameCollisionByCWD(ctx context.Context, socketPath string, nameMatches []daemon.WorkspaceSummary) (string, error) {
	if len(nameMatches) < 2 {
		return "", fmt.Errorf("name collision requires at least two matches")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	candidates, err := loadLiveWorkspaceCandidates(ctx, socketPath, nameMatches)
	if err != nil {
		return "", err
	}

	if len(candidates) == 0 {
		return "", userFacingError{message: "Workspace not found"}
	}

	if len(candidates) == 1 {
		return candidates[0].WorkspaceID, nil
	}

	workspaceID, resolveErr := resolveWorkspaceByCWD(cwd, candidates)
	if resolveErr == nil {
		return workspaceID, nil
	}

	if isWorkspaceCWDNoMatch(resolveErr) {
		return "", userFacingError{message: "Workspace name is ambiguous; run `ari workspace set <id-or-name>` to choose one"}
	}

	return "", resolveErr
}

func mapSessionRPCError(err error) error {
	if err == nil {
		return nil
	}
	if isDaemonUnavailable(err) {
		return userFacingError{message: notRunningMessage()}
	}
	if isPermissionDenied(err) {
		cfg, cfgErr := configuredDaemonConfig()
		if cfgErr != nil {
			return err
		}
		return socketPermissionError(cfg.Daemon.SocketPath)
	}
	if isTimeoutError(err) {
		return timeoutError()
	}

	var rpcErr *jsonrpc2.Error
	if errors.As(err, &rpcErr) {
		if rpcErr.Code == int64(rpc.SessionNotFound) {
			return userFacingError{message: "Workspace not found"}
		}
		if rpcErr.Code == int64(rpc.InvalidParams) {
			return userFacingError{message: rpcErr.Message}
		}
	}

	return err
}

func isSessionNotFoundError(err error) bool {
	var rpcErr *jsonrpc2.Error
	if !errors.As(err, &rpcErr) {
		return false
	}
	return rpcErr.Code == int64(rpc.SessionNotFound)
}

func absolutizeInputPath(cwd, input string) (string, error) {
	path := strings.TrimSpace(input)
	if path == "" {
		return "", userFacingError{message: "Path is required"}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return absPath, nil
}

func defaultSessionFolder(cwd string) string {
	backend, err := vcs.Detect(cwd)
	if err != nil {
		return cwd
	}
	if backend == nil || backend.Name() == "none" {
		return cwd
	}
	root := strings.TrimSpace(backend.Root())
	if root == "" {
		return cwd
	}
	return root
}
