// Package vcs provides version control system detection and operations.
package vcs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	// ErrNotSupported indicates an operation is not supported by the current VCS backend.
	ErrNotSupported = errors.New("operation not supported")
	// ErrNoVCS indicates no version control system was detected.
	ErrNoVCS = errors.New("no version control system detected")
)

// Commit represents a single commit in the VCS history.
type Commit struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}

// VCSBackend defines the interface for version control system operations.
type VCSBackend interface {
	// Name returns the identifier for this VCS ("git", "jj", "none").
	Name() string
	// IsAvailable returns true if this VCS is available in the current directory.
	IsAvailable() bool

	// CurrentBranch returns the name of the current branch/bookmark.
	CurrentBranch() (string, error)
	// RecentCommits returns the n most recent commits.
	RecentCommits(n int) ([]Commit, error)
	// ChangedFiles returns the list of files with uncommitted changes.
	ChangedFiles() ([]string, error)

	// CreateCommit creates a new commit with the given message.
	// May return ErrNotSupported for read-only backends.
	CreateCommit(message string) error
	// CreateBranch creates a new branch/bookmark with the given name.
	// May return ErrNotSupported for read-only backends.
	CreateBranch(name string) error

	// SetupAriIgnore configures the VCS to ignore the .ari/ directory.
	// Returns ErrNotSupported if the VCS doesn't support ignore files.
	SetupAriIgnore(ariDir string) error
}

// Detect finds and returns the appropriate VCS backend for the given directory.
// Detection order: JJ (.jj/) -> Git (.git/) -> None (fallback).
func Detect(dir string) (VCSBackend, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving absolute path: %w", err)
	}

	// Walk up the directory tree looking for VCS directories.
	for current := absDir; current != "/"; current = filepath.Dir(current) {
		// Check for JJ first (priority as per architecture spec).
		if hasDirectory(current, ".jj") {
			return &jjBackend{root: current}, nil
		}

		// Check for Git.
		if hasDirectory(current, ".git") {
			return &gitBackend{root: current}, nil
		}
	}

	// No VCS detected - return the none backend.
	return &noneBackend{root: absDir}, nil
}

// hasDirectory checks if the given subdirectory exists within root.
func hasDirectory(root, subdir string) bool {
	path := filepath.Join(root, subdir)
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
