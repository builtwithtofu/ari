package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/plan"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/provider"
)

const (
	minimumQuestionCount = 3
	maximumQuestionCount = 5
)

var ErrQuestionerResearchRequired = errors.New("research context is required")

// Questioner generates questions based on research context.
type Questioner struct {
	provider provider.Provider
}

// NewQuestioner creates a new questioner with provider.
func NewQuestioner(provider provider.Provider) *Questioner {
	return &Questioner{provider: provider}
}

// GenerateQuestions creates questions based on research context.
func (q *Questioner) GenerateQuestions(_ context.Context, research *ResearchContext) ([]plan.Question, error) {
	if research == nil {
		return nil, ErrQuestionerResearchRequired
	}

	questions := make([]plan.Question, 0, maximumQuestionCount)

	appendQuestion := func(questionType plan.QuestionType, prompt string, context string, options []string) {
		if len(questions) >= maximumQuestionCount {
			return
		}

		question := plan.Question{
			ID:      fmt.Sprintf("q-%d", len(questions)+1),
			Type:    questionType,
			Prompt:  prompt,
			Context: context,
			Options: options,
		}
		questions = append(questions, question)
	}

	if isVagueGoal(research.Goal) {
		appendQuestion(
			plan.QuestionTypeClarification,
			"What specific functionality do you need?",
			"The planning goal is broad and needs concrete outcomes.",
			nil,
		)
	}

	if len(research.Decisions) == 0 {
		appendQuestion(
			plan.QuestionTypeClarification,
			"Do you have preferred implementation approaches, libraries, or trade-offs?",
			"No related historical decisions were found.",
			nil,
		)
	}

	if len(research.Knowledge) == 0 {
		appendQuestion(
			plan.QuestionTypeResearch,
			"Are there existing patterns in this codebase that this should follow?",
			"No related knowledge entries were found.",
			nil,
		)
	}

	appendQuestion(
		plan.QuestionTypeScope,
		"Should this include adjacent setup and test changes, or is that out of scope?",
		"Define what is explicitly in and out of scope.",
		[]string{"Include setup and tests", "Implementation only", "Not sure yet"},
	)

	appendQuestion(
		plan.QuestionTypeCriteria,
		"What would make this implementation successful?",
		"Define concrete success criteria before implementation.",
		nil,
	)

	for len(questions) < minimumQuestionCount {
		appendQuestion(
			plan.QuestionTypeClarification,
			"Are there constraints I should optimize for (performance, security, timeline)?",
			"Additional constraints help guide planning decisions.",
			nil,
		)
	}

	return questions, nil
}

func isVagueGoal(goal string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(goal))
	if trimmed == "" {
		return true
	}

	words := strings.Fields(trimmed)
	if len(words) <= 3 {
		return true
	}

	genericGoals := []string{
		"improve",
		"fix",
		"update",
		"refactor",
		"cleanup",
		"work on",
	}

	for _, generic := range genericGoals {
		if trimmed == generic || strings.HasPrefix(trimmed, generic+" ") {
			return true
		}
	}

	return false
}
