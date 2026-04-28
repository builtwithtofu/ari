package daemon

import (
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

func TestDefaultHelperPromptsShareOneBaseContract(t *testing.T) {
	if helperPrompt() == "" {
		t.Fatalf("helper prompt is empty")
	}
}

func TestHelperWorkspaceContextUsesNormalWorkspaceScope(t *testing.T) {
	context := helperWorkspaceContext(&globaldb.Session{Name: "home", OriginRoot: "/home/user"})
	if !strings.Contains(context, "workspace home at /home/user") || !strings.Contains(context, "also answer general Ari questions") {
		t.Fatalf("helper context = %q", context)
	}
}
