package globaldb

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

type execCall struct {
	query string
	args  []any
}

type recordingDB struct {
	execCalls    []execCall
	queryRows    Rows
	queryErr     error
	rowsAffected int64
}

func (r *recordingDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r.execCalls = append(r.execCalls, execCall{query: query, args: args})
	if r.rowsAffected == 0 {
		return testResult(0), nil
	}
	return testResult(r.rowsAffected), nil
}

func (r *recordingDB) QueryContext(_ context.Context, _ string, _ ...any) (Rows, error) {
	if r.queryErr != nil {
		return nil, r.queryErr
	}
	if r.queryRows == nil {
		return &testRows{}, nil
	}
	return r.queryRows, nil
}

type testResult int64

func (t testResult) LastInsertId() (int64, error) {
	return int64(t), nil
}

func (t testResult) RowsAffected() (int64, error) {
	return int64(t), nil
}

type testRows struct {
	items  [][]any
	idx    int
	scanOK bool
	err    error
}

func (t *testRows) Next() bool {
	if t.idx >= len(t.items) {
		return false
	}
	t.idx++
	return true
}

func (t *testRows) Scan(dest ...any) error {
	if t.err != nil {
		return t.err
	}
	if t.idx == 0 || t.idx > len(t.items) {
		return errors.New("scan out of range")
	}
	row := t.items[t.idx-1]
	if len(row) != len(dest) {
		return errors.New("scan arg count mismatch")
	}
	for i := range row {
		s, ok := dest[i].(*string)
		if !ok {
			return errors.New("unsupported destination type")
		}
		v, ok := row[i].(string)
		if !ok {
			return errors.New("unsupported row value type")
		}
		*s = v
	}
	t.scanOK = true
	return nil
}

func (t *testRows) Err() error {
	return t.err
}

func (t *testRows) Close() error {
	return nil
}

func TestUpsertProjectRejectsRawAbsolutePathByDefault(t *testing.T) {
	s, err := NewStore(&recordingDB{})
	if err != nil {
		t.Fatalf("new store returned error: %v", err)
	}

	err = s.UpsertProject(context.Background(), Project{
		ProjectID:       "proj-1",
		ProjectIdentity: "/tmp/repo",
		CreatedAt:       "2026-02-22T10:00:00Z",
		UpdatedAt:       "2026-02-22T10:00:00Z",
	})
	if err == nil {
		t.Fatal("upsert project with raw absolute path returned nil error")
	}
	if !strings.Contains(err.Error(), "non-raw") {
		t.Fatalf("upsert project error = %v, want non-raw identity error", err)
	}
}

func TestUpsertProjectRequiresIdentityKindToBeKnownValue(t *testing.T) {
	s, err := NewStore(&recordingDB{})
	if err != nil {
		t.Fatalf("new store returned error: %v", err)
	}

	err = s.UpsertProject(context.Background(), Project{
		ProjectID:       "proj-1",
		ProjectIdentity: "project-ref-123",
		IdentityKind:    "other",
		CreatedAt:       "2026-02-22T10:00:00Z",
		UpdatedAt:       "2026-02-22T10:00:00Z",
	})
	if err == nil {
		t.Fatal("upsert project with invalid identity kind returned nil error")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("upsert project error = %v, want ErrInvalidInput", err)
	}
}

func TestUpsertProjectDefaultsToOpaqueIdentityKind(t *testing.T) {
	db := &recordingDB{}
	s, err := NewStore(db)
	if err != nil {
		t.Fatalf("new store returned error: %v", err)
	}

	err = s.UpsertProject(context.Background(), Project{
		ProjectID:       "proj-1",
		ProjectIdentity: "project-ref-123",
		CreatedAt:       "2026-02-22T10:00:00Z",
		UpdatedAt:       "2026-02-22T10:00:00Z",
	})
	if err != nil {
		t.Fatalf("upsert project returned error: %v", err)
	}

	if len(db.execCalls) != 1 {
		t.Fatalf("exec call count = %d, want 1", len(db.execCalls))
	}
	if len(db.execCalls[0].args) != 5 {
		t.Fatalf("upsert args count = %d, want 5", len(db.execCalls[0].args))
	}

	kindArg, ok := db.execCalls[0].args[2].(string)
	if !ok {
		t.Fatalf("identity kind arg type = %T, want string", db.execCalls[0].args[2])
	}
	if kindArg != ProjectIdentityKindOpaque {
		t.Fatalf("identity kind arg = %q, want %q", kindArg, ProjectIdentityKindOpaque)
	}
}

func TestUpsertProjectAllowsRawPathOnlyWhenExplicit(t *testing.T) {
	s, err := NewStore(&recordingDB{})
	if err != nil {
		t.Fatalf("new store returned error: %v", err)
	}

	err = s.UpsertProject(context.Background(), Project{
		ProjectID:       "proj-1",
		ProjectIdentity: "/tmp/repo",
		IdentityKind:    ProjectIdentityKindRawPath,
		CreatedAt:       "2026-02-22T10:00:00Z",
		UpdatedAt:       "2026-02-22T10:00:00Z",
	})
	if err != nil {
		t.Fatalf("upsert project with explicit raw path returned error: %v", err)
	}
}

func TestGetProjectByIDReturnsNotFoundSentinel(t *testing.T) {
	s, err := NewStore(&recordingDB{})
	if err != nil {
		t.Fatalf("new store returned error: %v", err)
	}

	_, err = s.GetProjectByID(context.Background(), "missing")
	if err == nil {
		t.Fatal("get project by id returned nil error for missing record")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("get project by id error = %v, want ErrNotFound", err)
	}
}

func TestGetSessionByIDReturnsNotFoundSentinel(t *testing.T) {
	s, err := NewStore(&recordingDB{})
	if err != nil {
		t.Fatalf("new store returned error: %v", err)
	}

	_, err = s.GetSessionByID(context.Background(), "missing")
	if err == nil {
		t.Fatal("get session by id returned nil error for missing record")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("get session by id error = %v, want ErrNotFound", err)
	}
}

func TestDeleteSessionByIDReturnsNotFoundSentinel(t *testing.T) {
	s, err := NewStore(&recordingDB{rowsAffected: 0})
	if err != nil {
		t.Fatalf("new store returned error: %v", err)
	}

	err = s.DeleteSessionByID(context.Background(), "missing")
	if err == nil {
		t.Fatal("delete session by id returned nil error for missing record")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete session by id error = %v, want ErrNotFound", err)
	}
}

func TestProjectSchemaAndQueriesUseIdentityContract(t *testing.T) {
	if strings.Contains(upsertProjectQuery, "root_path") || strings.Contains(projectByIdentityQuery, "root_path") || strings.Contains(listProjectsQuery, "root_path") {
		t.Fatal("project queries still reference root_path")
	}
	if !strings.Contains(projectByIdentityQuery, "ORDER BY created_at ASC, project_id ASC") {
		t.Fatal("project by identity query missing deterministic order")
	}
	if !strings.Contains(listProjectsQuery, "ORDER BY created_at ASC, project_id ASC") {
		t.Fatal("list projects query missing deterministic order")
	}
}

func TestSessionListQueryHasDeterministicOrder(t *testing.T) {
	if !strings.Contains(listSessionsByProjectQuery, "ORDER BY created_at ASC, session_id ASC") {
		t.Fatal("list sessions by project query missing deterministic order")
	}
}
