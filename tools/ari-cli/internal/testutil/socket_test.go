package testutil

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnixSocketPathUsesShortPathOutsideTestTempDir(t *testing.T) {
	base, err := os.MkdirTemp("/tmp", "ari-long-temp-")
	if err != nil {
		t.Fatalf("create long temp base: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove long temp base: %v", err)
		}
	})
	longTemp := filepath.Join(base, strings.Repeat("x", 96))
	if err := os.MkdirAll(longTemp, 0o755); err != nil {
		t.Fatalf("create long temp dir: %v", err)
	}
	t.Setenv("TMPDIR", longTemp)

	socketPath := UnixSocketPath(t)
	if len(socketPath) >= unixSocketPathLimit {
		t.Fatalf("socket path %q is %d bytes, want less than %d", socketPath, len(socketPath), unixSocketPathLimit)
	}
	if strings.HasPrefix(socketPath, longTemp) {
		t.Fatalf("socket path %q uses TMPDIR %q", socketPath, longTemp)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on generated socket path: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
}
