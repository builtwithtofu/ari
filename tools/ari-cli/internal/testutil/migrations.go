package testutil

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func ApplySQLMigrations(dbPath, migrationsDir string) error {
	absDBPath, err := filepath.Abs(dbPath)
	if err != nil {
		return fmt.Errorf("resolve db path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absDBPath), 0o755); err != nil {
		return fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", absDBPath)
	if err != nil {
		return fmt.Errorf("open sqlite db: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()
	if err := configureFastTestSQLite(db); err != nil {
		return err
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if err := applyMigrationFile(db, migrationsDir, entry.Name()); err != nil {
			return err
		}
	}

	return nil
}

func ApplyNamedSQLMigrations(dbPath, migrationsDir string, names ...string) error {
	absDBPath, err := filepath.Abs(dbPath)
	if err != nil {
		return fmt.Errorf("resolve db path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absDBPath), 0o755); err != nil {
		return fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", absDBPath)
	if err != nil {
		return fmt.Errorf("open sqlite db: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()
	if err := configureFastTestSQLite(db); err != nil {
		return err
	}

	for _, name := range names {
		if err := applyMigrationFile(db, migrationsDir, name); err != nil {
			return err
		}
	}
	return nil
}

func configureFastTestSQLite(db *sql.DB) error {
	for _, stmt := range []string{
		"PRAGMA journal_mode = MEMORY",
		"PRAGMA synchronous = OFF",
		"PRAGMA temp_store = MEMORY",
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("configure test sqlite %s: %w", stmt, err)
		}
	}
	return nil
}

func applyMigrationFile(db *sql.DB, migrationsDir, name string) error {
	script, err := os.ReadFile(filepath.Join(migrationsDir, name))
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}
	if _, err := db.Exec(string(script)); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	return nil
}
