package agent

import (
	"context"
	"database/sql"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/world"
	_ "modernc.org/sqlite"
)

func TestResearcherGatherContextFiltersRelevantEntries(t *testing.T) {
	queries := setupResearchTestWorld(t)
	ctx := context.Background()

	_, err := queries.CreateDecision(ctx, world.CreateDecisionParams{
		ID:        "decision-relevant",
		Title:     "Planning loop collects constraints",
		Content:   "Capture user constraints before execution",
		CreatedAt: "2026-02-22T00:00:00Z",
		UpdatedAt: "2026-02-22T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("create relevant decision: %v", err)
	}

	_, err = queries.CreateDecision(ctx, world.CreateDecisionParams{
		ID:        "decision-irrelevant",
		Title:     "Use SQLite",
		Content:   "Store world data locally",
		CreatedAt: "2026-02-22T00:00:00Z",
		UpdatedAt: "2026-02-22T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("create irrelevant decision: %v", err)
	}

	_, err = queries.CreateKnowledge(ctx, world.CreateKnowledgeParams{
		ID:        "knowledge-relevant",
		Type:      "note",
		Name:      "Question strategy",
		Content:   sql.NullString{String: "Ask research questions in planning phase", Valid: true},
		CreatedAt: "2026-02-22T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("create relevant knowledge: %v", err)
	}

	_, err = queries.CreateKnowledge(ctx, world.CreateKnowledgeParams{
		ID:        "knowledge-irrelevant",
		Type:      "constraint",
		Name:      "No network access",
		Content:   sql.NullString{String: "Use local fixtures", Valid: true},
		CreatedAt: "2026-02-22T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("create irrelevant knowledge: %v", err)
	}

	researcher := NewResearcher(queries)
	result, err := researcher.GatherContext(ctx, "planning questions")
	if err != nil {
		t.Fatalf("gather context: %v", err)
	}

	if result.Goal != "planning questions" {
		t.Fatalf("goal = %q, want %q", result.Goal, "planning questions")
	}
	if len(result.Decisions) != 1 {
		t.Fatalf("decision count = %d, want 1", len(result.Decisions))
	}
	if result.Decisions[0].ID != "decision-relevant" {
		t.Fatalf("decision id = %q, want %q", result.Decisions[0].ID, "decision-relevant")
	}
	if len(result.Knowledge) != 1 {
		t.Fatalf("knowledge count = %d, want 1", len(result.Knowledge))
	}
	if result.Knowledge[0].ID != "knowledge-relevant" {
		t.Fatalf("knowledge id = %q, want %q", result.Knowledge[0].ID, "knowledge-relevant")
	}
	if len(result.CodeFiles) != 0 {
		t.Fatalf("code files count = %d, want 0", len(result.CodeFiles))
	}
}

func TestResearcherGatherContextReturnsQueryError(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	researcher := NewResearcher(world.New(db))
	_, err = researcher.GatherContext(context.Background(), "planning")
	if err == nil {
		t.Fatal("gather context error = nil, want query error")
	}
}

func setupResearchTestWorld(t *testing.T) *world.Queries {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE decisions (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			context TEXT,
			consequences TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE plans (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			status TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);

		CREATE TABLE knowledge (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			name TEXT NOT NULL,
			content TEXT,
			metadata TEXT,
			created_at TEXT NOT NULL
		);

		CREATE TABLE knowledge_relations (
			from_id TEXT NOT NULL,
			to_id TEXT NOT NULL,
			relation TEXT NOT NULL,
			PRIMARY KEY (from_id, to_id, relation)
		);
	`)
	if err != nil {
		t.Fatalf("create schema: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return world.New(db)
}
