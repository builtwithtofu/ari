package globaldb

import (
	"context"
	"database/sql"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestBootstrapFailsWhenAtlasCLIIsMissing(t *testing.T) {
	err := bootstrapWithAtlasRunner(context.Background(), &sql.DB{}, atlasRunner{
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
	if !strings.Contains(err.Error(), "install atlas") {
		t.Fatalf("bootstrap error = %v, want actionable install guidance", err)
	}
}

func TestBootstrapUsesAtlasMigrateApplyForGlobalDB(t *testing.T) {
	var gotCmd string
	var gotArgs []string

	err := bootstrapWithAtlasRunner(context.Background(), &sql.DB{}, atlasRunner{
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
	if !strings.Contains(joined, "--env globaldb") {
		t.Fatalf("atlas args = %q, want --env globaldb", joined)
	}
	if !strings.Contains(joined, "--config ") {
		t.Fatalf("atlas args = %q, want --config", joined)
	}
}

func TestBootstrapCommandContractIsStableAcrossRepeatedRuns(t *testing.T) {
	type call struct {
		cmd  string
		args []string
	}

	var calls []call

	runner := atlasRunner{
		lookPath: func(name string) (string, error) {
			if name != "atlas" {
				t.Fatalf("lookPath name = %q, want atlas", name)
			}
			return "/usr/bin/atlas", nil
		},
		run: func(_ context.Context, cmd string, args ...string) error {
			calls = append(calls, call{cmd: cmd, args: append([]string(nil), args...)})
			return nil
		},
	}

	for i := 0; i < 2; i++ {
		if err := bootstrapWithAtlasRunner(context.Background(), &sql.DB{}, runner); err != nil {
			t.Fatalf("bootstrap run %d returned error: %v", i+1, err)
		}
	}

	if len(calls) != 2 {
		t.Fatalf("bootstrap call count = %d, want 2", len(calls))
	}

	wantConfigPath, err := atlasConfigPath()
	if err != nil {
		t.Fatalf("atlasConfigPath() error = %v", err)
	}
	wantArgs := []string{"migrate", "apply", "--env", "globaldb", "--config", wantConfigPath}

	if calls[0].cmd != "atlas" || calls[1].cmd != "atlas" {
		t.Fatalf("commands = [%q, %q], want [\"atlas\", \"atlas\"]", calls[0].cmd, calls[1].cmd)
	}

	if !reflect.DeepEqual(calls[0].args, calls[1].args) {
		t.Fatalf("atlas args differ across repeated runs: first=%q second=%q", strings.Join(calls[0].args, " "), strings.Join(calls[1].args, " "))
	}
	if !reflect.DeepEqual(calls[0].args, wantArgs) {
		t.Fatalf("atlas args = %q, want %q", strings.Join(calls[0].args, " "), strings.Join(wantArgs, " "))
	}
}

func TestBootstrapRequiresDB(t *testing.T) {
	err := bootstrapWithAtlasRunner(context.Background(), nil, atlasRunner{})
	if err == nil {
		t.Fatal("bootstrap returned nil error for nil db")
	}
	if !errors.Is(err, ErrBootstrapFailed) {
		t.Fatalf("bootstrap error = %v, want ErrBootstrapFailed", err)
	}
}
