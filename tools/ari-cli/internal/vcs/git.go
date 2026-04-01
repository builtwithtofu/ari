package vcs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitBackend implements VCSBackend for Git repositories.
type gitBackend struct {
	root string
}

// Name returns "git".
func (g *gitBackend) Name() string {
	return "git"
}

// IsAvailable returns true if Git is installed and the root directory is a Git repository.
func (g *gitBackend) IsAvailable() bool {
	// Check if git command exists.
	if _, err := exec.LookPath("git"); err != nil {
		return false
	}

	// Check if we're in a valid git repo.
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = g.root
	if err := cmd.Run(); err != nil {
		return false
	}

	return true
}

// CurrentBranch returns the current branch name.
func (g *gitBackend) CurrentBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = g.root
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting current branch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// RecentCommits returns the n most recent commits.
func (g *gitBackend) RecentCommits(n int) ([]Commit, error) {
	format := "%H|%s|%an|%ci"
	cmd := exec.Command("git", "log", fmt.Sprintf("-%d", n), fmt.Sprintf("--format=%s", format))
	cmd.Dir = g.root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("getting recent commits: %w", err)
	}

	return parseGitCommits(string(out)), nil
}

// ChangedFiles returns the list of files with uncommitted changes.
func (g *gitBackend) ChangedFiles() ([]string, error) {
	// Get staged changes.
	stagedCmd := exec.Command("git", "diff", "--cached", "--name-only")
	stagedCmd.Dir = g.root
	stagedOut, err := stagedCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("getting staged files: %w", err)
	}

	// Get unstaged changes.
	unstagedCmd := exec.Command("git", "diff", "--name-only")
	unstagedCmd.Dir = g.root
	unstagedOut, err := unstagedCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("getting unstaged files: %w", err)
	}

	// Get untracked files.
	untrackedCmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	untrackedCmd.Dir = g.root
	untrackedOut, err := untrackedCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("getting untracked files: %w", err)
	}

	// Combine and deduplicate.
	files := make(map[string]bool)
	for _, f := range strings.Split(string(stagedOut), "\n") {
		if f = strings.TrimSpace(f); f != "" {
			files[f] = true
		}
	}
	for _, f := range strings.Split(string(unstagedOut), "\n") {
		if f = strings.TrimSpace(f); f != "" {
			files[f] = true
		}
	}
	for _, f := range strings.Split(string(untrackedOut), "\n") {
		if f = strings.TrimSpace(f); f != "" {
			files[f] = true
		}
	}

	result := make([]string, 0, len(files))
	for f := range files {
		result = append(result, f)
	}
	return result, nil
}

// CreateCommit creates a new commit with the given message.
func (g *gitBackend) CreateCommit(message string) error {
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = g.root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating commit: %w (output: %s)", err, string(out))
	}
	return nil
}

// CreateBranch creates a new branch with the given name.
func (g *gitBackend) CreateBranch(name string) error {
	cmd := exec.Command("git", "checkout", "-b", name)
	cmd.Dir = g.root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating branch: %w (output: %s)", err, string(out))
	}
	return nil
}

// parseGitCommits parses the git log output into Commit structs.
func parseGitCommits(output string) []Commit {
	lines := strings.Split(output, "\n")
	commits := make([]Commit, 0, len(lines))

	for _, line := range lines {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}

		parts := strings.SplitN(line, "|", 4)
		if len(parts) != 4 {
			continue
		}

		commits = append(commits, Commit{
			Hash:    parts[0],
			Message: parts[1],
			Author:  parts[2],
			Date:    parts[3],
		})
	}

	return commits
}

// SetupAriIgnore configures Git to ignore the .ari/ directory.
func (g *gitBackend) SetupAriIgnore(ariDir string) error {
	gitignorePath := filepath.Join(g.root, ".gitignore")

	// Check if .ari/ is already ignored
	if content, err := os.ReadFile(gitignorePath); err == nil {
		if strings.Contains(string(content), ".ari/") {
			return nil // Already ignored
		}
	}

	// Append to .gitignore
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	_, err = f.WriteString("\n# Ari world directory\n.ari/\n")
	return err
}
