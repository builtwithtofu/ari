package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
	"github.com/spf13/viper"
)

var osUserHomeDir = os.UserHomeDir

type Config struct {
	Daemon          DaemonConfig `json:"daemon" mapstructure:"daemon"`
	LogLevel        string       `json:"log_level" mapstructure:"log_level"`
	VCSPreference   string       `json:"vcs_preference" mapstructure:"vcs_preference"`
	ActiveWorkspace string       `json:"active_workspace,omitempty" mapstructure:"active_workspace"`
	DefaultHarness  string       `json:"default_harness,omitempty" mapstructure:"default_harness"`
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
	v.SetDefault("active_workspace", "")
	v.SetDefault("default_harness", "")

	v.SetConfigName("config")
	v.SetConfigType("json")
	v.AddConfigPath(filepath.Join(userHomeDir(), ".ari"))
	v.SetEnvPrefix("ARI")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	cfgPath, pathErr := configPath()
	if pathErr == nil {
		configInfo, statErr := os.Stat(cfgPath)
		if statErr == nil && configInfo.Size() == 0 {
			// Treat empty config file as unset config.
		} else {
			if err := v.ReadInConfig(); err != nil {
				if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
					return nil, fmt.Errorf("read config: %w", err)
				}
			}
		}
	} else {
		if err := v.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return nil, fmt.Errorf("read config: %w", err)
			}
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
	default:
		return fmt.Errorf("validate config: vcs_preference must be one of auto, jj, git")
	}

	if harness := strings.TrimSpace(cfg.DefaultHarness); harness != "" {
		supported := supportedHarnessSet()
		if _, ok := supported[harness]; !ok {
			names := daemon.SupportedHarnesses()
			return fmt.Errorf("validate config: default_harness must be one of %s", strings.Join(names, ", "))
		}
	}

	return nil
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
		LogLevel:        logLevel,
		VCSPreference:   vcsPreference,
		ActiveWorkspace: strings.TrimSpace(cfg.ActiveWorkspace),
		DefaultHarness:  strings.TrimSpace(cfg.DefaultHarness),
	}, nil
}

func ReadDefaultHarness() (string, error) {
	cfg, err := Load()
	if err != nil {
		return "", err
	}
	if cfg == nil {
		return "", fmt.Errorf("read default harness: config is required")
	}
	return strings.TrimSpace(cfg.DefaultHarness), nil
}

func ReadActiveWorkspace() (string, error) {
	cfg, err := Load()
	if err != nil {
		return "", err
	}
	if cfg == nil {
		return "", fmt.Errorf("read active workspace: config is required")
	}
	return strings.TrimSpace(cfg.ActiveWorkspace), nil
}

func ReadPersistedActiveWorkspace() (string, error) {
	path, err := configPath()
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read persisted active workspace: %w", err)
	}
	if len(body) == 0 {
		return "", nil
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("read persisted active workspace: parse config: %w", err)
	}
	raw, ok := parsed["active_workspace"]
	if !ok {
		return "", nil
	}
	var workspaceID string
	if err := json.Unmarshal(raw, &workspaceID); err != nil {
		return "", fmt.Errorf("read persisted active workspace: parse active_workspace: %w", err)
	}
	return strings.TrimSpace(workspaceID), nil
}

func WriteActiveWorkspace(workspaceID string) error {
	return patchConfigKey("active_workspace", workspaceID, "write active workspace")
}

func WriteDefaultHarness(harness string) error {
	return patchConfigKey("default_harness", harness, "write default harness")
}

func patchConfigKey(key string, value string, op string) error {
	if key = strings.TrimSpace(key); key == "" {
		return fmt.Errorf("%s: key is required", op)
	}

	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("%s: mkdir config dir: %w", op, err)
	}

	parsed := map[string]json.RawMessage{}
	body, err := os.ReadFile(path)
	if err == nil {
		if len(body) > 0 {
			if err := json.Unmarshal(body, &parsed); err != nil {
				return fmt.Errorf("%s: parse config: %w", op, err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("%s: read config: %w", op, err)
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		delete(parsed, key)
	} else {
		raw, err := json.Marshal(trimmed)
		if err != nil {
			return fmt.Errorf("%s: marshal value: %w", op, err)
		}
		parsed[key] = raw
	}

	encoded, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return fmt.Errorf("%s: marshal config: %w", op, err)
	}
	encoded = append(encoded, '\n')

	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return fmt.Errorf("%s: write file: %w", op, err)
	}
	return nil
}

func supportedHarnessSet() map[string]struct{} {
	names := daemon.SupportedHarnesses()
	sort.Strings(names)
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}

func configPath() (string, error) {
	home, err := osUserHomeDir()
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
	home, err := osUserHomeDir()
	if err != nil {
		return "."
	}
	return home
}
