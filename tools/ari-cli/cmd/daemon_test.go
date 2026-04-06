package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

func TestRootRegistersDaemonCommand(t *testing.T) {
	root := NewRootCmd()
	daemonCmd, _, err := root.Find([]string{"daemon"})
	if err != nil {
		t.Fatalf("find daemon command: %v", err)
	}

	if daemonCmd == nil {
		t.Fatalf("expected daemon command to be registered")
	}

	if daemonCmd.Name() != "daemon" {
		t.Fatalf("unexpected command name: %q", daemonCmd.Name())
	}
}

func TestDaemonSubcommandsExist(t *testing.T) {
	daemon := NewDaemonCmd()

	start, _, err := daemon.Find([]string{"start"})
	if err != nil {
		t.Fatalf("find daemon start: %v", err)
	}

	stop, _, err := daemon.Find([]string{"stop"})
	if err != nil {
		t.Fatalf("find daemon stop: %v", err)
	}

	status, _, err := daemon.Find([]string{"status"})
	if err != nil {
		t.Fatalf("find daemon status: %v", err)
	}

	if start == nil || stop == nil || status == nil {
		t.Fatalf("expected daemon start/stop/status commands")
	}
}

func TestDaemonStatusWhenUnavailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	out, err := executeRootCommand("daemon", "status")
	if err != nil {
		t.Fatalf("execute daemon status: %v", err)
	}

	want := "Daemon is not running.\nHint: Start it with `ari daemon start`."
	if strings.TrimSpace(out) != want {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestDaemonStatusPermissionDeniedMessage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalStatusRPC := daemonStatusRPC
	daemonStatusRPC = func(context.Context, string) (daemon.StatusResponse, error) {
		return daemon.StatusResponse{}, os.ErrPermission
	}
	t.Cleanup(func() {
		daemonStatusRPC = originalStatusRPC
	})

	_, err := executeRootCommand("daemon", "status")
	if err == nil {
		t.Fatal("execute daemon status returned nil error")
	}

	want := "Permission denied: " + filepath.Join(home, ".ari", "daemon.sock") + ".\nHint: Check socket file permissions and ownership."
	if err.Error() != want {
		t.Fatalf("status error = %q, want %q", err.Error(), want)
	}
}

func TestDaemonStatusTimeoutMessage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalStatusRPC := daemonStatusRPC
	daemonStatusRPC = func(context.Context, string) (daemon.StatusResponse, error) {
		return daemon.StatusResponse{}, context.DeadlineExceeded
	}
	t.Cleanup(func() {
		daemonStatusRPC = originalStatusRPC
	})

	_, err := executeRootCommand("daemon", "status")
	if err == nil {
		t.Fatal("execute daemon status returned nil error")
	}

	want := "Daemon did not respond (timeout).\nHint: Try `ari daemon stop` or check the process."
	if err.Error() != want {
		t.Fatalf("status error = %q, want %q", err.Error(), want)
	}
}

func TestDaemonStopWhenUnavailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	out, err := executeRootCommand("daemon", "stop")
	if err != nil {
		t.Fatalf("execute daemon stop: %v", err)
	}

	want := "Daemon is not running.\nHint: Start it with `ari daemon start`."
	if strings.TrimSpace(out) != want {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestDaemonStopFallsBackToPIDSignalWhenRPCFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_DAEMON_PID_PATH", "~/env.pid")
	t.Setenv("ARI_DAEMON_SOCKET_PATH", "~/env.sock")

	originalStopRPC := daemonStopRPC
	originalPIDCheck := daemonPIDCheck
	originalStatusRPC := daemonStatusRPC
	originalSignal := daemonSignalProcess

	daemonStopRPC = func(context.Context, string) error {
		return errors.New("rpc timeout")
	}
	daemonPIDCheck = func(path string) (int, bool, error) {
		if path != filepath.Join(home, "env.pid") {
			t.Fatalf("pid path = %q, want %q", path, filepath.Join(home, "env.pid"))
		}
		return 321, true, nil
	}
	daemonStatusRPC = func(context.Context, string) (daemon.StatusResponse, error) {
		return daemon.StatusResponse{}, context.DeadlineExceeded
	}
	signalCalled := false
	daemonSignalProcess = func(pid int, sig syscall.Signal) error {
		signalCalled = true
		if pid != 321 {
			t.Fatalf("signal pid = %d, want 321", pid)
		}
		if sig != syscall.SIGTERM {
			t.Fatalf("signal = %v, want SIGTERM", sig)
		}
		return nil
	}

	t.Cleanup(func() {
		daemonStopRPC = originalStopRPC
		daemonPIDCheck = originalPIDCheck
		daemonStatusRPC = originalStatusRPC
		daemonSignalProcess = originalSignal
	})

	out, err := executeRootCommand("daemon", "stop")
	if err != nil {
		t.Fatalf("execute daemon stop: %v", err)
	}

	if !signalCalled {
		t.Fatal("expected fallback signal to be sent")
	}
	if strings.TrimSpace(out) != "Daemon stopping" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestDaemonStopTimeoutMessageWhenFallbackUnavailable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalStopRPC := daemonStopRPC
	originalPIDCheck := daemonPIDCheck
	originalStatusRPC := daemonStatusRPC
	t.Setenv("ARI_DAEMON_PID_PATH", "~/env.pid")
	t.Setenv("ARI_DAEMON_SOCKET_PATH", "~/env.sock")

	daemonStopRPC = func(context.Context, string) error {
		return context.DeadlineExceeded
	}
	daemonPIDCheck = func(string) (int, bool, error) {
		return 0, false, nil
	}
	daemonStatusRPC = func(context.Context, string) (daemon.StatusResponse, error) {
		return daemon.StatusResponse{}, os.ErrNotExist
	}
	t.Cleanup(func() {
		daemonStopRPC = originalStopRPC
		daemonPIDCheck = originalPIDCheck
		daemonStatusRPC = originalStatusRPC
	})

	_, err := executeRootCommand("daemon", "stop")
	if err == nil {
		t.Fatal("execute daemon stop returned nil error")
	}

	want := "Daemon did not respond (timeout).\nHint: Try `ari daemon stop` or check the process."
	if err.Error() != want {
		t.Fatalf("stop error = %q, want %q", err.Error(), want)
	}
}

func TestFallbackStopByPIDDoesNotSignalWhenSocketUnavailable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_DAEMON_PID_PATH", "~/env.pid")
	t.Setenv("ARI_DAEMON_SOCKET_PATH", "~/env.sock")

	originalPIDCheck := daemonPIDCheck
	originalStatusRPC := daemonStatusRPC
	originalSignal := daemonSignalProcess

	daemonPIDCheck = func(path string) (int, bool, error) {
		if path != filepath.Join(home, "env.pid") {
			t.Fatalf("pid path = %q, want %q", path, filepath.Join(home, "env.pid"))
		}
		return 777, true, nil
	}
	daemonStatusRPC = func(context.Context, string) (daemon.StatusResponse, error) {
		return daemon.StatusResponse{}, os.ErrNotExist
	}
	signalCalled := false
	daemonSignalProcess = func(int, syscall.Signal) error {
		signalCalled = true
		return nil
	}
	t.Cleanup(func() {
		daemonPIDCheck = originalPIDCheck
		daemonStatusRPC = originalStatusRPC
		daemonSignalProcess = originalSignal
	})

	stopped, err := fallbackStopByPID()
	if err != nil {
		t.Fatalf("fallbackStopByPID returned error: %v", err)
	}
	if stopped {
		t.Fatal("stopped = true, want false when socket unavailable")
	}
	if signalCalled {
		t.Fatal("signalCalled = true, want false for unavailable socket")
	}
}

func TestDaemonStartWhenAlreadyRunningFromPIDFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	pidPath := filepath.Join(home, ".ari", "daemon.pid")
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		t.Fatalf("create pid directory: %v", err)
	}
	if err := os.WriteFile(pidPath, []byte("1\n"), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	t.Setenv("ARI_DAEMON_PID_PATH", "~/custom.pid")

	original := daemonPIDCheck
	originalStatus := daemonStatusRPC
	daemonPIDCheck = func(path string) (int, bool, error) {
		if path != filepath.Join(home, "custom.pid") {
			t.Fatalf("pid path = %q, want %q", path, filepath.Join(home, "custom.pid"))
		}
		return 1, true, nil
	}
	daemonStatusRPC = func(context.Context, string) (daemon.StatusResponse, error) {
		return daemon.StatusResponse{PID: 1}, nil
	}
	t.Cleanup(func() {
		daemonPIDCheck = original
		daemonStatusRPC = originalStatus
	})

	out, err := executeRootCommand("daemon", "start")
	if err != nil {
		t.Fatalf("execute daemon start: %v", err)
	}

	want := "Daemon is already running (PID 1).\nHint: Run `ari daemon status` or `ari daemon stop`."
	if strings.TrimSpace(out) != want {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestEnsureDaemonRunningReturnsWhenHealthy(t *testing.T) {
	originalStatus := daemonStatusRPC
	originalLaunch := daemonAutoStartLaunch
	daemonStatusRPC = func(context.Context, string) (daemon.StatusResponse, error) {
		return daemon.StatusResponse{PID: 111}, nil
	}
	launchCalled := false
	daemonAutoStartLaunch = func(*config.Config) error {
		launchCalled = true
		return nil
	}
	t.Cleanup(func() {
		daemonStatusRPC = originalStatus
		daemonAutoStartLaunch = originalLaunch
	})

	cfg := &config.Config{Daemon: config.DaemonConfig{SocketPath: "/tmp/ari.sock", DBPath: "/tmp/ari.db", PIDPath: "/tmp/ari.pid"}}
	if err := ensureDaemonRunning(context.Background(), cfg); err != nil {
		t.Fatalf("ensureDaemonRunning returned error: %v", err)
	}
	if launchCalled {
		t.Fatal("daemon auto-start launched despite healthy daemon")
	}
}

func TestEnsureDaemonRunningLaunchesWhenUnavailable(t *testing.T) {
	originalStatus := daemonStatusRPC
	originalLaunch := daemonAutoStartLaunch
	statusCalls := 0
	daemonStatusRPC = func(context.Context, string) (daemon.StatusResponse, error) {
		statusCalls++
		if statusCalls < 3 {
			return daemon.StatusResponse{}, os.ErrNotExist
		}
		return daemon.StatusResponse{PID: 222}, nil
	}
	launchCalled := false
	daemonAutoStartLaunch = func(*config.Config) error {
		launchCalled = true
		return nil
	}
	t.Cleanup(func() {
		daemonStatusRPC = originalStatus
		daemonAutoStartLaunch = originalLaunch
	})

	cfg := &config.Config{Daemon: config.DaemonConfig{SocketPath: "/tmp/ari.sock", DBPath: "/tmp/ari.db", PIDPath: "/tmp/ari.pid"}}
	if err := ensureDaemonRunning(context.Background(), cfg); err != nil {
		t.Fatalf("ensureDaemonRunning returned error: %v", err)
	}
	if !launchCalled {
		t.Fatal("daemon auto-start launch not called for unavailable daemon")
	}
}

func TestEnsureDaemonRunningReturnsLaunchErrorWithoutPolling(t *testing.T) {
	originalStatus := daemonStatusRPC
	originalLaunch := daemonAutoStartLaunch
	daemonStatusRPC = func(context.Context, string) (daemon.StatusResponse, error) {
		return daemon.StatusResponse{}, os.ErrNotExist
	}
	daemonAutoStartLaunch = func(*config.Config) error {
		return errors.New("launch failed")
	}
	t.Cleanup(func() {
		daemonStatusRPC = originalStatus
		daemonAutoStartLaunch = originalLaunch
	})

	cfg := &config.Config{Daemon: config.DaemonConfig{SocketPath: "/tmp/ari.sock", DBPath: "/tmp/ari.db", PIDPath: "/tmp/ari.pid"}}
	err := ensureDaemonRunning(context.Background(), cfg)
	if err == nil {
		t.Fatal("ensureDaemonRunning returned nil error")
	}
	if err.Error() != "Daemon auto-start failed: launch failed" {
		t.Fatalf("ensureDaemonRunning error = %q, want %q", err.Error(), "Daemon auto-start failed: launch failed")
	}
}

func TestEnsureDaemonRunningPollTimeoutDoesNotOutliveDeadline(t *testing.T) {
	originalStatus := daemonStatusRPC
	originalLaunch := daemonAutoStartLaunch
	originalWindow := daemonAutoStartWaitWindow
	originalPollTimeout := daemonAutoStartPollTimeout
	originalPollInterval := daemonAutoStartPollInterval
	daemonAutoStartWaitWindow = 120 * time.Millisecond
	daemonAutoStartPollTimeout = 200 * time.Millisecond
	daemonAutoStartPollInterval = 1 * time.Millisecond

	statusCalls := 0
	daemonStatusRPC = func(ctx context.Context, _ string) (daemon.StatusResponse, error) {
		statusCalls++
		if statusCalls == 1 {
			return daemon.StatusResponse{}, os.ErrNotExist
		}
		<-ctx.Done()
		return daemon.StatusResponse{}, ctx.Err()
	}
	daemonAutoStartLaunch = func(*config.Config) error { return nil }
	t.Cleanup(func() {
		daemonStatusRPC = originalStatus
		daemonAutoStartLaunch = originalLaunch
		daemonAutoStartWaitWindow = originalWindow
		daemonAutoStartPollTimeout = originalPollTimeout
		daemonAutoStartPollInterval = originalPollInterval
	})

	start := time.Now()
	cfg := &config.Config{Daemon: config.DaemonConfig{SocketPath: "/tmp/ari.sock", DBPath: "/tmp/ari.db", PIDPath: "/tmp/ari.pid"}}
	err := ensureDaemonRunning(context.Background(), cfg)
	if err == nil {
		t.Fatal("ensureDaemonRunning returned nil error")
	}
	if err.Error() != "Daemon auto-start failed: daemon did not become ready" {
		t.Fatalf("ensureDaemonRunning error = %q, want %q", err.Error(), "Daemon auto-start failed: daemon did not become ready")
	}
	if time.Since(start) > 300*time.Millisecond {
		t.Fatalf("ensureDaemonRunning duration exceeded deadline budget: %v", time.Since(start))
	}
}

func TestEnsureDaemonRunningUsesSecondScaleStatusTimeouts(t *testing.T) {
	originalStatus := daemonStatusRPC
	originalLaunch := daemonAutoStartLaunch
	statusCalls := 0
	daemonStatusRPC = func(ctx context.Context, _ string) (daemon.StatusResponse, error) {
		statusCalls++
		deadline, ok := ctx.Deadline()
		if !ok {
			return daemon.StatusResponse{}, errors.New("status context deadline missing")
		}
		if time.Until(deadline) < time.Second {
			return daemon.StatusResponse{}, errors.New("status timeout below one second")
		}
		if statusCalls == 1 {
			return daemon.StatusResponse{}, os.ErrNotExist
		}
		return daemon.StatusResponse{PID: 333}, nil
	}
	daemonAutoStartLaunch = func(*config.Config) error { return nil }
	t.Cleanup(func() {
		daemonStatusRPC = originalStatus
		daemonAutoStartLaunch = originalLaunch
	})

	cfg := &config.Config{Daemon: config.DaemonConfig{SocketPath: "/tmp/ari.sock", DBPath: "/tmp/ari.db", PIDPath: "/tmp/ari.pid"}}
	if err := ensureDaemonRunning(context.Background(), cfg); err != nil {
		t.Fatalf("ensureDaemonRunning returned error: %v", err)
	}
	if statusCalls < 2 {
		t.Fatalf("statusCalls = %d, want at least 2", statusCalls)
	}
}

func TestNewDaemonAutoStartCommandUsesDetachedSession(t *testing.T) {
	cmd := newDaemonAutoStartCommand("/tmp/ari")

	if cmd == nil {
		t.Fatal("newDaemonAutoStartCommand returned nil command")
	}
	if cmd.Path != "/tmp/ari" {
		t.Fatalf("command path = %q, want %q", cmd.Path, "/tmp/ari")
	}
	if len(cmd.Args) != 4 {
		t.Fatalf("command args length = %d, want 4", len(cmd.Args))
	}
	if cmd.Args[1] != "daemon" || cmd.Args[2] != "start" || cmd.Args[3] != "--background-child" {
		t.Fatalf("command args = %#v, want daemon background child launch", cmd.Args)
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("command SysProcAttr is nil")
	}
	if !cmd.SysProcAttr.Setsid {
		t.Fatal("command SysProcAttr.Setsid = false, want true")
	}
}

func TestCheckRunningDaemonUsesSocketIdentity(t *testing.T) {
	originalCheck := daemonPIDCheck
	originalStatus := daemonStatusRPC
	daemonPIDCheck = func(path string) (int, bool, error) {
		return 999, true, nil
	}
	daemonStatusRPC = func(context.Context, string) (daemon.StatusResponse, error) {
		return daemon.StatusResponse{PID: 999}, nil
	}
	t.Cleanup(func() {
		daemonPIDCheck = originalCheck
		daemonStatusRPC = originalStatus
	})

	pid, running, err := checkRunningDaemon(context.Background(), "/tmp/ari.sock", "/tmp/ari.pid")
	if err != nil {
		t.Fatalf("checkRunningDaemon returned error: %v", err)
	}
	if !running {
		t.Fatal("running = false, want true from pid check")
	}
	if pid != 999 {
		t.Fatalf("pid = %d, want 999 from pid check", pid)
	}
}

func TestCheckRunningDaemonKeepsLivePIDWhenSocketUnavailableLegacyCase(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("555\n"), 0o600); err != nil {
		t.Fatalf("write pid marker: %v", err)
	}

	originalCheck := daemonPIDCheck
	originalStatus := daemonStatusRPC
	daemonPIDCheck = func(string) (int, bool, error) { return 555, true, nil }
	daemonStatusRPC = func(context.Context, string) (daemon.StatusResponse, error) {
		return daemon.StatusResponse{}, os.ErrNotExist
	}
	t.Cleanup(func() {
		daemonPIDCheck = originalCheck
		daemonStatusRPC = originalStatus
	})

	pid, running, err := checkRunningDaemon(context.Background(), "/tmp/ari.sock", pidPath)
	if err != nil {
		t.Fatalf("checkRunningDaemon returned error: %v", err)
	}
	if !running {
		t.Fatal("running = false, want true for live pid")
	}
	if pid != 555 {
		t.Fatalf("pid = %d, want 555 from live pid", pid)
	}
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("pid file stat error = %v, want retained", err)
	}
}

func TestCheckRunningDaemonKeepsLivePIDWhenSocketUnavailable(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("555\n"), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	originalCheck := daemonPIDCheck
	originalStatus := daemonStatusRPC
	daemonPIDCheck = func(string) (int, bool, error) { return 555, true, nil }
	daemonStatusRPC = func(context.Context, string) (daemon.StatusResponse, error) {
		return daemon.StatusResponse{}, os.ErrNotExist
	}
	t.Cleanup(func() {
		daemonPIDCheck = originalCheck
		daemonStatusRPC = originalStatus
	})

	pid, running, err := checkRunningDaemon(context.Background(), "/tmp/ari.sock", pidPath)
	if err != nil {
		t.Fatalf("checkRunningDaemon returned error: %v", err)
	}
	if !running {
		t.Fatal("running = false, want true when live pid exists")
	}
	if pid != 555 {
		t.Fatalf("pid = %d, want 555", pid)
	}
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("pid file stat error = %v, want retained", err)
	}
}

func TestCheckRunningDaemonTreatsSocketDaemonAsRunningOnPIDMismatch(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("111\n"), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	originalCheck := daemonPIDCheck
	originalStatus := daemonStatusRPC
	daemonPIDCheck = func(string) (int, bool, error) { return 111, true, nil }
	daemonStatusRPC = func(context.Context, string) (daemon.StatusResponse, error) {
		return daemon.StatusResponse{PID: 222}, nil
	}
	t.Cleanup(func() {
		daemonPIDCheck = originalCheck
		daemonStatusRPC = originalStatus
	})

	pid, running, err := checkRunningDaemon(context.Background(), "/tmp/ari.sock", pidPath)
	if err != nil {
		t.Fatalf("checkRunningDaemon returned error: %v", err)
	}
	if !running {
		t.Fatal("running = false, want true when daemon status succeeds")
	}
	if pid != 222 {
		t.Fatalf("pid = %d, want status pid 222", pid)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file stat error = %v, want removed stale pid file", err)
	}
}

func TestDaemonStopReturnsFallbackErrorWhenRPCUnavailableButFallbackFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_DAEMON_PID_PATH", "~/env.pid")
	t.Setenv("ARI_DAEMON_SOCKET_PATH", "~/env.sock")

	originalStopRPC := daemonStopRPC
	originalPIDCheck := daemonPIDCheck

	daemonStopRPC = func(context.Context, string) error {
		return os.ErrNotExist
	}
	daemonPIDCheck = func(string) (int, bool, error) {
		return 0, false, os.ErrPermission
	}
	t.Cleanup(func() {
		daemonStopRPC = originalStopRPC
		daemonPIDCheck = originalPIDCheck
	})

	_, err := executeRootCommand("daemon", "stop")
	if err == nil {
		t.Fatal("execute daemon stop returned nil error")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("error = %v, want permission error from fallback", err)
	}
}

func TestConfiguredSocketPathReadsConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ariDir := filepath.Join(home, ".ari")
	if err := os.MkdirAll(ariDir, 0o755); err != nil {
		t.Fatalf("create .ari dir: %v", err)
	}

	configBody := `{
		"daemon": {
			"socket_path": "~/.ari/custom.sock",
			"db_path": "~/.ari/custom.db"
		},
		"log_level": "info"
	}`

	if err := os.WriteFile(filepath.Join(ariDir, "config.json"), []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := configuredDaemonConfig()
	if err != nil {
		t.Fatalf("configuredDaemonConfig: %v", err)
	}

	want := filepath.Join(home, ".ari", "custom.sock")
	if cfg.Daemon.SocketPath != want {
		t.Fatalf("configured socket path = %q, want %q", cfg.Daemon.SocketPath, want)
	}

	wantDB := filepath.Join(home, ".ari", "custom.db")
	if cfg.Daemon.DBPath != wantDB {
		t.Fatalf("configured db path = %q, want %q", cfg.Daemon.DBPath, wantDB)
	}
}

func TestConfiguredDaemonConfigWithSourceUsesEnvironmentLabel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_DAEMON_SOCKET_PATH", "~/env.sock")
	t.Setenv("ARI_DAEMON_DB_PATH", "~/env.db")

	cfg, configPath, source, err := configuredDaemonConfigWithSource()
	if err != nil {
		t.Fatalf("configuredDaemonConfigWithSource: %v", err)
	}

	if source != "environment" {
		t.Fatalf("config source = %q, want environment", source)
	}
	if configPath != "" {
		t.Fatalf("config path = %q, want empty when environment-only", configPath)
	}

	if cfg.Daemon.SocketPath != filepath.Join(home, "env.sock") {
		t.Fatalf("configured socket path = %q, want env override", cfg.Daemon.SocketPath)
	}
	if cfg.Daemon.DBPath != filepath.Join(home, "env.db") {
		t.Fatalf("configured db path = %q, want env override", cfg.Daemon.DBPath)
	}
}

func TestConfiguredDaemonConfigWithSourceReturnsConfigValidationErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ARI_DAEMON_SOCKET_PATH", "")
	t.Setenv("ARI_DAEMON_DB_PATH", "~/db.sqlite")
	t.Setenv("ARI_DAEMON_PID_PATH", "~/daemon.pid")

	configDir := filepath.Join(home, ".ari")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	body := `{"daemon":{"socket_path":"","db_path":"~/.ari/db.sqlite","pid_path":"~/.ari/daemon.pid"},"log_level":"info"}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, _, err := configuredDaemonConfigWithSource()
	if err == nil {
		t.Fatal("configuredDaemonConfigWithSource returned nil error")
	}
	if !strings.Contains(err.Error(), "normalize config: socket path: path is required") {
		t.Fatalf("error = %q, want config normalization/validation message", err.Error())
	}
}

func TestDaemonStatusAndStopHappyPath(t *testing.T) {
	requireAtlas(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	ariDir := filepath.Join(home, ".ari")
	if err := os.MkdirAll(ariDir, 0o755); err != nil {
		t.Fatalf("create .ari dir: %v", err)
	}

	configPath := filepath.Join(ariDir, "config.json")
	configBody := `{
		"daemon": {
			"socket_path": "~/.ari/custom.sock",
			"db_path": "~/.ari/custom.db"
		},
		"log_level": "info"
	}`
	if err := os.WriteFile(configPath, []byte(configBody), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	dbPath := filepath.Join(home, ".ari", "custom.db")
	socketPath := filepath.Join(home, ".ari", "custom.sock")

	startOut := make(chan string, 1)
	errCh := make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		output, err := executeRootCommandWithContext(ctx, "daemon", "start")
		startOut <- output
		errCh <- err
	}()

	statusOut := ""
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case runErr := <-errCh:
			startOutput := <-startOut
			t.Fatalf("daemon start exited early: %v; output=%q", runErr, startOutput)
		default:
		}

		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for daemon status")
		}

		out, err := executeRootCommand("daemon", "status")
		if err == nil && strings.Contains(out, "Daemon: running") {
			statusOut = out
			break
		}

		time.Sleep(25 * time.Millisecond)
	}

	if !strings.Contains(statusOut, "Daemon: running") ||
		!strings.Contains(statusOut, "Version:") ||
		!strings.Contains(statusOut, "PID:") ||
		!strings.Contains(statusOut, "Uptime:") ||
		!strings.Contains(statusOut, "Socket:") ||
		!strings.Contains(statusOut, "Database:") ||
		!strings.Contains(statusOut, "Config:") ||
		!strings.Contains(statusOut, "Config Source:") {
		t.Fatalf("unexpected status output: %q", statusOut)
	}

	if !strings.Contains(statusOut, "Socket: "+socketPath) {
		t.Fatalf("status output = %q, want configured socket path", statusOut)
	}
	if !strings.Contains(statusOut, "Database: "+dbPath+" (healthy)") {
		t.Fatalf("status output = %q, want healthy configured database path", statusOut)
	}
	if !strings.Contains(statusOut, "Config: "+configPath) {
		t.Fatalf("status output = %q, want config path", statusOut)
	}
	if !strings.Contains(statusOut, "Config Source: file") {
		t.Fatalf("status output = %q, want file config source", statusOut)
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("stat bootstrapped database path: %v", err)
	}

	stopOut, err := executeRootCommand("daemon", "stop")
	if err != nil {
		t.Fatalf("execute daemon stop: %v", err)
	}
	if strings.TrimSpace(stopOut) != "Daemon stopping" {
		t.Fatalf("unexpected stop output: %q", stopOut)
	}

	select {
	case runErr := <-errCh:
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			t.Fatalf("daemon start command error: %v", runErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for daemon start command to exit")
	}

	if out := <-startOut; !strings.Contains(out, "Ari daemon starting") {
		t.Fatalf("unexpected start output: %q", out)
	}
}

func requireAtlas(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("atlas"); err != nil {
		t.Skip("atlas CLI is required for daemon bootstrap tests")
	}
}

func executeRootCommand(args ...string) (string, error) {
	return executeRootCommandWithContext(context.Background(), args...)
}

func executeRootCommandWithContext(ctx context.Context, args ...string) (string, error) {
	originalCommandEnsure := commandEnsureDaemonRunning
	originalAgentEnsure := agentEnsureDaemonRunning
	originalSessionEnsure := sessionEnsureDaemonRunning
	originalSessionGet := sessionGetRPC
	originalSessionList := sessionListRPC
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	agentEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }

	cwd := "."
	if wd, err := os.Getwd(); err == nil {
		cwd = wd
	}

	if len(args) > 0 && (args[0] == "command" || args[0] == "agent") {
		sessionGetRPC = func(_ context.Context, _ string, idOrName string) (daemon.SessionGetResponse, error) {
			resolved := strings.TrimSpace(idOrName)
			if resolved == "" {
				return daemon.SessionGetResponse{}, userFacingError{message: "Session identifier is required"}
			}
			return daemon.SessionGetResponse{
				SessionID:  resolved,
				Name:       resolved,
				OriginRoot: cwd,
				Folders:    []daemon.SessionFolderInfo{{Path: cwd, VCSType: "none", IsPrimary: true}},
			}, nil
		}
		sessionListRPC = func(context.Context, string) (daemon.SessionListResponse, error) {
			return daemon.SessionListResponse{Sessions: []daemon.SessionSummary{{SessionID: "sess-1", Name: "alpha", Status: "active", FolderCount: 1}}}, nil
		}
	}
	defer func() {
		commandEnsureDaemonRunning = originalCommandEnsure
		agentEnsureDaemonRunning = originalAgentEnsure
		sessionEnsureDaemonRunning = originalSessionEnsure
		sessionGetRPC = originalSessionGet
		sessionListRPC = originalSessionList
	}()

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetContext(ctx)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func executeRootCommandRaw(args ...string) (string, error) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetContext(context.Background())
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestCommandListReturnsEnsureDaemonError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := commandEnsureDaemonRunning
	commandEnsureDaemonRunning = func(context.Context, *config.Config) error {
		return userFacingError{message: "daemon ensure failed"}
	}
	t.Cleanup(func() {
		commandEnsureDaemonRunning = originalEnsure
	})

	_, err := executeRootCommandRaw("command", "list", "--session", "alpha")
	if err == nil {
		t.Fatal("command list returned nil error")
	}
	if err.Error() != "daemon ensure failed" {
		t.Fatalf("command list error = %q, want %q", err.Error(), "daemon ensure failed")
	}
}

func TestAgentListReturnsEnsureDaemonError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := agentEnsureDaemonRunning
	agentEnsureDaemonRunning = func(context.Context, *config.Config) error {
		return userFacingError{message: "daemon ensure failed"}
	}
	t.Cleanup(func() {
		agentEnsureDaemonRunning = originalEnsure
	})

	_, err := executeRootCommandRaw("agent", "list", "--session", "alpha")
	if err == nil {
		t.Fatal("agent list returned nil error")
	}
	if err.Error() != "daemon ensure failed" {
		t.Fatalf("agent list error = %q, want %q", err.Error(), "daemon ensure failed")
	}
}

func TestSessionListReturnsEnsureDaemonError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := sessionEnsureDaemonRunning
	sessionEnsureDaemonRunning = func(context.Context, *config.Config) error {
		return userFacingError{message: "daemon ensure failed"}
	}
	t.Cleanup(func() {
		sessionEnsureDaemonRunning = originalEnsure
	})

	_, err := executeRootCommandRaw("session", "list")
	if err == nil {
		t.Fatal("session list returned nil error")
	}
	if err.Error() != "daemon ensure failed" {
		t.Fatalf("session list error = %q, want %q", err.Error(), "daemon ensure failed")
	}
}
