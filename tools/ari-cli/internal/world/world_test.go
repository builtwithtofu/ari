package world

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func setupTestDB(t *testing.T) *sql.DB {
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

	return db
}

func TestDecisionCRUD(t *testing.T) {
	db := setupTestDB(t)
	q := New(db)
	ctx := context.Background()

	createdAt := now()
	updatedAt := now()

	_, err := q.CreateDecision(ctx, CreateDecisionParams{
		ID:           "decision-1",
		Title:        "Choose DB",
		Content:      "Use SQLite for local world data",
		Context:      sql.NullString{String: "CLI local storage", Valid: true},
		Consequences: sql.NullString{String: "Simple deployment", Valid: true},
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	})
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}

	decision, err := q.GetDecision(ctx, "decision-1")
	if err != nil {
		t.Fatalf("get decision: %v", err)
	}
	if decision.Title != "Choose DB" {
		t.Fatalf("unexpected decision title: %q", decision.Title)
	}

	decisions, err := q.ListDecisions(ctx)
	if err != nil {
		t.Fatalf("list decisions: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("unexpected decisions count: %d", len(decisions))
	}

	_, err = q.UpdateDecision(ctx, UpdateDecisionParams{
		ID:           "decision-1",
		Title:        "Choose embedded DB",
		Content:      "Use modern SQLite driver",
		Context:      sql.NullString{String: "No CGO", Valid: true},
		Consequences: sql.NullString{String: "Portable binaries", Valid: true},
		UpdatedAt:    now(),
	})
	if err != nil {
		t.Fatalf("update decision: %v", err)
	}

	updatedDecision, err := q.GetDecision(ctx, "decision-1")
	if err != nil {
		t.Fatalf("get updated decision: %v", err)
	}
	if updatedDecision.Title != "Choose embedded DB" {
		t.Fatalf("unexpected updated decision title: %q", updatedDecision.Title)
	}

	err = q.DeleteDecision(ctx, "decision-1")
	if err != nil {
		t.Fatalf("delete decision: %v", err)
	}

	_, err = q.GetDecision(ctx, "decision-1")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows after delete, got: %v", err)
	}
}

func TestPlanCRUD(t *testing.T) {
	db := setupTestDB(t)
	q := New(db)
	ctx := context.Background()

	_, err := q.CreatePlan(ctx, CreatePlanParams{
		ID:        "plan-1",
		Title:     "MVP rollout",
		Status:    "draft",
		Content:   "Define world model and commands",
		CreatedAt: now(),
		UpdatedAt: now(),
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	plan, err := q.GetPlan(ctx, "plan-1")
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if plan.Status != "draft" {
		t.Fatalf("unexpected plan status: %q", plan.Status)
	}

	plans, err := q.ListPlans(ctx)
	if err != nil {
		t.Fatalf("list plans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("unexpected plans count: %d", len(plans))
	}

	_, err = q.UpdatePlan(ctx, UpdatePlanParams{
		ID:        "plan-1",
		Title:     "MVP implementation",
		Status:    "active",
		Content:   "Execute core CRUD world loop",
		UpdatedAt: now(),
	})
	if err != nil {
		t.Fatalf("update plan: %v", err)
	}

	updatedPlan, err := q.GetPlan(ctx, "plan-1")
	if err != nil {
		t.Fatalf("get updated plan: %v", err)
	}
	if updatedPlan.Status != "active" {
		t.Fatalf("unexpected updated plan status: %q", updatedPlan.Status)
	}

	err = q.DeletePlan(ctx, "plan-1")
	if err != nil {
		t.Fatalf("delete plan: %v", err)
	}

	_, err = q.GetPlan(ctx, "plan-1")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows after delete, got: %v", err)
	}
}

func TestKnowledgeCRUD(t *testing.T) {
	db := setupTestDB(t)
	q := New(db)
	ctx := context.Background()

	_, err := q.CreateKnowledge(ctx, CreateKnowledgeParams{
		ID:        "knowledge-1",
		Type:      "constraint",
		Name:      "No CGO",
		Content:   sql.NullString{String: "CLI must stay portable", Valid: true},
		Metadata:  sql.NullString{String: `{"source":"tests"}`, Valid: true},
		CreatedAt: now(),
	})
	if err != nil {
		t.Fatalf("create knowledge: %v", err)
	}

	knowledge, err := q.GetKnowledge(ctx, "knowledge-1")
	if err != nil {
		t.Fatalf("get knowledge: %v", err)
	}
	if knowledge.Name != "No CGO" {
		t.Fatalf("unexpected knowledge name: %q", knowledge.Name)
	}

	knowledgeList, err := q.ListKnowledge(ctx)
	if err != nil {
		t.Fatalf("list knowledge: %v", err)
	}
	if len(knowledgeList) != 1 {
		t.Fatalf("unexpected knowledge count: %d", len(knowledgeList))
	}

	_, err = q.UpdateKnowledge(ctx, UpdateKnowledgeParams{
		ID:       "knowledge-1",
		Type:     "principle",
		Name:     "Portable runtime",
		Content:  sql.NullString{String: "Prefer pure Go dependencies", Valid: true},
		Metadata: sql.NullString{String: `{"updated":true}`, Valid: true},
	})
	if err != nil {
		t.Fatalf("update knowledge: %v", err)
	}

	updatedKnowledge, err := q.GetKnowledge(ctx, "knowledge-1")
	if err != nil {
		t.Fatalf("get updated knowledge: %v", err)
	}
	if updatedKnowledge.Type != "principle" {
		t.Fatalf("unexpected updated knowledge type: %q", updatedKnowledge.Type)
	}

	err = q.DeleteKnowledge(ctx, "knowledge-1")
	if err != nil {
		t.Fatalf("delete knowledge: %v", err)
	}

	_, err = q.GetKnowledge(ctx, "knowledge-1")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows after delete, got: %v", err)
	}
}
