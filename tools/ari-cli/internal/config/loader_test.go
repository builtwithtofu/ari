package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"os"
)

func TestLoadSuccess(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, AgentsFileName), `[
		{"name":"researcher","provider":"openai","model":"gpt-4.1-mini"},
		{"name":"planner","provider":"anthropic","model":"claude-3-5-sonnet"}
	]`)
	writeFile(t, filepath.Join(dir, ProvidersFileName), `[
		{"name":"openai","type":"openai","api_key_env":"OPENAI_API_KEY"},
		{"name":"anthropic","type":"anthropic","api_key_env":"ANTHROPIC_API_KEY"}
	]`)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load returned error: %v", err)
	}

	if len(cfg.Agents) != 2 {
		t.Fatalf("agents len = %d, want 2", len(cfg.Agents))
	}
	if len(cfg.Providers) != 2 {
		t.Fatalf("providers len = %d, want 2", len(cfg.Providers))
	}

	if cfg.Agents[0].Name != "planner" || cfg.Agents[1].Name != "researcher" {
		t.Fatalf("agents order = [%s, %s], want [planner, researcher]", cfg.Agents[0].Name, cfg.Agents[1].Name)
	}
}

func TestLoadAgentsMalformedJSON(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, AgentsFileName), `[{"name":"planner"`)
	writeFile(t, filepath.Join(dir, ProvidersFileName), `[{"name":"openai","type":"openai"}]`)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("load returned nil error for malformed agents JSON")
	}
	if !errors.Is(err, ErrMalformedJSON) {
		t.Fatalf("error = %v, want malformed JSON error", err)
	}
}

func TestLoadProvidersMissingRequiredField(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, AgentsFileName), `[{"name":"planner","provider":"openai","model":"gpt-4.1-mini"}]`)
	writeFile(t, filepath.Join(dir, ProvidersFileName), `[{"name":"openai"}]`)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("load returned nil error for missing required field")
	}
	if !errors.Is(err, ErrMissingRequiredField) {
		t.Fatalf("error = %v, want missing required field error", err)
	}

	want := "missing required field: providers[0].type"
	if err.Error() != want {
		t.Fatalf("error message = %q, want %q", err.Error(), want)
	}
}

func TestLoadDeterministicOrdering(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	agentsA := `[
		{"name":"zeta","provider":"openai","model":"m1"},
		{"name":" Planner ","provider":"openai","model":"m2"},
		{"name":"alpha","provider":"openai","model":"m3"}
	]`
	agentsB := `[
		{"name":"alpha","provider":"openai","model":"m3"},
		{"name":"zeta","provider":"openai","model":"m1"},
		{"name":" Planner ","provider":"openai","model":"m2"}
	]`
	providersA := `[
		{"name":"zai","type":"openai-compatible"},
		{"name":" OpenAI ","type":"openai"}
	]`
	providersB := `[
		{"name":" OpenAI ","type":"openai"},
		{"name":"zai","type":"openai-compatible"}
	]`

	writeFile(t, filepath.Join(dirA, AgentsFileName), agentsA)
	writeFile(t, filepath.Join(dirA, ProvidersFileName), providersA)
	writeFile(t, filepath.Join(dirB, AgentsFileName), agentsB)
	writeFile(t, filepath.Join(dirB, ProvidersFileName), providersB)

	cfgA, err := Load(dirA)
	if err != nil {
		t.Fatalf("load dirA returned error: %v", err)
	}
	cfgB, err := Load(dirB)
	if err != nil {
		t.Fatalf("load dirB returned error: %v", err)
	}

	if joinAgentNames(cfgA.Agents) != joinAgentNames(cfgB.Agents) {
		t.Fatalf("agent order mismatch: A=%q B=%q", joinAgentNames(cfgA.Agents), joinAgentNames(cfgB.Agents))
	}
	if joinProviderNames(cfgA.Providers) != joinProviderNames(cfgB.Providers) {
		t.Fatalf("provider order mismatch: A=%q B=%q", joinProviderNames(cfgA.Providers), joinProviderNames(cfgB.Providers))
	}

	if joinAgentNames(cfgA.Agents) != "alpha, Planner ,zeta" {
		t.Fatalf("agent order = %q, want %q", joinAgentNames(cfgA.Agents), "alpha, Planner ,zeta")
	}
	if joinProviderNames(cfgA.Providers) != " OpenAI ,zai" {
		t.Fatalf("provider order = %q, want %q", joinProviderNames(cfgA.Providers), " OpenAI ,zai")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s returned error: %v", path, err)
	}
}

func joinAgentNames(agents []AgentConfig) string {
	names := make([]string, 0, len(agents))
	for _, agent := range agents {
		names = append(names, agent.Name)
	}
	return strings.Join(names, ",")
}

func joinProviderNames(providers []ProviderConfig) string {
	names := make([]string, 0, len(providers))
	for _, provider := range providers {
		names = append(names, provider.Name)
	}
	return strings.Join(names, ",")
}
