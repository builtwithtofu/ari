package dbtest

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/testutil"

	_ "modernc.org/sqlite"
)

// NewDB opens a migrated SQLite database for tests with one connection and a
// consistent PRAGMA policy. Tests that need a Store should wrap the returned DB
// with globaldb.NewSQLStore in their own package to avoid import cycles.
func NewDB(t *testing.T, prefix string) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), fmt.Sprintf("%s-%d.db", prefix, time.Now().UnixNano()))
	if err := testutil.ApplySQLMigrations(dbPath, migrationsDir(t)); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, stmt := range []string{
		"PRAGMA journal_mode = DELETE",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("configure test sqlite %s: %v", stmt, err)
		}
	}
	t.Cleanup(func() {
		_ = db.Close()
		_ = removeIfPresent(dbPath + "-wal")
		_ = removeIfPresent(dbPath + "-shm")
	})
	return db
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve dbtest source location")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "migrations"))
}

func removeIfPresent(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
