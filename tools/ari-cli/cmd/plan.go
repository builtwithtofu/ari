package cmd

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/agent"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/headless"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/plan"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/provider"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/world"
	"github.com/spf13/cobra"
)

func NewPlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plan [goal]",
		Short: "Create an execution plan",
		Long:  "Interactive planning with research, questions, and refinement",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if isHeadless(cmd) {
				return headless.HeadlessUnsupportedError("plan")
			}

			goal := strings.TrimSpace(args[0])
			if goal == "" {
				return fmt.Errorf("goal must not be empty")
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
			providerClient := provider.NewSimulator(provider.PlanningScenario(nil))

			return runPlanWorkflow(context.Background(), cmd, goal, queries, providerClient)
		},
	}
}

func runPlanWorkflow(
	ctx context.Context,
	cmd *cobra.Command,
	goal string,
	queries *world.Queries,
	providerClient provider.Provider,
) error {
	reader := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()

	researcher := agent.NewResearcher(queries)
	fmt.Fprintln(out, "[1/4] Research phase...")
	researchContext, err := researcher.GatherContext(ctx, goal)
	if err != nil {
		return fmt.Errorf("gather context: %w", err)
	}
	fmt.Fprintf(
		out,
		"Research complete: %d decision(s), %d knowledge item(s).\n\n",
		len(researchContext.Decisions),
		len(researchContext.Knowledge),
	)

	questioner := agent.NewQuestioner(providerClient)
	fmt.Fprintln(out, "[2/4] Question phase...")
	questions, err := questioner.GenerateQuestions(ctx, researchContext)
	if err != nil {
		return fmt.Errorf("generate questions: %w", err)
	}

	answers := make([]plan.Answer, 0, len(questions))
	for i, question := range questions {
		fmt.Fprintf(out, "Q%d: %s\n", i+1, question.Prompt)
		if question.Context != "" {
			fmt.Fprintf(out, "  Context: %s\n", question.Context)
		}
		if len(question.Options) > 0 {
			fmt.Fprintln(out, "  Options:")
			for optionIndex, option := range question.Options {
				fmt.Fprintf(out, "    %d) %s\n", optionIndex+1, option)
			}
		}

		fmt.Fprint(out, "> ")
		answerText, readErr := reader.ReadString('\n')
		if readErr != nil {
			return fmt.Errorf("read answer for %s: %w", question.ID, readErr)
		}
		answerText = strings.TrimSpace(answerText)

		answerType := plan.ResponseTypeAnswer
		if answerText == "" {
			answerType = plan.ResponseTypeSkip
		}

		answers = append(answers, plan.Answer{
			QuestionID: question.ID,
			Type:       answerType,
			Content:    answerText,
		})
		fmt.Fprintln(out)
	}

	currentPlan := &plan.Plan{
		PlanID: fmt.Sprintf("plan-%d", time.Now().UTC().UnixNano()),
		Goal:   goal,
		Status: plan.PlanStatusWaitingApproval,
		Steps: []plan.Step{
			{
				StepID:      "s1",
				Type:        plan.StepTypeReasoning,
				Description: "Finalize implementation plan details",
				Status:      plan.StepStatusPlanned,
			},
		},
		Metadata: map[string]any{
			"research_decision_count":  len(researchContext.Decisions),
			"research_knowledge_count": len(researchContext.Knowledge),
			"initial_answers":          answers,
		},
	}

	refiner := agent.NewRefiner(providerClient, queries)
	fmt.Fprintln(out, "[3/4] Refinement phase...")
	fmt.Fprintln(out, "Available commands: approve, research more, gap analysis, update")

	for {
		fmt.Fprint(out, "Refinement command> ")
		command, readErr := reader.ReadString('\n')
		if readErr != nil {
			return fmt.Errorf("read refinement command: %w", readErr)
		}
		command = strings.ToLower(strings.TrimSpace(command))
		if command == "" {
			command = "update"
		}

		refinementAnswers := answers
		if command == "update" {
			fmt.Fprint(out, "Update note (optional)> ")
			updateNote, updateErr := reader.ReadString('\n')
			if updateErr != nil {
				return fmt.Errorf("read update note: %w", updateErr)
			}
			updateNote = strings.TrimSpace(updateNote)
			if updateNote != "" {
				refinementAnswers = append(
					append([]plan.Answer(nil), answers...),
					plan.Answer{QuestionID: "refinement-note", Type: plan.ResponseTypeAnswer, Content: updateNote},
				)
			}
		}

		if command == "research more" {
			fmt.Fprintln(out, "Running additional research...")
			researchContext, err = researcher.GatherContext(ctx, goal)
			if err != nil {
				return fmt.Errorf("gather additional context: %w", err)
			}
			fmt.Fprintf(
				out,
				"Additional research complete: %d decision(s), %d knowledge item(s).\n",
				len(researchContext.Decisions),
				len(researchContext.Knowledge),
			)
		}

		result, refineErr := refiner.Refine(ctx, currentPlan, refinementAnswers, command)
		if refineErr != nil {
			fmt.Fprintf(out, "Refinement failed: %v\n", refineErr)
			continue
		}

		currentPlan = result.Plan
		if result.Complete {
			fmt.Fprintln(out, "Plan approved and saved.")
			break
		}

		if result.NeedsMoreResearch {
			fmt.Fprintln(out, "Plan updated: more research requested.")
		} else {
			fmt.Fprintf(out, "Plan updated with command: %s\n", result.Command)
		}
	}

	fmt.Fprintln(out, "[4/4] Final plan summary")
	fmt.Fprintf(out, "Plan ID: %s\n", currentPlan.PlanID)
	fmt.Fprintf(out, "Goal: %s\n", currentPlan.Goal)
	fmt.Fprintf(out, "Status: %s\n", currentPlan.Status)
	fmt.Fprintf(out, "Steps: %d\n", len(currentPlan.Steps))

	return nil
}
