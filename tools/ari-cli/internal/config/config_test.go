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

	if !filepath.IsAbs(cfg.Daemon.SocketPath) {
		t.Fatalf("expected absolute socket path, got %q", cfg.Daemon.SocketPath)
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
			"socket_path": "~/.ari/custom.sock"
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

	if cfg.LogLevel != "warn" {
		t.Fatalf("unexpected log level: %q", cfg.LogLevel)
	}
}

func TestLoadReadsNestedEnvOverride(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("ARI_DAEMON_SOCKET_PATH", "~/env.sock")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Daemon.SocketPath != filepath.Join(tmpHome, "env.sock") {
		t.Fatalf("unexpected env socket path: %q", cfg.Daemon.SocketPath)
	}
}

func TestValidateRejectsInvalidLogLevel(t *testing.T) {
	err := Validate(&Config{
		Daemon: DaemonConfig{
			SocketPath: "/tmp/daemon.sock",
		},
		LogLevel: "verbose",
	})
	if err == nil {
		t.Fatalf("expected validation error for log level")
	}
}
