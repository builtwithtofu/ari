package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStoragePolicyPrecedence(t *testing.T) {
	workingDir := filepath.Join(string(filepath.Separator), "repo")
	userConfigDir := filepath.Join(string(filepath.Separator), "home", "dev", ".config")

	tests := []struct {
		name     string
		input    StoragePolicyInput
		wantMode StorageMode
		wantRoot string
		wantErr  error
		wantMsg  string
	}{
		{
			name: "default uses global storage root",
			input: StoragePolicyInput{
				WorkingDir:    workingDir,
				UserConfigDir: userConfigDir,
			},
			wantMode: StorageModeGlobal,
			wantRoot: filepath.Join(userConfigDir, "ari"),
		},
		{
			name: "default ignores empty values and uses global root",
			input: StoragePolicyInput{
				CLIStorageMode:    "   ",
				ConfigStorageMode: "",
				ConfigStorageRoot: "   ",
				Env: map[string]string{
					EnvStorageMode: "   ",
					EnvStorageRoot: "",
				},
				WorkingDir:    workingDir,
				UserConfigDir: userConfigDir,
			},
			wantMode: StorageModeGlobal,
			wantRoot: filepath.Join(userConfigDir, "ari"),
		},
		{
			name: "config mode enables project storage",
			input: StoragePolicyInput{
				ConfigStorageMode: string(StorageModeProject),
				WorkingDir:        workingDir,
				UserConfigDir:     userConfigDir,
			},
			wantMode: StorageModeProject,
			wantRoot: filepath.Join(workingDir, ".ari"),
		},
		{
			name: "env mode overrides config mode",
			input: StoragePolicyInput{
				ConfigStorageMode: string(StorageModeProject),
				Env: map[string]string{
					EnvStorageMode: string(StorageModeGlobal),
				},
				WorkingDir:    workingDir,
				UserConfigDir: userConfigDir,
			},
			wantMode: StorageModeGlobal,
			wantRoot: filepath.Join(userConfigDir, "ari"),
		},
		{
			name: "cli mode overrides env mode",
			input: StoragePolicyInput{
				CLIStorageMode:    string(StorageModeProject),
				ConfigStorageMode: string(StorageModeGlobal),
				Env: map[string]string{
					EnvStorageMode: string(StorageModeGlobal),
				},
				WorkingDir:    workingDir,
				UserConfigDir: userConfigDir,
			},
			wantMode: StorageModeProject,
			wantRoot: filepath.Join(workingDir, ".ari"),
		},
		{
			name: "root precedence follows cli env config",
			input: StoragePolicyInput{
				CLIStorageRoot:    filepath.Join(string(filepath.Separator), "cli-root"),
				ConfigStorageRoot: filepath.Join(string(filepath.Separator), "config-root"),
				Env: map[string]string{
					EnvStorageRoot: filepath.Join(string(filepath.Separator), "env-root"),
				},
				WorkingDir:    workingDir,
				UserConfigDir: userConfigDir,
			},
			wantMode: StorageModeGlobal,
			wantRoot: filepath.Join(string(filepath.Separator), "cli-root"),
		},
		{
			name: "env root is used when cli root empty",
			input: StoragePolicyInput{
				CLIStorageRoot:    " ",
				ConfigStorageRoot: filepath.Join(string(filepath.Separator), "config-root"),
				Env: map[string]string{
					EnvStorageRoot: filepath.Join(string(filepath.Separator), "env-root"),
				},
				WorkingDir:    workingDir,
				UserConfigDir: userConfigDir,
			},
			wantMode: StorageModeGlobal,
			wantRoot: filepath.Join(string(filepath.Separator), "env-root"),
		},
		{
			name: "config root is used when cli and env roots empty",
			input: StoragePolicyInput{
				CLIStorageRoot:    "",
				ConfigStorageRoot: "config-root",
				Env: map[string]string{
					EnvStorageRoot: " ",
				},
				WorkingDir:    workingDir,
				UserConfigDir: userConfigDir,
			},
			wantMode: StorageModeGlobal,
			wantRoot: filepath.Join(workingDir, "config-root"),
		},
		{
			name: "relative root resolves from working dir",
			input: StoragePolicyInput{
				CLIStorageRoot: "tmp/cache",
				WorkingDir:     workingDir,
			},
			wantMode: StorageModeGlobal,
			wantRoot: filepath.Join(workingDir, "tmp", "cache"),
		},
		{
			name: "project mode default root is project .ari",
			input: StoragePolicyInput{
				CLIStorageMode: string(StorageModeProject),
				WorkingDir:     workingDir,
			},
			wantMode: StorageModeProject,
			wantRoot: filepath.Join(workingDir, ".ari"),
		},
		{
			name: "invalid mode returns deterministic error",
			input: StoragePolicyInput{
				CLIStorageMode: "invalid",
				WorkingDir:     workingDir,
			},
			wantErr: ErrInvalidStorageMode,
			wantMsg: fmt.Sprintf("%s: %q (allowed: %q, %q)", ErrInvalidStorageMode.Error(), "invalid", StorageModeGlobal, StorageModeProject),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveStoragePolicy(tt.input)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("ResolveStoragePolicy() error = nil, want %v", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ResolveStoragePolicy() error = %v, want %v", err, tt.wantErr)
				}
				if tt.wantMsg != "" && err.Error() != tt.wantMsg {
					t.Fatalf("ResolveStoragePolicy() error message = %q, want %q", err.Error(), tt.wantMsg)
				}
				return
			}

			if err != nil {
				t.Fatalf("ResolveStoragePolicy() error = %v", err)
			}

			if got.Mode != tt.wantMode {
				t.Fatalf("ResolveStoragePolicy() mode = %q, want %q", got.Mode, tt.wantMode)
			}

			if got.Root != tt.wantRoot {
				t.Fatalf("ResolveStoragePolicy() root = %q, want %q", got.Root, tt.wantRoot)
			}
		})
	}
}

func TestResolveStoragePolicyProjectStorageRequiresExplicitOptIn(t *testing.T) {
	workingDir := filepath.Join(string(filepath.Separator), "repo")

	tests := []struct {
		name    string
		input   StoragePolicyInput
		wantErr string
	}{
		{
			name: "config root to .ari without project mode",
			input: StoragePolicyInput{
				ConfigStorageRoot: ".ari",
				WorkingDir:        workingDir,
			},
			wantErr: fmt.Sprintf("%s: root %q resolves to project-local .ari; %s", ErrProjectStorageRequiresOpt.Error(), filepath.Join(workingDir, ".ari"), projectStorageOptInHint),
		},
		{
			name: "env root to .ari without project mode",
			input: StoragePolicyInput{
				Env: map[string]string{
					EnvStorageRoot: ".ari",
				},
				WorkingDir: workingDir,
			},
			wantErr: fmt.Sprintf("%s: root %q resolves to project-local .ari; %s", ErrProjectStorageRequiresOpt.Error(), filepath.Join(workingDir, ".ari"), projectStorageOptInHint),
		},
		{
			name: "cli root to .ari without project mode",
			input: StoragePolicyInput{
				CLIStorageRoot: ".ari",
				WorkingDir:     workingDir,
			},
			wantErr: fmt.Sprintf("%s: root %q resolves to project-local .ari; %s", ErrProjectStorageRequiresOpt.Error(), filepath.Join(workingDir, ".ari"), projectStorageOptInHint),
		},
		{
			name: "abs project .ari root without project mode",
			input: StoragePolicyInput{
				CLIStorageRoot: filepath.Join(workingDir, ".ari"),
				WorkingDir:     workingDir,
			},
			wantErr: fmt.Sprintf("%s: root %q resolves to project-local .ari; %s", ErrProjectStorageRequiresOpt.Error(), filepath.Join(workingDir, ".ari"), projectStorageOptInHint),
		},
		{
			name: "relative canonical project .ari root without project mode",
			input: StoragePolicyInput{
				CLIStorageRoot: "./.ari",
				WorkingDir:     workingDir,
			},
			wantErr: fmt.Sprintf("%s: root %q resolves to project-local .ari; %s", ErrProjectStorageRequiresOpt.Error(), filepath.Join(workingDir, ".ari"), projectStorageOptInHint),
		},
		{
			name: "relative project .ari slash root without project mode",
			input: StoragePolicyInput{
				CLIStorageRoot: ".ari/",
				WorkingDir:     workingDir,
			},
			wantErr: fmt.Sprintf("%s: root %q resolves to project-local .ari; %s", ErrProjectStorageRequiresOpt.Error(), filepath.Join(workingDir, ".ari"), projectStorageOptInHint),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveStoragePolicy(tt.input)
			if err == nil {
				t.Fatal("ResolveStoragePolicy() error = nil, want project storage opt-in error")
			}
			if !errors.Is(err, ErrProjectStorageRequiresOpt) {
				t.Fatalf("ResolveStoragePolicy() error = %v, want %v", err, ErrProjectStorageRequiresOpt)
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("ResolveStoragePolicy() error message = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestResolveStoragePolicyAllowsProjectStorageWithExplicitProjectMode(t *testing.T) {
	workingDir := filepath.Join(string(filepath.Separator), "repo")

	got, err := ResolveStoragePolicy(StoragePolicyInput{
		CLIStorageMode: string(StorageModeProject),
		CLIStorageRoot: ".ari",
		WorkingDir:     workingDir,
	})
	if err != nil {
		t.Fatalf("ResolveStoragePolicy() error = %v", err)
	}

	if got.Mode != StorageModeProject {
		t.Fatalf("ResolveStoragePolicy() mode = %q, want %q", got.Mode, StorageModeProject)
	}
	if got.Root != filepath.Join(workingDir, ".ari") {
		t.Fatalf("ResolveStoragePolicy() root = %q, want %q", got.Root, filepath.Join(workingDir, ".ari"))
	}
}

func TestResolveStoragePolicyDefaultDoesNotCreateProjectDotAri(t *testing.T) {
	workingDir := t.TempDir()
	userConfigDir := filepath.Join(t.TempDir(), "config-home")
	projectDotAri := filepath.Join(workingDir, ".ari")

	if _, err := os.Stat(projectDotAri); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("precondition: project .ari stat error = %v, want not-exist", err)
	}

	got, err := ResolveStoragePolicy(StoragePolicyInput{
		WorkingDir:    workingDir,
		UserConfigDir: userConfigDir,
	})
	if err != nil {
		t.Fatalf("ResolveStoragePolicy() error = %v", err)
	}

	if got.Mode != StorageModeGlobal {
		t.Fatalf("ResolveStoragePolicy() mode = %q, want %q", got.Mode, StorageModeGlobal)
	}

	wantRoot := filepath.Join(userConfigDir, "ari")
	if got.Root != wantRoot {
		t.Fatalf("ResolveStoragePolicy() root = %q, want %q", got.Root, wantRoot)
	}

	if _, err := os.Stat(projectDotAri); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("project .ari stat error after default resolution = %v, want not-exist", err)
	}
}

func TestResolveStoragePolicyInvalidEnvModeReturnsDeterministicError(t *testing.T) {
	workingDir := filepath.Join(string(filepath.Separator), "repo")

	_, err := ResolveStoragePolicy(StoragePolicyInput{
		Env: map[string]string{
			EnvStorageMode: "legacy",
		},
		WorkingDir: workingDir,
	})
	if err == nil {
		t.Fatal("ResolveStoragePolicy() error = nil, want invalid storage mode error")
	}
	if !errors.Is(err, ErrInvalidStorageMode) {
		t.Fatalf("ResolveStoragePolicy() error = %v, want %v", err, ErrInvalidStorageMode)
	}

	want := fmt.Sprintf("%s: %q (allowed: %q, %q)", ErrInvalidStorageMode.Error(), "legacy", StorageModeGlobal, StorageModeProject)
	if err.Error() != want {
		t.Fatalf("ResolveStoragePolicy() error message = %q, want %q", err.Error(), want)
	}
}
