package daemon

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigrationsKeepHarnessSessionIdentityColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migration-check.db")
	if err := applyMigrationSQLFiles(dbPath); err != nil {
		t.Fatalf("applyMigrationSQLFiles returned error: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	type columnInfo struct {
		name         string
		hasDefault   bool
		defaultValue string
	}

	wantColumns := []columnInfo{
		{name: "harness"},
		{name: "provider_session_id", hasDefault: true, defaultValue: "''"},
		{name: "provider_metadata_json", hasDefault: true, defaultValue: "'{}'"},
	}

	for _, tc := range wantColumns {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.Query("PRAGMA table_info(harness_sessions)")
			if err != nil {
				t.Fatalf("PRAGMA table_info returned error: %v", err)
			}
			defer func() {
				_ = rows.Close()
			}()

			found := false
			for rows.Next() {
				var cid int
				var name string
				var colType string
				var notNull int
				var defaultValue sql.NullString
				var pk int
				if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
					t.Fatalf("rows.Scan returned error: %v", err)
				}
				if name != tc.name {
					continue
				}
				found = true
				if tc.hasDefault {
					if !defaultValue.Valid {
						t.Fatalf("column %s default is missing", tc.name)
					}
					if defaultValue.String != tc.defaultValue {
						t.Fatalf("column %s default = %q, want %q", tc.name, defaultValue.String, tc.defaultValue)
					}
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("rows.Err returned error: %v", err)
			}
			if !found {
				t.Fatalf("harness_sessions column %s not found", tc.name)
			}
		})
	}
}
