package router

import (
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
)

// RouterConfig extends the base config with router-specific settings
type RouterConfig struct {
	*config.Config
	Strategy string
}

// LoadRouterConfig loads config and validates router-specific settings
func LoadRouterConfig() (*RouterConfig, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	return &RouterConfig{
		Config:   cfg,
		Strategy: cfg.Router.Strategy,
	}, nil
}
