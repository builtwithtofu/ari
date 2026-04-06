package daemon

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/testutil"
)

func applyMigrationSQLFiles(dbPath string) error {
	migrationsDir, err := daemonMigrationsDir()
	if err != nil {
		return err
	}
	return testutil.ApplySQLMigrations(dbPath, migrationsDir)
}

func daemonMigrationsDir() (string, error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve source location")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations")), nil
}
