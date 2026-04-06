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
	"golang.org/x/term"
)

var (
	sessionEnsureDaemonRunning         = ensureDaemonRunning
	sessionSwitchIsInteractiveTerminal = func(cmd *cobra.Command) bool {
		if cmd == nil {
			return false
		}
		inputFile, ok := cmd.InOrStdin().(*os.File)
		if !ok {
			return false
		}
		return term.IsTerminal(int(inputFile.Fd()))
	}
	sessionCreateRPC = func(ctx context.Context, socketPath string, req daemon.SessionCreateRequest) (daemon.SessionCreateResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.SessionCreateResponse
		if err := rpcClient.Call(ctx, "session.create", req, &response); err != nil {
			return daemon.SessionCreateResponse{}, err
		}
		return response, nil
	}
	sessionListRPC = func(ctx context.Context, socketPath string) (daemon.SessionListResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.SessionListResponse
		if err := rpcClient.Call(ctx, "session.list", daemon.SessionListRequest{}, &response); err != nil {
			return daemon.SessionListResponse{}, err
		}
		return response, nil
	}
	sessionGetRPC = func(ctx context.Context, socketPath, sessionID string) (daemon.SessionGetResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.SessionGetResponse
		if err := rpcClient.Call(ctx, "session.get", daemon.SessionGetRequest{SessionID: sessionID}, &response); err != nil {
			return daemon.SessionGetResponse{}, err
		}
		return response, nil
	}
	sessionCloseRPC = func(ctx context.Context, socketPath, sessionID string) (daemon.SessionCloseResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.SessionCloseResponse
		if err := rpcClient.Call(ctx, "session.close", daemon.SessionCloseRequest{SessionID: sessionID}, &response); err != nil {
			return daemon.SessionCloseResponse{}, err
		}
		return response, nil
	}
	sessionSuspendRPC = func(ctx context.Context, socketPath, sessionID string) (daemon.SessionSuspendResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.SessionSuspendResponse
		if err := rpcClient.Call(ctx, "session.suspend", daemon.SessionSuspendRequest{SessionID: sessionID}, &response); err != nil {
			return daemon.SessionSuspendResponse{}, err
		}
		return response, nil
	}
	sessionResumeRPC = func(ctx context.Context, socketPath, sessionID string) (daemon.SessionResumeResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.SessionResumeResponse
		if err := rpcClient.Call(ctx, "session.resume", daemon.SessionResumeRequest{SessionID: sessionID}, &response); err != nil {
			return daemon.SessionResumeResponse{}, err
		}
		return response, nil
	}
	sessionAddFolderRPC = func(ctx context.Context, socketPath string, req daemon.SessionAddFolderRequest) (daemon.SessionAddFolderResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.SessionAddFolderResponse
		if err := rpcClient.Call(ctx, "session.add_folder", req, &response); err != nil {
			return daemon.SessionAddFolderResponse{}, err
		}
		return response, nil
	}
	sessionRemoveFolderRPC = func(ctx context.Context, socketPath string, req daemon.SessionRemoveFolderRequest) error {
		rpcClient := client.New(socketPath)
		var response daemon.SessionRemoveFolderResponse
		return rpcClient.Call(ctx, "session.remove_folder", req, &response)
	}
)

func NewSessionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "session", Short: "Manage Ari sessions"}
	cmd.AddCommand(newSessionCreateCmd())
	cmd.AddCommand(newSessionListCmd())
	cmd.AddCommand(newSessionShowCmd())
	cmd.AddCommand(newSessionCloseCmd())
	cmd.AddCommand(newSessionSuspendCmd())
	cmd.AddCommand(newSessionResumeCmd())
	cmd.AddCommand(newSessionSetCmd())
	cmd.AddCommand(newSessionCurrentCmd())
	cmd.AddCommand(newSessionSwitchCmd())
	cmd.AddCommand(newSessionClearCmd())
	cmd.AddCommand(newSessionFolderCmd())
	return cmd
}

func newSessionSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <id-or-name>",
		Short: "Set active workspace session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := resolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}
			return writeAndReportActiveSession(cmd, sessionID)
		},
	}
}

func newSessionCurrentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show active workspace session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sessionID, err := config.ReadActiveSession()
			if err != nil {
				return err
			}
			if strings.TrimSpace(sessionID) == "" {
				return userFacingError{message: "No active workspace session is set"}
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Current workspace session: %s\n", sessionID)
			return err
		},
	}
}

func newSessionClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Clear active workspace session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := config.WriteActiveSession(""); err != nil {
				return err
			}
			if strings.TrimSpace(os.Getenv("ARI_ACTIVE_SESSION")) != "" {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "Cleared persisted active workspace session; ARI_ACTIVE_SESSION still overrides it in this shell")
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Cleared active workspace session")
			return err
		},
	}
}

func newSessionSwitchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "switch",
		Short: "Switch active workspace session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			if !sessionSwitchIsInteractiveTerminal(cmd) {
				return userFacingError{message: "session switch requires an interactive terminal; use session set <id-or-name>"}
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			response, err := sessionListRPC(ctx, cfg.Daemon.SocketPath)
			if err != nil {
				return mapSessionRPCError(err)
			}

			available := make([]daemon.SessionSummary, 0, len(response.Sessions))
			for _, session := range response.Sessions {
				if strings.EqualFold(strings.TrimSpace(session.Status), "closed") {
					continue
				}
				available = append(available, session)
			}

			if len(available) == 0 {
				return userFacingError{message: "No open sessions available; create one with `ari session create <name>`"}
			}

			selected := daemon.SessionSummary{}
			if len(available) == 1 {
				selected = available[0]
			} else {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Select workspace session:"); err != nil {
					return err
				}
				for index, session := range available {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %d) %s (%s)\n", index+1, session.Name, session.SessionID); err != nil {
						return err
					}
				}
				if _, err := fmt.Fprint(cmd.OutOrStdout(), "Enter selection number: "); err != nil {
					return err
				}

				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil {
					return userFacingError{message: "Unable to read session selection"}
				}
				selection, err := strconv.Atoi(strings.TrimSpace(line))
				if err != nil || selection < 1 || selection > len(available) {
					return userFacingError{message: "Invalid session selection"}
				}
				selected = available[selection-1]
			}

			return writeAndReportActiveSession(cmd, selected.SessionID)
		},
	}
}

func writeAndReportActiveSession(cmd *cobra.Command, sessionID string) error {
	if cmd == nil {
		return fmt.Errorf("active session write: command is required")
	}
	if strings.TrimSpace(sessionID) == "" {
		return userFacingError{message: "Session identifier is required"}
	}
	if err := config.WriteActiveSession(sessionID); err != nil {
		return err
	}
	if strings.TrimSpace(os.Getenv("ARI_ACTIVE_SESSION")) != "" {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Persisted active workspace set: %s; ARI_ACTIVE_SESSION still overrides it in this shell\n", sessionID)
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Active workspace set: %s\n", sessionID)
	return err
}

func newSessionCreateCmd() *cobra.Command {
	var folder string
	var cleanup string
	var vcsPreference string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
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

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			response, err := sessionCreateRPC(ctx, cfg.Daemon.SocketPath, daemon.SessionCreateRequest{
				Name:          args[0],
				Folder:        folderPath,
				OriginRoot:    cwd,
				CleanupPolicy: cleanup,
				VCSPreference: vcsPreference,
			})
			if err != nil {
				return mapSessionRPCError(err)
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Session created: %s (%s)\n", response.Name, response.SessionID); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  Folder: %s (%s)\n", response.Folder, response.VCSType); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  Origin: %s\n", response.OriginRoot); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&folder, "folder", "", "Initial session folder (defaults to CWD)")
	cmd.Flags().StringVar(&cleanup, "cleanup", "manual", "Cleanup policy: manual|on_close")
	cmd.Flags().StringVar(&vcsPreference, "vcs-preference", "", "VCS preference: auto|jj|git (defaults to global config)")

	return cmd
}

func newSessionListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			response, err := sessionListRPC(ctx, cfg.Daemon.SocketPath)
			if err != nil {
				return mapSessionRPCError(err)
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "ID       NAME          STATUS     FOLDERS  CREATED"); err != nil {
				return err
			}
			for _, session := range response.Sessions {
				shortID := session.SessionID
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

func newSessionShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id-or-name>",
		Short: "Show session details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := resolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			response, err := sessionGetRPC(ctx, cfg.Daemon.SocketPath, sessionID)
			if err != nil {
				return mapSessionRPCError(err)
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Session: %s (%s)\n", response.Name, response.SessionID); err != nil {
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

func newSessionCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close <id-or-name>",
		Short: "Close session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			sessionID, err := resolveSessionIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			resp, err := sessionCloseRPC(ctx, cfg.Daemon.SocketPath, sessionID)
			if err != nil {
				return mapSessionRPCError(err)
			}

			activeSession, err := config.ReadPersistedActiveSession()
			if err != nil {
				return err
			}
			if strings.TrimSpace(activeSession) == sessionID {
				if err := config.WriteActiveSession(""); err != nil {
					return err
				}
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Session close: %s\n", resp.Status)
			return err
		},
	}
}

func newSessionSuspendCmd() *cobra.Command {
	return newSessionStatusCommand("suspend", "Suspend session", func(ctx context.Context, socketPath, sessionID string) (string, error) {
		resp, err := sessionSuspendRPC(ctx, socketPath, sessionID)
		if err != nil {
			return "", err
		}
		return resp.Status, nil
	})
}

func newSessionResumeCmd() *cobra.Command {
	return newSessionStatusCommand("resume", "Resume session", func(ctx context.Context, socketPath, sessionID string) (string, error) {
		resp, err := sessionResumeRPC(ctx, socketPath, sessionID)
		if err != nil {
			return "", err
		}
		return resp.Status, nil
	})
}

func newSessionStatusCommand(use, short string, call func(context.Context, string, string) (string, error)) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <id-or-name>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
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

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Session %s: %s\n", use, status)
			return err
		},
	}
}

func newSessionFolderCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "folder", Short: "Manage session folders"}

	cmd.AddCommand(&cobra.Command{
		Use:   "add <id-or-name> <path>",
		Short: "Add folder to session",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
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

			response, err := sessionAddFolderRPC(ctx, cfg.Daemon.SocketPath, daemon.SessionAddFolderRequest{SessionID: sessionID, FolderPath: folderPath})
			if err != nil {
				return mapSessionRPCError(err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Added folder: %s (%s)\n", response.FolderPath, response.VCSType)
			return err
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove <id-or-name> <path>",
		Short: "Remove folder from session",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := sessionEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
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

			if err := sessionRemoveFolderRPC(ctx, cfg.Daemon.SocketPath, daemon.SessionRemoveFolderRequest{SessionID: sessionID, FolderPath: folderPath}); err != nil {
				return mapSessionRPCError(err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Removed folder: %s\n", folderPath)
			return err
		},
	})

	return cmd
}

func resolveSessionIdentifier(ctx context.Context, socketPath, idOrName string) (string, error) {
	idOrName = strings.TrimSpace(idOrName)
	if idOrName == "" {
		return "", userFacingError{message: "Session identifier is required"}
	}

	if session, err := sessionGetRPC(ctx, socketPath, idOrName); err == nil {
		return session.SessionID, nil
	} else if !isSessionNotFoundError(err) {
		return "", mapSessionRPCError(err)
	}

	list, err := sessionListRPC(ctx, socketPath)
	if err != nil {
		return "", mapSessionRPCError(err)
	}

	prefixMatches := make([]string, 0)
	for _, session := range list.Sessions {
		if session.Name == idOrName {
			return session.SessionID, nil
		}
		if strings.HasPrefix(session.SessionID, idOrName) {
			prefixMatches = append(prefixMatches, session.SessionID)
		}
	}
	if len(prefixMatches) == 1 {
		return prefixMatches[0], nil
	}
	if len(prefixMatches) > 1 {
		return "", userFacingError{message: "Session ID prefix is ambiguous"}
	}

	return "", userFacingError{message: "Session not found"}
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
			return userFacingError{message: "Session not found"}
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
