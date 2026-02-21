package provider

import (
	"context"
	"errors"
	"testing"
)

type stubProvider struct {
	name string
}

func (s stubProvider) Name() string {
	return s.name
}

func (s stubProvider) Complete(context.Context, CompletionRequest) (CompletionResponse, error) {
	return CompletionResponse{}, nil
}

func TestRegistryRegisterAndResolveSuccess(t *testing.T) {
	registry := NewRegistry()
	openai := stubProvider{name: "openai"}

	if err := registry.Register("openai", openai); err != nil {
		t.Fatalf("register returned error: %v", err)
	}

	resolved, err := registry.Resolve(" OPENAI ")
	if err != nil {
		t.Fatalf("resolve returned error: %v", err)
	}

	if resolved.Name() != openai.Name() {
		t.Fatalf("resolved provider name = %q, want %q", resolved.Name(), openai.Name())
	}
}

func TestRegistryUnknownProviderError(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register("openai", stubProvider{name: "openai"}); err != nil {
		t.Fatalf("register openai returned error: %v", err)
	}
	if err := registry.Register("anthropic", stubProvider{name: "anthropic"}); err != nil {
		t.Fatalf("register anthropic returned error: %v", err)
	}

	_, err := registry.Resolve("kimi")
	if err == nil {
		t.Fatal("resolve returned nil error for unknown provider")
	}
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("resolve error = %v, want unknown provider error", err)
	}

	want := "unknown provider: \"kimi\" (available: anthropic, openai)"
	if err.Error() != want {
		t.Fatalf("error message = %q, want %q", err.Error(), want)
	}
}

func TestRegistryDeterministicResolutionBehavior(t *testing.T) {
	registryA := NewRegistry()
	if err := registryA.Register("zai", stubProvider{name: "zai"}); err != nil {
		t.Fatalf("register zai in registryA returned error: %v", err)
	}
	if err := registryA.Register("openai", stubProvider{name: "openai"}); err != nil {
		t.Fatalf("register openai in registryA returned error: %v", err)
	}

	registryB := NewRegistry()
	if err := registryB.Register("openai", stubProvider{name: "openai"}); err != nil {
		t.Fatalf("register openai in registryB returned error: %v", err)
	}
	if err := registryB.Register("zai", stubProvider{name: "zai"}); err != nil {
		t.Fatalf("register zai in registryB returned error: %v", err)
	}

	_, errA := registryA.Resolve("unknown")
	_, errB := registryB.Resolve("unknown")
	if errA == nil || errB == nil {
		t.Fatal("resolve returned nil unknown-provider error")
	}

	if errA.Error() != errB.Error() {
		t.Fatalf("unknown-provider error mismatch: A=%q B=%q", errA.Error(), errB.Error())
	}

	providerA, err := registryA.Resolve(" OpenAI")
	if err != nil {
		t.Fatalf("resolve OpenAI in registryA returned error: %v", err)
	}
	providerB, err := registryB.Resolve("openai")
	if err != nil {
		t.Fatalf("resolve openai in registryB returned error: %v", err)
	}

	if providerA.Name() != providerB.Name() {
		t.Fatalf("resolved provider mismatch: A=%q B=%q", providerA.Name(), providerB.Name())
	}
}
