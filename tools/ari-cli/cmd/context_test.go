package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

func TestContextExcerptTailCallsPublicRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := contextEnsureDaemonRunning
	originalTail := contextTailRPC
	contextEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	contextTailRPC = func(_ context.Context, _ string, req daemon.ContextExcerptCreateFromTailRequest) (daemon.ContextExcerptResponse, error) {
		if req.SourceSessionID != "planner-main" || req.ContextExcerptID != "plan-tail" || req.Count != 10 {
			t.Fatalf("context.excerpt.create_from_tail request = %#v", req)
		}
		return daemon.ContextExcerptResponse{ContextExcerptID: "plan-tail"}, nil
	}
	t.Cleanup(func() {
		contextEnsureDaemonRunning = originalEnsure
		contextTailRPC = originalTail
	})

	out, err := executeRootCommand("context", "excerpt", "tail", "--session", "planner-main", "--last", "10", "--id", "plan-tail")
	if err != nil {
		t.Fatalf("context excerpt tail returned error: %v", err)
	}
	if !strings.Contains(out, "Context excerpt created: plan-tail") {
		t.Fatalf("context excerpt tail output = %q, want created excerpt id", out)
	}
}

func TestContextExcerptShowCallsPublicRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := contextEnsureDaemonRunning
	originalGet := contextGetRPC
	contextEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	contextGetRPC = func(_ context.Context, _ string, req daemon.ContextExcerptGetRequest) (daemon.ContextExcerptResponse, error) {
		if req.ContextExcerptID != "plan-tail" {
			t.Fatalf("context.excerpt.get request = %#v", req)
		}
		return daemon.ContextExcerptResponse{ContextExcerptID: "plan-tail", SourceSessionID: "planner-main", SelectorType: "tail", AppendedMessage: "Begin phase 1"}, nil
	}
	t.Cleanup(func() {
		contextEnsureDaemonRunning = originalEnsure
		contextGetRPC = originalGet
	})

	out, err := executeRootCommand("context", "excerpt", "show", "plan-tail")
	if err != nil {
		t.Fatalf("context excerpt show returned error: %v", err)
	}
	for _, want := range []string{"plan-tail", "planner-main", "tail", "Begin phase 1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("context excerpt show output = %q, want %q", out, want)
		}
	}
}

func TestContextExcerptRangeCallsPublicRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := contextEnsureDaemonRunning
	originalRange := contextRangeRPC
	contextEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	contextRangeRPC = func(_ context.Context, _ string, req daemon.ContextExcerptCreateFromRangeRequest) (daemon.ContextExcerptResponse, error) {
		if req.SourceSessionID != "planner-main" || req.ContextExcerptID != "plan-range" || req.StartSequence != 2 || req.EndSequence != 7 {
			t.Fatalf("context.excerpt.create_from_range request = %#v", req)
		}
		return daemon.ContextExcerptResponse{ContextExcerptID: "plan-range"}, nil
	}
	t.Cleanup(func() {
		contextEnsureDaemonRunning = originalEnsure
		contextRangeRPC = originalRange
	})

	out, err := executeRootCommand("context", "excerpt", "range", "--session", "planner-main", "--start", "2", "--end", "7", "--id", "plan-range")
	if err != nil {
		t.Fatalf("context excerpt range returned error: %v", err)
	}
	if !strings.Contains(out, "Context excerpt created: plan-range") {
		t.Fatalf("context excerpt range output = %q, want created excerpt id", out)
	}
}

func TestContextExcerptMessagesCallsPublicRPC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalEnsure := contextEnsureDaemonRunning
	originalMessages := contextMessagesRPC
	contextEnsureDaemonRunning = func(context.Context, *config.Config) error { return nil }
	contextMessagesRPC = func(_ context.Context, _ string, req daemon.ContextExcerptCreateFromExplicitIDsRequest) (daemon.ContextExcerptResponse, error) {
		if req.SourceSessionID != "planner-main" || req.ContextExcerptID != "plan-msgs" || len(req.MessageIDs) != 2 || req.MessageIDs[0] != "msg-2" || req.MessageIDs[1] != "msg-9" {
			t.Fatalf("context.excerpt.create_from_explicit_ids request = %#v", req)
		}
		return daemon.ContextExcerptResponse{ContextExcerptID: "plan-msgs"}, nil
	}
	t.Cleanup(func() {
		contextEnsureDaemonRunning = originalEnsure
		contextMessagesRPC = originalMessages
	})

	out, err := executeRootCommand("context", "excerpt", "messages", "--session", "planner-main", "--message-id", "msg-2", "--message-id", "msg-9", "--id", "plan-msgs")
	if err != nil {
		t.Fatalf("context excerpt messages returned error: %v", err)
	}
	if !strings.Contains(out, "Context excerpt created: plan-msgs") {
		t.Fatalf("context excerpt messages output = %q, want created excerpt id", out)
	}
}
