package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/headless"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/world"
	"github.com/spf13/cobra"
)

func NewAskCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ask [query]",
		Short: "Search Ari world knowledge",
		Long:  "Search Ari world decisions and knowledge using simple text matching.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if isHeadless(cmd) {
				return headless.HeadlessUnsupportedError("ask")
			}

			query := strings.TrimSpace(args[0])
			if query == "" {
				return fmt.Errorf("query must not be empty")
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
			ctx := context.Background()

			decisions, err := queries.ListDecisions(ctx)
			if err != nil {
				return fmt.Errorf("list decisions: %w", err)
			}

			knowledge, err := queries.ListKnowledge(ctx)
			if err != nil {
				return fmt.Errorf("list knowledge: %w", err)
			}

			needle := strings.ToLower(query)
			matchedDecisionCount := 0
			matchedKnowledgeCount := 0

			fmt.Fprintf(cmd.OutOrStdout(), "Query: %q\n", query)
			fmt.Fprintf(cmd.OutOrStdout(), "World: %s\n\n", worldPath)

			fmt.Fprintln(cmd.OutOrStdout(), "Decisions:")
			for _, d := range decisions {
				if !matchesDecision(d, needle) {
					continue
				}
				matchedDecisionCount++
				fmt.Fprintf(cmd.OutOrStdout(), "- [%s] %s\n", d.ID, d.Title)
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", previewText(d.Content, 160))
			}
			if matchedDecisionCount == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "- No matching decisions")
			}

			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintln(cmd.OutOrStdout(), "Knowledge:")
			for _, k := range knowledge {
				if !matchesKnowledge(k, needle) {
					continue
				}
				matchedKnowledgeCount++
				fmt.Fprintf(cmd.OutOrStdout(), "- [%s] %s\n", k.ID, k.Name)
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", previewText(k.Content.String, 160))
			}
			if matchedKnowledgeCount == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "- No matching knowledge")
			}

			fmt.Fprintf(
				cmd.OutOrStdout(),
				"\nFound %d decision(s) and %d knowledge item(s).\n",
				matchedDecisionCount,
				matchedKnowledgeCount,
			)

			return nil
		},
	}
}

func findNearestWorldDB() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get current directory: %w", err)
	}

	current := cwd
	for {
		worldPath := filepath.Join(current, ".ari", "world.db")
		if info, statErr := os.Stat(worldPath); statErr == nil && !info.IsDir() {
			return worldPath, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("not in an Ari world: no .ari directory found")
		}
		current = parent
	}
}

func matchesDecision(d world.Decision, needle string) bool {
	combined := strings.ToLower(strings.Join([]string{d.Title, d.Content}, "\n"))
	return strings.Contains(combined, needle)
}

func matchesKnowledge(k world.Knowledge, needle string) bool {
	combined := strings.ToLower(strings.Join([]string{k.Type, k.Name, k.Content.String}, "\n"))
	return strings.Contains(combined, needle)
}

func previewText(text string, maxLen int) string {
	cleaned := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if cleaned == "" {
		return "(no content)"
	}
	if len(cleaned) <= maxLen {
		return cleaned
	}
	return cleaned[:maxLen-3] + "..."
}
