package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRootRegistersDaemonCommand(t *testing.T) {
	root := NewRootCmd()
	daemonCmd, _, err := root.Find([]string{"daemon"})
	if err != nil {
		t.Fatalf("find daemon command: %v", err)
	}

	if daemonCmd == nil {
		t.Fatalf("expected daemon command to be registered")
	}

	if daemonCmd.Name() != "daemon" {
		t.Fatalf("unexpected command name: %q", daemonCmd.Name())
	}
}

func TestDaemonSubcommandsExist(t *testing.T) {
	daemon := NewDaemonCmd()

	start, _, err := daemon.Find([]string{"start"})
	if err != nil {
		t.Fatalf("find daemon start: %v", err)
	}

	stop, _, err := daemon.Find([]string{"stop"})
	if err != nil {
		t.Fatalf("find daemon stop: %v", err)
	}

	status, _, err := daemon.Find([]string{"status"})
	if err != nil {
		t.Fatalf("find daemon status: %v", err)
	}

	if start == nil || stop == nil || status == nil {
		t.Fatalf("expected daemon start/stop/status commands")
	}
}

func TestDaemonStatusWhenUnavailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	out, err := executeRootCommand("daemon", "status")
	if err != nil {
		t.Fatalf("execute daemon status: %v", err)
	}

	if strings.TrimSpace(out) != "Daemon is not running" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestDaemonStopWhenUnavailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	out, err := executeRootCommand("daemon", "stop")
	if err != nil {
		t.Fatalf("execute daemon stop: %v", err)
	}

	if strings.TrimSpace(out) != "Daemon is not running" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestConfiguredSocketPathReadsConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ariDir := filepath.Join(home, ".ari")
	if err := os.MkdirAll(ariDir, 0o755); err != nil {
		t.Fatalf("create .ari dir: %v", err)
	}

	configBody := `{
		"daemon": {
			"socket_path": "~/.ari/custom.sock",
			"db_path": "~/.ari/custom.db"
		},
		"log_level": "info"
	}`

	if err := os.WriteFile(filepath.Join(ariDir, "config.json"), []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := configuredDaemonConfig()
	if err != nil {
		t.Fatalf("configuredDaemonConfig: %v", err)
	}

	want := filepath.Join(home, ".ari", "custom.sock")
	if cfg.Daemon.SocketPath != want {
		t.Fatalf("configured socket path = %q, want %q", cfg.Daemon.SocketPath, want)
	}

	wantDB := filepath.Join(home, ".ari", "custom.db")
	if cfg.Daemon.DBPath != wantDB {
		t.Fatalf("configured db path = %q, want %q", cfg.Daemon.DBPath, wantDB)
	}
}

func TestConfiguredDaemonConfigWithSourceUsesEnvironmentLabel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_DAEMON_SOCKET_PATH", "~/env.sock")
	t.Setenv("ARI_DAEMON_DB_PATH", "~/env.db")

	cfg, _, source, err := configuredDaemonConfigWithSource()
	if err != nil {
		t.Fatalf("configuredDaemonConfigWithSource: %v", err)
	}

	if source != "environment" {
		t.Fatalf("config source = %q, want environment", source)
	}

	if cfg.Daemon.SocketPath != filepath.Join(home, "env.sock") {
		t.Fatalf("configured socket path = %q, want env override", cfg.Daemon.SocketPath)
	}
	if cfg.Daemon.DBPath != filepath.Join(home, "env.db") {
		t.Fatalf("configured db path = %q, want env override", cfg.Daemon.DBPath)
	}
}

func TestDaemonStatusAndStopHappyPath(t *testing.T) {
	requireAtlas(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	ariDir := filepath.Join(home, ".ari")
	if err := os.MkdirAll(ariDir, 0o755); err != nil {
		t.Fatalf("create .ari dir: %v", err)
	}

	configPath := filepath.Join(ariDir, "config.json")
	configBody := `{
		"daemon": {
			"socket_path": "~/.ari/custom.sock",
			"db_path": "~/.ari/custom.db"
		},
		"log_level": "info"
	}`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	dbPath := filepath.Join(home, ".ari", "custom.db")
	socketPath := filepath.Join(home, ".ari", "custom.sock")

	startOut := make(chan string, 1)
	errCh := make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		output, err := executeRootCommandWithContext(ctx, "daemon", "start")
		startOut <- output
		errCh <- err
	}()

	statusOut := ""
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case runErr := <-errCh:
			startOutput := <-startOut
			t.Fatalf("daemon start exited early: %v; output=%q", runErr, startOutput)
		default:
		}

		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for daemon status")
		}

		out, err := executeRootCommand("daemon", "status")
		if err == nil && strings.Contains(out, "Daemon: running") {
			statusOut = out
			break
		}

		time.Sleep(25 * time.Millisecond)
	}

	if !strings.Contains(statusOut, "Daemon: running") ||
		!strings.Contains(statusOut, "Version:") ||
		!strings.Contains(statusOut, "PID:") ||
		!strings.Contains(statusOut, "Uptime:") ||
		!strings.Contains(statusOut, "Socket:") ||
		!strings.Contains(statusOut, "Database:") ||
		!strings.Contains(statusOut, "Config:") ||
		!strings.Contains(statusOut, "Config Source:") {
		t.Fatalf("unexpected status output: %q", statusOut)
	}

	if !strings.Contains(statusOut, "Socket: "+socketPath) {
		t.Fatalf("status output = %q, want configured socket path", statusOut)
	}
	if !strings.Contains(statusOut, "Database: "+dbPath+" (healthy)") {
		t.Fatalf("status output = %q, want healthy configured database path", statusOut)
	}
	if !strings.Contains(statusOut, "Config: "+configPath) {
		t.Fatalf("status output = %q, want config path", statusOut)
	}
	if !strings.Contains(statusOut, "Config Source: file") {
		t.Fatalf("status output = %q, want file config source", statusOut)
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("stat bootstrapped database path: %v", err)
	}

	stopOut, err := executeRootCommand("daemon", "stop")
	if err != nil {
		t.Fatalf("execute daemon stop: %v", err)
	}
	if strings.TrimSpace(stopOut) != "Daemon stopping" {
		t.Fatalf("unexpected stop output: %q", stopOut)
	}

	select {
	case runErr := <-errCh:
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			t.Fatalf("daemon start command error: %v", runErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for daemon start command to exit")
	}

	if out := <-startOut; !strings.Contains(out, "Ari daemon starting") {
		t.Fatalf("unexpected start output: %q", out)
	}
}

func requireAtlas(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("atlas"); err != nil {
		t.Skip("atlas CLI is required for daemon bootstrap tests")
	}
}

func executeRootCommand(args ...string) (string, error) {
	return executeRootCommandWithContext(context.Background(), args...)
}

func executeRootCommandWithContext(ctx context.Context, args ...string) (string, error) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetContext(ctx)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}
