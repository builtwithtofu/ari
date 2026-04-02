package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadRemovePIDFileRoundTrip(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")

	if err := WritePIDFile(pidPath, 12345); err != nil {
		t.Fatalf("WritePIDFile returned error: %v", err)
	}

	pid, err := ReadPIDFile(pidPath)
	if err != nil {
		t.Fatalf("ReadPIDFile returned error: %v", err)
	}
	if pid != 12345 {
		t.Fatalf("pid = %d, want 12345", pid)
	}

	if err := RemovePIDFile(pidPath); err != nil {
		t.Fatalf("RemovePIDFile returned error: %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid path stat error = %v, want not exists", err)
	}
}

func TestCheckPIDFileRemovesStaleFile(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("4242\n"), 0o600); err != nil {
		t.Fatalf("write stale pid file: %v", err)
	}

	original := pidIsRunning
	pidIsRunning = func(pid int) (bool, error) {
		if pid != 4242 {
			t.Fatalf("pid check = %d, want 4242", pid)
		}
		return false, nil
	}
	t.Cleanup(func() {
		pidIsRunning = original
	})

	pid, running, err := CheckPIDFile(pidPath)
	if err != nil {
		t.Fatalf("CheckPIDFile returned error: %v", err)
	}
	if running {
		t.Fatal("running = true, want false for stale pid")
	}
	if pid != 4242 {
		t.Fatalf("pid = %d, want 4242", pid)
	}

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid path stat error = %v, want file removed", err)
	}
}

func TestCheckPIDFileReturnsRunningPID(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("8181\n"), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	original := pidIsRunning
	pidIsRunning = func(pid int) (bool, error) {
		if pid != 8181 {
			t.Fatalf("pid check = %d, want 8181", pid)
		}
		return true, nil
	}
	t.Cleanup(func() {
		pidIsRunning = original
	})

	pid, running, err := CheckPIDFile(pidPath)
	if err != nil {
		t.Fatalf("CheckPIDFile returned error: %v", err)
	}
	if !running {
		t.Fatal("running = false, want true")
	}
	if pid != 8181 {
		t.Fatalf("pid = %d, want 8181", pid)
	}
}

func TestCheckPIDFileRemovesMalformedFile(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("not-a-pid\n"), 0o600); err != nil {
		t.Fatalf("write malformed pid file: %v", err)
	}

	pid, running, err := CheckPIDFile(pidPath)
	if err != nil {
		t.Fatalf("CheckPIDFile returned error: %v", err)
	}
	if running {
		t.Fatal("running = true, want false")
	}
	if pid != 0 {
		t.Fatalf("pid = %d, want 0 for malformed file", pid)
	}

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid path stat error = %v, want file removed", err)
	}
}

func TestRemovePIDFileIfOwned(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "daemon.pid")
	if err := os.WriteFile(pidPath, []byte("6000\n"), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	if err := RemovePIDFileIfOwned(pidPath, 7000); err != nil {
		t.Fatalf("RemovePIDFileIfOwned mismatch returned error: %v", err)
	}
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("pid file stat after mismatch = %v, want retained", err)
	}

	if err := RemovePIDFileIfOwned(pidPath, 6000); err != nil {
		t.Fatalf("RemovePIDFileIfOwned owned returned error: %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file stat after owned remove = %v, want not exists", err)
	}
}
