package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var pidIsRunning = isProcessRunning

func DefaultPIDFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".ari", "daemon.pid"), nil
}

func WritePIDFile(pidPath string, pid int) error {
	if strings.TrimSpace(pidPath) == "" {
		return fmt.Errorf("pid path is required")
	}
	if pid <= 0 {
		return fmt.Errorf("pid must be positive")
	}

	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		return fmt.Errorf("create pid file directory: %w", err)
	}

	file, err := os.OpenFile(pidPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("pid file already exists: %w", os.ErrExist)
		}
		return fmt.Errorf("write pid file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	if _, err := fmt.Fprintf(file, "%d\n", pid); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}

	return nil
}

func ReadPIDFile(pidPath string) (int, error) {
	if strings.TrimSpace(pidPath) == "" {
		return 0, fmt.Errorf("pid path is required")
	}

	content, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, fmt.Errorf("read pid file: %w", err)
	}

	value := strings.TrimSpace(string(content))
	pid, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse pid file %q: %w", pidPath, err)
	}
	if pid <= 0 {
		return 0, fmt.Errorf("parse pid file %q: pid must be positive", pidPath)
	}

	return pid, nil
}

func RemovePIDFile(pidPath string) error {
	if strings.TrimSpace(pidPath) == "" {
		return fmt.Errorf("pid path is required")
	}

	if err := os.Remove(pidPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove pid file: %w", err)
	}

	return nil
}

func RemovePIDFileIfOwned(pidPath string, pid int) error {
	if strings.TrimSpace(pidPath) == "" {
		return fmt.Errorf("pid path is required")
	}
	if pid <= 0 {
		return fmt.Errorf("pid must be positive")
	}

	currentPID, err := ReadPIDFile(pidPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return RemovePIDFile(pidPath)
	}

	if currentPID != pid {
		return nil
	}

	return RemovePIDFile(pidPath)
}

func CheckPIDFile(pidPath string) (int, bool, error) {
	if strings.TrimSpace(pidPath) == "" {
		return 0, false, fmt.Errorf("pid path is required")
	}

	if _, err := os.Stat(pidPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("stat pid file: %w", err)
	}

	pid, err := ReadPIDFile(pidPath)
	if err != nil {
		if removeErr := RemovePIDFile(pidPath); removeErr != nil {
			return 0, false, removeErr
		}
		return 0, false, nil
	}

	running, err := pidIsRunning(pid)
	if err != nil {
		return 0, false, fmt.Errorf("check pid %d: %w", pid, err)
	}
	if running {
		return pid, true, nil
	}

	if err := RemovePIDFile(pidPath); err != nil {
		return 0, false, err
	}

	return pid, false, nil
}

func isProcessRunning(pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("pid must be positive")
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, err
	}

	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if errors.Is(err, syscall.EPERM) {
		return true, nil
	}

	return false, err
}
