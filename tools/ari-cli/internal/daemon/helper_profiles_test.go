package daemon

import (
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

func TestDefaultHelperPromptsShareOneBaseContract(t *testing.T) {
	if systemHelperPrompt() != helperPrompt() {
		t.Fatalf("system helper prompt diverged from shared helper prompt")
	}
	if projectHelperPrompt() != helperPrompt() {
		t.Fatalf("project helper prompt diverged from shared helper prompt")
	}
}

func TestHelperWorkspaceContextDistinguishesScopeWithoutHelperTypes(t *testing.T) {
	systemContext := helperWorkspaceContext(&globaldb.Session{Kind: "system", Name: "system"})
	if !strings.Contains(systemContext, "system/global starter workspace") || strings.Contains(systemContext, "project workspace") {
		t.Fatalf("system helper context = %q", systemContext)
	}
	projectContext := helperWorkspaceContext(&globaldb.Session{Kind: "project", Name: "alpha", OriginRoot: "/repo/alpha"})
	if !strings.Contains(projectContext, "project workspace alpha at /repo/alpha") || !strings.Contains(projectContext, "also answer general Ari questions") {
		t.Fatalf("project helper context = %q", projectContext)
	}
}
