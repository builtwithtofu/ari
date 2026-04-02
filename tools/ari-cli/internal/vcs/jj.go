package vcs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// jjBackend implements VCSBackend for Jujutsu (JJ) repositories.
type jjBackend struct {
	root string
}

// Name returns "jj".
func (j *jjBackend) Name() string {
	return "jj"
}

// Root returns the repository root path.
func (j *jjBackend) Root() string {
	return j.root
}

// IsAvailable returns true if JJ is installed and the root directory is a JJ repository.
func (j *jjBackend) IsAvailable() bool {
	// Check if jj command exists.
	if _, err := exec.LookPath("jj"); err != nil {
		return false
	}

	// Check if we're in a valid jj repo.
	cmd := exec.Command("jj", "root")
	cmd.Dir = j.root
	if err := cmd.Run(); err != nil {
		return false
	}

	return true
}

// CurrentBranch returns the current bookmark name (JJ uses bookmarks instead of branches).
func (j *jjBackend) CurrentBranch() (string, error) {
	// Try to get the active bookmark.
	cmd := exec.Command("jj", "bookmark", "list", "--col", "no-pager", "--color=never")
	cmd.Dir = j.root
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting current bookmark: %w", err)
	}

	// Parse output to find active bookmark (marked with *).
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, "*") {
			// Extract bookmark name from line like "* main abc1234 ..."
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1], nil
			}
		}
	}

	// No active bookmark found - return the current change ID.
	changeCmd := exec.Command("jj", "log", "--no-graph", "-r", "@-", "-T", "change_id")
	changeCmd.Dir = j.root
	changeOut, err := changeCmd.Output()
	if err != nil {
		return "@", nil // Fallback to working copy.
	}
	return strings.TrimSpace(string(changeOut)), nil
}

// RecentCommits returns the n most recent commits (changes in JJ terminology).
func (j *jjBackend) RecentCommits(n int) ([]Commit, error) {
	// Template: change_id, commit_id, author, timestamp, description.
	template := "change_id ++ \"|\" ++ commit_id.short(8) ++ \"|\" ++ author ++ \"|\" ++ format_timestamp(timestamp) ++ \"|\" ++ description\n"
	cmd := exec.Command("jj", "log", "--no-graph", "--no-pager", "--color=never", "-n", fmt.Sprintf("%d", n), "-T", template)
	cmd.Dir = j.root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("getting recent commits: %w", err)
	}

	return parseJJCommits(string(out)), nil
}

// ChangedFiles returns the list of files with uncommitted changes.
func (j *jjBackend) ChangedFiles() ([]string, error) {
	// Get files changed between parent and working copy.
	cmd := exec.Command("jj", "diff", "--summary", "--no-pager", "--color=never", "-r", "@-", "-r", "@")
	cmd.Dir = j.root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("getting changed files: %w", err)
	}

	return parseJJChangedFiles(string(out)), nil
}

// CreateCommit creates a new commit (change in JJ) with the given message.
func (j *jjBackend) CreateCommit(message string) error {
	cmd := exec.Command("jj", "commit", "-m", message)
	cmd.Dir = j.root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating commit: %w (output: %s)", err, string(out))
	}
	return nil
}

// CreateBranch creates a new bookmark (branch equivalent in JJ) with the given name.
func (j *jjBackend) CreateBranch(name string) error {
	cmd := exec.Command("jj", "bookmark", "create", name)
	cmd.Dir = j.root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating branch: %w (output: %s)", err, string(out))
	}
	return nil
}

// parseJJCommits parses the JJ log output into Commit structs.
func parseJJCommits(output string) []Commit {
	lines := strings.Split(output, "\n")
	commits := make([]Commit, 0, len(lines))

	for _, line := range lines {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}

		parts := strings.SplitN(line, "|", 5)
		if len(parts) < 4 {
			continue
		}

		// Parts: change_id, commit_id (short), author, timestamp, description (optional).
		message := ""
		if len(parts) >= 5 {
			message = parts[4]
		}

		commits = append(commits, Commit{
			Hash:    parts[0], // Use change_id as primary hash.
			Message: message,
			Author:  parts[2],
			Date:    parts[3],
		})
	}

	return commits
}

// parseJJChangedFiles parses the JJ diff summary output into file paths.
func parseJJChangedFiles(output string) []string {
	lines := strings.Split(output, "\n")
	files := make([]string, 0, len(lines))

	for _, line := range lines {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}

		// Lines look like: "M path/to/file" or "A path/to/file" or "D path/to/file".
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			files = append(files, parts[1])
		}
	}

	return files
}

// SetupAriIgnore configures JJ to ignore the .ari/ directory.
func (j *jjBackend) SetupAriIgnore(ariDir string) error {
	// JJ respects .gitignore, so use that
	gitignorePath := filepath.Join(j.root, ".gitignore")

	if content, err := os.ReadFile(gitignorePath); err == nil {
		if strings.Contains(string(content), ".ari/") {
			return nil
		}
	}

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
