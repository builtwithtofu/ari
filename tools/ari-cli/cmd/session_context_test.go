package cmd

import (
	"strings"
	"testing"
)

func TestResolveWorkspaceSessionReferencePanicsOnNilReader(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("resolveWorkspaceSessionReference did not panic")
		}
		message, ok := recovered.(string)
		if !ok {
			t.Fatalf("panic type = %T, want string", recovered)
		}
		if !strings.Contains(message, "readActive must not be nil") {
			t.Fatalf("panic = %q, want readActive nil guard message", message)
		}
	}()

	_, _ = resolveWorkspaceSessionReference("", nil)
}
