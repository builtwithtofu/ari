package process

import (
	"errors"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestProcessLifecycleStateTransitionsAndExitCode(t *testing.T) {
	p, err := New("/bin/sh", []string{"-c", "sleep 0.1; exit 7"}, Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if p.State() != StateStarting {
		t.Fatalf("State() after New = %q, want %q", p.State(), StateStarting)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	waitForState(t, p, StateRunning)

	result, err := p.Wait()
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("Wait ExitCode = %d, want %d", result.ExitCode, 7)
	}
	if result.Signaled {
		t.Fatal("Wait Signaled = true, want false")
	}

	if p.State() != StateExited {
		t.Fatalf("State() after Wait = %q, want %q", p.State(), StateExited)
	}

	exitCode, ok := p.ExitCode()
	if !ok {
		t.Fatal("ExitCode() ok = false, want true")
	}
	if exitCode != 7 {
		t.Fatalf("ExitCode() = %d, want %d", exitCode, 7)
	}

	if p.PID() <= 0 {
		t.Fatalf("PID() = %d, want > 0", p.PID())
	}
}

func TestProcessStopSendsSIGTERMToProcessGroup(t *testing.T) {
	tempDir := t.TempDir()
	childPIDFile := filepath.Join(tempDir, "child.pid")
	childMarkerFile := filepath.Join(tempDir, "child.term")

	p, err := New("/bin/sh", []string{"-c", `
trap 'exit 0' TERM
sh -c 'trap "echo child-term > "$2"; exit 0" TERM; while true; do sleep 1; done' ignored "$1" "$2" &
child=$!
printf "%s\n" "$child" > "$1"
while true; do sleep 1; done
`, "ignored", childPIDFile, childMarkerFile}, Options{Dir: tempDir})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	waitForState(t, p, StateRunning)

	childPID := readPIDFileEventually(t, childPIDFile)

	if err := p.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	result, err := p.Wait()
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("Wait ExitCode = %d, want %d", result.ExitCode, 0)
	}

	if _, ok := p.ExitCode(); !ok {
		t.Fatal("ExitCode() ok = false, want true")
	}

	if err := waitForFileContent(t, childMarkerFile, "child-term"); err != nil {
		t.Fatalf("child marker check failed: %v", err)
	}

	_ = childPID
}

func TestProcessStopWhileOutputReadIsBlockedDoesNotHang(t *testing.T) {
	p, err := New("/bin/sh", []string{"-c", "cat"}, Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	waitForState(t, p, StateRunning)

	stopped := make(chan error, 1)
	go func() {
		stopped <- p.Stop()
	}()

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Stop returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop hung while output read was blocked")
	}

	if _, err := p.Wait(); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
}

func TestProcessWaitReapsChildProcess(t *testing.T) {
	p, err := New("/bin/sh", []string{"-c", "exit 0"}, Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	pid := p.PID()
	if pid <= 0 {
		t.Fatalf("PID() = %d, want > 0", pid)
	}

	if _, err := p.Wait(); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}

	err = syscall.Kill(pid, 0)
	if !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("Kill(pid, 0) error = %v, want ESRCH after Wait reaps child", err)
	}
}

func TestProcessStateUpdatesWithoutExplicitWait(t *testing.T) {
	p, err := New("/bin/sh", []string{"-c", "exit 7"}, Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	waitForState(t, p, StateExited)

	exitCode, ok := p.ExitCode()
	if !ok {
		t.Fatal("ExitCode() ok = false, want true without explicit Wait")
	}
	if exitCode != 7 {
		t.Fatalf("ExitCode() = %d, want %d", exitCode, 7)
	}
}

func TestProcessExternalSIGTERMReportsSignaled(t *testing.T) {
	p, err := New("/bin/sh", []string{"-c", "while true; do sleep 1; done"}, Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	waitForState(t, p, StateRunning)

	pid := p.PID()
	if pid <= 0 {
		t.Fatalf("PID() = %d, want > 0", pid)
	}

	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		t.Fatalf("Kill(-pid, SIGTERM) returned error: %v", err)
	}

	result, err := p.Wait()
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if !result.Signaled {
		t.Fatal("Wait Signaled = false, want true for external SIGTERM")
	}
	if result.ExitCode != 128+int(syscall.SIGTERM) {
		t.Fatalf("Wait ExitCode = %d, want %d", result.ExitCode, 128+int(syscall.SIGTERM))
	}
}

func TestProcessStopEscalatesWhenSIGTERMIgnored(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable returned error: %v", err)
	}
	readyFile := filepath.Join(t.TempDir(), "helper.ready")

	p, err := New("/usr/bin/env", []string{"ARI_PROCESS_HELPER=1", "ARI_PROCESS_READY=" + readyFile, executable, "-test.run=TestHelperProcessIgnoreTERM"}, Options{Dir: t.TempDir(), StopTimeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	waitForState(t, p, StateRunning)
	if err := waitForFileContent(t, readyFile, "ready"); err != nil {
		t.Fatalf("helper readiness check failed: %v", err)
	}

	if err := p.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	result, err := p.Wait()
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if !result.Signaled {
		t.Fatal("Wait Signaled = false, want true after SIGKILL escalation")
	}
	if result.ExitCode != 128+int(syscall.SIGKILL) {
		t.Fatalf("Wait ExitCode = %d, want %d", result.ExitCode, 128+int(syscall.SIGKILL))
	}
}

func TestProcessWriteCapturesOutputInRingBuffer(t *testing.T) {
	p, err := New("/bin/sh", []string{"-c", "cat"}, Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() {
		_ = p.Stop()
		_, _ = p.Wait()
	}()

	waitForState(t, p, StateRunning)

	n, err := p.Write([]byte("hello from stdin\n"))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len("hello from stdin\n") {
		t.Fatalf("Write n = %d, want %d", n, len("hello from stdin\n"))
	}

	waitForSnapshotContains(t, p, "hello from stdin")
}

func TestProcessOutputSubscriptionReceivesOutput(t *testing.T) {
	p, err := New("/bin/sh", []string{"-c", "cat"}, Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() {
		_ = p.Stop()
		_, _ = p.Wait()
	}()

	waitForState(t, p, StateRunning)

	updates, unsubscribe := p.SubscribeOutput()
	defer unsubscribe()

	if _, err := p.Write([]byte("subscribed line\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	select {
	case chunk := <-updates:
		if !strings.Contains(string(chunk), "subscribed line") {
			t.Fatalf("subscription chunk = %q, want contains %q", string(chunk), "subscribed line")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for output subscription update")
	}
}

func TestProcessResizeSetsPTYWindowSize(t *testing.T) {
	p, err := New("/bin/sh", []string{}, Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() {
		_ = p.Stop()
		_, _ = p.Wait()
	}()

	waitForState(t, p, StateRunning)

	if err := p.Resize(40, 120); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}

	if _, err := p.Write([]byte("stty size\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	waitForSnapshotContains(t, p, "40 120")

	if _, err := p.Write([]byte("exit\n")); err != nil {
		t.Fatalf("Write exit returned error: %v", err)
	}
	if _, err := p.Wait(); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
}

func TestProcessStopAlreadyStoppedIsNoOp(t *testing.T) {
	p, err := New("/bin/sh", []string{"-c", "exit 0"}, Options{Dir: t.TempDir(), StopTimeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if _, err := p.Wait(); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}

	if err := p.Stop(); err != nil {
		t.Fatalf("first Stop returned error: %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("second Stop returned error: %v", err)
	}
}

func TestProcessResizeStoppedReturnsError(t *testing.T) {
	p, err := New("/bin/sh", []string{"-c", "exit 0"}, Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if _, err := p.Wait(); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}

	err = p.Resize(24, 80)
	if err == nil {
		t.Fatal("Resize returned nil error for stopped process")
	}
}

func TestProcessWriteStoppedReturnsError(t *testing.T) {
	p, err := New("/bin/sh", []string{"-c", "exit 0"}, Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if _, err := p.Wait(); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}

	_, err = p.Write([]byte("after exit\n"))
	if err == nil {
		t.Fatal("Write returned nil error for stopped process")
	}
}

func TestProcessConcurrentWriteAndSnapshot(t *testing.T) {
	p, err := New("/bin/sh", []string{"-c", "cat"}, Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer func() {
		_ = p.Stop()
		_, _ = p.Wait()
	}()

	waitForState(t, p, StateRunning)

	const writers = 6
	const writesPerWriter = 120

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				if _, err := p.Write([]byte("chunk\n")); err != nil {
					t.Errorf("Write returned error: %v", err)
					return
				}
				_ = p.OutputSnapshot()
			}
		}()
	}

	wg.Wait()

	if len(p.OutputSnapshot()) == 0 {
		t.Fatal("OutputSnapshot is empty after concurrent write/snapshot")
	}
}

func TestProcessLargeOutputKeepsNewestData(t *testing.T) {
	p, err := New("/bin/sh", []string{"-c", `i=0; while [ $i -lt 200000 ]; do printf 'line-%06d\n' "$i"; i=$((i+1)); done`}, Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	if _, err := p.Wait(); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}

	snapshot := p.OutputSnapshot()
	if len(snapshot) == 0 {
		t.Fatal("OutputSnapshot is empty after large output")
	}
	if len(snapshot) > 1<<20 {
		t.Fatalf("OutputSnapshot length = %d, want <= %d", len(snapshot), 1<<20)
	}
	if !strings.Contains(string(snapshot), "line-199999") {
		t.Fatalf("OutputSnapshot missing tail marker %q", "line-199999")
	}
}

func TestProcessSelfExitUpdatesStateWithoutExplicitWait(t *testing.T) {
	p, err := New("/bin/sh", []string{"-c", "exit 3"}, Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	waitForState(t, p, StateExited)

	exitCode, ok := p.ExitCode()
	if !ok {
		t.Fatal("ExitCode() ok = false, want true after self exit")
	}
	if exitCode != 3 {
		t.Fatalf("ExitCode() = %d, want %d", exitCode, 3)
	}
}

func TestProcessStopDoesNotSignalAfterWaitCompletes(t *testing.T) {
	originalKill := processGroupKill
	killCalls := 0
	processGroupKill = func(_ int, _ syscall.Signal) error {
		killCalls++
		return nil
	}
	t.Cleanup(func() {
		processGroupKill = originalKill
	})

	p, err := New("/bin/sh", []string{"-c", "exit 0"}, Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if _, err := p.Wait(); err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}

	if err := p.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	if killCalls != 0 {
		t.Fatalf("processGroupKill call count = %d, want %d", killCalls, 0)
	}
}

func TestPumpOutputDrainsTailAfterDoneIsClosed(t *testing.T) {
	p := &Process{
		outputBuffer: NewRingBuffer(64),
		outputSubs:   make(map[chan []byte]struct{}),
		done:         make(chan struct{}),
		ptyFile: &pumpOutputPTYStub{
			reads: [][]byte{[]byte("tail-bytes")},
		},
	}

	close(p.done)
	p.pumpOutput()

	snapshot := p.OutputSnapshot()
	if !strings.Contains(string(snapshot), "tail-bytes") {
		t.Fatalf("OutputSnapshot() = %q, want contains %q", string(snapshot), "tail-bytes")
	}
}

func TestHelperProcessIgnoreTERM(t *testing.T) {
	if os.Getenv("ARI_PROCESS_HELPER") != "1" {
		t.Skip("helper process test")
	}

	signal.Ignore(syscall.SIGTERM)
	readyFile := os.Getenv("ARI_PROCESS_READY")
	if readyFile != "" {
		if err := os.WriteFile(readyFile, []byte("ready\n"), 0o600); err != nil {
			panic(err)
		}
	}
	for {
		time.Sleep(100 * time.Millisecond)
	}
}

func waitForState(t *testing.T, p *Process, want State) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.State() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("State() = %q, want %q before timeout", p.State(), want)
}

func waitForSnapshotContains(t *testing.T, p *Process, want string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := string(p.OutputSnapshot())
		if strings.Contains(snapshot, want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("OutputSnapshot() did not contain %q before timeout", want)
}

func readPIDFileEventually(t *testing.T, path string) int {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		pidText := strings.TrimSpace(string(data))
		pid, err := strconv.Atoi(pidText)
		if err != nil {
			t.Fatalf("Atoi(%q) returned error: %v", pidText, err)
		}
		if pid <= 0 {
			t.Fatalf("parsed child PID = %d, want > 0", pid)
		}
		return pid
	}

	t.Fatalf("timed out waiting for child pid file %q", path)
	return 0
}

func waitForFileContent(t *testing.T, path string, want string) error {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		if strings.TrimSpace(string(data)) == want {
			return nil
		}

		time.Sleep(10 * time.Millisecond)
	}

	return errors.New("timed out waiting for expected file content")
}

type pumpOutputPTYStub struct {
	reads [][]byte
	index int
}

func (s *pumpOutputPTYStub) Read(p []byte) (int, error) {
	if s.index >= len(s.reads) {
		return 0, io.EOF
	}

	chunk := s.reads[s.index]
	s.index++
	n := copy(p, chunk)
	return n, nil
}

func (s *pumpOutputPTYStub) Write(_ []byte) (int, error) {
	return 0, errors.New("not implemented")
}

func (s *pumpOutputPTYStub) Close() error {
	return nil
}

func (s *pumpOutputPTYStub) Fd() uintptr {
	return 0
}
