package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsReturnsAbsolutePaths(t *testing.T) {
	cfg := Defaults()
	if cfg == nil {
		t.Fatalf("expected defaults config")
	}

	if cfg.Daemon.SocketPath == "" {
		t.Fatalf("expected socket path")
	}
	if cfg.Daemon.DBPath == "" {
		t.Fatalf("expected database path")
	}
	if cfg.Daemon.PIDPath == "" {
		t.Fatalf("expected pid path")
	}

	if !filepath.IsAbs(cfg.Daemon.SocketPath) {
		t.Fatalf("expected absolute socket path, got %q", cfg.Daemon.SocketPath)
	}

	if !filepath.IsAbs(cfg.Daemon.DBPath) {
		t.Fatalf("expected absolute db path, got %q", cfg.Daemon.DBPath)
	}
	if !filepath.IsAbs(cfg.Daemon.PIDPath) {
		t.Fatalf("expected absolute pid path, got %q", cfg.Daemon.PIDPath)
	}

	if cfg.LogLevel != "info" {
		t.Fatalf("expected info log level, got %q", cfg.LogLevel)
	}
}

func TestLoadUsesDefaultsWhenMissingConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Daemon.SocketPath != filepath.Join(tmpHome, ".ari", "daemon.sock") {
		t.Fatalf("unexpected socket path: %q", cfg.Daemon.SocketPath)
	}
	if cfg.Daemon.DBPath != filepath.Join(tmpHome, ".ari", "ari.db") {
		t.Fatalf("unexpected db path: %q", cfg.Daemon.DBPath)
	}
	if cfg.Daemon.PIDPath != filepath.Join(tmpHome, ".ari", "daemon.pid") {
		t.Fatalf("unexpected pid path: %q", cfg.Daemon.PIDPath)
	}
}

func TestLoadExpandsTildePaths(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	configDir := filepath.Join(tmpHome, ".ari")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}

	configBody := `{
		"daemon": {
			"socket_path": "~/.ari/custom.sock",
			"db_path": "~/.ari/custom.db",
			"pid_path": "~/.ari/custom.pid"
		},
		"log_level": "WARN"
	}`

	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Daemon.SocketPath != filepath.Join(tmpHome, ".ari", "custom.sock") {
		t.Fatalf("unexpected socket path: %q", cfg.Daemon.SocketPath)
	}
	if cfg.Daemon.DBPath != filepath.Join(tmpHome, ".ari", "custom.db") {
		t.Fatalf("unexpected db path: %q", cfg.Daemon.DBPath)
	}
	if cfg.Daemon.PIDPath != filepath.Join(tmpHome, ".ari", "custom.pid") {
		t.Fatalf("unexpected pid path: %q", cfg.Daemon.PIDPath)
	}

	if cfg.LogLevel != "warn" {
		t.Fatalf("unexpected log level: %q", cfg.LogLevel)
	}
}

func TestLoadReadsNestedEnvOverride(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("ARI_DAEMON_SOCKET_PATH", "~/env.sock")
	t.Setenv("ARI_DAEMON_DB_PATH", "~/env.db")
	t.Setenv("ARI_DAEMON_PID_PATH", "~/env.pid")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Daemon.SocketPath != filepath.Join(tmpHome, "env.sock") {
		t.Fatalf("unexpected env socket path: %q", cfg.Daemon.SocketPath)
	}
	if cfg.Daemon.DBPath != filepath.Join(tmpHome, "env.db") {
		t.Fatalf("unexpected env db path: %q", cfg.Daemon.DBPath)
	}
	if cfg.Daemon.PIDPath != filepath.Join(tmpHome, "env.pid") {
		t.Fatalf("unexpected env pid path: %q", cfg.Daemon.PIDPath)
	}
}

func TestValidateRejectsInvalidLogLevel(t *testing.T) {
	err := Validate(&Config{
		Daemon: DaemonConfig{
			SocketPath: "/tmp/daemon.sock",
			DBPath:     "/tmp/ari.db",
			PIDPath:    "/tmp/daemon.pid",
		},
		LogLevel: "verbose",
	})
	if err == nil {
		t.Fatalf("expected validation error for log level")
	}
}
