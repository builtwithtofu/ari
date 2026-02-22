package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	planpkg "github.com/builtwithtofu/ari/tools/ari-cli/internal/plan"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/provider"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/tools"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/world"
	"github.com/spf13/cobra"
)

const (
	buildProviderEnv      = "ARI_BUILD_PROVIDER"
	buildProviderModelEnv = "ARI_BUILD_MODEL"

	buildProviderOpenRouter = "openrouter"
	buildProviderSimulator  = "simulator"
)

func NewBuildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Execute a plan",
		Long:  "Execute a plan by ID",
		RunE: func(cmd *cobra.Command, _ []string) error {
			planID, err := cmd.Flags().GetString("plan")
			if err != nil {
				return fmt.Errorf("read --plan flag: %w", err)
			}
			planID = strings.TrimSpace(planID)
			if planID == "" {
				return fmt.Errorf("plan ID is required")
			}

			worldPath, err := findNearestWorldDB()
			if err != nil {
				return err
			}

			db, err := sql.Open("sqlite", worldPath)
			if err != nil {
				return fmt.Errorf("open world db: %w", err)
			}
			defer db.Close()

			queries := world.New(db)
			prov, err := selectBuildProvider()
			if err != nil {
				return err
			}

			registry, err := defaultBuildToolRegistry()
			if err != nil {
				return fmt.Errorf("configure tools: %w", err)
			}

			emitter := protocol.NewEmitter()
			runner := planpkg.NewStepRunner(prov, registry, emitter)
			executor := planpkg.NewExecutor(queries, emitter, runner)

			ctx := context.Background()
			buildPlan, err := executor.LoadPlanWithStatus(ctx, planID)
			if err != nil {
				return fmt.Errorf("load plan %q: %w", planID, err)
			}

			out := cmd.OutOrStdout()
			if isHeadless(cmd) {
				out = cmd.OutOrStderr()
			}

			if !isHeadless(cmd) {
				display := newBuildProgressDisplay(cmd.OutOrStdout(), buildPlan)
				emitter.SetOutput(display)
			}

			fmt.Fprintf(out, "Executing plan: %s\n", buildPlan.PlanID)
			fmt.Fprintf(out, "Goal: %s\n", buildPlan.Goal)
			fmt.Fprintf(out, "World: %s\n\n", worldPath)

			result, runErr := executor.Run(ctx, buildPlan)
			if runErr != nil {
				if result != nil {
					fmt.Fprintf(
						out,
						"\nExecution failed. Steps run: %d, steps failed: %d\n",
						result.StepsRun,
						result.StepsFailed,
					)
				}
				fmt.Fprintf(out, "Status: failed\n")
				return fmt.Errorf("execute plan %q: %w", planID, runErr)
			}

			fmt.Fprintf(
				out,
				"\nExecution complete. Steps run: %d, steps failed: %d\n",
				result.StepsRun,
				result.StepsFailed,
			)
			fmt.Fprintf(out, "Status: success\n")

			return nil
		},
	}

	cmd.Flags().String("plan", "", "Plan ID to execute")
	if err := cmd.MarkFlagRequired("plan"); err != nil {
		panic(fmt.Sprintf("failed to mark plan flag as required: %v", err))
	}

	return cmd
}

func selectBuildProvider() (provider.Provider, error) {
	providerName := strings.TrimSpace(strings.ToLower(os.Getenv(buildProviderEnv)))
	if providerName == "" {
		providerName = buildProviderOpenRouter
	}

	switch providerName {
	case buildProviderSimulator:
		return provider.NewSimulator(provider.SimpleResponse("simulated execution response")), nil
	case buildProviderOpenRouter:
		model := strings.TrimSpace(os.Getenv(buildProviderModelEnv))
		if model == "" {
			model = provider.ModelGeminiFlash15
		}

		openRouterProvider, err := provider.NewOpenRouterProvider("", model)
		if err != nil {
			return nil, fmt.Errorf(
				"configure OpenRouter provider: %w (set OPENROUTER_API_KEY or use %s=%s)",
				err,
				buildProviderEnv,
				buildProviderSimulator,
			)
		}
		return openRouterProvider, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q (supported: %s, %s)", providerName, buildProviderOpenRouter, buildProviderSimulator)
	}
}

func defaultBuildToolRegistry() (*tools.ToolRegistry, error) {
	registry := tools.NewToolRegistry()

	defaultTools := []tools.Tool{
		tools.ReadFileTool{},
		tools.WriteFileTool{},
		tools.RunCommandTool{},
		tools.AskUserTool{},
	}

	for _, tool := range defaultTools {
		if err := registry.Register(tool.Name(), tool); err != nil {
			return nil, err
		}
	}

	return registry, nil
}

type buildProgressDisplay struct {
	out              io.Writer
	stepDescriptions map[string]string
	pending          []byte
}

func newBuildProgressDisplay(out io.Writer, plan *planpkg.Plan) *buildProgressDisplay {
	stepDescriptions := make(map[string]string, len(plan.Steps))
	for _, step := range plan.Steps {
		stepDescriptions[step.StepID] = step.Description
	}

	return &buildProgressDisplay{
		out:              out,
		stepDescriptions: stepDescriptions,
	}
}

func (d *buildProgressDisplay) Write(p []byte) (int, error) {
	d.pending = append(d.pending, p...)

	for {
		lineEnd := bytes.IndexByte(d.pending, '\n')
		if lineEnd < 0 {
			break
		}

		line := d.pending[:lineEnd]
		d.pending = d.pending[lineEnd+1:]
		d.handleEventLine(line)
	}

	return len(p), nil
}

func (d *buildProgressDisplay) handleEventLine(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}

	var event protocol.Event
	if err := json.Unmarshal(line, &event); err != nil {
		return
	}

	if event.Type != "step_status_changed" {
		return
	}

	data, ok := event.Data.(map[string]any)
	if !ok {
		return
	}

	stepID, _ := data["step_id"].(string)
	status, _ := data["status"].(string)
	current := toInt(data["current"])
	total := toInt(data["total"])

	stepDesc := d.stepDescriptions[stepID]
	if stepDesc == "" {
		stepDesc = stepID
	}

	switch status {
	case string(planpkg.StepStatusExecuting):
		fmt.Fprintf(d.out, "Executing step %d/%d: %s\n", current, total, stepDesc)
	case string(planpkg.StepStatusCompleted):
		fmt.Fprintf(d.out, "  [ok] Completed\n")
	}
}

func toInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
