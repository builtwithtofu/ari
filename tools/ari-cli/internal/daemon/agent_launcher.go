package daemon

import (
	"fmt"
	"strings"
)

type agentLaunchSpec struct {
	Command string
	Args    []string
}

type agentLauncher interface {
	prepare(command string, args []string) (agentLaunchSpec, error)
}

type passthroughAgentLauncher struct{}

func (passthroughAgentLauncher) prepare(command string, args []string) (agentLaunchSpec, error) {
	if command = strings.TrimSpace(command); command == "" {
		return agentLaunchSpec{}, fmt.Errorf("agent command is required")
	}

	return agentLaunchSpec{
		Command: command,
		Args:    append([]string(nil), args...),
	}, nil
}

type namedHarnessLauncher struct {
	binary string
}

func (l namedHarnessLauncher) prepare(command string, args []string) (agentLaunchSpec, error) {
	if command = strings.TrimSpace(command); command == "" {
		command = l.binary
	}
	if command == "" {
		return agentLaunchSpec{}, fmt.Errorf("agent command is required")
	}

	return agentLaunchSpec{
		Command: command,
		Args:    append([]string(nil), args...),
	}, nil
}

func resolveAgentLauncher(harness string) (agentLauncher, error) {
	harness = strings.TrimSpace(harness)
	if harness == "" {
		return passthroughAgentLauncher{}, nil
	}

	switch harness {
	case "claude-code":
		return namedHarnessLauncher{binary: "claude-code"}, nil
	case "codex":
		return namedHarnessLauncher{binary: "codex"}, nil
	case "opencode":
		return namedHarnessLauncher{binary: "opencode"}, nil
	default:
		return nil, fmt.Errorf("unsupported harness %q", harness)
	}
}
