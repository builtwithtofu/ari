package daemon

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/testutil"

	_ "modernc.org/sqlite"
)

func TestMigrationsAddAgentHarnessIdentityColumns(t *testing.T) {
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
		{name: "harness_resumable_id"},
		{name: "harness_metadata", hasDefault: true, defaultValue: "'{}'"},
	}

	for _, tc := range wantColumns {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := db.Query("PRAGMA table_info(agents)")
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
				t.Fatalf("agents column %s not found", tc.name)
			}
		})
	}
}

func TestAgentHarnessIdentityMigrationPreservesExistingRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migration-upgrade.db")
	migrationsDir, err := daemonMigrationsDir()
	if err != nil {
		t.Fatalf("daemonMigrationsDir returned error: %v", err)
	}

	if err := testutil.ApplyNamedSQLMigrations(dbPath, migrationsDir,
		"202602220901_init_globaldb.sql",
		"202604012220_daemon_meta.sql",
		"202604032210_command_tracking.sql",
		"202604040757_agent_tracking.sql",
		"202604040948_agent_name_scope_fix.sql",
	); err != nil {
		t.Fatalf("applyNamedMigrations pre-upgrade returned error: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if _, err := db.Exec(`
INSERT INTO sessions (session_id, name, status, vcs_preference, origin_root, cleanup_policy, created_at, updated_at)
VALUES ('sess-1', 'alpha', 'active', 'auto', '/tmp', 'manual', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
INSERT INTO agents (agent_id, session_id, name, command, args, status, started_at)
VALUES ('agt-1', 'sess-1', 'claude', 'claude-code', '[]', 'running', '2026-01-01T00:00:00Z');
`); err != nil {
		t.Fatalf("seed pre-upgrade rows: %v", err)
	}

	if err := testutil.ApplyNamedSQLMigrations(dbPath, migrationsDir, "202604061845_agent_harness_identity.sql"); err != nil {
		t.Fatalf("applyNamedMigrations upgrade returned error: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agents WHERE agent_id = 'agt-1'`).Scan(&count); err != nil {
		t.Fatalf("query upgraded row count: %v", err)
	}
	if count != 1 {
		t.Fatalf("upgraded row count = %d, want 1", count)
	}

	var metadata string
	if err := db.QueryRow(`SELECT harness_metadata FROM agents WHERE agent_id = 'agt-1'`).Scan(&metadata); err != nil {
		t.Fatalf("query upgraded metadata default: %v", err)
	}
	if metadata != "{}" {
		t.Fatalf("harness_metadata = %q, want %q", metadata, "{}")
	}
}
