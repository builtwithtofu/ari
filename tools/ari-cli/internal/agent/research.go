package agent

import (
	"context"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/world"
)

// ResearchContext is the planning context gathered from the world database.
type ResearchContext struct {
	Goal      string
	Decisions []world.Decision
	Knowledge []world.Knowledge
	CodeFiles []string
}

// Researcher gathers context for planning.
type Researcher struct {
	world *world.Queries
}

// NewResearcher creates a new researcher with world DB access.
func NewResearcher(worldQueries *world.Queries) *Researcher {
	return &Researcher{world: worldQueries}
}

// GatherContext collects relevant information for a planning goal.
func (r *Researcher) GatherContext(ctx context.Context, goal string) (*ResearchContext, error) {
	decisions, err := r.world.ListDecisions(ctx)
	if err != nil {
		return nil, err
	}

	knowledge, err := r.world.ListKnowledge(ctx)
	if err != nil {
		return nil, err
	}

	context := &ResearchContext{
		Goal:      goal,
		Decisions: filterDecisions(goal, decisions),
		Knowledge: filterKnowledge(goal, knowledge),
		CodeFiles: []string{},
	}

	return context, nil
}

func filterDecisions(goal string, decisions []world.Decision) []world.Decision {
	if len(decisions) == 0 {
		return nil
	}

	relevant := make([]world.Decision, 0, len(decisions))
	for _, decision := range decisions {
		if isDecisionRelevant(goal, decision) {
			relevant = append(relevant, decision)
		}
	}

	return relevant
}

func filterKnowledge(goal string, knowledge []world.Knowledge) []world.Knowledge {
	if len(knowledge) == 0 {
		return nil
	}

	relevant := make([]world.Knowledge, 0, len(knowledge))
	for _, entry := range knowledge {
		if isKnowledgeRelevant(goal, entry) {
			relevant = append(relevant, entry)
		}
	}

	return relevant
}

func isDecisionRelevant(goal string, decision world.Decision) bool {
	return anyKeywordMatch(goal, decision.Title, decision.Content)
}

func isKnowledgeRelevant(goal string, entry world.Knowledge) bool {
	content := ""
	if entry.Content.Valid {
		content = entry.Content.String
	}

	return anyKeywordMatch(goal, entry.Name, content)
}

func anyKeywordMatch(goal string, fields ...string) bool {
	goal = strings.TrimSpace(strings.ToLower(goal))
	if goal == "" {
		return false
	}

	if containsAnyField(goal, fields) {
		return true
	}

	for _, keyword := range strings.Fields(goal) {
		if len(keyword) < 3 {
			continue
		}
		if containsAnyField(keyword, fields) {
			return true
		}
	}

	return false
}

func containsAnyField(needle string, fields []string) bool {
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), needle) {
			return true
		}
	}

	return false
}
