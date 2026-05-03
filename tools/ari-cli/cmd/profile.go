package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/client"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	profileEnsureDaemonRunning = ensureDaemonRunning
	profileCreateRPC           = func(ctx context.Context, socketPath string, req daemon.AgentProfileCreateRequest) (daemon.AgentProfileResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.AgentProfileResponse
		if err := rpcClient.Call(ctx, "profile.create", req, &response); err != nil {
			return daemon.AgentProfileResponse{}, err
		}
		return response, nil
	}
	profileGetRPC = func(ctx context.Context, socketPath string, req daemon.AgentProfileGetRequest) (daemon.AgentProfileResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.AgentProfileResponse
		if err := rpcClient.Call(ctx, "profile.get", req, &response); err != nil {
			return daemon.AgentProfileResponse{}, err
		}
		return response, nil
	}
	profileListRPC = func(ctx context.Context, socketPath string, req daemon.AgentProfileListRequest) (daemon.AgentProfileListResponse, error) {
		rpcClient := client.New(socketPath)
		var response daemon.AgentProfileListResponse
		if err := rpcClient.Call(ctx, "profile.list", req, &response); err != nil {
			return daemon.AgentProfileListResponse{}, err
		}
		return response, nil
	}
)

func NewProfileCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "profile", Short: "Manage Ari profiles", Hidden: true}
	cmd.AddCommand(newProfileCreateCmd())
	cmd.AddCommand(newProfileListCmd())
	cmd.AddCommand(newProfileShowCmd())
	cmd.AddCommand(newProfileDefaultsCmd())
	return cmd
}

func newProfileDefaultsCmd() *cobra.Command {
	var harness, model, invocationClass string
	cmd := &cobra.Command{
		Use:   "defaults",
		Short: "Set top-level profile defaults",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			if err := validateProfileDefaultsInput(harness, invocationClass); err != nil {
				return err
			}
			if strings.TrimSpace(harness) != "" {
				if err := config.WriteDefaultHarness(harness); err != nil {
					return err
				}
			}
			if strings.TrimSpace(model) != "" {
				if err := config.WritePreferredModel(model); err != nil {
					return err
				}
			}
			if strings.TrimSpace(invocationClass) != "" {
				if err := config.WriteDefaultInvocationClass(invocationClass); err != nil {
					return err
				}
			}
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "defaults\tharness=%s\tmodel=%s\tinvocation_class=%s\n", cfg.DefaultHarness, cfg.PreferredModel, cfg.DefaultInvocationClass)
			return err
		},
	}
	cmd.Flags().StringVar(&harness, "harness", "", "Preferred harness for profiles without an explicit harness")
	cmd.Flags().StringVar(&model, "model", "", "Preferred model or variant for profiles without an explicit model")
	cmd.Flags().StringVar(&invocationClass, "invocation-class", "", "Default invocation class: agent or temporary")
	return cmd
}

func validateProfileDefaultsInput(harness, invocationClass string) error {
	if harness = strings.TrimSpace(harness); harness != "" {
		valid := false
		for _, supported := range daemon.SupportedHarnesses() {
			if harness == supported {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("validate config: default_harness must be one of %s", strings.Join(daemon.SupportedHarnesses(), ", "))
		}
	}
	if invocationClass = strings.TrimSpace(invocationClass); invocationClass != "" {
		switch daemon.HarnessInvocationClass(invocationClass) {
		case daemon.HarnessInvocationAgent, daemon.HarnessInvocationTemporary:
		default:
			return fmt.Errorf("validate config: default_invocation_class must be one of agent, temporary")
		}
	}
	return nil
}

func newProfileCreateCmd() *cobra.Command {
	var workspaceID, harness, model, prompt, invocationClass string
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create or update a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := profileEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			profile, err := profileCreateRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentProfileCreateRequest{WorkspaceID: workspaceID, Name: args[0], Harness: harness, Model: model, Prompt: prompt, InvocationClass: daemon.HarnessInvocationClass(invocationClass)})
			if err != nil {
				return err
			}
			return printProfile(cmd, profile)
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace-id", "", "Create a workspace-scoped profile instead of a global profile")
	cmd.Flags().StringVar(&harness, "harness", "", "Preferred harness for this profile")
	cmd.Flags().StringVar(&model, "model", "", "Preferred model or variant for this profile")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt or system seed for this profile")
	cmd.Flags().StringVar(&invocationClass, "invocation-class", "", "Invocation class: agent or temporary")
	return cmd
}

func newProfileShowCmd() *cobra.Command {
	var workspaceID string
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := profileEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			profile, err := profileGetRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentProfileGetRequest{WorkspaceID: workspaceID, Name: args[0]})
			if err != nil {
				return err
			}
			return printProfile(cmd, profile)
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace-id", "", "Prefer a workspace-scoped profile before global fallback")
	return cmd
}

func newProfileListCmd() *cobra.Command {
	var workspaceID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			cfg, err := configuredDaemonConfig()
			if err != nil {
				return err
			}
			if err := profileEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			resp, err := profileListRPC(ctx, cfg.Daemon.SocketPath, daemon.AgentProfileListRequest{WorkspaceID: workspaceID})
			if err != nil {
				return err
			}
			for _, profile := range resp.Profiles {
				if err := printProfile(cmd, profile); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace-id", "", "List workspace-scoped profiles instead of global profiles")
	return cmd
}

func printProfile(cmd *cobra.Command, profile daemon.AgentProfileResponse) error {
	scope := "global"
	if strings.TrimSpace(profile.WorkspaceID) != "" {
		scope = "workspace:" + strings.TrimSpace(profile.WorkspaceID)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\n", profile.Name, scope, profile.Harness, profile.Model, profile.InvocationClass)
	return err
}
