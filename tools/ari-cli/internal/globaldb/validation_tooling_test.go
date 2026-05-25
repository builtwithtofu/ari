package globaldb

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRepoValidationGateIncludesDatabaseTooling(t *testing.T) {
	justfile := readRepoJustfile(t)

	for _, want := range []string{
		"sqlc-generate:",
		"cd tools/ari-cli && sqlc generate",
		"sqlc-check:",
		"cd tools/ari-cli && sqlc compile",
		"atlas-validate:",
		"cd tools/ari-cli && atlas migrate validate --dir file://migrations",
	} {
		if !strings.Contains(justfile, want) {
			t.Fatalf("justfile missing %q", want)
		}
	}

	verifyLine := lineWithPrefix(t, justfile, "verify:")
	for _, dep := range []string{"sqlc-check", "atlas-validate", "nix-fmt-check", "fmt-check", "lint", "build", "test", "flake-check"} {
		if !strings.Contains(verifyLine, dep) {
			t.Fatalf("verify target %q missing dependency %q", verifyLine, dep)
		}
	}
}

func readRepoJustfile(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", "..", "..", "justfile"))
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	return string(contents)
}

func lineWithPrefix(t *testing.T, contents, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(contents, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("missing line with prefix %q", prefix)
	return ""
}
