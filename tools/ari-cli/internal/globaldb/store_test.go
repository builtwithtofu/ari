package globaldb

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

type execCall struct {
	query string
	args  []any
}

type queryCall struct {
	query string
	args  []any
}

type recordingDB struct {
	execCalls  []execCall
	queryCalls []queryCall
	queryRows  Rows
	queryErr   error
	execErr    error
}

func (r *recordingDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r.execCalls = append(r.execCalls, execCall{query: query, args: args})
	if r.execErr != nil {
		return nil, r.execErr
	}
	return testResult(1), nil
}

func (r *recordingDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	r.queryCalls = append(r.queryCalls, queryCall{query: query, args: args})
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
	items [][]any
	idx   int
	err   error
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
	return nil
}

func (t *testRows) Err() error {
	return t.err
}

func (t *testRows) Close() error {
	return nil
}

func TestSetMetaRoundTripUsesUpsertQuery(t *testing.T) {
	db := &recordingDB{}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}

	if err := store.SetMeta(context.Background(), "version", "0.3.0-dev"); err != nil {
		t.Fatalf("SetMeta returned error: %v", err)
	}

	if len(db.execCalls) != 1 {
		t.Fatalf("exec call count = %d, want 1", len(db.execCalls))
	}
	if db.execCalls[0].query != upsertMetaQuery {
		t.Fatalf("exec query = %q, want upsertMetaQuery", db.execCalls[0].query)
	}
	if len(db.execCalls[0].args) != 2 {
		t.Fatalf("exec args count = %d, want 2", len(db.execCalls[0].args))
	}
}

func TestGetMetaReturnsStoredValue(t *testing.T) {
	db := &recordingDB{queryRows: &testRows{items: [][]any{{"0.3.0-dev"}}}}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}

	value, err := store.GetMeta(context.Background(), "version")
	if err != nil {
		t.Fatalf("GetMeta returned error: %v", err)
	}
	if value != "0.3.0-dev" {
		t.Fatalf("GetMeta value = %q, want 0.3.0-dev", value)
	}

	if len(db.queryCalls) != 1 {
		t.Fatalf("query call count = %d, want 1", len(db.queryCalls))
	}
	if db.queryCalls[0].query != metaByKeyQuery {
		t.Fatalf("query = %q, want metaByKeyQuery", db.queryCalls[0].query)
	}
}

func TestGetMetaReturnsNotFoundSentinel(t *testing.T) {
	store, err := NewStore(&recordingDB{})
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}

	_, err = store.GetMeta(context.Background(), "missing")
	if err == nil {
		t.Fatal("GetMeta returned nil error for missing key")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetMeta error = %v, want ErrNotFound", err)
	}
}

func TestMetaMethodsRequireKey(t *testing.T) {
	store, err := NewStore(&recordingDB{})
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}

	_, err = store.GetMeta(context.Background(), "")
	if err == nil {
		t.Fatal("GetMeta returned nil error for empty key")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("GetMeta error = %v, want ErrInvalidInput", err)
	}

	err = store.SetMeta(context.Background(), "", "value")
	if err == nil {
		t.Fatal("SetMeta returned nil error for empty key")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("SetMeta error = %v, want ErrInvalidInput", err)
	}
}
