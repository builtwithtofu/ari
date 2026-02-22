package headless

import "fmt"

// HeadlessContract defines the guarantees for --headless mode.
//
// WHEN --headless is set:
//   - stdout: Line-delimited JSON (JSONL) - one protocol.Event per line
//   - stderr: Human-readable diagnostics (errors, warnings)
//   - Exit codes: 0=success, 1=error, 2=validation/user error
//
// SUPPORTED COMMANDS:
//   - build: YES (emits step_status, tool_call, tool_result events)
//   - plan: NO (returns error - interactive workflow)
//   - init: NO (returns error - one-time setup)
//   - ask: NO (returns error - query-only, no events)
//   - review: NO (returns error - not yet designed for streaming)
//
// NON-GOALS (this scope):
//   - No ARI_HEADLESS env var
//   - No stdin/JSON input mode
//   - No protocol schema changes
//   - No parallel step execution
type HeadlessContract struct {
	StdoutFormat string // "jsonl"
	StderrFormat string // "text"
	ExitOK       int    // 0
	ExitError    int    // 1
	ExitUser     int    // 2
}

// ContractV0 is the initial headless contract for Ari v0.
var ContractV0 = HeadlessContract{
	StdoutFormat: "jsonl",
	StderrFormat: "text",
	ExitOK:       0,
	ExitError:    1,
	ExitUser:     2,
}

// SupportedCommands maps command names to headless support status.
var SupportedCommands = map[string]bool{
	"build":  true,
	"plan":   false, // interactive workflow
	"init":   false, // one-time setup
	"ask":    false, // query-only, no events
	"review": false, // not designed for streaming
}

// IsHeadlessSupported returns whether a command supports headless mode.
func IsHeadlessSupported(command string) bool {
	return SupportedCommands[command]
}

// HeadlessUnsupportedError returns an error for unsupported commands.
func HeadlessUnsupportedError(command string) error {
	return fmt.Errorf(
		"command %q does not support --headless mode (interactive workflow)",
		command,
	)
}
