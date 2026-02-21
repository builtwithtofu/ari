package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvStorageMode = "ARI_STORAGE_MODE"
	EnvStorageRoot = "ARI_STORAGE_ROOT"
)

type StorageMode string

const (
	StorageModeGlobal  StorageMode = "global"
	StorageModeProject StorageMode = "project"
)

var (
	ErrInvalidStorageMode        = errors.New("invalid storage mode")
	ErrProjectStorageRequiresOpt = errors.New("project storage requires explicit opt-in")
)

const projectStorageOptInHint = "set storage mode to 'project' via --storage-mode=project, ARI_STORAGE_MODE=project, or config"

type StoragePolicyInput struct {
	CLIStorageMode    string
	CLIStorageRoot    string
	ConfigStorageMode string
	ConfigStorageRoot string
	Env               map[string]string
	WorkingDir        string
	UserConfigDir     string
}

type StoragePolicy struct {
	Mode StorageMode
	Root string
}

func ResolveStoragePolicy(input StoragePolicyInput) (StoragePolicy, error) {
	mode, err := resolveStorageMode(input)
	if err != nil {
		return StoragePolicy{}, err
	}

	root, err := resolveStorageRoot(input, mode)
	if err != nil {
		return StoragePolicy{}, err
	}

	if mode != StorageModeProject && pointsToProjectStorage(root, input.WorkingDir) {
		return StoragePolicy{}, fmt.Errorf("%w: root %q resolves to project-local .ari; %s", ErrProjectStorageRequiresOpt, root, projectStorageOptInHint)
	}

	return StoragePolicy{Mode: mode, Root: root}, nil
}

func DefaultGlobalStorageRoot(userConfigDir string) (string, error) {
	if strings.TrimSpace(userConfigDir) == "" {
		var err error
		userConfigDir, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	}

	return filepath.Join(userConfigDir, "ari"), nil
}

func resolveStorageMode(input StoragePolicyInput) (StorageMode, error) {
	rawMode := strings.TrimSpace(firstNonEmpty(
		input.CLIStorageMode,
		input.Env[EnvStorageMode],
		input.ConfigStorageMode,
		string(StorageModeGlobal),
	))

	mode := StorageMode(strings.ToLower(rawMode))
	if mode != StorageModeGlobal && mode != StorageModeProject {
		return "", fmt.Errorf("%w: %q (allowed: %q, %q)", ErrInvalidStorageMode, rawMode, StorageModeGlobal, StorageModeProject)
	}

	return mode, nil
}

func resolveStorageRoot(input StoragePolicyInput, mode StorageMode) (string, error) {
	root := strings.TrimSpace(firstNonEmpty(
		input.CLIStorageRoot,
		input.Env[EnvStorageRoot],
		input.ConfigStorageRoot,
	))

	if root != "" {
		if filepath.IsAbs(root) {
			return filepath.Clean(root), nil
		}
		if strings.TrimSpace(input.WorkingDir) == "" {
			return filepath.Clean(root), nil
		}
		return filepath.Clean(filepath.Join(input.WorkingDir, root)), nil
	}

	if mode == StorageModeProject {
		if strings.TrimSpace(input.WorkingDir) == "" {
			return filepath.Clean(".ari"), nil
		}
		return filepath.Join(input.WorkingDir, ".ari"), nil
	}

	return DefaultGlobalStorageRoot(input.UserConfigDir)
}

func pointsToProjectStorage(root, workingDir string) bool {
	cleanRoot := filepath.Clean(strings.TrimSpace(root))
	if cleanRoot == "" {
		return false
	}

	if cleanRoot == ".ari" {
		return true
	}

	cleanWorkingDir := filepath.Clean(strings.TrimSpace(workingDir))
	if cleanWorkingDir == "" {
		return false
	}

	projectRoot := filepath.Join(cleanWorkingDir, ".ari")
	return cleanRoot == projectRoot
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
