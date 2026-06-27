package testutil

import (
	"path/filepath"
	"testing"
)

func SocketPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "s.sock")
}
