package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

func TestTelemetryRollupPrintsKnownAndUnknownValues(t *testing.T) {
	h := newCommandHarness(t)
	input := int64(12)
	pid := int64(123)
	swapTestValue(t, &telemetryEnsureDaemonRunning, func(context.Context, *config.Config) error { return nil })
	swapTestValue(t, &telemetryRollupRPC, func(_ context.Context, _ string, req daemon.TelemetryRollupRequest) (daemon.TelemetryRollupResponse, error) {
		if req.WorkspaceID != "ws-1" {
			t.Fatalf("workspace id = %q, want ws-1", req.WorkspaceID)
		}
		return daemon.TelemetryRollupResponse{Rollups: []daemon.TelemetryRollup{{Group: daemon.TelemetryRollupGroup{Profile: "executor", Harness: "codex", Model: "gpt-5.1-codex", InvocationClass: "sticky"}, Runs: 2, Completed: 1, Failed: 1, InputTokens: daemon.TelemetryKnownInt64{Known: true, Value: &input}, OutputTokens: daemon.TelemetryKnownInt64{Known: false}, EstimatedCost: daemon.TelemetryKnownInt64{Known: false}, DurationMS: daemon.TelemetryKnownInt64{Known: false}, Process: daemon.TelemetryProcessRollup{OwnedByAri: true, PID: daemon.TelemetryKnownInt64{Known: true, Value: &pid}, ExitCode: daemon.TelemetryKnownInt64{Known: false}, OrphanState: "not_orphaned", Ports: []daemon.ProcessPortObservation{{Port: 5173, Protocol: "tcp", Confidence: "detected"}}}}}}, nil
	})

	out, err := h.execute("telemetry", "rollup", "--workspace-id", "ws-1")
	if err != nil {
		t.Fatalf("telemetry rollup returned error: %v", err)
	}
	expected := "telemetry\tprofile=executor\tharness=codex\tmodel=gpt-5.1-codex\tinvocation_class=sticky\truns=2\tcompleted=1\tfailed=1\ninput_tokens=12\toutput_tokens=unknown\testimated_cost=unknown\tduration_ms=unknown\texit_code=unknown\nprocess_owned=true\tpid=123\tcpu_time_ms=unknown\tmemory_rss_bytes_peak=unknown\tchild_processes_peak=unknown\norphan_state=not_orphaned\tports=tcp/5173/detected\n"
	if out != expected {
		t.Fatalf("output = %q, want %q", out, expected)
	}
}

func TestTelemetryRollupRequiresWorkspaceID(t *testing.T) {
	h := newCommandHarness(t)
	swapTestValue(t, &telemetryRollupRPC, func(context.Context, string, daemon.TelemetryRollupRequest) (daemon.TelemetryRollupResponse, error) {
		return daemon.TelemetryRollupResponse{}, errors.New("rollup should not be called")
	})

	_, err := h.execute("telemetry", "rollup")
	if err == nil || err.Error() != "Provide --workspace-id" {
		t.Fatalf("telemetry rollup error = %v, want workspace requirement", err)
	}
}
