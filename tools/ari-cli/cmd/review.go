package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/provider"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/vcs"
	"github.com/spf13/cobra"
)

func NewReviewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "review [revision-range]",
		Short: "Review changes",
		Long:  "Review VCS changes and generate summary",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			revRange := ""
			if len(args) > 0 {
				revRange = args[0]
			}
			return runReview(cmd, revRange)
		},
	}
}

func runReview(cmd *cobra.Command, revRange string) error {
	vcsBackend, err := vcs.Detect("")
	if err != nil {
		return fmt.Errorf("detect vcs: %w", err)
	}

	if vcsBackend.Name() == "none" {
		return fmt.Errorf("no version control system detected")
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Detected VCS: %s\n\n", vcsBackend.Name())

	diff, err := getDiff(vcsBackend, revRange)
	if err != nil {
		return fmt.Errorf("get diff: %w", err)
	}

	if diff == "" {
		fmt.Fprintln(cmd.OutOrStdout(), "No changes to review.")
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Changes:")
	fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("-", 40))
	lines := strings.Split(diff, "\n")
	if len(lines) > 50 {
		fmt.Fprintln(cmd.OutOrStdout(), strings.Join(lines[:50], "\n"))
		fmt.Fprintf(cmd.OutOrStdout(), "\n... (%d more lines)\n", len(lines)-50)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), diff)
	}
	fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("-", 40))
	fmt.Fprintln(cmd.OutOrStdout())

	summary, err := generateReviewSummary(diff)
	if err != nil {
		return fmt.Errorf("generate summary: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Review Summary:")
	fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("=", 40))
	fmt.Fprintln(cmd.OutOrStdout(), summary)

	return nil
}

func getDiff(backend vcs.VCSBackend, revRange string) (string, error) {
	switch backend.Name() {
	case "git":
		return getGitDiff(revRange)
	case "jj":
		return getJJDiff(revRange)
	default:
		return "", fmt.Errorf("unsupported vcs: %s", backend.Name())
	}
}

func getGitDiff(revRange string) (string, error) {
	var cmd *exec.Cmd

	if revRange == "" {
		cmd = exec.Command("git", "diff", "HEAD~1..HEAD")
	} else {
		cmd = exec.Command("git", "diff", revRange)
	}

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git diff failed: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("git diff: %w", err)
	}

	return string(out), nil
}

func getJJDiff(revRange string) (string, error) {
	var cmd *exec.Cmd

	if revRange == "" {
		cmd = exec.Command("jj", "diff", "-r", "@-", "--no-pager", "--color=never")
	} else {
		cmd = exec.Command("jj", "diff", "-r", revRange, "--no-pager", "--color=never")
	}

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("jj diff failed: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("jj diff: %w", err)
	}

	return string(out), nil
}

func generateReviewSummary(diff string) (string, error) {
	apiKey := os.Getenv("ARI_API_KEY")
	if apiKey == "" {
		return generatePlaceholderSummary(diff), nil
	}

	prov, err := provider.NewOpenRouterProvider(apiKey, "anthropic/claude-3.5-sonnet")
	if err != nil {
		return "", fmt.Errorf("create provider: %w", err)
	}

	prompt := fmt.Sprintf(`Review the following changes and provide a summary:
- What was changed
- Why it might have been changed  
- Any potential issues or improvements

Changes:
%s`, truncateDiff(diff, 4000))

	req := provider.CompletionRequest{
		Model: "anthropic/claude-3.5-sonnet",
		Messages: []provider.Message{
			{
				Role:    provider.MessageRoleSystem,
				Content: "You are a code reviewer. Provide concise, actionable feedback.",
			},
			{
				Role:    provider.MessageRoleUser,
				Content: prompt,
			},
		},
		MaxTokens: 1000,
	}

	ctx := context.Background()
	resp, err := prov.Complete(ctx, req)
	if err != nil {
		return "", fmt.Errorf("llm request: %w", err)
	}

	return resp.Message.Content, nil
}

func truncateDiff(diff string, maxChars int) string {
	if len(diff) <= maxChars {
		return diff
	}
	return diff[:maxChars] + "\n... (truncated)"
}

func generatePlaceholderSummary(diff string) string {
	var added, removed, files int
	lines := strings.Split(diff, "\n")
	currentFile := ""

	for _, line := range lines {
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			if currentFile == "" {
				files++
				currentFile = line
			} else {
				currentFile = ""
			}
		} else if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			removed++
		}
	}

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("Files changed: ~%d\n", files/2))
	summary.WriteString(fmt.Sprintf("Lines added: ~%d\n", added))
	summary.WriteString(fmt.Sprintf("Lines removed: ~%d\n\n", removed))
	summary.WriteString("Note: Set ARI_API_KEY environment variable for AI-powered review summary.\n")
	summary.WriteString("The AI summary would include:\n")
	summary.WriteString("- Analysis of what was changed\n")
	summary.WriteString("- Potential rationale for the changes\n")
	summary.WriteString("- Suggestions for improvements or issues to consider")

	return summary.String()
}
