package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestResolveHarnessExecutableReportsMissingExecutable(t *testing.T) {
	_, executable, err := resolveHarnessExecutable("testharness", "", "ari-test-missing-binary")
	var unavailable *HarnessUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Reason != "missing_executable" {
		t.Fatalf("err = %v, want missing_executable unavailability", err)
	}
	if executable != "ari-test-missing-binary" || unavailable.Executable != executable {
		t.Fatalf("executable = %q (error %q), want defaulted name reported", executable, unavailable.Executable)
	}
}

func TestResolveHarnessExecutablePrefersConfiguredName(t *testing.T) {
	path, executable, err := resolveHarnessExecutable("testharness", " sh ", "ari-test-missing-binary")
	if err != nil {
		t.Fatalf("resolveHarnessExecutable returned error: %v", err)
	}
	if executable != "sh" || strings.TrimSpace(path) == "" {
		t.Fatalf("path = %q executable = %q, want resolved sh", path, executable)
	}
}

func TestHarnessCommandRunRedactsArgsFromStartFailureProbe(t *testing.T) {
	_, err := harnessCommand{harness: "testharness", path: "/missing/testharness", executable: "testharness", args: []string{"prompt-secret"}, startFailedUnavailable: true}.run(context.Background())
	var unavailable *HarnessUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Reason != "start_failed" {
		t.Fatalf("err = %v, want start_failed unavailability", err)
	}
	if unavailable.Probe != "testharness" || strings.Contains(unavailable.Probe, "prompt-secret") {
		t.Fatalf("probe = %q, want executable only", unavailable.Probe)
	}
}

func TestHarnessCommandRunCapturesOutputAndExitCode(t *testing.T) {
	path, executable, err := resolveHarnessExecutable("testharness", "sh", "sh")
	if err != nil {
		t.Fatalf("resolveHarnessExecutable returned error: %v", err)
	}
	result, err := harnessCommand{harness: "testharness", path: path, executable: executable, args: []string{"-c", "printf out; printf err 1>&2"}}.run(context.Background())
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if string(result.Output) != "outerr" || result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("result = %#v, want combined output and zero exit code", result)
	}
}

func TestHarnessCommandRunWritesStdin(t *testing.T) {
	path, executable, err := resolveHarnessExecutable("testharness", "sh", "sh")
	if err != nil {
		t.Fatalf("resolveHarnessExecutable returned error: %v", err)
	}
	input := "stdin-sentinel"
	result, err := harnessCommand{harness: "testharness", path: path, executable: executable, args: []string{"-c", "cat"}, stdin: &input}.run(context.Background())
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if string(result.Output) != input {
		t.Fatalf("output = %q, want stdin echoed", result.Output)
	}
}

func TestHarnessCommandRunWaitErrorPolicies(t *testing.T) {
	path, executable, err := resolveHarnessExecutable("testharness", "sh", "sh")
	if err != nil {
		t.Fatalf("resolveHarnessExecutable returned error: %v", err)
	}
	base := harnessCommand{harness: "testharness", path: path, executable: executable, args: []string{"-c", "printf partial; exit 3"}}

	kept := base
	kept.keepResultOnWaitErr = true
	kept.waitErrWrap = "run testharness"
	result, err := kept.run(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "run testharness: ") {
		t.Fatalf("err = %v, want wrapped wait error", err)
	}
	if string(result.Output) != "partial" || result.ExitCode == nil || *result.ExitCode != 3 {
		t.Fatalf("result = %#v, want partial output and exit code 3 kept on error", result)
	}

	dropped := base
	result, err = dropped.run(context.Background())
	if err == nil || strings.Contains(err.Error(), "run testharness") {
		t.Fatalf("err = %v, want raw wait error", err)
	}
	if result.Output != nil || result.ExitCode != nil {
		t.Fatalf("result = %#v, want empty result dropped on error", result)
	}
}
