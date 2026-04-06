package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Daemon        DaemonConfig `json:"daemon" mapstructure:"daemon"`
	LogLevel      string       `json:"log_level" mapstructure:"log_level"`
	VCSPreference string       `json:"vcs_preference" mapstructure:"vcs_preference"`
	ActiveSession string       `json:"active_session,omitempty" mapstructure:"active_session"`
}

type DaemonConfig struct {
	SocketPath string `json:"socket_path" mapstructure:"socket_path"`
	DBPath     string `json:"db_path" mapstructure:"db_path"`
	PIDPath    string `json:"pid_path" mapstructure:"pid_path"`
}

func Defaults() *Config {
	home := userHomeDir()
	return &Config{
		Daemon: DaemonConfig{
			SocketPath: filepath.Join(home, ".ari", "daemon.sock"),
			DBPath:     filepath.Join(home, ".ari", "ari.db"),
			PIDPath:    filepath.Join(home, ".ari", "daemon.pid"),
		},
		LogLevel:      "info",
		VCSPreference: "auto",
	}
}

func Load() (*Config, error) {
	v := viper.New()
	defaults := Defaults()

	v.SetDefault("daemon.socket_path", defaults.Daemon.SocketPath)
	v.SetDefault("daemon.db_path", defaults.Daemon.DBPath)
	v.SetDefault("daemon.pid_path", defaults.Daemon.PIDPath)
	v.SetDefault("log_level", defaults.LogLevel)
	v.SetDefault("vcs_preference", defaults.VCSPreference)
	v.SetDefault("active_session", "")

	v.SetConfigName("config")
	v.SetConfigType("json")
	v.AddConfigPath(filepath.Join(userHomeDir(), ".ari"))
	v.SetEnvPrefix("ARI")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	normalized, err := normalizeConfig(&cfg)
	if err != nil {
		return nil, err
	}

	if err := Validate(normalized); err != nil {
		return nil, err
	}

	return normalized, nil
}

func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("validate config: config is required")
	}

	if cfg.Daemon.SocketPath == "" {
		return fmt.Errorf("validate config: daemon.socket_path is required")
	}

	if cfg.Daemon.DBPath == "" {
		return fmt.Errorf("validate config: daemon.db_path is required")
	}

	if cfg.Daemon.PIDPath == "" {
		return fmt.Errorf("validate config: daemon.pid_path is required")
	}

	level := strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	vcsPreference := strings.ToLower(strings.TrimSpace(cfg.VCSPreference))
	switch level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("validate config: log_level must be one of debug, info, warn, error")
	}

	switch vcsPreference {
	case "auto", "jj", "git":
		return nil
	default:
		return fmt.Errorf("validate config: vcs_preference must be one of auto, jj, git")
	}
}

func normalizeConfig(cfg *Config) (*Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("normalize config: config is required")
	}

	socketPath, err := normalizePath(cfg.Daemon.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("normalize config: socket path: %w", err)
	}

	dbPath, err := normalizePath(cfg.Daemon.DBPath)
	if err != nil {
		return nil, fmt.Errorf("normalize config: db path: %w", err)
	}

	pidPath, err := normalizePath(cfg.Daemon.PIDPath)
	if err != nil {
		return nil, fmt.Errorf("normalize config: pid path: %w", err)
	}

	logLevel := strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	vcsPreference := strings.ToLower(strings.TrimSpace(cfg.VCSPreference))

	return &Config{
		Daemon: DaemonConfig{
			SocketPath: socketPath,
			DBPath:     dbPath,
			PIDPath:    pidPath,
		},
		LogLevel:      logLevel,
		VCSPreference: vcsPreference,
		ActiveSession: strings.TrimSpace(cfg.ActiveSession),
	}, nil
}

func ReadActiveSession() (string, error) {
	path, err := configPath()
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read active session: %w", err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("read active session: parse config: %w", err)
	}
	raw, ok := parsed["active_session"]
	if !ok {
		return "", nil
	}
	var sessionID string
	if err := json.Unmarshal(raw, &sessionID); err != nil {
		return "", fmt.Errorf("read active session: parse active_session: %w", err)
	}
	return strings.TrimSpace(sessionID), nil
}

func WriteActiveSession(sessionID string) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("write active session: mkdir config dir: %w", err)
	}

	parsed := map[string]json.RawMessage{}
	body, err := os.ReadFile(path)
	if err == nil {
		if len(body) > 0 {
			if err := json.Unmarshal(body, &parsed); err != nil {
				return fmt.Errorf("write active session: parse config: %w", err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("write active session: read config: %w", err)
	}

	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		delete(parsed, "active_session")
	} else {
		raw, err := json.Marshal(trimmed)
		if err != nil {
			return fmt.Errorf("write active session: marshal value: %w", err)
		}
		parsed["active_session"] = raw
	}

	encoded, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return fmt.Errorf("write active session: marshal config: %w", err)
	}
	encoded = append(encoded, '\n')

	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return fmt.Errorf("write active session: write file: %w", err)
	}
	return nil
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".ari", "config.json"), nil
}

func normalizePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("path is required")
	}

	if strings.HasPrefix(trimmed, "~/") {
		trimmed = filepath.Join(userHomeDir(), trimmed[2:])
	}

	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}

	return abs, nil
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}
