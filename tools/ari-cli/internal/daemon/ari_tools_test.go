package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestAriToolSchemaExposesStarterToolsAndScopeMetadata(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	resp := callMethod[AriToolListResponse](t, registry, "ari.tool.list", AriToolListRequest{})
	got := map[string]AriToolSchema{}
	for _, tool := range resp.Tools {
		got[tool.Name] = tool
	}
	for _, name := range []string{"ari.defaults.get", "ari.defaults.set", "ari.profile.draft", "ari.profile.save", "ari.self_check", "ari.run.explain_latest", "ari.session.fanout", "ari.fanout.status", "ari.inbox.list"} {
		tool, ok := got[name]
		if !ok {
			t.Fatalf("missing tool %q in %#v", name, resp.Tools)
		}
		if len(tool.RequiredScopeFields) == 0 || !tool.ScopeRequired {
			t.Fatalf("tool %q missing scope metadata contract: %#v", name, tool)
		}
	}
	if !got["ari.defaults.set"].ApprovalRequired || got["ari.defaults.get"].ApprovalRequired || got["ari.session.fanout"].ApprovalRequired || got["ari.fanout.status"].ApprovalRequired || got["ari.inbox.list"].ApprovalRequired {
		t.Fatalf("unexpected approval flags: %#v", got)
	}
	if got["ari.defaults.get"].OperationKind != daemonOperationKindReadOnly || got["ari.defaults.set"].OperationKind != daemonOperationKindMutating || got["ari.session.fanout"].OperationKind != daemonOperationKindMutating || got["ari.fanout.status"].OperationKind != daemonOperationKindReadOnly || got["ari.inbox.list"].OperationKind != daemonOperationKindReadOnly {
		t.Fatalf("unexpected operation kinds: defaults.get=%#v defaults.set=%#v", got["ari.defaults.get"], got["ari.defaults.set"])
	}
	if !got["ari.fanout.status"].ReadOnly || !got["ari.inbox.list"].ReadOnly || got["ari.session.fanout"].ReadOnly {
		t.Fatalf("unexpected read-only flags: fanout=%#v status=%#v inbox=%#v", got["ari.session.fanout"], got["ari.fanout.status"], got["ari.inbox.list"])
	}
	if len(got["ari.defaults.get"].TrustChoices) != 0 || !containsAriToolTestString(got["ari.defaults.set"].TrustChoices, "trust_always_by_operation_type") {
		t.Fatalf("unexpected trust choices: defaults.get=%#v defaults.set=%#v", got["ari.defaults.get"], got["ari.defaults.set"])
	}
	if _, ok := registry.Get("ari.approval.issue"); ok {
		t.Fatalf("ari.approval.issue must not be helper-callable")
	}
	if _, ok := registry.Get("ari.trust_rule.save"); ok {
		t.Fatalf("trust-rule storage must not be helper-callable in this tranche")
	}
}

func TestAriToolCatalogIsPrunedAndDoesNotExposeRawDaemonRPCs(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	resp := callMethod[AriToolListResponse](t, registry, "ari.tool.list", AriToolListRequest{})
	want := []string{"ari.defaults.get", "ari.defaults.set", "ari.profile.draft", "ari.profile.save", "ari.self_check", "ari.run.explain_latest", "ari.session.fanout", "ari.fanout.status", "ari.inbox.list"}
	if len(resp.Tools) != len(want) {
		t.Fatalf("tool catalog len = %d, want pruned %d: %#v", len(resp.Tools), len(want), resp.Tools)
	}
	for i, tool := range resp.Tools {
		if tool.Name != want[i] {
			t.Fatalf("tool[%d] = %q, want %q in pruned Ari-shaped catalog", i, tool.Name, want[i])
		}
		if strings.Contains(tool.Name, "workspace.create") || strings.Contains(tool.Name, "workspace.add_folder") || strings.Contains(tool.Name, "context.set") || strings.Contains(tool.Name, "init.apply") {
			t.Fatalf("tool %q exposes raw daemon RPC/project setup surface", tool.Name)
		}
		if tool.OperationKind != daemonOperationKindReadOnly && tool.OperationKind != daemonOperationKindMutating {
			t.Fatalf("tool %q missing helper/MCP operation kind metadata: %#v", tool.Name, tool)
		}
	}
}

func TestAriSessionFanoutToolStartsWorkersFromScopedStickySession(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "fanout-worker-1", WorkspaceID: "ws-1", Name: "researcher", Harness: "fanout-harness", Model: "model-1", Prompt: "research"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig first target returned error: %v", err)
	}
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "fanout-worker-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "fanout-harness", Model: "model-1", Prompt: "review"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig second target returned error: %v", err)
	}
	release := make(chan struct{})
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	d.setHarnessFactoryForTest("fanout-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &blockingItemsHarness{name: "fanout-harness", started: make(chan struct{}), release: release, items: []TimelineItem{{Kind: "agent_text", Text: "answer"}}}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	resp := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.session.fanout", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, Input: map[string]any{"target_profile_ids": []string{"fanout-worker-1", "fanout-worker-2"}, "body": "fan out", "fanout_group_id": "fg-tool"}})
	if resp.Status != "ok" || resp.Output["fanout_group_id"] != "fg-tool" || resp.Output["source_session_id"] != "run-1" || resp.Output["workspace_id"] != "ws-1" || resp.Output["wait_mode"] != "none" || resp.Output["wait_status"] != "running" || resp.Output["wait_timed_out"] != false {
		t.Fatalf("fanout tool response = %#v", resp)
	}
	members, ok := resp.Output["members"].([]map[string]any)
	if !ok || len(members) != 2 {
		t.Fatalf("fanout tool members = %#v, want two structured member outputs", resp.Output["members"])
	}
	for _, member := range members {
		if member["fanout_member_id"] == "" || member["target_profile_id"] == "" || member["worker_session_id"] == "" || member["request_agent_message_id"] == "" || member["status"] != "running" || member["request_status"] != "delivered" {
			t.Fatalf("fanout tool member = %#v, want worker session, request id, and running status", member)
		}
	}
	stored, err := store.ListFanoutMembers(ctx, "fg-tool")
	if err != nil {
		t.Fatalf("ListFanoutMembers returned error: %v", err)
	}
	if len(stored) != 2 || stored[0].Status != "running" || stored[1].Status != "running" {
		t.Fatalf("stored fanout members = %#v, want same durable records created through tool", stored)
	}
	records, err := store.ListOperationRecords(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListOperationRecords returned error: %v", err)
	}
	foundFanoutRecord := false
	for _, record := range records {
		if record.OperationType == "ari_session_fanout" && record.Source == daemonOperationSourceTool && record.TrustDecision == "scoped_source_session" {
			foundFanoutRecord = true
		}
	}
	if !foundFanoutRecord {
		t.Fatalf("operation records = %#v, want ari_session_fanout tool audit record", records)
	}
	close(release)
}

func TestAriSessionFanoutToolSeparatesFanoutGroupIDFromRequestMessageEvidence(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "fanout-worker", WorkspaceID: "ws-1", Name: "worker", Harness: "fanout-harness", Model: "model-1", Prompt: "work"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	d.setHarnessFactoryForTest("fanout-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("fanout-harness", []TimelineItem{{Kind: "agent_text", Text: "answer"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	resp := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.session.fanout", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, Input: map[string]any{"target_profile_ids": []string{"fanout-worker"}, "body": "fan out", "fanout_group_id": "fg-explicit"}})
	if resp.Output["fanout_group_id"] != "fg-explicit" {
		t.Fatalf("fanout response = %#v, want explicit group id", resp)
	}
	group, err := store.GetFanoutGroup(ctx, "fg-explicit")
	if err != nil {
		t.Fatalf("GetFanoutGroup returned error: %v", err)
	}
	if group.RequestAgentMessageID != "" {
		t.Fatalf("fanout group = %#v, want group id not tunneled as request_agent_message_id", group)
	}
}

func TestAriSessionFanoutToolWaitsForAnyTerminalWorker(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "fast-worker", WorkspaceID: "ws-1", Name: "fast", Harness: "fast-fanout-harness", Model: "model-1", Prompt: "fast"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig fast returned error: %v", err)
	}
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "slow-worker", WorkspaceID: "ws-1", Name: "slow", Harness: "slow-fanout-harness", Model: "model-1", Prompt: "slow"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig slow returned error: %v", err)
	}
	slowRelease := make(chan struct{})
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	d.setHarnessFactoryForTest("fast-fanout-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("fast-fanout-harness", []TimelineItem{{Kind: "agent_text", Text: "fast answer"}}), nil
	})
	d.setHarnessFactoryForTest("slow-fanout-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &blockingItemsHarness{name: "slow-fanout-harness", started: make(chan struct{}), release: slowRelease, items: []TimelineItem{{Kind: "agent_text", Text: "slow answer"}}}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	resp := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.session.fanout", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, Input: map[string]any{"target_profile_ids": []string{"fast-worker", "slow-worker"}, "body": "fan out", "fanout_group_id": "fg-wait-any", "wait": map[string]any{"mode": "any", "timeout_ms": 1000}}})
	if resp.Status != "ok" || resp.Output["wait_status"] != "partial" || resp.Output["wait_timed_out"] != false {
		t.Fatalf("fanout wait-any response = %#v", resp)
	}
	members := fanoutToolMembersForTest(t, resp)
	if members["fast-worker"]["status"] != "completed" || members["fast-worker"]["final_response_id"] == "" {
		t.Fatalf("fast member = %#v, want completed with final response evidence", members["fast-worker"])
	}
	if members["slow-worker"]["status"] != "running" {
		t.Fatalf("slow member = %#v, want still running", members["slow-worker"])
	}
	close(slowRelease)
}

func TestAriSessionFanoutToolWaitsForAllTerminalWorkers(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "worker-1", WorkspaceID: "ws-1", Name: "one", Harness: "done-fanout-harness", Model: "model-1", Prompt: "one"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig first returned error: %v", err)
	}
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "worker-2", WorkspaceID: "ws-1", Name: "two", Harness: "done-fanout-harness", Model: "model-1", Prompt: "two"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig second returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	d.setHarnessFactoryForTest("done-fanout-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("done-fanout-harness", []TimelineItem{{Kind: "agent_text", Text: "answer"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	resp := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.session.fanout", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, Input: map[string]any{"target_profile_ids": []string{"worker-1", "worker-2"}, "body": "fan out", "fanout_group_id": "fg-wait-all", "wait": map[string]any{"mode": "all", "timeout_ms": 1000}}})
	if resp.Status != "ok" || resp.Output["wait_status"] != "completed" || resp.Output["wait_timed_out"] != false {
		t.Fatalf("fanout wait-all response = %#v", resp)
	}
	members := fanoutToolMembersForTest(t, resp)
	for profileID, member := range members {
		if member["status"] != "completed" || member["final_response_id"] == "" {
			t.Fatalf("member %q = %#v, want completed with final response evidence", profileID, member)
		}
	}
}

func TestAriSessionFanoutToolWaitTimeoutDoesNotCancelWorkersOrCreateWorkerTimeoutInbox(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "slow-worker", WorkspaceID: "ws-1", Name: "slow", Harness: "slow-fanout-harness", Model: "model-1", Prompt: "slow"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig slow returned error: %v", err)
	}
	slowRelease := make(chan struct{})
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	d.setHarnessFactoryForTest("slow-fanout-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return &blockingItemsHarness{name: "slow-fanout-harness", started: make(chan struct{}), release: slowRelease, items: []TimelineItem{{Kind: "agent_text", Text: "slow answer"}}}, nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	resp := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.session.fanout", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, Input: map[string]any{"target_profile_ids": []string{"slow-worker"}, "body": "fan out", "fanout_group_id": "fg-wait-timeout", "wait": map[string]any{"mode": "all", "timeout_ms": 25}}})
	if resp.Status != "ok" || resp.Output["wait_status"] != "partial" || resp.Output["wait_timed_out"] != true {
		t.Fatalf("fanout wait-timeout response = %#v", resp)
	}
	members := fanoutToolMembersForTest(t, resp)
	if members["slow-worker"]["status"] != "running" {
		t.Fatalf("slow member = %#v, want running after wait timeout", members["slow-worker"])
	}
	stored, err := store.ListFanoutMembers(ctx, "fg-wait-timeout")
	if err != nil {
		t.Fatalf("ListFanoutMembers returned error: %v", err)
	}
	if len(stored) != 1 || stored[0].Status != "running" {
		t.Fatalf("stored members = %#v, want wait timeout not to cancel or fail worker", stored)
	}
	inbox, err := store.ListStickyInboxItems(ctx, "ws-1", "run-1")
	if err != nil {
		t.Fatalf("ListStickyInboxItems returned error: %v", err)
	}
	for _, item := range inbox {
		if item.Kind == "worker_timed_out" {
			t.Fatalf("inbox items = %#v, want no worker_timed_out from fanout wait timeout", inbox)
		}
	}
	close(slowRelease)
}

func TestAriSessionFanoutToolRejectsUnboundedBlockingWaitBeforeStartingWorkers(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "slow-worker", WorkspaceID: "ws-1", Name: "slow", Harness: "slow-fanout-harness", Model: "model-1", Prompt: "slow"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig slow returned error: %v", err)
	}
	starts := 0
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	d.setHarnessFactoryForTest("slow-fanout-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		starts++
		return newFakeHarness("slow-fanout-harness", []TimelineItem{{Kind: "agent_text", Text: "answer"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "ari.tool.call", AriToolCallRequest{Name: "ari.session.fanout", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, Input: map[string]any{"target_profile_ids": []string{"slow-worker"}, "body": "fan out", "fanout_group_id": "fg-unbounded", "wait": map[string]any{"mode": "all"}}})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "missing_wait_timeout" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want missing_wait_timeout before start", data)
	}
	if starts != 0 {
		t.Fatalf("harness starts = %d, want unbounded wait rejected before start", starts)
	}
}

func TestAriSessionFanoutToolRejectsNonStringWaitModeBeforeStartingWorkers(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "slow-worker", WorkspaceID: "ws-1", Name: "slow", Harness: "slow-fanout-harness", Model: "model-1", Prompt: "slow"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig slow returned error: %v", err)
	}
	starts := 0
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	d.setHarnessFactoryForTest("slow-fanout-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		starts++
		return newFakeHarness("slow-fanout-harness", []TimelineItem{{Kind: "agent_text", Text: "answer"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "ari.tool.call", AriToolCallRequest{Name: "ari.session.fanout", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, Input: map[string]any{"target_profile_ids": []string{"slow-worker"}, "body": "fan out", "fanout_group_id": "fg-bad-wait", "wait": map[string]any{"mode": 1, "timeout_ms": 1000}}})
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "invalid_wait_mode" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want invalid_wait_mode before start", data)
	}
	if starts != 0 {
		t.Fatalf("harness starts = %d, want invalid wait rejected before start", starts)
	}
}

func TestAriSessionFanoutToolPrevalidatesDurableIDAndContextExcerptConflicts(t *testing.T) {
	tests := []struct {
		name       string
		seed       func(t *testing.T, store *globaldb.Store, ctx context.Context)
		input      map[string]any
		wantReason string
	}{
		{name: "fanout group already exists", input: map[string]any{"target_profile_ids": []string{"fanout-worker"}, "body": "fan out", "fanout_group_id": "fg-existing"}, seed: func(t *testing.T, store *globaldb.Store, ctx context.Context) {
			t.Helper()
			if err := store.CreateFanoutGroup(ctx, globaldb.FanoutGroup{FanoutGroupID: "fg-existing", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1"}); err != nil {
				t.Fatalf("CreateFanoutGroup returned error: %v", err)
			}
		}, wantReason: "fanout_group_exists"},
		{name: "generated worker session exists", input: map[string]any{"target_profile_ids": []string{"fanout-worker"}, "body": "fan out", "fanout_group_id": "fg-conflict-run"}, seed: func(t *testing.T, store *globaldb.Store, ctx context.Context) {
			t.Helper()
			if err := store.CreateHarnessSession(ctx, globaldb.HarnessSession{SessionID: "fg-conflict-run-cfanout-worker-run", WorkspaceID: "ws-1", AgentID: "fanout-worker", Harness: "fanout-harness", Status: "running", Usage: globaldb.HarnessSessionUsageEphemeral}); err != nil {
				t.Fatalf("CreateHarnessSession returned error: %v", err)
			}
		}, wantReason: "fanout_worker_session_exists"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newCommandMethodTestStore(t)
			ctx := context.Background()
			seedRunLogMessageMethodData(t, store, ctx)
			if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "fanout-worker", WorkspaceID: "ws-1", Name: "worker", Harness: "fanout-harness", Model: "model-1", Prompt: "work"}); err != nil {
				t.Fatalf("CreateHarnessSessionConfig returned error: %v", err)
			}
			tt.seed(t, store, ctx)
			starts := 0
			registry := rpc.NewMethodRegistry()
			d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
			d.setHarnessFactoryForTest("fanout-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
				_ = req
				_ = primaryFolder
				_ = sink
				starts++
				return newFakeHarness("fanout-harness", []TimelineItem{{Kind: "agent_text", Text: "answer"}}), nil
			})
			if err := d.registerMethods(registry, store); err != nil {
				t.Fatalf("registerMethods returned error: %v", err)
			}
			err := callMethodError(registry, "ari.tool.call", AriToolCallRequest{Name: "ari.session.fanout", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, Input: tt.input})
			data := requireHandlerErrorData(t, err)
			if data["reason"] != tt.wantReason || data["start_invoked"] != false {
				t.Fatalf("error data = %#v, want %q before start", data, tt.wantReason)
			}
			if starts != 0 {
				t.Fatalf("harness starts = %d, want conflict rejected before start", starts)
			}
		})
	}
}

func fanoutToolMembersForTest(t *testing.T, resp AriToolCallResponse) map[string]map[string]any {
	t.Helper()
	members, ok := resp.Output["members"].([]map[string]any)
	if !ok {
		t.Fatalf("members = %#v, want structured member maps", resp.Output["members"])
	}
	result := make(map[string]map[string]any, len(members))
	for _, member := range members {
		profileID, ok := member["target_profile_id"].(string)
		if !ok || profileID == "" {
			t.Fatalf("member = %#v, want target_profile_id", member)
		}
		result[profileID] = member
	}
	return result
}

func assertFanoutToolMemberStatuses(t *testing.T, resp AriToolCallResponse, want map[string]string) {
	t.Helper()
	members := fanoutToolMembersForTest(t, resp)
	for profileID, wantStatus := range want {
		member, ok := members[profileID]
		if !ok {
			t.Fatalf("members = %#v, missing profile %q", members, profileID)
		}
		if member["status"] != wantStatus {
			t.Fatalf("member %q = %#v, want status %q", profileID, member, wantStatus)
		}
	}
	if len(members) != len(want) {
		t.Fatalf("members = %#v, want exactly %#v", members, want)
	}
}

func assertAriToolInboxKinds(t *testing.T, resp AriToolCallResponse, want map[string]string) {
	t.Helper()
	items, ok := resp.Output["items"].([]map[string]any)
	if !ok {
		t.Fatalf("inbox items = %#v, want structured item maps", resp.Output["items"])
	}
	got := map[string]string{}
	for _, item := range items {
		memberID, ok := item["fanout_member_id"].(string)
		if !ok || memberID == "" {
			t.Fatalf("inbox item = %#v, want fanout_member_id", item)
		}
		if item["worker_session_id"] == "" || item["status"] != "unread" {
			t.Fatalf("inbox item = %#v, want unread evidence links", item)
		}
		if item["kind"] != "worker_stopped" && item["final_response_id"] == "" {
			t.Fatalf("inbox item = %#v, want final response evidence for terminal worker result", item)
		}
		got[memberID], _ = item["kind"].(string)
	}
	for memberID, wantKind := range want {
		if got[memberID] != wantKind {
			t.Fatalf("inbox kinds = %#v, want %s=%s", got, memberID, wantKind)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("inbox kinds = %#v, want exactly %#v", got, want)
	}
}

func TestAriFanoutStatusAndInboxListToolsReadDurableWorkerOutcomes(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "worker-1", WorkspaceID: "ws-1", Name: "one", Harness: "done-fanout-harness", Model: "model-1", Prompt: "one"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig first returned error: %v", err)
	}
	if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "worker-2", WorkspaceID: "ws-1", Name: "two", Harness: "done-fanout-harness", Model: "model-1", Prompt: "two"}); err != nil {
		t.Fatalf("CreateHarnessSessionConfig second returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	d.setHarnessFactoryForTest("done-fanout-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("done-fanout-harness", []TimelineItem{{Kind: "agent_text", Text: "answer"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	scope := AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", WithinDefaultScope: true}
	_ = callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.session.fanout", Scope: scope, Input: map[string]any{"target_profile_ids": []string{"worker-1", "worker-2"}, "body": "fan out", "fanout_group_id": "fg-read", "wait": map[string]any{"mode": "all", "timeout_ms": 1000}}})

	status := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.fanout.status", Scope: scope, Input: map[string]any{"fanout_group_id": "fg-read", "source_session_id": "run-1"}})
	if status.Status != "ok" || status.Output["fanout_group_id"] != "fg-read" || status.Output["workspace_id"] != "ws-1" || status.Output["source_session_id"] != "run-1" || status.Output["status"] != "completed" {
		t.Fatalf("fanout status response = %#v", status)
	}
	statusMembers := fanoutToolMembersForTest(t, status)
	for profileID, member := range statusMembers {
		if member["status"] != "completed" || member["worker_session_id"] == "" || member["request_agent_message_id"] == "" || member["final_response_id"] == "" {
			t.Fatalf("status member %q = %#v, want durable evidence-linked completion", profileID, member)
		}
	}

	inbox := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.inbox.list", Scope: scope, Input: map[string]any{"source_session_id": "run-1", "unread_only": true}})
	if inbox.Status != "ok" || inbox.Output["workspace_id"] != "ws-1" || inbox.Output["source_session_id"] != "run-1" {
		t.Fatalf("inbox response = %#v", inbox)
	}
	items, ok := inbox.Output["items"].([]map[string]any)
	if !ok || len(items) != 2 {
		t.Fatalf("inbox items = %#v, want two structured worker outcome items", inbox.Output["items"])
	}
	for _, item := range items {
		if item["kind"] != "worker_completed" || item["status"] != "unread" || item["fanout_group_id"] != "fg-read" || item["fanout_member_id"] == "" || item["worker_session_id"] == "" || item["final_response_id"] == "" {
			t.Fatalf("inbox item = %#v, want unread evidence-linked worker_completed", item)
		}
	}
}

func TestAriFanoutStatusAndInboxListToolsRejectOutOfScopeReads(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateWorkspace(ctx, "ws-2", "other", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateWorkspace ws-2 returned error: %v", err)
	}
	if err := store.CreateFanoutGroup(ctx, globaldb.FanoutGroup{FanoutGroupID: "fg-other", WorkspaceID: "ws-2", SourceSessionID: "other-run", SourceAgentID: "other", Body: "other"}); err != nil {
		t.Fatalf("CreateFanoutGroup returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	scope := AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", WithinDefaultScope: true}
	unknownErr := callMethodError(registry, "ari.tool.call", AriToolCallRequest{Name: "ari.fanout.status", Scope: scope, Input: map[string]any{"fanout_group_id": "missing"}})
	if data := requireHandlerErrorData(t, unknownErr); data["reason"] != "unknown_fanout_group" {
		t.Fatalf("unknown group error data = %#v", data)
	}
	mismatchErr := callMethodError(registry, "ari.tool.call", AriToolCallRequest{Name: "ari.fanout.status", Scope: scope, Input: map[string]any{"fanout_group_id": "fg-other"}})
	if data := requireHandlerErrorData(t, mismatchErr); data["reason"] != "fanout_scope_mismatch" {
		t.Fatalf("mismatch error data = %#v", data)
	}
	inboxMismatchErr := callMethodError(registry, "ari.tool.call", AriToolCallRequest{Name: "ari.inbox.list", Scope: scope, Input: map[string]any{"source_session_id": "other-run"}})
	if data := requireHandlerErrorData(t, inboxMismatchErr); data["reason"] != "source_scope_mismatch" {
		t.Fatalf("inbox mismatch error data = %#v", data)
	}
}

func TestAriSessionFanoutToolRejectsInvalidInputsBeforeStartingWorkers(t *testing.T) {
	tests := []struct {
		name        string
		input       map[string]any
		scope       AriToolScope
		seed        func(t *testing.T, store *globaldb.Store, ctx context.Context)
		wantReason  string
		wantGroupID string
	}{
		{name: "unknown source", input: map[string]any{"target_profile_ids": []string{"fanout-worker"}, "body": "fan out", "fanout_group_id": "fg-unknown-source", "source_session_id": "missing-run"}, scope: AriToolScope{SourceRunID: "missing-run", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, wantReason: "unknown_source_session", wantGroupID: "fg-unknown-source"},
		{name: "workspace mismatch", input: map[string]any{"target_profile_ids": []string{"fanout-worker"}, "body": "fan out", "fanout_group_id": "fg-ws-mismatch"}, scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-2", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, seed: func(t *testing.T, store *globaldb.Store, ctx context.Context) {
			t.Helper()
			if err := store.CreateWorkspace(ctx, "ws-2", "other", t.TempDir(), "manual", "auto"); err != nil {
				t.Fatalf("CreateWorkspace ws-2 returned error: %v", err)
			}
		}, wantReason: "source_workspace_mismatch", wantGroupID: "fg-ws-mismatch"},
		{name: "source profile mismatch", input: map[string]any{"target_profile_ids": []string{"fanout-worker"}, "body": "fan out", "fanout_group_id": "fg-source-profile-mismatch"}, scope: AriToolScope{SourceRunID: "other-run", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, seed: func(t *testing.T, store *globaldb.Store, ctx context.Context) {
			t.Helper()
			if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "other-agent", WorkspaceID: "ws-1", Name: "other", Harness: "fanout-harness"}); err != nil {
				t.Fatalf("CreateHarnessSessionConfig other returned error: %v", err)
			}
			if err := store.CreateHarnessSession(ctx, globaldb.HarnessSession{SessionID: "other-run", WorkspaceID: "ws-1", AgentID: "other-agent", Harness: "fanout-harness", Status: "waiting", Usage: globaldb.HarnessSessionUsageSticky}); err != nil {
				t.Fatalf("CreateHarnessSession other returned error: %v", err)
			}
		}, wantReason: "source_profile_mismatch", wantGroupID: "fg-source-profile-mismatch"},
		{name: "duplicate targets", input: map[string]any{"target_profile_ids": []string{"fanout-worker", " fanout-worker "}, "body": "fan out", "fanout_group_id": "fg-duplicate"}, scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, wantReason: "duplicate_target_profile", wantGroupID: "fg-duplicate"},
		{name: "missing body", input: map[string]any{"target_profile_ids": []string{"fanout-worker"}, "fanout_group_id": "fg-missing-body"}, scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, wantReason: "missing_required_fields", wantGroupID: "fg-missing-body"},
		{name: "unknown profile", input: map[string]any{"target_profile_ids": []string{"missing-profile"}, "body": "fan out", "fanout_group_id": "fg-unknown-profile"}, scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, wantReason: "unknown_profile", wantGroupID: "fg-unknown-profile"},
		{name: "context mismatch", input: map[string]any{"target_profile_ids": []string{"fanout-worker"}, "body": "fan out", "fanout_group_id": "fg-context-mismatch", "context_excerpt_ids": []string{"bad-excerpt"}}, scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "ws-1", ProfileID: "agent-1", ProfileName: "executor", ToolName: "ari.session.fanout", WithinDefaultScope: true}, seed: func(t *testing.T, store *globaldb.Store, ctx context.Context) {
			t.Helper()
			if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "other-worker", WorkspaceID: "ws-1", Name: "other", Harness: "fanout-harness"}); err != nil {
				t.Fatalf("CreateHarnessSessionConfig other returned error: %v", err)
			}
			if _, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "bad-excerpt", SourceSessionID: "run-1", TargetAgentID: "other-worker", Count: 1}); err != nil {
				t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
			}
		}, wantReason: "context_excerpt_mismatch", wantGroupID: "fg-context-mismatch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newCommandMethodTestStore(t)
			ctx := context.Background()
			seedRunLogMessageMethodData(t, store, ctx)
			if err := store.CreateHarnessSessionConfig(ctx, globaldb.HarnessSessionConfig{AgentID: "fanout-worker", WorkspaceID: "ws-1", Name: "researcher", Harness: "fanout-harness", Model: "model-1", Prompt: "research"}); err != nil {
				t.Fatalf("CreateHarnessSessionConfig target returned error: %v", err)
			}
			if tt.seed != nil {
				tt.seed(t, store, ctx)
			}
			starts := 0
			registry := rpc.NewMethodRegistry()
			d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
			d.setHarnessFactoryForTest("fanout-harness", func(req HarnessSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
				_ = req
				_ = primaryFolder
				_ = sink
				starts++
				return newFakeHarness("fanout-harness", []TimelineItem{{Kind: "agent_text", Text: "answer"}}), nil
			})
			if err := d.registerMethods(registry, store); err != nil {
				t.Fatalf("registerMethods returned error: %v", err)
			}

			err := callMethodError(registry, "ari.tool.call", AriToolCallRequest{Name: "ari.session.fanout", Scope: tt.scope, Input: tt.input})
			data := requireHandlerErrorData(t, err)
			if data["reason"] != tt.wantReason || data["start_invoked"] != false {
				t.Fatalf("error data = %#v, want reason %q and no start", data, tt.wantReason)
			}
			if starts != 0 {
				t.Fatalf("harness starts = %d, want validation before worker start", starts)
			}
			members, listErr := store.ListFanoutMembers(ctx, tt.wantGroupID)
			if listErr != nil {
				t.Fatalf("ListFanoutMembers returned error: %v", listErr)
			}
			if len(members) != 0 {
				t.Fatalf("fanout members = %#v, want none after failed validation", members)
			}
		})
	}
}

func containsAriToolTestString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestAriDefaultsSetRequiresScopedSingleUseApproval(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"default_harness":"codex"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	req := AriToolCallRequest{
		Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true},
		Name:  "ari.defaults.set",
		Input: map[string]any{"default_harness": "opencode", "preferred_model": "gpt-next"},
	}
	missingApprovalErr := callMethodError(registry, "ari.tool.call", req)
	if missingApprovalErr == nil || !strings.Contains(missingApprovalErr.Error(), "approval_required") {
		t.Fatalf("missing approval error = %v, want approval_required", missingApprovalErr)
	}

	req.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")
	resp := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", req)
	if resp.Status != "ok" || resp.ApplicationStatus != "restart_required" {
		t.Fatalf("defaults.set response = %#v", resp)
	}
	defaults := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.defaults.get", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.get", WithinDefaultScope: true}})
	if defaults.Output["default_harness"] != "opencode" || defaults.Output["preferred_model"] != "gpt-next" {
		t.Fatalf("defaults after set = %#v", defaults.Output)
	}

	reuseErr := callMethodError(registry, "ari.tool.call", req)
	if reuseErr == nil || !strings.Contains(reuseErr.Error(), "approval_reused") {
		t.Fatalf("reused approval error = %v, want approval_reused", reuseErr)
	}
}

func TestAriToolCallsRequireProfileIDInScope(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	err := callMethodError(registry, "ari.tool.call", AriToolCallRequest{Name: "ari.defaults.get", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileName: "helper", ToolName: "ari.defaults.get", WithinDefaultScope: true}})
	if err == nil || !strings.Contains(err.Error(), "missing_scope") {
		t.Fatalf("missing profile_id error = %v, want missing_scope", err)
	}
}

func TestAriToolsRejectWrongScopeHashAndStaleApprovals(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "codex"}}

	wrongScope := req
	wrongScope.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")
	wrongScope.Approval.Scope.WorkspaceID = "other-workspace"
	if err := callMethodError(registry, "ari.tool.call", wrongScope); err == nil || !strings.Contains(err.Error(), "approval_mismatch") {
		t.Fatalf("wrong-scope approval error = %v, want approval_mismatch", err)
	}

	wrongHash := req
	wrongHash.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")
	wrongHash.Approval.RequestHash = "sha256:not-this-request"
	if err := callMethodError(registry, "ari.tool.call", wrongHash); err == nil || !strings.Contains(err.Error(), "approval_mismatch") {
		t.Fatalf("wrong-hash approval error = %v, want approval_mismatch", err)
	}

	stale := req
	stale.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")
	stale.Approval.ApprovedAt = time.Now().UTC().Add(-11 * time.Minute).Format(time.RFC3339)
	if err := storeAriApproval(context.Background(), store, storedAriApproval{Approval: stale.Approval}); err != nil {
		t.Fatalf("store stale approval: %v", err)
	}
	if err := callMethodError(registry, "ari.tool.call", stale); err == nil || !strings.Contains(err.Error(), "approval_stale") {
		t.Fatalf("stale approval error = %v, want approval_stale", err)
	}
}

func TestAriToolsRejectRepurposedIssuedApproval(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	approvedSave := AriToolCallRequest{Name: "ari.profile.save", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.profile.save", WithinDefaultScope: true}, Input: map[string]any{"name": "reviewer", "harness": "codex"}}
	issued := storeIssuedApprovalForToolRequest(t, store, approvedSave, "tester")
	maliciousDefaults := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "opencode"}}
	issued.Scope = AriToolApprovalScope{WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", SourceRunID: "run-1"}
	issued.RequestHash, _ = HashAriToolRequest(maliciousDefaults.Name, maliciousDefaults.Input)
	maliciousDefaults.Approval = issued
	if err := callMethodError(registry, "ari.tool.call", maliciousDefaults); err == nil || !strings.Contains(err.Error(), "approval_mismatch") {
		t.Fatalf("repurposed approval error = %v, want approval_mismatch", err)
	}
}

func TestAriToolsRejectApprovalForDifferentProfile(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "codex"}}
	req.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")
	req.Scope.ProfileID = "ap-other"
	req.Scope.ProfileName = "other"
	if err := callMethodError(registry, "ari.tool.call", req); err == nil || !strings.Contains(err.Error(), "approval_wrong_scope") {
		t.Fatalf("different-profile approval error = %v, want approval_wrong_scope", err)
	}
}

func TestAriToolsRejectForgedApprovalWithoutDaemonIssuedRecord(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "codex"}}
	req.Approval = forgedApprovalForToolRequest(t, req)
	if err := callMethodError(registry, "ari.tool.call", req); err == nil || !strings.Contains(err.Error(), "approval_unknown") {
		t.Fatalf("forged approval error = %v, want approval_unknown", err)
	}
}

func TestAriApprovalsCanOnlyBeConsumedOnceConcurrently(t *testing.T) {
	store := newCommandMethodTestStore(t)
	home := ensureHomeWorkspaceForToolTest(t, store)
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "codex"}}
	req.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")

	const workers = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- validateAndConsumeAriApproval(context.Background(), store, req)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	reused := 0
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		if strings.Contains(err.Error(), "approval_reused") {
			reused++
			continue
		}
		t.Fatalf("unexpected consume error: %v", err)
	}
	if successes != 1 || reused != workers-1 {
		t.Fatalf("consume results: successes=%d reused=%d", successes, reused)
	}
}

func TestAriProfileDraftTreatsMissingHarnessAsOptional(t *testing.T) {
	resp, err := ariProfileDraft(map[string]any{"name": "reviewer"})
	if err != nil {
		t.Fatalf("ariProfileDraft returned error: %v", err)
	}
	if resp.Status != "draft" || resp.Output["name"] != "reviewer" || resp.Output["harness"] != "" {
		t.Fatalf("draft response = %#v", resp)
	}
}

func TestAriProfileDraftRejectsMissingName(t *testing.T) {
	_, err := ariProfileDraft(map[string]any{"harness": "codex"})
	if err == nil || !strings.Contains(err.Error(), "missing_profile_name") {
		t.Fatalf("missing name error = %v, want missing_profile_name", err)
	}
}

func TestAriDefaultsSetRejectsMissingWorkspace(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.CreateWorkspace(context.Background(), "project-1", "alpha", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "missing", ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "codex"}}
	req.Approval = forgedApprovalForToolRequest(t, req)
	if err := callMethodError(registry, "ari.tool.call", req); err == nil || !strings.Contains(err.Error(), "globaldb record not found") {
		t.Fatalf("missing workspace error = %v, want not found", err)
	}
}

func TestAriApprovalsRemainSingleUseAfterDaemonRestart(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "codex"}}
	req.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")
	_ = callMethod[AriToolCallResponse](t, registry, "ari.tool.call", req)

	restarted := rpc.NewMethodRegistry()
	d2 := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d2.registerMethods(restarted, store); err != nil {
		t.Fatalf("registerMethods after restart returned error: %v", err)
	}
	if err := callMethodError(restarted, "ari.tool.call", req); err == nil || !strings.Contains(err.Error(), "approval_reused") {
		t.Fatalf("post-restart reuse error = %v, want approval_reused", err)
	}
	records, err := store.ListOperationRecords(context.Background(), "")
	if err != nil {
		t.Fatalf("ListOperationRecords returned error: %v", err)
	}
	foundDefaultsSet := false
	for _, record := range records {
		if record.OperationType == "ari_defaults_set" && record.Source == daemonOperationSourceTool && record.TrustDecision == "approved_once" {
			foundDefaultsSet = true
		}
	}
	if !foundDefaultsSet {
		t.Fatalf("operation records = %#v, want ari_defaults_set tool record", records)
	}
}

func TestAriDefaultsSetValidatesWholeRequestBeforeWriting(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"default_harness":"codex"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: true}, Input: map[string]any{"default_harness": "opencode", "default_invocation_class": "bad"}}
	req.Approval = storeIssuedApprovalForToolRequest(t, store, req, "tester")
	if err := callMethodError(registry, "ari.tool.call", req); err == nil || !strings.Contains(err.Error(), "invalid_default_invocation_class") {
		t.Fatalf("invalid defaults error = %v, want invalid_default_invocation_class", err)
	}
	var persisted map[string]string
	if err := readJSONFile(configPath, &persisted); err != nil {
		t.Fatalf("read config: %v", err)
	}
	if persisted["default_harness"] != "codex" {
		t.Fatalf("default_harness after failed set = %q, want codex", persisted["default_harness"])
	}
}

func TestAriProfileDraftAndSaveSeparateDraftFromPersistedWrite(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	scope := AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.profile.draft", WithinDefaultScope: true}
	draftReq := AriToolCallRequest{Name: "ari.profile.draft", Scope: scope, Input: map[string]any{"name": "frontend-reviewer", "harness": "codex", "prompt": "Review UI regressions"}}
	draft := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", draftReq)
	if draft.Status != "draft" || draft.Output["profile_id"] != nil {
		t.Fatalf("draft response = %#v", draft)
	}
	_, err := store.GetProfile(context.Background(), home.ID, "frontend-reviewer")
	if !errors.Is(err, globaldb.ErrNotFound) {
		t.Fatalf("draft persisted profile lookup error = %v, want ErrNotFound", err)
	}

	saveReq := AriToolCallRequest{Name: "ari.profile.save", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.profile.save", WithinDefaultScope: true}, Input: draft.Output}
	if err := callMethodError(registry, "ari.tool.call", saveReq); err == nil || !strings.Contains(err.Error(), "approval_required") {
		t.Fatalf("profile.save without approval error = %v, want approval_required", err)
	}
	saveReq.Approval = storeIssuedApprovalForToolRequest(t, store, saveReq, "tester")
	saved := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", saveReq)
	if saved.Status != "ok" || saved.Output["name"] != "frontend-reviewer" {
		t.Fatalf("save response = %#v", saved)
	}
	records, err := store.ListOperationRecords(context.Background(), home.ID)
	if err != nil {
		t.Fatalf("ListOperationRecords returned error: %v", err)
	}
	foundProfileSave := false
	for _, record := range records {
		if record.OperationType == "ari_profile_save" && record.Source == daemonOperationSourceTool && record.TrustDecision == "approved_once" {
			foundProfileSave = true
		}
	}
	if !foundProfileSave {
		t.Fatalf("operation records = %#v, want ari_profile_save tool record", records)
	}
}

func TestAriDefaultsSetRequiresDefaultScope(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", filepath.Join(t.TempDir(), "config.json"), "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	if err := store.CreateWorkspace(context.Background(), "project-1", "alpha", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	req := AriToolCallRequest{Name: "ari.defaults.set", Scope: AriToolScope{SourceRunID: "run-1", WorkspaceID: "project-1", ProfileID: "ap-helper", ProfileName: "helper", ToolName: "ari.defaults.set", WithinDefaultScope: false}, Input: map[string]any{"default_harness": "codex"}}
	err := callMethodError(registry, "ari.tool.call", req)
	if err == nil || !strings.Contains(err.Error(), "handoff_required") {
		t.Fatalf("out-of-scope defaults.set error = %v, want handoff_required", err)
	}
}

func TestAriReadOnlyToolsDoNotRequireApprovalOrMutateState(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"default_harness":"codex","preferred_model":"keep"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", configPath, "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	home := ensureHomeWorkspaceForToolTest(t, store)
	scope := AriToolScope{SourceRunID: "run-1", WorkspaceID: home.ID, ProfileID: "ap-helper", ProfileName: "helper", WithinDefaultScope: true}
	selfCheck := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.self_check", Scope: scope})
	if selfCheck.Status != "ok" || selfCheck.Output["daemon_version"] != "test-version" || selfCheck.Output["config_readable"] != true {
		t.Fatalf("self_check response = %#v", selfCheck)
	}
	explain := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.run.explain_latest", Scope: scope})
	if explain.Status != "ok" || explain.Output["summary"] == "" {
		t.Fatalf("run.explain_latest response = %#v", explain)
	}
	defaults := callMethod[AriToolCallResponse](t, registry, "ari.tool.call", AriToolCallRequest{Name: "ari.defaults.get", Scope: scope})
	if defaults.Output["default_harness"] != "codex" || defaults.Output["preferred_model"] != "keep" {
		t.Fatalf("defaults changed after read tools: %#v", defaults.Output)
	}
}

func ensureHomeWorkspaceForToolTest(t *testing.T, store *globaldb.Store) *globaldb.Workspace {
	t.Helper()
	home := t.TempDir()
	if err := store.CreateWorkspace(context.Background(), "home-tools", "home", home, "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.AddFolder(context.Background(), "home-tools", home, "unknown", true); err != nil {
		t.Fatalf("AddFolder returned error: %v", err)
	}
	session, err := store.GetWorkspace(context.Background(), "home-tools")
	if err != nil {
		t.Fatalf("GetSession returned error: %v", err)
	}
	return session
}

func storeIssuedApprovalForToolRequest(t *testing.T, store *globaldb.Store, req AriToolCallRequest, approvedBy string) AriToolApproval {
	t.Helper()
	hash, err := HashAriToolRequest(req.Name, req.Input)
	if err != nil {
		t.Fatalf("HashAriToolRequest returned error: %v", err)
	}
	approval := AriToolApproval{ApprovalID: "approval-issued-" + strings.ReplaceAll(req.Name, ".", "-") + "-" + strings.ReplaceAll(req.Scope.SourceRunID, "-", "_"), ApprovedBy: approvedBy, ApprovedAt: time.Now().UTC().Format(time.RFC3339), Scope: AriToolApprovalScope{WorkspaceID: req.Scope.WorkspaceID, ProfileID: req.Scope.ProfileID, ProfileName: req.Scope.ProfileName, ToolName: req.Name, SourceRunID: req.Scope.SourceRunID}, RequestHash: hash}
	if err := storeAriApproval(context.Background(), store, storedAriApproval{Approval: approval}); err != nil {
		t.Fatalf("store approval: %v", err)
	}
	return approval
}

func forgedApprovalForToolRequest(t *testing.T, req AriToolCallRequest) AriToolApproval {
	t.Helper()
	hash, err := HashAriToolRequest(req.Name, req.Input)
	if err != nil {
		t.Fatalf("HashAriToolRequest returned error: %v", err)
	}
	return AriToolApproval{ApprovalID: "approval-forged-" + strings.ReplaceAll(req.Name, ".", "-"), ApprovedBy: "tester", ApprovedAt: time.Now().UTC().Format(time.RFC3339), Scope: AriToolApprovalScope{WorkspaceID: req.Scope.WorkspaceID, ProfileID: req.Scope.ProfileID, ProfileName: req.Scope.ProfileName, ToolName: req.Name, SourceRunID: req.Scope.SourceRunID}, RequestHash: hash}
}

func TestAriToolRequestHashIsStable(t *testing.T) {
	left, err := HashAriToolRequest("ari.defaults.set", map[string]any{"preferred_model": "m", "default_harness": "codex"})
	if err != nil {
		t.Fatalf("HashAriToolRequest left returned error: %v", err)
	}
	raw := json.RawMessage(`{"default_harness":"codex","preferred_model":"m"}`)
	right, err := HashAriToolRequest("ari.defaults.set", raw)
	if err != nil {
		t.Fatalf("HashAriToolRequest right returned error: %v", err)
	}
	if left != right || !strings.HasPrefix(left, "sha256:") {
		t.Fatalf("hashes = %q and %q", left, right)
	}
}
