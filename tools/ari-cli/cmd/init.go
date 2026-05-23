package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/cobra"
)

type initPromptOutput interface {
	InOrStdin() io.Reader
	OutOrStdout() io.Writer
}

var (
	initConfiguredDaemonConfig = configuredDaemonConfig
	initEnsureDaemonRunning    = ensureDaemonRunning
	initOptionsRPC             = func(ctx context.Context, socketPath string) (daemon.InitOptionsResponse, error) {
		return callDaemonRPC[daemon.InitOptionsResponse](ctx, socketPath, "init.options", daemon.InitOptionsRequest{})
	}
	initApplyRPC = func(ctx context.Context, socketPath string, req daemon.InitApplyRequest) (daemon.InitApplyResponse, error) {
		return callDaemonRPC[daemon.InitApplyResponse](ctx, socketPath, "init.apply", req)
	}
	initPromptHarness   = promptInitHarness
	initPromptSelection = promptInitSelection
)

type initSelection struct {
	Harness string
	Model   string
	Root    string
}

// Trust-model explanation signals printed after successful init.
// Tests assert against these constants rather than matching prose.
const (
	HelperTrustSignalReadOnly = "Read-only helper tools run without confirmation."
	HelperTrustSignalMutating = "Mutating helper tools will ask for trust on first use."
	HelperTrustSignalChoices  = "Trust choices: allow-once, allow-always, decline."
)

func NewInitCmd() *cobra.Command {
	var harness string
	var model string
	var root string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Ari onboarding defaults",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Welcome to Ari."); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Example: ari workspace setup my-project ~/Projects/my-project"); err != nil {
				return err
			}
			cfg, err := initConfiguredDaemonConfig()
			if err != nil {
				return err
			}
			if err := initEnsureDaemonRunning(cmd.Context(), cfg); err != nil {
				return err
			}
			selected := initSelection{Harness: strings.TrimSpace(harness), Model: strings.TrimSpace(model), Root: strings.TrimSpace(root)}
			interactive := !cmd.Flags().Changed("harness") && !cmd.Flags().Changed("model") && !cmd.Flags().Changed("root")
			if interactive || selected.Harness == "" {
				ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
				options, err := initOptionsRPC(ctx, cfg.Daemon.SocketPath)
				cancel()
				if err != nil {
					return err
				}
				if interactive {
					prompted, err := initPromptSelection(cmd, options, selected)
					if err != nil {
						return err
					}
					selected.Harness = prompted.Harness
					selected.Model = prompted.Model
					selected.Root = prompted.Root
				} else {
					selected.Harness, err = initPromptHarness(cmd, options.Harnesses)
					if err != nil {
						return err
					}
				}
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			response, err := initApplyRPC(ctx, cfg.Daemon.SocketPath, daemon.InitApplyRequest{Harness: selected.Harness, Model: selected.Model, Root: selected.Root})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Default harness set: %s\n", response.DefaultHarness); err != nil {
				return err
			}
			if response.HomeWorkspaceReady {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Home workspace ready: home"); err != nil {
					return err
				}
			}
			if response.HomeHelperReady {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Home helper ready: helper"); err != nil {
					return err
				}
			}
			writeHelperTrustExplanation(cmd.OutOrStdout())
			return nil
		},
	}
	cmd.Flags().StringVar(&harness, "harness", "", "Default Ari harness")
	cmd.Flags().StringVar(&model, "model", "", "Preferred Ari model")
	cmd.Flags().StringVar(&root, "root", "", "Default Ari workspace root")
	return cmd
}

func promptInitSelection(cmd initPromptOutput, options daemon.InitOptionsResponse, selected initSelection) (initSelection, error) {
	scanner := bufio.NewScanner(cmd.InOrStdin())
	var err error
	if strings.TrimSpace(selected.Harness) == "" {
		selected.Harness, err = promptInitHarnessWithScanner(cmd, scanner, options.Harnesses)
		if err != nil {
			return initSelection{}, err
		}
	}
	if strings.TrimSpace(selected.Model) == "" {
		selected.Model, err = promptInitModelWithScanner(cmd, scanner, options.Models)
		if err != nil {
			return initSelection{}, err
		}
	}
	if strings.TrimSpace(selected.Root) == "" {
		selected.Root, err = promptInitRootWithScanner(cmd, scanner, options.Roots)
		if err != nil {
			return initSelection{}, err
		}
	}
	return selected, nil
}

// writeHelperTrustExplanation prints the helper trust model explanation
// to the writer. It uses stable signal constants so tests can assert on
// specific phrases without matching long prose.
func writeHelperTrustExplanation(w io.Writer) {
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Helper tool trust model:")
	_, _ = fmt.Fprintf(w, "  %s\n", HelperTrustSignalReadOnly)
	_, _ = fmt.Fprintf(w, "  %s\n", HelperTrustSignalMutating)
	_, _ = fmt.Fprintf(w, "  %s\n", HelperTrustSignalChoices)
}

func promptInitHarness(cmd initPromptOutput, options []daemon.InitHarnessOption) (string, error) {
	scanner := bufio.NewScanner(cmd.InOrStdin())
	return promptInitHarnessWithScanner(cmd, scanner, options)
}

func promptInitHarnessWithScanner(cmd initPromptOutput, scanner *bufio.Scanner, options []daemon.InitHarnessOption) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("no harness options available")
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Choose your default harness:"); err != nil {
		return "", err
	}
	for index, option := range options {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s\n", index+1, option.Label); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), "Harness: "); err != nil {
		return "", err
	}
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read harness choice: %w", err)
		}
		return "", fmt.Errorf("read harness choice: no input")
	}
	choice, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil {
		return "", fmt.Errorf("harness choice must be a number between 1 and %d", len(options))
	}
	if choice < 1 || choice > len(options) {
		return "", fmt.Errorf("harness choice must be between 1 and %d", len(options))
	}
	return options[choice-1].Name, nil
}

func promptInitModelWithScanner(cmd initPromptOutput, scanner *bufio.Scanner, options []daemon.InitModelOption) (string, error) {
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Choose your preferred model:"); err != nil {
		return "", err
	}
	for index, option := range options {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s\n", index+1, option.Label); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), "Model (enter a model name or press Enter for default): "); err != nil {
		return "", err
	}
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read model choice: %w", err)
		}
		return "", fmt.Errorf("read model choice: no input")
	}
	text := strings.TrimSpace(scanner.Text())
	if choice, err := strconv.Atoi(text); err == nil && choice >= 1 && choice <= len(options) {
		return options[choice-1].Name, nil
	}
	return text, nil
}

func promptInitRootWithScanner(cmd initPromptOutput, scanner *bufio.Scanner, options []daemon.InitRootOption) (string, error) {
	defaultRoot := "~/"
	if len(options) > 0 && strings.TrimSpace(options[0].Path) != "" {
		defaultRoot = options[0].Path
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Workspace root [%s]: ", defaultRoot); err != nil {
		return "", err
	}
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read root choice: %w", err)
		}
		return "", fmt.Errorf("read root choice: no input")
	}
	root := strings.TrimSpace(scanner.Text())
	if root == "" {
		return defaultRoot, nil
	}
	return root, nil
}
