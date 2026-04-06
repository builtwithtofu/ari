package config

import (
	"encoding/json"
	"errors"
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
	if cfg.VCSPreference != "auto" {
		t.Fatalf("expected auto vcs preference, got %q", cfg.VCSPreference)
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
	if cfg.VCSPreference != "auto" {
		t.Fatalf("unexpected vcs preference: %q", cfg.VCSPreference)
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
		"log_level": "WARN",
		"vcs_preference": "GIT"
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
	if cfg.VCSPreference != "git" {
		t.Fatalf("unexpected vcs preference: %q", cfg.VCSPreference)
	}
}

func TestLoadReadsNestedEnvOverride(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("ARI_DAEMON_SOCKET_PATH", "~/env.sock")
	t.Setenv("ARI_DAEMON_DB_PATH", "~/env.db")
	t.Setenv("ARI_DAEMON_PID_PATH", "~/env.pid")
	t.Setenv("ARI_VCS_PREFERENCE", "jj")

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
	if cfg.VCSPreference != "jj" {
		t.Fatalf("unexpected env vcs preference: %q", cfg.VCSPreference)
	}
}

func TestValidateRejectsInvalidLogLevel(t *testing.T) {
	err := Validate(&Config{
		Daemon: DaemonConfig{
			SocketPath: "/tmp/daemon.sock",
			DBPath:     "/tmp/ari.db",
			PIDPath:    "/tmp/daemon.pid",
		},
		LogLevel:      "verbose",
		VCSPreference: "auto",
	})
	if err == nil {
		t.Fatalf("expected validation error for log level")
	}
}

func TestValidateRejectsInvalidVCSPreference(t *testing.T) {
	err := Validate(&Config{
		Daemon: DaemonConfig{
			SocketPath: "/tmp/daemon.sock",
			DBPath:     "/tmp/ari.db",
			PIDPath:    "/tmp/daemon.pid",
		},
		LogLevel:      "info",
		VCSPreference: "mercurial",
	})
	if err == nil {
		t.Fatalf("expected validation error for vcs preference")
	}
}

func TestWriteAndReadActiveSession(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := WriteActiveSession("sess-123"); err != nil {
		t.Fatalf("WriteActiveSession returned error: %v", err)
	}

	got, err := ReadActiveSession()
	if err != nil {
		t.Fatalf("ReadActiveSession returned error: %v", err)
	}
	if got != "sess-123" {
		t.Fatalf("ReadActiveSession = %q, want %q", got, "sess-123")
	}

	if err := WriteActiveSession(""); err != nil {
		t.Fatalf("WriteActiveSession clear returned error: %v", err)
	}

	got, err = ReadActiveSession()
	if err != nil {
		t.Fatalf("ReadActiveSession after clear returned error: %v", err)
	}
	if got != "" {
		t.Fatalf("ReadActiveSession after clear = %q, want empty", got)
	}
}

func TestWriteActiveSessionPatchesOnlyActiveSessionKey(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("ARI_DAEMON_SOCKET_PATH", filepath.Join(tmpHome, "env.sock"))

	configDir := filepath.Join(tmpHome, ".ari")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	original := `{"daemon":{"socket_path":"/tmp/original.sock","db_path":"/tmp/original.db","pid_path":"/tmp/original.pid"},"log_level":"debug"}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := WriteActiveSession("sess-abc"); err != nil {
		t.Fatalf("WriteActiveSession returned error: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(configDir, "config.json"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if parsed["active_session"] != "sess-abc" {
		t.Fatalf("active_session = %v, want %q", parsed["active_session"], "sess-abc")
	}
	daemonValue, ok := parsed["daemon"].(map[string]any)
	if !ok {
		t.Fatalf("daemon config missing after patch write")
	}
	if daemonValue["socket_path"] != "/tmp/original.sock" {
		t.Fatalf("daemon.socket_path = %v, want %q", daemonValue["socket_path"], "/tmp/original.sock")
	}
}

func TestReadActiveSessionUsesEnvironmentOverride(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("ARI_ACTIVE_SESSION", "sess-env")

	got, err := ReadActiveSession()
	if err != nil {
		t.Fatalf("ReadActiveSession returned error: %v", err)
	}
	if got != "sess-env" {
		t.Fatalf("ReadActiveSession with env override = %q, want %q", got, "sess-env")
	}
}

func TestReadPersistedActiveSessionHandlesEmptyConfigFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	configDir := filepath.Join(tmpHome, ".ari")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte{}, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	got, err := ReadPersistedActiveSession()
	if err != nil {
		t.Fatalf("ReadPersistedActiveSession returned error: %v", err)
	}
	if got != "" {
		t.Fatalf("ReadPersistedActiveSession = %q, want empty", got)
	}
}

func TestLoadTreatsEmptyConfigFileAsDefaults(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	configDir := filepath.Join(tmpHome, ".ari")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte{}, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load returned nil config")
	}
	if cfg.Daemon.SocketPath == "" || cfg.Daemon.DBPath == "" || cfg.Daemon.PIDPath == "" {
		t.Fatalf("Load daemon defaults were not populated: %+v", cfg.Daemon)
	}
}

func TestLoadFallsBackWhenHomeDirectoryCannotBeResolved(t *testing.T) {
	originalUserHomeDir := osUserHomeDir
	osUserHomeDir = func() (string, error) {
		return "", errors.New("home unavailable")
	}
	t.Cleanup(func() {
		osUserHomeDir = originalUserHomeDir
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load returned nil config")
	}
	if cfg.Daemon.SocketPath == "" {
		t.Fatalf("Load returned empty daemon socket path")
	}
}
