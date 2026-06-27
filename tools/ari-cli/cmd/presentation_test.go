package cmd

import (
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

func TestPresentationStatusLabelFallsBackToNormalizedStatus(t *testing.T) {
	got := presentationStatusLabel(daemon.Presentation{Status: daemon.PresentationStatusNeedsAuth}, "auth_required")
	if got != string(daemon.PresentationStatusNeedsAuth) {
		t.Fatalf("presentationStatusLabel = %q, want normalized status", got)
	}
}
