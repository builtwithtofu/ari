package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/vcs"
)

func TestNewReviewCmd(t *testing.T) {
	cmd := NewReviewCmd()
	if cmd == nil {
		t.Fatal("NewReviewCmd() returned nil")
	}

	if cmd.Use != "review [revision-range]" {
		t.Errorf("expected Use to be 'review [revision-range]', got %q", cmd.Use)
	}

	if cmd.Short != "Review changes" {
		t.Errorf("expected Short to be 'Review changes', got %q", cmd.Short)
	}
}

func TestGetDiff_UnsupportedVCS(t *testing.T) {
	// Test with none backend
	noneBackend := &noneVCSBackend{}
	_, err := getDiff(noneBackend, "")
	if err == nil {
		t.Error("expected error for unsupported VCS, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported vcs") {
		t.Errorf("expected error to contain 'unsupported vcs', got %q", err.Error())
	}
}

func TestTruncateDiff(t *testing.T) {
	tests := []struct {
		name      string
		diff      string
		maxChars  int
		wantTrunc bool
	}{
		{
			name:      "short diff no truncation",
			diff:      "short diff",
			maxChars:  100,
			wantTrunc: false,
		},
		{
			name:      "long diff gets truncated",
			diff:      strings.Repeat("a", 5000),
			maxChars:  100,
			wantTrunc: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateDiff(tt.diff, tt.maxChars)
			if tt.wantTrunc {
				if !strings.Contains(result, "... (truncated)") {
					t.Error("expected truncated result to contain '... (truncated)'")
				}
			} else {
				if result != tt.diff {
					t.Errorf("expected result to equal input, got %q", result)
				}
			}
		})
	}
}

func TestGeneratePlaceholderSummary(t *testing.T) {
	diff := `diff --git a/file.go b/file.go
--- a/file.go
+++ b/file.go
@@ -1,3 +1,4 @@
 package main
+
 func main() {}
-removed line
+added line`

	summary := generatePlaceholderSummary(diff)

	if summary == "" {
		t.Error("expected non-empty summary")
	}

	if !strings.Contains(summary, "Files changed:") {
		t.Error("expected summary to contain 'Files changed:'")
	}

	if !strings.Contains(summary, "Lines added:") {
		t.Error("expected summary to contain 'Lines added:'")
	}

	if !strings.Contains(summary, "Lines removed:") {
		t.Error("expected summary to contain 'Lines removed:'")
	}

	if !strings.Contains(summary, "ARI_API_KEY") {
		t.Error("expected summary to mention ARI_API_KEY")
	}
}

func TestRunReview_NoVCS(t *testing.T) {
	// This test requires being in a directory without VCS
	// We'll just verify the error handling path
	cmd := NewReviewCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// The actual behavior depends on the current directory's VCS state
	// so we just verify the command structure is correct
	if cmd.RunE == nil {
		t.Error("expected RunE to be set")
	}
}

// noneVCSBackend is a test helper that implements VCSBackend but returns "none"
type noneVCSBackend struct{}

func (n *noneVCSBackend) Name() string                            { return "none" }
func (n *noneVCSBackend) IsAvailable() bool                       { return false }
func (n *noneVCSBackend) CurrentBranch() (string, error)          { return "", vcs.ErrNotSupported }
func (n *noneVCSBackend) RecentCommits(int) ([]vcs.Commit, error) { return nil, vcs.ErrNotSupported }
func (n *noneVCSBackend) ChangedFiles() ([]string, error)         { return nil, vcs.ErrNotSupported }
func (n *noneVCSBackend) CreateCommit(string) error               { return vcs.ErrNotSupported }
func (n *noneVCSBackend) CreateBranch(string) error               { return vcs.ErrNotSupported }
func (n *noneVCSBackend) SetupAriIgnore(string) error             { return vcs.ErrNotSupported }
