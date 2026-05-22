package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	workspaceEnsureDaemonRunning         = ensureDaemonRunning
	workspaceSwitchIsInteractiveTerminal = func(cmd *cobra.Command) bool {
		return isInteractiveTerminal(cmd)
	}
	workspaceCreateRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceCreateRequest) (daemon.WorkspaceCreateResponse, error) {
		return callDaemonRPC[daemon.WorkspaceCreateResponse](ctx, socketPath, "workspace.create", req)
	}
	workspaceSetupExistingRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceSetupExistingRequest) (daemon.WorkspaceSetupExistingResponse, error) {
		return callDaemonRPC[daemon.WorkspaceSetupExistingResponse](ctx, socketPath, "workspace.setup_existing", req)
	}
	workspaceListRPC = func(ctx context.Context, socketPath string) (daemon.WorkspaceListResponse, error) {
		return callDaemonRPC[daemon.WorkspaceListResponse](ctx, socketPath, "workspace.list", daemon.WorkspaceListRequest{})
	}
	workspaceGetRPC = func(ctx context.Context, socketPath, workspaceID string) (daemon.WorkspaceGetResponse, error) {
		return callDaemonRPC[daemon.WorkspaceGetResponse](ctx, socketPath, "workspace.get", daemon.WorkspaceGetRequest{WorkspaceID: workspaceID})
	}
	workspaceSuspendRPC = func(ctx context.Context, socketPath, workspaceID string) (daemon.WorkspaceSuspendResponse, error) {
		return callDaemonRPC[daemon.WorkspaceSuspendResponse](ctx, socketPath, "workspace.suspend", daemon.WorkspaceSuspendRequest{WorkspaceID: workspaceID})
	}
	workspaceResumeRPC = func(ctx context.Context, socketPath, workspaceID string) (daemon.WorkspaceResumeResponse, error) {
		return callDaemonRPC[daemon.WorkspaceResumeResponse](ctx, socketPath, "workspace.resume", daemon.WorkspaceResumeRequest{WorkspaceID: workspaceID})
	}
	workspaceAddFolderRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceAddFolderRequest) (daemon.WorkspaceAddFolderResponse, error) {
		return callDaemonRPC[daemon.WorkspaceAddFolderResponse](ctx, socketPath, "workspace.add_folder", req)
	}
	workspaceRemoveFolderRPC = func(ctx context.Context, socketPath string, req daemon.WorkspaceRemoveFolderRequest) error {
		_, err := callDaemonRPC[daemon.WorkspaceRemoveFolderResponse](ctx, socketPath, "workspace.remove_folder", req)
		return err
	}
	workspaceContextSetRPC = func(ctx context.Context, socketPath string, req daemon.ContextSetRequest) (daemon.ContextSetResponse, error) {
		return callDaemonRPC[daemon.ContextSetResponse](ctx, socketPath, "context.set", req)
	}
)

func NewWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "workspace", Short: "Manage Ari workspaces"}
	cmd.AddCommand(newWorkspaceCreateCmd())
	cmd.AddCommand(newWorkspaceSetupCmd())
	cmd.AddCommand(newWorkspaceListCmd())
	cmd.AddCommand(newWorkspaceShowCmd())
	cmd.AddCommand(newWorkspaceSuspendCmd())
	cmd.AddCommand(newWorkspaceResumeCmd())
	cmd.AddCommand(newWorkspaceSetCmd())
	cmd.AddCommand(newWorkspaceUseCmd())
	cmd.AddCommand(newWorkspaceCurrentCmd())
	cmd.AddCommand(newWorkspaceSwitchCmd())
	cmd.AddCommand(newWorkspaceClearCmd())
	cmd.AddCommand(newWorkspaceFolderCmd())
	return cmd
}

func newWorkspaceSetupCmd() *cobra.Command {
	var vcsPreference string
	cmd := &cobra.Command{
		Use:   "setup <name> <folder>",
		Short: "Create and select a project workspace from an existing folder",
		Args:  cobra.ExactArgs(2),
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
			folderPath, err := absolutizeInputPath(cwd, args[1])
			if err != nil {
				return err
			}
			if strings.TrimSpace(vcsPreference) == "" {
				vcsPreference = cfg.VCSPreference
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			response, err := workspaceSetupExistingRPC(ctx, cfg.Daemon.SocketPath, daemon.WorkspaceSetupExistingRequest{Name: args[0], Folder: folderPath, VCSPreference: vcsPreference})
			if err != nil {
				return mapSessionRPCError(err)
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Project workspace ready: %s (%s)\n", response.Name, response.WorkspaceID); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  Folder: %s (%s)\n", response.Folder, response.VCSType); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  Active workspace: %s\n", response.ActiveWorkspace); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "  Inspect: ari workspace show")
			return err
		},
	}
	cmd.Flags().StringVar(&vcsPreference, "vcs-preference", "", "VCS preference: auto|jj|git (defaults to global config)")
	return cmd
}

func newWorkspaceUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <id-or-name>",
		Short: "Use active workspace",
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
			workspaceID, err := resolveWorkspaceIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}
			resp, err := workspaceContextSetRPC(ctx, cfg.Daemon.SocketPath, daemon.ContextSetRequest{WorkspaceID: workspaceID})
			if err != nil {
				return mapWorkspaceRPCError(err)
			}
			return writeAndReportActiveWorkspace(cmd, resp.Current.WorkspaceID)
		},
	}
}

func newWorkspaceSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "set <id-or-name>",
		Short:  "Set active workspace",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
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

			workspaceID, err := resolveWorkspaceIdentifier(lookupCtx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}
			if err := writeAndReportActiveWorkspace(cmd, workspaceID); err != nil {
				return err
			}
			return nil
		},
	}
}

func newWorkspaceCurrentCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "current",
		Short:  "Show active workspace",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			workspaceID, err := readActiveWorkspaceID()
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Current workspace: %s\n", workspaceID)
			return err
		},
	}
}

func newWorkspaceClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "clear",
		Short:  "Clear active workspace",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return clearAndReportActiveWorkspace(cmd)
		},
	}
}

func newWorkspaceSwitchCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "switch",
		Short:  "Switch active workspace",
		Hidden: true,
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
				return mapWorkspaceRPCError(err)
			}

			available := response.Workspaces

			if len(available) == 0 {
				return userFacingError{message: "No workspaces available; create one with `ari workspace create <name>`"}
			}

			selected := daemon.WorkspaceSummary{}
			if len(available) == 1 {
				selected = available[0]
			} else {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Select workspace:"); err != nil {
					return err
				}
				for index, workspace := range available {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %d) %s (%s)\n", index+1, workspace.Name, workspace.WorkspaceID); err != nil {
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

			if err := writeAndReportActiveWorkspace(cmd, selected.WorkspaceID); err != nil {
				return err
			}
			return nil
		},
	}
}

func newWorkspaceCreateCmd() *cobra.Command {
	var folder string
	var cleanup string
	var vcsPreference string

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
			folderPath := ""
			if strings.TrimSpace(folder) != "" {
				folderPath, err = absolutizeInputPath(cwd, folder)
				if err != nil {
					return err
				}
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
				OriginRoot:    folderPath,
				CleanupPolicy: cleanup,
				VCSPreference: vcsPreference,
			})
			if err != nil {
				return mapWorkspaceRPCError(err)
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Workspace created: %s (%s)\n", response.Name, response.WorkspaceID); err != nil {
				return err
			}
			if folderPath != "" {
				addResp, err := workspaceAddFolderRPC(createCtx, cfg.Daemon.SocketPath, daemon.WorkspaceAddFolderRequest{WorkspaceID: response.WorkspaceID, FolderPath: folderPath})
				if err != nil {
					return userFacingError{message: fmt.Sprintf("Workspace created: %s (%s), but adding folder failed: %v", response.Name, response.WorkspaceID, mapWorkspaceRPCError(err))}
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  Folder: %s (%s)\n", addResp.FolderPath, addResp.VCSType); err != nil {
					return err
				}
			}
			if response.OriginRoot != "" {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  Origin: %s\n", response.OriginRoot); err != nil {
					return err
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&folder, "folder", "", "Initial workspace folder")
	cmd.Flags().StringVar(&cleanup, "cleanup", "manual", "Cleanup policy: manual")
	cmd.Flags().StringVar(&vcsPreference, "vcs-preference", "", "VCS preference: auto|jj|git (defaults to global config)")

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
				return mapWorkspaceRPCError(err)
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "ID       NAME          STATUS     FOLDERS  CREATED"); err != nil {
				return err
			}
			for _, workspace := range response.Workspaces {
				shortID := workspace.WorkspaceID
				if len(shortID) > 8 {
					shortID = shortID[:8]
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%-8s %-13s %-10s %-7d %s\n", shortID, workspace.Name, workspace.Status, workspace.FolderCount, workspace.CreatedAt); err != nil {
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

			workspaceID, err := resolveWorkspaceIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			response, err := workspaceGetRPC(ctx, cfg.Daemon.SocketPath, workspaceID)
			if err != nil {
				return mapWorkspaceRPCError(err)
			}

			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s (%s)\n", response.Name, response.WorkspaceID); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Status: %s\n", response.Status); err != nil {
				return err
			}
			origin := response.OriginRoot
			if origin == "" {
				origin = "none"
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Origin: %s\n", origin); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Cleanup: %s\n", response.CleanupPolicy); err != nil {
				return err
			}
			if len(response.Folders) == 0 {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Folders: none"); err != nil {
					return err
				}
				return nil
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

func newWorkspaceSuspendCmd() *cobra.Command {
	return newWorkspaceStatusCommand("suspend", "Suspend workspace", func(ctx context.Context, socketPath, workspaceID string) (string, error) {
		resp, err := workspaceSuspendRPC(ctx, socketPath, workspaceID)
		if err != nil {
			return "", err
		}
		return resp.Status, nil
	})
}

func newWorkspaceResumeCmd() *cobra.Command {
	return newWorkspaceStatusCommand("resume", "Resume workspace", func(ctx context.Context, socketPath, workspaceID string) (string, error) {
		resp, err := workspaceResumeRPC(ctx, socketPath, workspaceID)
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

			workspaceID, err := resolveWorkspaceIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
			if err != nil {
				return err
			}

			status, err := call(ctx, cfg.Daemon.SocketPath, workspaceID)
			if err != nil {
				return mapWorkspaceRPCError(err)
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

			workspaceID, err := resolveWorkspaceIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
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

			response, err := workspaceAddFolderRPC(ctx, cfg.Daemon.SocketPath, daemon.WorkspaceAddFolderRequest{WorkspaceID: workspaceID, FolderPath: folderPath})
			if err != nil {
				return mapWorkspaceRPCError(err)
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

			workspaceID, err := resolveWorkspaceIdentifier(ctx, cfg.Daemon.SocketPath, args[0])
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

			if err := workspaceRemoveFolderRPC(ctx, cfg.Daemon.SocketPath, daemon.WorkspaceRemoveFolderRequest{WorkspaceID: workspaceID, FolderPath: folderPath}); err != nil {
				return mapWorkspaceRPCError(err)
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Removed folder: %s\n", folderPath)
			return err
		},
	})

	return cmd
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
