package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func TestRunLogMessagesTailMethodReturnsOrderedNormalizedMessages(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	for _, msg := range []globaldb.RunLogMessage{
		{MessageID: "msg-1", WorkspaceID: "ws-1", SessionID: "run-1", AgentID: "agent-1", Sequence: 1, Role: "user", Status: "completed", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "one"}}},
		{MessageID: "msg-2", WorkspaceID: "ws-1", SessionID: "run-1", AgentID: "agent-1", Sequence: 2, Role: "assistant", Status: "completed", Parts: []globaldb.RunLogMessagePart{{PartID: "part-2", Sequence: 1, Kind: "text", Text: "two"}}},
		{MessageID: "msg-3", WorkspaceID: "ws-1", SessionID: "run-1", AgentID: "agent-1", Sequence: 3, Role: "tool", Status: "completed", Parts: []globaldb.RunLogMessagePart{{PartID: "part-3", Sequence: 1, Kind: "text", Text: "three"}}},
	} {
		if err := store.AppendRunLogMessage(ctx, msg); err != nil {
			t.Fatalf("AppendRunLogMessage(%s) returned error: %v", msg.MessageID, err)
		}
	}

	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	got := callMethod[RunLogMessagesTailResponse](t, registry, "run.messages.tail", RunLogMessagesTailRequest{SessionID: "run-1", Count: 2})
	if len(got.Messages) != 2 || got.Messages[0].MessageID != "msg-2" || got.Messages[1].MessageID != "msg-3" {
		t.Fatalf("messages = %#v, want msg-2,msg-3 in run order", got.Messages)
	}
	if got.Messages[1].Role != "tool" || len(got.Messages[1].Parts) != 1 || got.Messages[1].Parts[0].Text != "three" {
		t.Fatalf("message details = %#v, want role and parts preserved", got.Messages[1])
	}
}

func TestRunLogMessagesListMethodReturnsCursorLimitedMessages(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	for _, msg := range []globaldb.RunLogMessage{
		{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "user", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "one"}}},
		{MessageID: "msg-2", SessionID: "run-1", Sequence: 2, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-2", Sequence: 1, Kind: "text", Text: "two"}}},
		{MessageID: "msg-3", SessionID: "run-1", Sequence: 3, Role: "tool", ProviderMessageID: "provider-msg-3", ProviderChannel: "analysis", ProviderKind: "function_call_output", RawMetadataJSON: `{"provider":"codex"}`, Parts: []globaldb.RunLogMessagePart{{PartID: "part-3", Sequence: 1, Kind: "tool_result", Text: "three", ToolCallID: "call-1", RawJSON: `{"ok":true}`}}},
	} {
		if err := store.AppendRunLogMessage(ctx, msg); err != nil {
			t.Fatalf("AppendRunLogMessage(%s) returned error: %v", msg.MessageID, err)
		}
	}

	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	got := callMethod[RunLogMessagesListResponse](t, registry, "run.messages.list", RunLogMessagesListRequest{SessionID: "run-1", AfterSequence: 1, Limit: 2})
	if len(got.Messages) != 2 || got.Messages[0].MessageID != "msg-2" || got.Messages[1].MessageID != "msg-3" || got.Messages[1].ProviderMessageID != "provider-msg-3" || got.Messages[1].ProviderChannel != "analysis" || got.Messages[1].RawMetadataJSON != `{"provider":"codex"}` || got.Messages[1].Parts[0].ToolCallID != "call-1" {
		t.Fatalf("messages = %#v, want page after sequence 1 in run order", got.Messages)
	}
}

func TestRunLogMessagesMethodsRejectUnknownRun(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	for _, tc := range []struct {
		method string
		params any
	}{
		{method: "run.messages.tail", params: RunLogMessagesTailRequest{SessionID: "missing-run", Count: 1}},
		{method: "run.messages.list", params: RunLogMessagesListRequest{SessionID: "missing-run", Limit: 10}},
	} {
		err := callMethodError(registry, tc.method, tc.params)
		handlerErr, ok := err.(*rpc.HandlerError)
		if !ok {
			t.Fatalf("%s error = %T %[2]v, want HandlerError", tc.method, err)
		}
		if handlerErr.Code != rpc.InvalidParams {
			t.Fatalf("%s error code = %d, want InvalidParams", tc.method, handlerErr.Code)
		}
	}
}

func TestRunLogMessagesTailMethodReturnsPartMetadata(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "tool_call", ToolName: "web.search", ToolCallID: "call-1", RawJSON: `{"query":"ari"}`}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}

	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	got := callMethod[RunLogMessagesTailResponse](t, registry, "run.messages.tail", RunLogMessagesTailRequest{SessionID: "run-1", Count: 1})
	if len(got.Messages) != 1 || len(got.Messages[0].Parts) != 1 || got.Messages[0].Parts[0].ToolName != "web.search" || got.Messages[0].Parts[0].ToolCallID != "call-1" || got.Messages[0].Parts[0].RawJSON != `{"query":"ari"}` {
		t.Fatalf("messages = %#v, want normalized part metadata", got.Messages)
	}
}

func TestExecutorMethodsExposeADRTerminology(t *testing.T) {
	store := newCommandMethodTestStore(t)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	for _, name := range []string{"profile.run", "profile.create", "profile.get", "profile.list", "profile.helper.ensure", "profile.helper.get", "context.excerpt.create_from_tail", "context.excerpt.create_from_range", "context.excerpt.create_from_explicit_ids", "context.excerpt.get", "agent.message.send"} {
		method, ok := registry.Get(name)
		if !ok {
			t.Fatalf("method %q is not registered", name)
		}
		for _, stale := range []string{"agent profile", "agent profiles", "message excerpt", "direct message"} {
			if strings.Contains(strings.ToLower(method.Description), stale) {
				t.Fatalf("method %q description = %q, want no stale %q terminology", name, method.Description, stale)
			}
		}
	}

	for _, name := range []string{"agent.profile.run", "agent.profile.create", "agent.profile.get", "agent.profile.list", "agent.profile.helper.ensure", "agent.profile.helper.get", "message.excerpt.create_from_tail", "message.excerpt.create_from_range", "message.excerpt.create_from_explicit_ids", "message.excerpt.get"} {
		if _, ok := registry.Get(name); ok {
			t.Fatalf("legacy method %q is still registered", name)
		}
	}
}

func TestAgentMessageSendMethodDeliversVisibleMessage(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "plan"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	excerpt, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}

	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	got := callMethod[AgentMessageSendResponse](t, registry, "agent.message.send", AgentMessageSendRequest{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "review this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, StartSessionID: "run-2"})
	if got.AgentMessage.TargetSessionID != "run-2" || got.AgentMessage.Status != "delivered" {
		t.Fatalf("direct message = %#v, want delivered run-2", got.AgentMessage)
	}
	storedExcerpt := callMethod[ContextExcerptResponse](t, registry, "context.excerpt.get", ContextExcerptGetRequest{ContextExcerptID: excerpt.ContextExcerptID})
	if storedExcerpt.SelectorType != "last_n" || storedExcerpt.SelectorJSON == "" || storedExcerpt.Visibility != "visible_context" {
		t.Fatalf("stored excerpt = %#v, want selector and visibility in daemon response", storedExcerpt)
	}
	if storedExcerpt.TargetSessionID != "" {
		t.Fatalf("stored excerpt target run = %q, want immutable excerpt not bound to delivery", storedExcerpt.TargetSessionID)
	}
	tail, err := store.TailRunLogMessages(ctx, "run-2", 2)
	if err != nil {
		t.Fatalf("TailRunLogMessages target returned error: %v", err)
	}
	if len(tail) != 2 || tail[0].Role != "assistant" || tail[0].Parts[0].Text != "plan" || tail[1].Role != "user" || tail[1].Parts[0].Text != "review this" {
		t.Fatalf("target tail = %#v, want excerptd context followed by visible direct message", tail)
	}
}

func TestAgentMessageSendMethodDeliversExcerptAppendedMessage(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "plan"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	excerpt, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1, AppendedMessage: "use this plan"})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}

	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	_ = callMethod[AgentMessageSendResponse](t, registry, "agent.message.send", AgentMessageSendRequest{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "review this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, StartSessionID: "run-2"})
	tail, err := store.TailRunLogMessages(ctx, "run-2", 3)
	if err != nil {
		t.Fatalf("TailRunLogMessages target returned error: %v", err)
	}
	if len(tail) != 3 || tail[0].Parts[0].Text != "plan" || tail[1].Role != "user" || tail[1].Parts[0].Text != "use this plan" || tail[2].Parts[0].Text != "review this" {
		t.Fatalf("target tail = %#v, want copied excerpt, appended message, direct body", tail)
	}
}

func TestContextExcerptCreateFromTailMethodReturnsOrderedImmutableExcerpt(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "user", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "question"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage msg-1 returned error: %v", err)
	}
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "msg-2", SessionID: "run-1", Sequence: 2, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-2", Sequence: 1, Kind: "text", Text: "answer"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage msg-2 returned error: %v", err)
	}

	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	created := callMethod[ContextExcerptResponse](t, registry, "context.excerpt.create_from_tail", ContextExcerptCreateFromTailRequest{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 2, AppendedMessage: "continue"})
	if len(created.Items) != 2 || created.Items[0].CopiedText != "question" || created.Items[1].CopiedText != "answer" || created.AppendedMessage != "continue" {
		t.Fatalf("created excerpt = %#v, want ordered copied messages", created)
	}
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "msg-3", SessionID: "run-1", Sequence: 3, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-3", Sequence: 1, Kind: "text", Text: "new"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage msg-3 returned error: %v", err)
	}
	got := callMethod[ContextExcerptResponse](t, registry, "context.excerpt.get", ContextExcerptGetRequest{ContextExcerptID: "excerpt-1"})
	if len(got.Items) != 2 || got.Items[1].CopiedText != "answer" {
		t.Fatalf("got excerpt = %#v, want immutable copied messages", got)
	}
}

func TestContextExcerptCreateFromRangeAndExplicitMethods(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	for _, msg := range []globaldb.RunLogMessage{
		{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "user", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "one"}}},
		{MessageID: "msg-2", SessionID: "run-1", Sequence: 2, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-2", Sequence: 1, Kind: "text", Text: "two"}}},
		{MessageID: "msg-3", SessionID: "run-1", Sequence: 3, Role: "tool", Parts: []globaldb.RunLogMessagePart{{PartID: "part-3", Sequence: 1, Kind: "tool_result", Text: "three"}}},
	} {
		if err := store.AppendRunLogMessage(ctx, msg); err != nil {
			t.Fatalf("AppendRunLogMessage(%s) returned error: %v", msg.MessageID, err)
		}
	}

	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	ranged := callMethod[ContextExcerptResponse](t, registry, "context.excerpt.create_from_range", ContextExcerptCreateFromRangeRequest{ContextExcerptID: "excerpt-range", SourceSessionID: "run-1", TargetAgentID: "agent-2", StartSequence: 2, EndSequence: 3})
	if ranged.SelectorType != "range" || len(ranged.Items) != 2 || ranged.Items[0].SourceMessageID != "msg-2" || ranged.Items[1].SourceMessageID != "msg-3" {
		t.Fatalf("range excerpt = %#v, want msg-2,msg-3", ranged)
	}
	explicit := callMethod[ContextExcerptResponse](t, registry, "context.excerpt.create_from_explicit_ids", ContextExcerptCreateFromExplicitIDsRequest{ContextExcerptID: "excerpt-explicit", SourceSessionID: "run-1", TargetAgentID: "agent-2", MessageIDs: []string{"msg-3", "msg-1"}})
	if explicit.SelectorType != "explicit_ids" || len(explicit.Items) != 2 || explicit.Items[0].SourceMessageID != "msg-3" || explicit.Items[1].SourceMessageID != "msg-1" {
		t.Fatalf("explicit excerpt = %#v, want requested explicit ID order", explicit)
	}
}

func TestAgentMessageSendMethodLeavesExcerptImmutable(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "plan"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	excerpt, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}

	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	_ = callMethod[AgentMessageSendResponse](t, registry, "agent.message.send", AgentMessageSendRequest{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "review this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, StartSessionID: "run-2"})
	got := callMethod[ContextExcerptResponse](t, registry, "context.excerpt.get", ContextExcerptGetRequest{ContextExcerptID: excerpt.ContextExcerptID})
	if got.TargetSessionID != "" || len(got.Items) != 1 || got.Items[0].CopiedText != "plan" {
		t.Fatalf("excerpt = %#v, want immutable excerpt not bound to delivery run", got)
	}
}

func TestPlannerAgentMessageToExecutorDeliversSelectedPlan(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "planner", WorkspaceID: "ws-1", Name: "planner", Harness: "planner-harness"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig planner returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "executor-target", WorkspaceID: "ws-1", Name: "executor-target", Harness: "executor-harness"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig executor returned error: %v", err)
	}
	if err := store.CreateAgentSession(ctx, globaldb.AgentSession{SessionID: "planner-run", WorkspaceID: "ws-1", AgentID: "planner", Harness: "planner-harness", Status: "running", Usage: "durable", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateAgentSession planner returned error: %v", err)
	}
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "planner-msg-1", SessionID: "planner-run", Sequence: 1, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "planner-part-1", Sequence: 1, Kind: "text", Text: "Build the endpoint"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage planner returned error: %v", err)
	}
	excerpt, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "planner-excerpt", SourceSessionID: "planner-run", TargetAgentID: "executor-target", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}

	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	got := callMethod[AgentMessageSendResponse](t, registry, "agent.message.send", AgentMessageSendRequest{AgentMessageID: "planner-dm", SourceSessionID: "planner-run", TargetAgentID: "executor-target", Body: "Please implement", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, StartSessionID: "executor-run"})
	if got.AgentMessage.SourceAgentID != "planner" || got.AgentMessage.TargetAgentID != "executor-target" || got.AgentMessage.TargetSessionID != "executor-run" {
		t.Fatalf("direct message = %#v, want planner to executor delivery", got.AgentMessage)
	}
	tail, err := store.TailRunLogMessages(ctx, "executor-run", 2)
	if err != nil {
		t.Fatalf("TailRunLogMessages executor returned error: %v", err)
	}
	if len(tail) != 2 || tail[0].Parts[0].Text != "Build the endpoint" || tail[1].Parts[0].Text != "Please implement" {
		t.Fatalf("executor tail = %#v, want selected plan then planner instruction", tail)
	}
}

func TestEphemeralAgentCallRunsTargetAndRoutesReplyToCaller(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "librarian", Harness: "test-harness", Model: "model-1", Prompt: "research"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "Spring question"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	excerpt, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}

	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("test-harness", []TimelineItem{{Kind: "agent_text", Text: "Use Spring Boot 4 feature flags."}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	got := callMethod[EphemeralAgentCallResponse](t, registry, "agent.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "Research this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, ReplyAgentMessageID: "dm-reply-1"})
	if got.Run.Usage != "ephemeral" || got.Run.Status != "completed" || got.Run.AgentID != "agent-2" || got.Reply.AgentMessageID != "dm-reply-1" || got.Reply.TargetSessionID != "run-1" {
		t.Fatalf("ephemeral call = %#v, want ephemeral target run and reply routed to caller", got)
	}
	targetTail, err := store.TailRunLogMessages(ctx, got.Run.SessionID, 3)
	if err != nil {
		t.Fatalf("TailRunLogMessages target returned error: %v", err)
	}
	if len(targetTail) != 3 || targetTail[0].Parts[0].Text != "Spring question" || targetTail[1].Parts[0].Text != "Research this" || targetTail[2].Role != "assistant" || targetTail[2].Parts[0].Text != "Use Spring Boot 4 feature flags." {
		t.Fatalf("target tail = %#v, want excerptd context, request, harness response", targetTail)
	}
	sourceTail, err := store.TailRunLogMessages(ctx, "run-1", 2)
	if err != nil {
		t.Fatalf("TailRunLogMessages source returned error: %v", err)
	}
	if sourceTail[len(sourceTail)-1].Role != "user" || sourceTail[len(sourceTail)-1].Parts[0].Text != "Use Spring Boot 4 feature flags." {
		t.Fatalf("source tail = %#v, want librarian reply visible in caller run", sourceTail)
	}
}

func TestEphemeralAgentCallCoversPlannerExecutorReviewerAndParallelOrchestratorWorkflows(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	for _, agent := range []globaldb.AgentSessionConfig{
		{AgentID: "planner", WorkspaceID: "ws-1", Name: "planner", Harness: "planner-harness"},
		{AgentID: "executor", WorkspaceID: "ws-1", Name: "implementation-executor", Harness: "executor-harness"},
		{AgentID: "reviewer", WorkspaceID: "ws-1", Name: "reviewer", Harness: "reviewer-harness"},
	} {
		if err := store.CreateAgentSessionConfig(ctx, agent); err != nil {
			t.Fatalf("CreateAgentSessionConfig(%s) returned error: %v", agent.AgentID, err)
		}
	}
	for _, msg := range []globaldb.RunLogMessage{
		{MessageID: "plan-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "plan-part-1", Sequence: 1, Kind: "text", Text: "Implement API"}}},
		{MessageID: "plan-2", SessionID: "run-1", Sequence: 2, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "plan-part-2", Sequence: 1, Kind: "text", Text: "Add tests"}}},
		{MessageID: "plan-3", SessionID: "run-1", Sequence: 3, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "plan-part-3", Sequence: 1, Kind: "text", Text: "Run verify"}}},
	} {
		if err := store.AppendRunLogMessage(ctx, msg); err != nil {
			t.Fatalf("AppendRunLogMessage(%s) returned error: %v", msg.MessageID, err)
		}
	}
	lastTwo, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-last-two", SourceSessionID: "run-1", TargetAgentID: "reviewer", Count: 2})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}
	firstPlan, err := store.CreateContextExcerptFromExplicitIDs(ctx, globaldb.CreateContextExcerptFromExplicitIDsParams{ContextExcerptID: "excerpt-first-plan", SourceSessionID: "run-1", TargetAgentID: "executor", MessageIDs: []string{"plan-1"}})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromExplicitIDs returned error: %v", err)
	}

	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	for harness, text := range map[string]string{"executor-harness": "executor finished", "reviewer-harness": "reviewer approved", "planner-harness": "planner split work"} {
		harness := harness
		text := text
		d.setHarnessFactoryForTest(harness, func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
			_ = req
			_ = primaryFolder
			_ = sink
			return newFakeHarness(harness, []TimelineItem{{Kind: "agent_text", Text: text}}), nil
		})
	}
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	executorCall := callMethod[EphemeralAgentCallResponse](t, registry, "agent.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-executor", SourceSessionID: "run-1", TargetAgentID: "executor", Body: "execute plan", ContextExcerptIDs: []string{firstPlan.ContextExcerptID}})
	reviewerCall := callMethod[EphemeralAgentCallResponse](t, registry, "agent.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-reviewer", SourceSessionID: "run-1", TargetAgentID: "reviewer", Body: "review last updates", ContextExcerptIDs: []string{lastTwo.ContextExcerptID}})
	plannerCall := callMethod[EphemeralAgentCallResponse](t, registry, "agent.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-planner", SourceSessionID: executorCall.Run.SessionID, TargetAgentID: "planner", Body: "plan follow-up"})
	if executorCall.Run.AgentID != "executor" || reviewerCall.Run.AgentID != "reviewer" || plannerCall.Run.AgentID != "planner" {
		t.Fatalf("calls = %#v %#v %#v, want target-specific ephemeral runs", executorCall.Run, reviewerCall.Run, plannerCall.Run)
	}
	reviewerTail, err := store.TailRunLogMessages(ctx, reviewerCall.Run.SessionID, 4)
	if err != nil {
		t.Fatalf("TailRunLogMessages reviewer returned error: %v", err)
	}
	if len(reviewerTail) != 4 || reviewerTail[0].Parts[0].Text != "Add tests" || reviewerTail[1].Parts[0].Text != "Run verify" || reviewerTail[2].Parts[0].Text != "review last updates" || reviewerTail[3].Parts[0].Text != "reviewer approved" {
		t.Fatalf("reviewer tail = %#v, want last-N excerpt then review request", reviewerTail)
	}
	sourceTail, err := store.TailRunLogMessages(ctx, "run-1", 6)
	if err != nil {
		t.Fatalf("TailRunLogMessages source returned error: %v", err)
	}
	texts := []string{}
	for _, msg := range sourceTail {
		if len(msg.Parts) > 0 {
			texts = append(texts, msg.Parts[0].Text)
		}
	}
	if !containsString(texts, "executor finished") || !containsString(texts, "reviewer approved") {
		t.Fatalf("source texts = %#v, want parallel orchestrator replies routed independently", texts)
	}
}

func TestAgentSessionConfigCreateMethodDoesNotCreateRun(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "workspace", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	got := callMethod[AgentSessionConfigCreateResponse](t, registry, "workspace.agent.create", AgentSessionConfigCreateRequest{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "planner", Harness: "codex"})
	if got.Agent.Name != "planner" {
		t.Fatalf("agent = %#v, want planner", got.Agent)
	}
	runs, err := store.ListAgentSessions(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListAgentSessions returned error: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs = %#v, want no run from agent create", runs)
	}
}

func TestAgentSessionConfigRosterMethodsAndRun(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "workspace", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	_ = callMethod[AgentSessionConfigCreateResponse](t, registry, "workspace.agent.create", AgentSessionConfigCreateRequest{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "planner", Harness: "codex", Model: "gpt-5", Prompt: "plan"})
	listed := callMethod[AgentSessionConfigListResponse](t, registry, "workspace.agent.list", AgentSessionConfigListRequest{WorkspaceID: "ws-1"})
	if len(listed.Agents) != 1 || listed.Agents[0].Name != "planner" {
		t.Fatalf("listed = %#v, want planner", listed)
	}
	updated := callMethod[AgentSessionConfigResponse](t, registry, "workspace.agent.update", AgentSessionConfigUpdateRequest{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "planner", Harness: "claude", Model: "sonnet", Prompt: "revise"})
	if updated.Harness != "claude" || updated.Prompt != "revise" {
		t.Fatalf("updated = %#v, want changed agent", updated)
	}
	run := callMethod[AgentSessionConfigSessionResponse](t, registry, "workspace.agent.run", AgentSessionConfigSessionRequest{SessionID: "run-1", AgentID: "agent-1", CWD: t.TempDir()})
	if run.Run.AgentID != "agent-1" || run.Run.Harness != "claude" || run.Run.Status != "waiting" {
		t.Fatalf("run = %#v, want run from updated agent", run)
	}
	_ = callMethod[AgentSessionConfigDeleteResponse](t, registry, "workspace.agent.delete", AgentSessionConfigDeleteRequest{AgentID: "agent-1"})
	listed = callMethod[AgentSessionConfigListResponse](t, registry, "workspace.agent.list", AgentSessionConfigListRequest{WorkspaceID: "ws-1"})
	if len(listed.Agents) != 0 {
		t.Fatalf("listed after delete = %#v, want empty", listed)
	}
}

func seedRunLogMessageMethodData(t *testing.T, store *globaldb.Store, ctx context.Context) {
	t.Helper()
	if err := store.CreateSession(ctx, "ws-1", "workspace", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "executor", Harness: "codex"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig returned error: %v", err)
	}
	if err := store.CreateAgentSession(ctx, globaldb.AgentSession{SessionID: "run-1", WorkspaceID: "ws-1", AgentID: "agent-1", Harness: "codex", Status: "running", Usage: "durable", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateAgentSession returned error: %v", err)
	}
}
