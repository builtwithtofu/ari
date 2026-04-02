package vcs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, dir string)
		wantName string
	}{
		{
			name: "detects jj when jj directory exists",
			setup: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, ".jj"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			wantName: "jj",
		},
		{
			name: "detects git when git directory exists",
			setup: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			wantName: "git",
		},
		{
			name: "detects git when git marker is file",
			setup: func(t *testing.T, dir string) {
				if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /tmp/worktree/.git/worktrees/test"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantName: "git",
		},
		{
			name:     "falls back to none when no vcs detected",
			setup:    func(t *testing.T, dir string) {},
			wantName: "none",
		},
		{
			name: "prefers jj over git when both present",
			setup: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, ".jj"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			wantName: "jj",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(t, tmpDir)

			backend, err := Detect(tmpDir)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}

			if got := backend.Name(); got != tt.wantName {
				t.Errorf("Name() = %q, want %q", got, tt.wantName)
			}
		})
	}
}

func TestDetect_ParentDirectory(t *testing.T) {
	// Test that detection walks up the directory tree.
	tmpDir := t.TempDir()
	childDir := filepath.Join(tmpDir, "a", "b", "c")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create .git in the root.
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	backend, err := Detect(childDir)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	if got := backend.Name(); got != "git" {
		t.Errorf("Name() = %q, want %q", got, "git")
	}

	if got := backend.Root(); got != tmpDir {
		t.Errorf("Root() = %q, want %q", got, tmpDir)
	}
}

func TestNoneBackend(t *testing.T) {
	backend := &noneBackend{root: "/tmp"}

	if got := backend.Name(); got != "none" {
		t.Errorf("Name() = %q, want %q", got, "none")
	}

	if got := backend.IsAvailable(); got != false {
		t.Errorf("IsAvailable() = %v, want %v", got, false)
	}

	if got := backend.Root(); got != "/tmp" {
		t.Errorf("Root() = %q, want %q", got, "/tmp")
	}

	if _, err := backend.CurrentBranch(); !errors.Is(err, ErrNotSupported) {
		t.Errorf("CurrentBranch() error = %v, want ErrNotSupported", err)
	}

	if _, err := backend.RecentCommits(5); !errors.Is(err, ErrNotSupported) {
		t.Errorf("RecentCommits() error = %v, want ErrNotSupported", err)
	}

	if _, err := backend.ChangedFiles(); !errors.Is(err, ErrNotSupported) {
		t.Errorf("ChangedFiles() error = %v, want ErrNotSupported", err)
	}

	if err := backend.CreateCommit("test"); !errors.Is(err, ErrNotSupported) {
		t.Errorf("CreateCommit() error = %v, want ErrNotSupported", err)
	}

	if err := backend.CreateBranch("test"); !errors.Is(err, ErrNotSupported) {
		t.Errorf("CreateBranch() error = %v, want ErrNotSupported", err)
	}
}

func TestParseGitCommits(t *testing.T) {
	input := `abc1234|Initial commit|Alice|2024-01-01 10:00:00 +0000
				def5678|Add feature|Bob|2024-01-02 11:00:00 +0000`

	commits := parseGitCommits(input)

	if len(commits) != 2 {
		t.Fatalf("len(commits) = %d, want 2", len(commits))
	}

	if commits[0].Hash != "abc1234" {
		t.Errorf("commits[0].Hash = %q, want %q", commits[0].Hash, "abc1234")
	}
	if commits[0].Message != "Initial commit" {
		t.Errorf("commits[0].Message = %q, want %q", commits[0].Message, "Initial commit")
	}
	if commits[0].Author != "Alice" {
		t.Errorf("commits[0].Author = %q, want %q", commits[0].Author, "Alice")
	}

	if commits[1].Hash != "def5678" {
		t.Errorf("commits[1].Hash = %q, want %q", commits[1].Hash, "def5678")
	}
}

func TestParseJJCommits(t *testing.T) {
	input := `xyz1234|abc5678|Alice|2024-01-01 10:00:00 +0000|Initial commit
				uvw5678|def9012|Bob|2024-01-02 11:00:00 +0000|Add feature`

	commits := parseJJCommits(input)

	if len(commits) != 2 {
		t.Fatalf("len(commits) = %d, want 2", len(commits))
	}

	if commits[0].Hash != "xyz1234" {
		t.Errorf("commits[0].Hash = %q, want %q", commits[0].Hash, "xyz1234")
	}
	if commits[0].Message != "Initial commit" {
		t.Errorf("commits[0].Message = %q, want %q", commits[0].Message, "Initial commit")
	}

	if commits[1].Hash != "uvw5678" {
		t.Errorf("commits[1].Hash = %q, want %q", commits[1].Hash, "uvw5678")
	}
}

func TestParseJJChangedFiles(t *testing.T) {
	input := `M path/to/modified.go
A path/to/added.go
D path/to/deleted.go`

	files := parseJJChangedFiles(input)

	want := []string{"path/to/modified.go", "path/to/added.go", "path/to/deleted.go"}
	if len(files) != len(want) {
		t.Fatalf("len(files) = %d, want %d", len(files), len(want))
	}

	for i, f := range want {
		if files[i] != f {
			t.Errorf("files[%d] = %q, want %q", i, files[i], f)
		}
	}
}

func TestHasDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Test with non-existent directory.
	if hasDirectory(tmpDir, "nonexistent") {
		t.Error("hasDirectory() = true for non-existent dir, want false")
	}

	// Test with existing directory.
	subDir := filepath.Join(tmpDir, "exists")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !hasDirectory(tmpDir, "exists") {
		t.Error("hasDirectory() = false for existing dir, want true")
	}

	// Test with file (not directory).
	filePath := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	if hasDirectory(tmpDir, "file.txt") {
		t.Error("hasDirectory() = true for file, want false")
	}
}
