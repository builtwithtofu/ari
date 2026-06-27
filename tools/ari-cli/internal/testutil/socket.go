package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

const unixSocketPathLimit = 100

// UnixSocketPath returns a short filesystem path suitable for AF_UNIX tests.
//
// testing.T.TempDir includes the test name, and nix/CI runners often place it
// below another long temporary directory. Those paths can exceed sockaddr_un's
// small sun_path limit (108 bytes on Linux, commonly 104 on BSD/macOS) before a
// socket filename is appended. Keep socket tests under /tmp with a short random
// directory instead, and use t.TempDir only for non-socket artifacts.
func UnixSocketPath(t testing.TB) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "ari-sock-") //nolint:usetesting // t.TempDir makes AF_UNIX paths too long on CI.
	if err != nil {
		t.Fatalf("create unix socket temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove unix socket temp dir %q: %v", dir, err)
		}
	})

	path := filepath.Join(dir, "s.sock")
	if len(path) >= unixSocketPathLimit {
		t.Fatalf("unix socket path %q is %d bytes, want less than %d", path, len(path), unixSocketPathLimit)
	}

	return path
}
