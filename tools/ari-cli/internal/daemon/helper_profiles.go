package daemon

import (
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

func helperPrompt() string {
	return "Help the user understand and configure Ari. Use Ari and workspace state before general advice. Teach concepts, diagnose setup, draft profile/default changes, summarize what is known, and request approval before writes. Route project source changes to explicit project profiles."
}

func systemHelperPrompt() string {
	return helperPrompt()
}

func projectHelperPrompt() string {
	return helperPrompt()
}

func helperWorkspaceContext(workspace *globaldb.Session) string {
	if workspace == nil {
		return "Scope: unknown workspace. Ask Ari for workspace context before making scope-specific claims."
	}
	if workspace.Kind == "system" {
		return "Scope: system/global starter workspace. No project root is selected; help generally with Ari, local setup, profiles, defaults, diagnostics, and handoffs."
	}
	if workspace.OriginRoot != "" {
		return fmt.Sprintf("Scope: project workspace %s at %s. Use project context when relevant, and also answer general Ari questions when asked.", workspace.Name, workspace.OriginRoot)
	}
	return fmt.Sprintf("Scope: project workspace %s. Use project context when relevant, and also answer general Ari questions when asked.", workspace.Name)
}

func buildHelperLaunchArgs(workspace *globaldb.Session, args []string) []string {
	contextNote := helperWorkspaceContext(workspace)
	if len(args) == 0 {
		return []string{contextNote}
	}
	userPrompt := strings.TrimSpace(strings.Join(args, " "))
	if userPrompt == "" {
		return []string{contextNote}
	}
	return []string{contextNote + "\n\nUser request: " + userPrompt}
}
