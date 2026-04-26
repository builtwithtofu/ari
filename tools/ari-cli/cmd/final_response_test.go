package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

func TestFinalResponseShowAndExportUseArtifactTextOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalEnsure := finalResponseEnsureDaemonRunning
	originalGet := finalResponseGetRPC
	finalResponseEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	finalResponseGetRPC = func(_ context.Context, _ string, req daemon.FinalResponseGetRequest) (daemon.FinalResponseResponse, error) {
		if req.RunID != "run_1" {
			t.Fatalf("get request = %#v, want run_1", req)
		}
		return daemon.FinalResponseResponse{FinalResponseID: "fr_1", RunID: "run_1", WorkspaceID: "ws-1", TaskID: "task-1", ContextPacketID: "ctx_1", Status: "completed", Text: "Shareable answer", EvidenceLinks: []daemon.FinalResponseEvidenceLink{{Kind: "context_packet", ID: "ctx_1"}}}, nil
	}
	t.Cleanup(func() {
		finalResponseEnsureDaemonRunning = originalEnsure
		finalResponseGetRPC = originalGet
	})

	showOut, err := executeRootCommand("final-response", "show", "--run-id", "run_1")
	if err != nil {
		t.Fatalf("final-response show returned error: %v", err)
	}
	if !strings.Contains(showOut, "Shareable answer") || !strings.Contains(showOut, "evidence=context_packet:ctx_1") {
		t.Fatalf("show output = %q, want text and evidence", showOut)
	}

	exportOut, err := executeRootCommand("final-response", "export", "--run-id", "run_1")
	if err != nil {
		t.Fatalf("final-response export returned error: %v", err)
	}
	if exportOut != "Shareable answer\n" {
		t.Fatalf("export output = %q, want only shareable text", exportOut)
	}
	if strings.Contains(exportOut, "ctx_1") || strings.Contains(exportOut, "provider") || strings.Contains(exportOut, "transcript") {
		t.Fatalf("export output = %q, must not include provenance, provider ids, or transcripts", exportOut)
	}
}

func TestFinalResponseListRequiresWorkspaceID(t *testing.T) {
	originalList := finalResponseListRPC
	finalResponseListRPC = func(context.Context, string, daemon.FinalResponseListRequest) (daemon.FinalResponseListResponse, error) {
		return daemon.FinalResponseListResponse{}, errors.New("list should not be called")
	}
	t.Cleanup(func() { finalResponseListRPC = originalList })

	_, err := executeRootCommand("final-response", "list")
	if err == nil || err.Error() != "Provide --workspace-id" {
		t.Fatalf("final-response list error = %v, want workspace requirement", err)
	}
}
