package globaldb

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapFailsWhenAtlasCLIIsMissing(t *testing.T) {
	err := bootstrapWithAtlasRunner(context.Background(), "/tmp/ari.db", atlasRunner{
		lookPath: func(string) (string, error) {
			return "", exec.ErrNotFound
		},
		run: func(context.Context, string, ...string) error {
			t.Fatal("run should not be called when atlas is missing")
			return nil
		},
	})

	if err == nil {
		t.Fatal("bootstrap returned nil error when atlas is missing")
	}
	if !errors.Is(err, ErrBootstrapFailed) {
		t.Fatalf("bootstrap error = %v, want ErrBootstrapFailed", err)
	}
	if !errors.Is(err, ErrAtlasUnavailable) {
		t.Fatalf("bootstrap error = %v, want ErrAtlasUnavailable", err)
	}
	want := "globaldb bootstrap failed: atlas CLI unavailable: install atlas and ensure it is on PATH"
	if err.Error() != want {
		t.Fatalf("bootstrap error message = %q, want %q", err.Error(), want)
	}
}

func TestBootstrapUsesAtlasMigrateApplyForPath(t *testing.T) {
	var gotCmd string
	var gotArgs []string

	err := bootstrapWithAtlasRunner(context.Background(), "/tmp/custom.db", atlasRunner{
		lookPath: func(name string) (string, error) {
			if name != "atlas" {
				t.Fatalf("lookPath name = %q, want atlas", name)
			}
			return "/usr/bin/atlas", nil
		},
		run: func(_ context.Context, cmd string, args ...string) error {
			gotCmd = cmd
			gotArgs = append([]string(nil), args...)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("bootstrap returned error: %v", err)
	}

	if gotCmd != "atlas" {
		t.Fatalf("command = %q, want atlas", gotCmd)
	}

	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "migrate apply") {
		t.Fatalf("atlas args = %q, want migrate apply", joined)
	}
	if !strings.Contains(joined, "--url sqlite:///tmp/custom.db") {
		t.Fatalf("atlas args = %q, want --url sqlite:///tmp/custom.db", joined)
	}

	migrationsDir, err := atlasMigrationsDir()
	if err != nil {
		t.Fatalf("atlasMigrationsDir: %v", err)
	}

	if !strings.Contains(joined, "--dir file://"+migrationsDir) {
		t.Fatalf("atlas args = %q, want --dir file://%s", joined, migrationsDir)
	}
}

func TestBootstrapRequiresPath(t *testing.T) {
	err := bootstrapWithAtlasRunner(context.Background(), "", atlasRunner{})
	if err == nil {
		t.Fatal("bootstrap returned nil error for empty db path")
	}
	if !errors.Is(err, ErrBootstrapFailed) {
		t.Fatalf("bootstrap error = %v, want ErrBootstrapFailed", err)
	}
}

func TestBootstrapNormalizesRelativeDBPath(t *testing.T) {
	var gotArgs []string

	err := bootstrapWithAtlasRunner(context.Background(), "ari-relative.db", atlasRunner{
		lookPath: func(name string) (string, error) {
			if name != "atlas" {
				t.Fatalf("lookPath name = %q, want atlas", name)
			}
			return "/usr/bin/atlas", nil
		},
		run: func(_ context.Context, _ string, args ...string) error {
			gotArgs = append([]string(nil), args...)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("bootstrap returned error: %v", err)
	}

	absPath, err := filepath.Abs("ari-relative.db")
	if err != nil {
		t.Fatalf("resolve abs path: %v", err)
	}

	if !strings.Contains(strings.Join(gotArgs, " "), "--url sqlite://"+absPath) {
		t.Fatalf("atlas args = %q, want --url sqlite://%s", strings.Join(gotArgs, " "), absPath)
	}
}
