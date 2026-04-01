package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Daemon   DaemonConfig `json:"daemon" mapstructure:"daemon"`
	LogLevel string       `json:"log_level" mapstructure:"log_level"`
}

type DaemonConfig struct {
	SocketPath string `json:"socket_path" mapstructure:"socket_path"`
}

func Defaults() *Config {
	home := userHomeDir()
	return &Config{
		Daemon: DaemonConfig{
			SocketPath: filepath.Join(home, ".ari", "daemon.sock"),
		},
		LogLevel: "info",
	}
}

func Load() (*Config, error) {
	v := viper.New()
	defaults := Defaults()

	v.SetDefault("daemon.socket_path", defaults.Daemon.SocketPath)
	v.SetDefault("log_level", defaults.LogLevel)

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

	level := strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	switch level {
	case "debug", "info", "warn", "error":
		return nil
	default:
		return fmt.Errorf("validate config: log_level must be one of debug, info, warn, error")
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

	logLevel := strings.ToLower(strings.TrimSpace(cfg.LogLevel))

	return &Config{
		Daemon: DaemonConfig{
			SocketPath: socketPath,
		},
		LogLevel: logLevel,
	}, nil
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
