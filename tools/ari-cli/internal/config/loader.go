package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	AgentsFileName    = "agents.json"
	ProvidersFileName = "providers.json"
)

var (
	ErrMalformedJSON        = errors.New("malformed JSON")
	ErrMissingRequiredField = errors.New("missing required field")
)

type Config struct {
	Agents    []AgentConfig
	Providers []ProviderConfig
}

type AgentConfig struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type ProviderConfig struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
}

func Load(dir string) (Config, error) {
	agents, err := LoadAgents(filepath.Join(dir, AgentsFileName))
	if err != nil {
		return Config{}, err
	}

	providers, err := LoadProviders(filepath.Join(dir, ProvidersFileName))
	if err != nil {
		return Config{}, err
	}

	return Config{
		Agents:    agents,
		Providers: providers,
	}, nil
}

func LoadAgents(path string) ([]AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var agents []AgentConfig
	if err := json.Unmarshal(data, &agents); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrMalformedJSON, path, err)
	}

	for i, agent := range agents {
		if strings.TrimSpace(agent.Name) == "" {
			return nil, fmt.Errorf("%w: agents[%d].name", ErrMissingRequiredField, i)
		}
		if strings.TrimSpace(agent.Provider) == "" {
			return nil, fmt.Errorf("%w: agents[%d].provider", ErrMissingRequiredField, i)
		}
		if strings.TrimSpace(agent.Model) == "" {
			return nil, fmt.Errorf("%w: agents[%d].model", ErrMissingRequiredField, i)
		}
	}

	sort.Slice(agents, func(i, j int) bool {
		left := canonicalName(agents[i].Name)
		right := canonicalName(agents[j].Name)
		if left == right {
			return agents[i].Name < agents[j].Name
		}
		return left < right
	})

	return agents, nil
}

func LoadProviders(path string) ([]ProviderConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var providers []ProviderConfig
	if err := json.Unmarshal(data, &providers); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrMalformedJSON, path, err)
	}

	for i, provider := range providers {
		if strings.TrimSpace(provider.Name) == "" {
			return nil, fmt.Errorf("%w: providers[%d].name", ErrMissingRequiredField, i)
		}
		if strings.TrimSpace(provider.Type) == "" {
			return nil, fmt.Errorf("%w: providers[%d].type", ErrMissingRequiredField, i)
		}
	}

	sort.Slice(providers, func(i, j int) bool {
		left := canonicalName(providers[i].Name)
		right := canonicalName(providers[j].Name)
		if left == right {
			return providers[i].Name < providers[j].Name
		}
		return left < right
	})

	return providers, nil
}

func canonicalName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
