package agent

import (
	"context"
	"errors"
	"testing"
)

type stubAgent struct {
	name string
}

func (s stubAgent) Name() string {
	return s.name
}

func (s stubAgent) Run(context.Context, Request) (Response, error) {
	return Response{State: StateIdle}, nil
}

func TestRegistryRegisterAndResolveSuccess(t *testing.T) {
	registry := NewRegistry()
	planner := stubAgent{name: "planner"}

	if err := registry.Register("planner", planner); err != nil {
		t.Fatalf("register returned error: %v", err)
	}

	resolved, err := registry.Resolve(" PLANNER ")
	if err != nil {
		t.Fatalf("resolve returned error: %v", err)
	}

	if resolved.Name() != planner.Name() {
		t.Fatalf("resolved agent name = %q, want %q", resolved.Name(), planner.Name())
	}
}

func TestRegistryUnknownAgentError(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register("planner", stubAgent{name: "planner"}); err != nil {
		t.Fatalf("register planner returned error: %v", err)
	}
	if err := registry.Register("researcher", stubAgent{name: "researcher"}); err != nil {
		t.Fatalf("register researcher returned error: %v", err)
	}

	_, err := registry.Resolve("executor")
	if err == nil {
		t.Fatal("resolve returned nil error for unknown agent")
	}
	if !errors.Is(err, ErrUnknownAgent) {
		t.Fatalf("resolve error = %v, want unknown agent error", err)
	}

	want := "unknown agent: \"executor\" (available: planner, researcher)"
	if err.Error() != want {
		t.Fatalf("error message = %q, want %q", err.Error(), want)
	}
}

func TestRegistryDeterministicResolutionBehavior(t *testing.T) {
	registryA := NewRegistry()
	if err := registryA.Register("zeta", stubAgent{name: "zeta"}); err != nil {
		t.Fatalf("register zeta in registryA returned error: %v", err)
	}
	if err := registryA.Register("planner", stubAgent{name: "planner"}); err != nil {
		t.Fatalf("register planner in registryA returned error: %v", err)
	}

	registryB := NewRegistry()
	if err := registryB.Register("planner", stubAgent{name: "planner"}); err != nil {
		t.Fatalf("register planner in registryB returned error: %v", err)
	}
	if err := registryB.Register("zeta", stubAgent{name: "zeta"}); err != nil {
		t.Fatalf("register zeta in registryB returned error: %v", err)
	}

	_, errA := registryA.Resolve("unknown")
	_, errB := registryB.Resolve("unknown")
	if errA == nil || errB == nil {
		t.Fatal("resolve returned nil unknown-agent error")
	}

	if errA.Error() != errB.Error() {
		t.Fatalf("unknown-agent error mismatch: A=%q B=%q", errA.Error(), errB.Error())
	}

	agentA, err := registryA.Resolve(" Planner")
	if err != nil {
		t.Fatalf("resolve Planner in registryA returned error: %v", err)
	}
	agentB, err := registryB.Resolve("planner")
	if err != nil {
		t.Fatalf("resolve planner in registryB returned error: %v", err)
	}

	if agentA.Name() != agentB.Name() {
		t.Fatalf("resolved agent mismatch: A=%q B=%q", agentA.Name(), agentB.Name())
	}
}
