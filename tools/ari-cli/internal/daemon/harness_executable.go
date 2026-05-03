package daemon

import (
	"os"
	"strings"
)

const (
	EnvCodexExecutable    = "ARI_CODEX_EXECUTABLE"
	EnvClaudeExecutable   = "ARI_CLAUDE_EXECUTABLE"
	EnvOpenCodeExecutable = "ARI_OPENCODE_EXECUTABLE"
)

func harnessExecutable(defaultName, envName string) string {
	return HarnessExecutable(defaultName, envName)
}

func HarnessExecutable(defaultName, envName string) string {
	if override := strings.TrimSpace(os.Getenv(envName)); override != "" {
		return override
	}
	return strings.TrimSpace(defaultName)
}
