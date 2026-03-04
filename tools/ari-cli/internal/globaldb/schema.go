package globaldb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

var ErrBootstrapFailed = errors.New("globaldb bootstrap failed")

var ErrAtlasUnavailable = errors.New("atlas CLI unavailable")

const (
	ProjectIdentityKindOpaque  = "opaque_ref"
	ProjectIdentityKindRawPath = "raw_path"
)

type atlasRunner struct {
	lookPath func(string) (string, error)
	run      func(context.Context, string, ...string) error
}

func defaultAtlasRunner() atlasRunner {
	return atlasRunner{
		lookPath: exec.LookPath,
		run: func(ctx context.Context, cmd string, args ...string) error {
			command := exec.CommandContext(ctx, cmd, args...)
			if output, err := command.CombinedOutput(); err != nil {
				return fmt.Errorf("%w: atlas migrate apply failed: %v: %s", ErrBootstrapFailed, err, output)
			}
			return nil
		},
	}
}

func Bootstrap(ctx context.Context, dbPath string) error {
	return bootstrapWithAtlasRunner(ctx, dbPath, defaultAtlasRunner())
}

func bootstrapWithAtlasRunner(ctx context.Context, dbPath string, runner atlasRunner) error {
	if dbPath == "" {
		return fmt.Errorf("%w: db path is required", ErrBootstrapFailed)
	}

	absDBPath, err := filepath.Abs(dbPath)
	if err != nil {
		return fmt.Errorf("%w: resolve db path: %v", ErrBootstrapFailed, err)
	}

	if err := os.MkdirAll(filepath.Dir(absDBPath), 0o755); err != nil {
		return fmt.Errorf("%w: create db directory: %v", ErrBootstrapFailed, err)
	}

	if runner.lookPath == nil || runner.run == nil {
		return fmt.Errorf("%w: atlas runner is required", ErrBootstrapFailed)
	}

	if _, err := runner.lookPath("atlas"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("%w: %w: install atlas and ensure it is on PATH", ErrBootstrapFailed, ErrAtlasUnavailable)
		}
		return fmt.Errorf("%w: locate atlas executable: %v", ErrBootstrapFailed, err)
	}

	migrationsDir, err := atlasMigrationsDir()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBootstrapFailed, err)
	}

	if err := runner.run(ctx, "atlas", "migrate", "apply", "--url", "sqlite://"+absDBPath, "--dir", "file://"+migrationsDir); err != nil {
		return err
	}

	return nil
}

func atlasMigrationsDir() (string, error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("resolve source location")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "migrations")), nil
}
