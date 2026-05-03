package globaldb

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/testutil"

	_ "modernc.org/sqlite"
)

func newGlobalDBTestStore(t *testing.T, prefix string) *Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), fmt.Sprintf("%s-%d.db", prefix, time.Now().UnixNano()))
	if err := applyGlobalDBTestSchema(dbPath); err != nil {
		t.Fatalf("apply test schema: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		t.Fatalf("set busy timeout: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	store, err := NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore returned error: %v", err)
	}

	return store
}

func applyGlobalDBTestSchema(dbPath string) error {
	migrationsDir, err := atlasMigrationsDir()
	if err != nil {
		return err
	}
	return testutil.ApplySQLMigrations(dbPath, migrationsDir)
}
