package config

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/viper"
	"github.com/xeipuuv/gojsonschema"
)

type Config struct {
	Router    RouterConfig              `json:"router" mapstructure:"router"`
	Models    ModelConfig               `json:"models" mapstructure:"models"`
	Providers map[string]ProviderConfig `json:"providers,omitempty" mapstructure:"providers"`
}

type RouterConfig struct {
	Strategy    string  `json:"strategy" mapstructure:"strategy"`
	DailyBudget float64 `json:"daily_budget" mapstructure:"daily_budget"`
}

type ModelConfig struct {
	Default string `json:"default" mapstructure:"default"`
	Edits   string `json:"edits" mapstructure:"edits"`
	Review  string `json:"review" mapstructure:"review"`
}

type ProviderConfig struct {
	BaseURL   string `json:"base_url,omitempty" mapstructure:"base_url"`
	APIKeyEnv string `json:"api_key_env,omitempty" mapstructure:"api_key_env"`
}

func Defaults() *Config {
	return &Config{
		Router: RouterConfig{
			Strategy:    "balanced",
			DailyBudget: 10.00,
		},
		Models: ModelConfig{
			Default: "openai/gpt-4o",
			Edits:   "openai/gpt-4o-mini",
			Review:  "anthropic/claude-opus-4-5",
		},
	}
}

func Load() (*Config, error) {
	v := viper.New()

	defaults := Defaults()
	v.SetDefault("router.strategy", defaults.Router.Strategy)
	v.SetDefault("router.daily_budget", defaults.Router.DailyBudget)
	v.SetDefault("models.default", defaults.Models.Default)
	v.SetDefault("models.edits", defaults.Models.Edits)
	v.SetDefault("models.review", defaults.Models.Review)

	v.SetConfigName("config")
	v.SetConfigType("json")
	v.AddConfigPath("$HOME/.ari")
	v.AddConfigPath(".")

	v.SetEnvPrefix("ARI")
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

	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

func Validate(cfg *Config) error {
	schema, err := Schema()
	if err != nil {
		return err
	}

	data, _ := json.Marshal(cfg)
	result, err := schema.Validate(gojsonschema.NewBytesLoader(data))
	if err != nil {
		return err
	}

	if !result.Valid() {
		var errs []string
		for _, err := range result.Errors() {
			errs = append(errs, err.String())
		}
		return fmt.Errorf("config validation failed: %v", errs)
	}

	return nil
}
