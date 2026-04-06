package daemon

import (
	"fmt"
	"sort"
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

type harnessDefinition struct {
	launcher      agentLauncher
	resumableFlag string
}

var harnessDefinitions = map[string]harnessDefinition{
	"claude-code": {
		launcher:      namedHarnessLauncher{binary: "claude-code"},
		resumableFlag: "--resume",
	},
	"codex": {
		launcher: namedHarnessLauncher{binary: "codex"},
	},
	"opencode": {
		launcher:      namedHarnessLauncher{binary: "opencode"},
		resumableFlag: "--session",
	},
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
	definition, ok := harnessDefinitions[harness]
	if !ok {
		return nil, fmt.Errorf("unsupported harness %q", harness)
	}
	return definition.launcher, nil
}

func resumableFlagForHarness(harness string) string {
	definition, ok := harnessDefinitions[strings.TrimSpace(harness)]
	if !ok {
		return ""
	}
	return definition.resumableFlag
}

func SupportedHarnesses() []string {
	names := make([]string, 0, len(harnessDefinitions))
	for name := range harnessDefinitions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
