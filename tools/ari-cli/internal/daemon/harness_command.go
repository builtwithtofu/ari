package daemon

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// resolveHarnessExecutable resolves a harness CLI binary on PATH, defaulting
// the executable name when adapter options leave it empty. A missing binary
// is reported as the canonical missing_executable unavailability.
func resolveHarnessExecutable(harness, configured, defaultName string) (string, string, error) {
	executable := strings.TrimSpace(configured)
	if executable == "" {
		executable = defaultName
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return "", executable, &HarnessUnavailableError{Harness: harness, Reason: "missing_executable", Executable: executable, Probe: executable + " --version", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}
	return path, executable, nil
}

// contextPacketPrompt is the shared adapter prompt extraction: the rendered
// context packet is the start prompt.
func contextPacketPrompt(req ExecutorStartRequest) string {
	return strings.TrimSpace(req.ContextPacket)
}

// harnessCommand is one buffered harness CLI invocation sharing the adapter
// scaffolding: working dir, auth projection env, combined output capture,
// process metrics sampling, and exit-code reporting. Policy fields keep each
// adapter's documented error contract.
type harnessCommand struct {
	harness    string
	path       string
	executable string
	args       []string
	cwd        string
	projection HarnessAuthProjectionPlan
	// stdin is written and closed after start when non-nil.
	stdin *string
	// startFailedUnavailable reports start errors as a start_failed
	// HarnessUnavailableError instead of the raw error.
	startFailedUnavailable bool
	// waitErrWrap wraps wait errors as "<waitErrWrap>: %w" when non-empty.
	waitErrWrap string
	// keepResultOnWaitErr returns the captured output alongside a wait error.
	keepResultOnWaitErr bool
}

func (c harnessCommand) run(ctx context.Context) (commandRunResult, error) {
	cmd := exec.CommandContext(ctx, c.path, c.args...)
	cmd.Dir = strings.TrimSpace(c.cwd)
	cmd.Env = commandEnvWithProjection(c.projection)
	var stdin io.WriteCloser
	if c.stdin != nil {
		var err error
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return commandRunResult{}, err
		}
	}
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		if stdin != nil {
			_ = stdin.Close()
		}
		if c.startFailedUnavailable {
			return commandRunResult{}, &HarnessUnavailableError{Harness: c.harness, Reason: "start_failed", Executable: c.executable, Probe: c.executable, RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: true}
		}
		return commandRunResult{}, err
	}
	if stdin != nil {
		if _, err := io.WriteString(stdin, *c.stdin); err != nil {
			_ = stdin.Close()
			_ = cmd.Wait()
			return commandRunResult{}, fmt.Errorf("write %s stdin: %w", c.harness, err)
		}
		if err := stdin.Close(); err != nil {
			_ = cmd.Wait()
			return commandRunResult{}, fmt.Errorf("close %s stdin: %w", c.harness, err)
		}
	}
	sample := sampleLinuxProcessMetrics(ctx, HarnessSession{PID: cmd.Process.Pid})
	err := cmd.Wait()
	exitCode := cmd.ProcessState.ExitCode()
	result := commandRunResult{Output: []byte(output.String()), ProcessSample: &sample, ExitCode: &exitCode}
	if err != nil {
		if c.waitErrWrap != "" {
			err = fmt.Errorf("%s: %w", c.waitErrWrap, err)
		}
		if c.keepResultOnWaitErr {
			return result, err
		}
		return commandRunResult{}, err
	}
	return result, nil
}
