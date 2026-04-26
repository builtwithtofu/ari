package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	_ "modernc.org/sqlite"
)

func TestAgentSpawnSendOutputStopLifecycle(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		WorkspaceID: "sess-1",
		Name:        "harness-1",
		Command:     "/bin/sh",
		Args:        []string{"-c", "while read line; do printf 'ack:%s\\n' \"$line\"; done"},
	})

	if spawnResp.AgentID == "" {
		t.Fatal("agent.spawn returned empty agent_id")
	}
	if spawnResp.Status != "running" {
		t.Fatalf("agent.spawn status = %q, want %q", spawnResp.Status, "running")
	}

	sendResp := callMethod[AgentSendResponse](t, registry, "agent.send", AgentSendRequest{
		WorkspaceID: "sess-1",
		AgentID:     spawnResp.AgentID,
		Input:       "ping\n",
	})
	if sendResp.Status != "sent" {
		t.Fatalf("agent.send status = %q, want %q", sendResp.Status, "sent")
	}

	waitForAgentOutput(t, registry, "sess-1", spawnResp.AgentID, "ack:ping")

	outputResp := callMethod[AgentOutputResponse](t, registry, "agent.output", AgentOutputRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
	if !strings.Contains(outputResp.Output, "ack:ping") {
		t.Fatalf("agent.output = %q, want contains %q", outputResp.Output, "ack:ping")
	}

	stopResp := callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
	if stopResp.Status != "stopping" {
		t.Fatalf("agent.stop status = %q, want %q", stopResp.Status, "stopping")
	}

	waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")
}

func TestAgentSpawnUsesSessionPrimaryFolderAsCWD(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		WorkspaceID: "sess-1",
		Command:     "/bin/sh",
		Args:        []string{"-c", "pwd"},
	})

	waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "exited")

	outputResp := callMethod[AgentOutputResponse](t, registry, "agent.output", AgentOutputRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
	if !strings.Contains(outputResp.Output, workspace) {
		t.Fatalf("agent.output = %q, want contains workspace %q", outputResp.Output, workspace)
	}

	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
}

func TestAgentSpawnUsesExecutionRootPathWhenProvided(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	primaryFolder := t.TempDir()
	secondaryFolder := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", primaryFolder)
	if err := store.AddFolder(context.Background(), "sess-1", secondaryFolder, "git", false); err != nil {
		t.Fatalf("AddFolder secondary returned error: %v", err)
	}

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		WorkspaceID:       "sess-1",
		Command:           "/bin/sh",
		Args:              []string{"-c", "pwd"},
		ExecutionRootPath: secondaryFolder,
	})

	waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "exited")

	outputResp := callMethod[AgentOutputResponse](t, registry, "agent.output", AgentOutputRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
	if !strings.Contains(outputResp.Output, secondaryFolder) {
		t.Fatalf("agent.output = %q, want execution root %q", outputResp.Output, secondaryFolder)
	}
	if strings.Contains(outputResp.Output, primaryFolder) {
		t.Fatalf("agent.output = %q, did not expect primary folder %q", outputResp.Output, primaryFolder)
	}

	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
}

func TestAgentSpawnRejectsExecutionRootPathOutsideWorkspace(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	primaryFolder := t.TempDir()
	outsideFolder := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", primaryFolder)

	spec, ok := registry.Get("agent.spawn")
	if !ok {
		t.Fatal("agent.spawn method not registered")
	}
	raw, err := json.Marshal(AgentSpawnRequest{WorkspaceID: "sess-1", Command: "/bin/sh", Args: []string{"-c", "pwd"}, ExecutionRootPath: outsideFolder})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("agent.spawn returned nil error for execution root outside workspace")
	}
	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("agent.spawn error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.InvalidParams {
		t.Fatalf("agent.spawn error code = %d, want %d", rpcErr.Code, rpc.InvalidParams)
	}
}

func TestAgentSpawnRejectsInvalidInvocationClass(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	seedSessionWithPrimaryFolder(t, store, "sess-1", t.TempDir())
	err := callMethodError(registry, "agent.spawn", AgentSpawnRequest{WorkspaceID: "sess-1", InvocationClass: "temproary", Command: "/bin/sh", Args: []string{"-c", "exit 0"}})
	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("agent.spawn error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.InvalidParams {
		t.Fatalf("agent.spawn error code = %d, want %d", rpcErr.Code, rpc.InvalidParams)
	}
}

func TestAgentSpawnRejectsRelativeExecutionRootPath(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	primaryFolder := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", primaryFolder)

	spec, ok := registry.Get("agent.spawn")
	if !ok {
		t.Fatal("agent.spawn method not registered")
	}
	raw, err := json.Marshal(AgentSpawnRequest{WorkspaceID: "sess-1", Command: "/bin/sh", Args: []string{"-c", "pwd"}, ExecutionRootPath: "relative/path"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("agent.spawn returned nil error for relative execution root")
	}
	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("agent.spawn error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.InvalidParams {
		t.Fatalf("agent.spawn error code = %d, want %d", rpcErr.Code, rpc.InvalidParams)
	}
}

func TestAgentListAndGetIncludeSpawnedAgent(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		WorkspaceID: "sess-1",
		Name:        "opencode",
		Command:     "/bin/sh",
		Args:        []string{"-c", "while true; do sleep 1; done"},
	})

	listResp := callMethod[AgentListResponse](t, registry, "agent.list", AgentListRequest{WorkspaceID: "sess-1"})
	if len(listResp.Agents) != 1 {
		t.Fatalf("agent.list len = %d, want 1", len(listResp.Agents))
	}
	if listResp.Agents[0].AgentID != spawnResp.AgentID {
		t.Fatalf("agent.list[0].agent_id = %q, want %q", listResp.Agents[0].AgentID, spawnResp.AgentID)
	}

	getResp := callMethod[AgentGetResponse](t, registry, "agent.get", AgentGetRequest{WorkspaceID: "sess-1", AgentID: "opencode"})
	if getResp.AgentID != spawnResp.AgentID {
		t.Fatalf("agent.get agent_id = %q, want %q", getResp.AgentID, spawnResp.AgentID)
	}

	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
	waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "stopped")
}

func TestAgentListHidesTemporaryAgentsByDefault(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}
	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	regular := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{WorkspaceID: "sess-1", Name: "regular", Command: "/bin/sh", Args: []string{"-c", "while true; do sleep 1; done"}})
	temporary := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{WorkspaceID: "sess-1", Name: "temporary", InvocationClass: string(HarnessInvocationTemporary), Command: "/bin/sh", Args: []string{"-c", "while true; do sleep 1; done"}})

	defaultList := callMethod[AgentListResponse](t, registry, "agent.list", AgentListRequest{WorkspaceID: "sess-1"})
	if len(defaultList.Agents) != 1 || defaultList.Agents[0].AgentID != regular.AgentID || defaultList.Agents[0].InvocationClass != string(HarnessInvocationAgent) {
		t.Fatalf("default list = %#v, want only regular agent", defaultList.Agents)
	}
	fullList := callMethod[AgentListResponse](t, registry, "agent.list", AgentListRequest{WorkspaceID: "sess-1", ShowTemporary: true})
	if len(fullList.Agents) != 2 {
		t.Fatalf("full list len = %d, want 2: %#v", len(fullList.Agents), fullList.Agents)
	}
	getResp := callMethod[AgentGetResponse](t, registry, "agent.get", AgentGetRequest{WorkspaceID: "sess-1", AgentID: temporary.AgentID})
	if getResp.InvocationClass != string(HarnessInvocationTemporary) {
		t.Fatalf("temporary get invocation class = %q, want temporary", getResp.InvocationClass)
	}

	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{WorkspaceID: "sess-1", AgentID: regular.AgentID})
	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{WorkspaceID: "sess-1", AgentID: temporary.AgentID})
}

func TestAgentSendReturnsAgentNotRunningAfterSelfExit(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		WorkspaceID: "sess-1",
		Command:     "/bin/sh",
		Args:        []string{"-c", "printf done; exit 0"},
	})

	waitForAgentStatus(t, registry, "sess-1", spawnResp.AgentID, "exited")

	spec, ok := registry.Get("agent.send")
	if !ok {
		t.Fatal("agent.send method not registered")
	}
	raw, err := json.Marshal(AgentSendRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID, Input: "late input"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("agent.send returned nil error for exited agent")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("agent.send error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.AgentNotRunning {
		t.Fatalf("agent.send error code = %d, want %d", rpcErr.Code, rpc.AgentNotRunning)
	}
}

func TestAgentSpawnHarnessLauncherUsesDefaultBinaryWhenCommandMissing(t *testing.T) {
	launcher, err := resolveAgentLauncher("opencode")
	if err != nil {
		t.Fatalf("resolveAgentLauncher returned error: %v", err)
	}

	spec, err := launcher.prepare("", []string{"--resume"})
	if err != nil {
		t.Fatalf("launcher.prepare returned error: %v", err)
	}
	if spec.Command != "opencode" {
		t.Fatalf("launcher command = %q, want %q", spec.Command, "opencode")
	}
	if len(spec.Args) != 1 || spec.Args[0] != "--resume" {
		t.Fatalf("launcher args = %v, want [--resume]", spec.Args)
	}
}

func TestAgentSpawnPersistsHarnessSessionIdentityFromResumeArgs(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)
	shim := writeNoopCommandShim(t)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		WorkspaceID: "sess-1",
		Harness:     "opencode",
		Command:     shim,
		Args:        []string{"--session", "resume-xyz"},
	})

	getResp := callMethod[AgentGetResponse](t, registry, "agent.get", AgentGetRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
	if getResp.Harness != "opencode" {
		t.Fatalf("agent.get harness = %q, want %q", getResp.Harness, "opencode")
	}
	if getResp.HarnessResumableID != "resume-xyz" {
		t.Fatalf("agent.get harness_resumable_id = %q, want %q", getResp.HarnessResumableID, "resume-xyz")
	}
	if string(getResp.HarnessMetadata) != `{"resume_source":"argv"}` {
		t.Fatalf("agent.get harness_metadata = %q, want %q", string(getResp.HarnessMetadata), `{"resume_source":"argv"}`)
	}

	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
}

func TestAgentSpawnLeavesResumableIdentityEmptyWhenHarnessDoesNotExposeOne(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)
	shim := writeNoopCommandShim(t)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		WorkspaceID: "sess-1",
		Harness:     "codex",
		Command:     shim,
		Args:        []string{"--model", "gpt-5-codex"},
	})

	getResp := callMethod[AgentGetResponse](t, registry, "agent.get", AgentGetRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
	if getResp.Harness != "codex" {
		t.Fatalf("agent.get harness = %q, want %q", getResp.Harness, "codex")
	}
	if getResp.HarnessResumableID != "" {
		t.Fatalf("agent.get harness_resumable_id = %q, want empty", getResp.HarnessResumableID)
	}
	if string(getResp.HarnessMetadata) != "{}" {
		t.Fatalf("agent.get harness_metadata = %q, want %q", string(getResp.HarnessMetadata), "{}")
	}

	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
}

func TestAgentSpawnInfersHarnessFromExplicitCommand(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)
	shim := writeNamedNoopCommandShim(t, "claude-code")

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		WorkspaceID: "sess-1",
		Command:     shim,
		Args:        []string{"--resume=cl-abc"},
	})

	getResp := callMethod[AgentGetResponse](t, registry, "agent.get", AgentGetRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
	if getResp.Harness != "claude-code" {
		t.Fatalf("agent.get harness = %q, want %q", getResp.Harness, "claude-code")
	}
	if getResp.HarnessResumableID != "cl-abc" {
		t.Fatalf("agent.get harness_resumable_id = %q, want %q", getResp.HarnessResumableID, "cl-abc")
	}

	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
}

func TestAgentSpawnUsesHarnessProjectionSeam(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	originalProjector := agentHarnessProjector
	agentHarnessProjector = harnessProjectorFunc(func(harness string, args []string) HarnessProjection {
		if harness != "opencode" {
			t.Fatalf("projector harness = %q, want %q", harness, "opencode")
		}
		if len(args) != 2 || args[0] != "--session" || args[1] != "resume-xyz" {
			t.Fatalf("projector args = %v, want [--session resume-xyz]", args)
		}
		projectedHarness := "projected-harness"
		projectedResume := "projected-resume"
		return HarnessProjection{
			Harness:     &projectedHarness,
			ResumableID: &projectedResume,
			Metadata:    `{"resume_source":"projector"}`,
		}
	})
	t.Cleanup(func() {
		agentHarnessProjector = originalProjector
	})

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)
	shim := writeNoopCommandShim(t)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		WorkspaceID: "sess-1",
		Harness:     "opencode",
		Command:     shim,
		Args:        []string{"--session", "resume-xyz"},
	})

	getResp := callMethod[AgentGetResponse](t, registry, "agent.get", AgentGetRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
	if getResp.Harness != "projected-harness" {
		t.Fatalf("agent.get harness = %q, want %q", getResp.Harness, "projected-harness")
	}
	if getResp.HarnessResumableID != "projected-resume" {
		t.Fatalf("agent.get harness_resumable_id = %q, want %q", getResp.HarnessResumableID, "projected-resume")
	}
	if string(getResp.HarnessMetadata) != `{"resume_source":"projector"}` {
		t.Fatalf("agent.get harness_metadata = %q, want %q", string(getResp.HarnessMetadata), `{"resume_source":"projector"}`)
	}

	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
}

func TestAgentSpawnUsesDefaultProjectorWhenSeamIsNil(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	originalProjector := agentHarnessProjector
	agentHarnessProjector = nil
	t.Cleanup(func() {
		agentHarnessProjector = originalProjector
	})

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)
	shim := writeNoopCommandShim(t)

	spawnResp := callMethod[AgentSpawnResponse](t, registry, "agent.spawn", AgentSpawnRequest{
		WorkspaceID: "sess-1",
		Harness:     "opencode",
		Command:     shim,
		Args:        []string{"--session", "resume-xyz"},
	})

	getResp := callMethod[AgentGetResponse](t, registry, "agent.get", AgentGetRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
	if getResp.Harness != "opencode" {
		t.Fatalf("agent.get harness = %q, want %q", getResp.Harness, "opencode")
	}
	if getResp.HarnessResumableID != "resume-xyz" {
		t.Fatalf("agent.get harness_resumable_id = %q, want %q", getResp.HarnessResumableID, "resume-xyz")
	}
	if string(getResp.HarnessMetadata) != `{"resume_source":"argv"}` {
		t.Fatalf("agent.get harness_metadata = %q, want %q", string(getResp.HarnessMetadata), `{"resume_source":"argv"}`)
	}

	_ = callMethod[AgentStopResponse](t, registry, "agent.stop", AgentStopRequest{WorkspaceID: "sess-1", AgentID: spawnResp.AgentID})
}

func TestParseHarnessResumableID(t *testing.T) {
	tests := []struct {
		name    string
		harness string
		args    []string
		want    string
	}{
		{name: "opencode long form", harness: "opencode", args: []string{"--session", "op-123"}, want: "op-123"},
		{name: "opencode equals form", harness: "opencode", args: []string{"--session=op-456"}, want: "op-456"},
		{name: "opencode missing value", harness: "opencode", args: []string{"--session"}, want: ""},
		{name: "claude long form", harness: "claude-code", args: []string{"--resume", "cl-123"}, want: "cl-123"},
		{name: "claude equals form", harness: "claude-code", args: []string{"--resume=cl-456"}, want: "cl-456"},
		{name: "claude missing value", harness: "claude-code", args: []string{"--resume"}, want: ""},
		{name: "claude next flag", harness: "claude-code", args: []string{"--resume", "--model", "sonnet"}, want: ""},
		{name: "unsupported harness", harness: "codex", args: []string{"--resume", "cx-1"}, want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseHarnessResumableID(tc.harness, tc.args); got != tc.want {
				t.Fatalf("parseHarnessResumableID = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAgentSpawnRejectsUnknownHarness(t *testing.T) {
	store := newAgentMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")

	if err := d.registerAgentMethods(registry, store); err != nil {
		t.Fatalf("registerAgentMethods returned error: %v", err)
	}

	workspace := t.TempDir()
	seedSessionWithPrimaryFolder(t, store, "sess-1", workspace)

	spec, ok := registry.Get("agent.spawn")
	if !ok {
		t.Fatal("agent.spawn method not registered")
	}

	raw, err := json.Marshal(AgentSpawnRequest{WorkspaceID: "sess-1", Harness: "unknown-harness", Args: []string{"--resume"}})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	_, err = spec.Call(context.Background(), raw)
	if err == nil {
		t.Fatal("agent.spawn returned nil error for unknown harness")
	}

	var rpcErr *rpc.HandlerError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("agent.spawn error type = %T, want *rpc.HandlerError", err)
	}
	if rpcErr.Code != rpc.InvalidParams {
		t.Fatalf("agent.spawn error code = %d, want %d", rpcErr.Code, rpc.InvalidParams)
	}
}

func TestPersistAgentStatusWithRetryHonorsContextCancellation(t *testing.T) {
	originalUpdate := updateAgentStatus
	updateAgentStatus = func(_ *globaldb.Store, ctx context.Context, _ globaldb.UpdateAgentStatusParams) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return errors.New("context was not forwarded")
		}
	}
	t.Cleanup(func() {
		updateAgentStatus = originalUpdate
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := persistAgentStatusWithRetry(ctx, nil, globaldb.UpdateAgentStatusParams{WorkspaceID: "sess-1", AgentID: "agt-1", Status: "running"}, 60*time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("persistAgentStatusWithRetry error = %v, want context.Canceled", err)
	}
}

func TestWriteAllBytesRetriesPartialWrites(t *testing.T) {
	writer := &partialWriter{writes: []int{3, 2, 4}}

	err := writeAllBytes(writer, []byte("abcdefghi"))
	if err != nil {
		t.Fatalf("writeAllBytes returned error: %v", err)
	}
	if writer.total != 9 {
		t.Fatalf("writeAllBytes wrote %d bytes, want 9", writer.total)
	}
}

func TestWriteAllBytesReturnsErrorOnZeroProgress(t *testing.T) {
	writer := &partialWriter{writes: []int{0}}

	err := writeAllBytes(writer, []byte("abc"))
	if err == nil {
		t.Fatal("writeAllBytes returned nil error for zero-byte write")
	}
	if !strings.Contains(err.Error(), "zero bytes") {
		t.Fatalf("writeAllBytes error = %q, want mentions zero bytes", err.Error())
	}
}

type partialWriter struct {
	writes []int
	index  int
	total  int
}

func (w *partialWriter) Write(p []byte) (int, error) {
	if w.index >= len(w.writes) {
		n := len(p)
		w.total += n
		return n, nil
	}
	n := w.writes[w.index]
	w.index++
	if n > len(p) {
		n = len(p)
	}
	if n < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	w.total += n
	return n, nil
}

func waitForAgentStatus(t *testing.T, registry *rpc.MethodRegistry, sessionID, agentID, want string) {
	t.Helper()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		resp := callMethod[AgentGetResponse](t, registry, "agent.get", AgentGetRequest{WorkspaceID: sessionID, AgentID: agentID})
		if resp.Status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("agent %s status did not reach %q before timeout", agentID, want)
}

func waitForAgentOutput(t *testing.T, registry *rpc.MethodRegistry, sessionID, agentID, wantSubstring string) {
	t.Helper()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		resp := callMethod[AgentOutputResponse](t, registry, "agent.output", AgentOutputRequest{WorkspaceID: sessionID, AgentID: agentID})
		if strings.Contains(resp.Output, wantSubstring) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("agent %s output did not contain %q before timeout", agentID, wantSubstring)
}

func newAgentMethodTestStore(t *testing.T) *globaldb.Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "agent-method.db")
	if err := applyMigrationSQLFiles(dbPath); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		t.Fatalf("set busy timeout: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	store, err := globaldb.NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore returned error: %v", err)
	}

	return store
}

func writeNoopCommandShim(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "noop.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	return path
}

func writeNamedNoopCommandShim(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	return path
}
