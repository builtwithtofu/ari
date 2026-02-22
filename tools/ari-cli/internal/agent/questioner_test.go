package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/plan"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/provider"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/world"
)

func TestGenerateQuestionsVagueGoalWithNoContext(t *testing.T) {
	sim := provider.NewSimulator(provider.PlanningScenario([]string{"q1", "q2"}))
	questioner := NewQuestioner(sim)

	questions, err := questioner.GenerateQuestions(context.Background(), &ResearchContext{})
	if err != nil {
		t.Fatalf("GenerateQuestions returned error: %v", err)
	}

	if len(questions) < 3 || len(questions) > 5 {
		t.Fatalf("question count = %d, want between 3 and 5", len(questions))
	}

	assertHasType(t, questions, plan.QuestionTypeClarification)
	assertHasType(t, questions, plan.QuestionTypeScope)
	assertHasType(t, questions, plan.QuestionTypeCriteria)

	assertHasPromptContaining(t, questions, "What specific functionality do you need?")
	assertHasPromptContaining(t, questions, "out of scope")
	assertHasPromptContaining(t, questions, "implementation successful")
}

func TestGenerateQuestionsWithExistingDecisionsAsksFewerQuestions(t *testing.T) {
	sim := provider.NewSimulator(provider.PlanningScenario([]string{"q1", "q2"}))
	questioner := NewQuestioner(sim)

	research := &ResearchContext{
		Goal: "Implement role-based permissions for API endpoints",
		Decisions: []world.Decision{
			{ID: "d1", Title: "Use policy-based authorization", Content: "Prefer explicit policy checks"},
		},
		Knowledge: []world.Knowledge{
			{ID: "k1", Name: "authorization pattern"},
		},
	}

	questions, err := questioner.GenerateQuestions(context.Background(), research)
	if err != nil {
		t.Fatalf("GenerateQuestions returned error: %v", err)
	}

	if len(questions) != 3 {
		t.Fatalf("question count = %d, want 3", len(questions))
	}

	assertHasType(t, questions, plan.QuestionTypeScope)
	assertHasType(t, questions, plan.QuestionTypeCriteria)
	assertHasPromptContaining(t, questions, "out of scope")

	if hasPromptContaining(questions, "preferred implementation approaches") {
		t.Fatalf("questions unexpectedly include approach preference prompt: %#v", questions)
	}
}

func TestGenerateQuestionsRequiresResearchContext(t *testing.T) {
	sim := provider.NewSimulator(provider.PlanningScenario([]string{"q1", "q2"}))
	questioner := NewQuestioner(sim)

	questions, err := questioner.GenerateQuestions(context.Background(), nil)
	if err == nil {
		t.Fatal("GenerateQuestions returned nil error")
	}
	if !errors.Is(err, ErrQuestionerResearchRequired) {
		t.Fatalf("error = %v, want wrapped %v", err, ErrQuestionerResearchRequired)
	}
	if questions != nil {
		t.Fatalf("questions = %#v, want nil", questions)
	}
}

func assertHasType(t *testing.T, questions []plan.Question, want plan.QuestionType) {
	t.Helper()

	for _, question := range questions {
		if question.Type == want {
			return
		}
	}

	t.Fatalf("questions do not include type %q: %#v", want, questions)
}

func assertHasPromptContaining(t *testing.T, questions []plan.Question, snippet string) {
	t.Helper()

	if !hasPromptContaining(questions, snippet) {
		t.Fatalf("questions do not include prompt containing %q: %#v", snippet, questions)
	}
}

func hasPromptContaining(questions []plan.Question, snippet string) bool {
	for _, question := range questions {
		if strings.Contains(question.Prompt, snippet) {
			return true
		}
	}

	return false
}
