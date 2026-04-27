package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInitMethodsExposeOptionsStateAndApplyThroughRPC(t *testing.T) {
	stubBootstrap(t)

	configPath := filepath.Join(t.TempDir(), "config.json")
	socketPath := testSocketPath(t)
	dbPath := filepath.Join(t.TempDir(), "ari.db")
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	d := New(socketPath, dbPath, pidPath, configPath, "test", "test-version")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Start(ctx)
	}()

	var options InitOptionsResponse
	callDaemonMethod(t, socketPath, "init.options", InitOptionsRequest{}, &options)
	if len(options.Harnesses) != 3 {
		t.Fatalf("unexpected harness options: %#v", options.Harnesses)
	}
	if options.Harnesses[0].Name != "claude-code" || options.Harnesses[1].Name != "codex" || options.Harnesses[2].Name != "opencode" {
		t.Fatalf("unexpected harness order: %#v", options.Harnesses)
	}

	var before InitStateResponse
	callDaemonMethod(t, socketPath, "init.state", InitStateRequest{}, &before)
	if before.Initialized || before.DefaultHarness != "" || before.SystemWorkspaceReady || before.SystemHelperReady {
		t.Fatalf("unexpected initial state: %#v", before)
	}

	var applied InitApplyResponse
	callDaemonMethod(t, socketPath, "init.apply", InitApplyRequest{Harness: "codex"}, &applied)
	if !applied.Initialized || applied.DefaultHarness != "codex" || !applied.DefaultHarnessSet || !applied.SystemHelperReady {
		t.Fatalf("unexpected apply response: %#v", applied)
	}

	var after InitStateResponse
	callDaemonMethod(t, socketPath, "init.state", InitStateRequest{}, &after)
	if !after.Initialized || after.DefaultHarness != "codex" || !after.SystemWorkspaceReady || !after.SystemHelperReady {
		t.Fatalf("unexpected state after apply: %#v", after)
	}

	var persisted map[string]string
	if err := readJSONFile(configPath, &persisted); err != nil {
		t.Fatalf("read config: %v", err)
	}
	if persisted["default_harness"] != "codex" {
		t.Fatalf("default_harness = %q, want codex", persisted["default_harness"])
	}

	var stop StopResponse
	tryStopDaemonMethod(t, socketPath, &stop)
	if err := <-errCh; err != nil {
		t.Fatalf("daemon start returned error: %v", err)
	}
}

func tryStopDaemonMethod(t *testing.T, socketPath string, stop *StopResponse) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := tryDaemonMethod(ctx, socketPath, "daemon.stop", StopRequest{}, stop); err != nil && !strings.Contains(err.Error(), "connection is closed") {
		t.Fatalf("call daemon.stop: %v", err)
	}
}

func TestInitApplyRejectsInvalidHarnessAndPreservesConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"preferred_model":"keep-me","default_harness":"codex"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/ari.sock", "/tmp/ari.db", "/tmp/ari.pid", configPath, "test", "test-version")

	_, err := d.applyInit(context.Background(), nil, InitApplyRequest{Harness: "unknown"})
	if err == nil {
		t.Fatal("applyInit returned nil error for invalid harness")
	}

	var persisted map[string]string
	if readErr := readJSONFile(configPath, &persisted); readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	if persisted["default_harness"] != "codex" || persisted["preferred_model"] != "keep-me" {
		t.Fatalf("config was not preserved: %#v", persisted)
	}
}

func readJSONFile(path string, out any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}
