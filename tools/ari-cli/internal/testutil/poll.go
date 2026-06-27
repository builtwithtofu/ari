package testutil

import (
	"testing"
	"time"
)

// Eventually polls condition until it succeeds or timeout expires. It is for
// tests that must observe asynchronous state without open-coded sleeps.
func Eventually(t *testing.T, timeout, interval time.Duration, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(interval)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, description)
}
