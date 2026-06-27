package cmd

import "testing"

type commandHarness struct {
	t *testing.T
}

func newCommandHarness(t *testing.T) *commandHarness {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return &commandHarness{t: t}
}

func (h *commandHarness) execute(args ...string) (string, error) {
	h.t.Helper()
	return executeRootCommand(args...)
}

func swapTestValue[T any](t *testing.T, target *T, replacement T) {
	t.Helper()
	original := *target
	*target = replacement
	t.Cleanup(func() { *target = original })
}
