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

	for _, name := range []string{"session.start", "profile.create", "profile.get", "profile.list", "context.excerpt.create_from_tail", "context.excerpt.create_from_range", "context.excerpt.create_from_explicit_ids", "context.excerpt.get", "session.message.send"} {
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

	for _, name := range []string{"agent.profile.run", "agent.profile.create", "agent.profile.get", "agent.profile.list", "agent.profile.helper.ensure", "agent.profile.helper.get", "profile.helper.ensure", "profile.helper.get", "message.excerpt.create_from_tail", "message.excerpt.create_from_range", "message.excerpt.create_from_explicit_ids", "message.excerpt.get"} {
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
	got := callMethod[AgentMessageSendResponse](t, registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "review this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, StartSessionID: "run-2"})
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
	_ = callMethod[AgentMessageSendResponse](t, registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "review this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, StartSessionID: "run-2"})
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
	_ = callMethod[AgentMessageSendResponse](t, registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "review this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, StartSessionID: "run-2"})
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
	if err := store.CreateAgentSession(ctx, globaldb.AgentSession{SessionID: "executor-run", WorkspaceID: "ws-1", AgentID: "executor-target", Harness: "executor-harness", Status: "waiting", Usage: "durable", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateAgentSession executor returned error: %v", err)
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
	got := callMethod[AgentMessageSendResponse](t, registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "planner-dm", SourceSessionID: "planner-run", TargetSessionID: "executor-run", Body: "Please implement", ContextExcerptIDs: []string{excerpt.ContextExcerptID}})
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

func TestSessionMessageSendTrimsContextExcerptIDsInResponse(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "Build the endpoint"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	excerpt, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-trim", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	got := callMethod[AgentMessageSendResponse](t, registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-trim", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "Please implement", ContextExcerptIDs: []string{" " + excerpt.ContextExcerptID + " "}, StartSessionID: "run-2"})
	if len(got.AgentMessage.ContextExcerptIDs) != 1 || got.AgentMessage.ContextExcerptIDs[0] != excerpt.ContextExcerptID {
		t.Fatalf("context excerpt ids = %#v, want trimmed excerpt id", got.AgentMessage.ContextExcerptIDs)
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

	got := callMethod[EphemeralAgentCallResponse](t, registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "Research this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, ReplyAgentMessageID: "dm-reply-1"})
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
	final := callMethod[FinalResponseResponse](t, registry, "final_response.get", FinalResponseGetRequest{SessionID: got.Run.SessionID})
	if final.SessionID != got.Run.SessionID || final.Status != "completed" || final.Text != "Use Spring Boot 4 feature flags." {
		t.Fatalf("final response = %#v, want persisted ephemeral call final response by session", final)
	}
}

func TestEphemeralAgentCallResolvesTargetByProfileName(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "Spring question"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}

	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("test-harness", []TimelineItem{{ID: "ti_reply", Kind: "agent_text", Text: "Reviewed", Status: "completed"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	created := callMethod[AgentProfileResponse](t, registry, "profile.create", AgentProfileCreateRequest{WorkspaceID: "ws-1", Name: "reviewer", Harness: "test-harness", Model: "model-1", Prompt: "review", InvocationClass: HarnessInvocationTemporary})
	excerpt, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: created.ProfileID, Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}
	got := callMethod[EphemeralAgentCallResponse](t, registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-1", SourceSessionID: "run-1", TargetAgentID: "reviewer", Body: "Research this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, ReplyAgentMessageID: "dm-reply-1"})
	if got.Run.AgentID != created.ProfileID || got.Run.SourceSessionID != "run-1" || got.Reply.Body != "Reviewed" {
		t.Fatalf("call response = %#v, want profile-name resolution to stored target profile id", got)
	}
}

func TestEphemeralAgentCallRejectsUnknownTargetProfile(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-missing", SourceSessionID: "run-1", TargetAgentID: "missing-profile", Body: "Research this"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "unknown_profile" || data["profile"] != "missing-profile" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown_profile details", data)
	}
}

func TestEphemeralAgentCallRejectsUnknownSourceSessionWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-missing-source", SourceSessionID: "missing-source", TargetAgentID: "agent-2", Body: "Research this"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "unknown_source_session" || data["source_session_id"] != "missing-source" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown_source_session details", data)
	}
}

func TestEphemeralAgentCallRejectsCrossWorkspaceTargetWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateSession(ctx, "ws-2", "workspace-2", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession ws-2 returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-other", WorkspaceID: "ws-2", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-cross-ws", SourceSessionID: "run-1", TargetAgentID: "agent-other", Body: "Research this"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "target_workspace_mismatch" || data["target_agent_id"] != "agent-other" || data["source_workspace_id"] != "ws-1" || data["target_workspace_id"] != "ws-2" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want cross-workspace target details", data)
	}
}

func TestEphemeralAgentCallRejectsMissingRequiredFieldsWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-missing-body", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: ""})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "missing_required_fields" || data["missing_field"] != "body" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want missing required field details", data)
	}
}

func TestSessionFanoutSendsVisibleMessageToTargetSession(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "reviewer", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	if err := store.CreateAgentSession(ctx, globaldb.AgentSession{SessionID: "reviewer-run", WorkspaceID: "ws-1", AgentID: "reviewer", Harness: "opencode", Status: "waiting", Usage: "durable"}); err != nil {
		t.Fatalf("CreateAgentSession target returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	got := callMethod[AgentMessageSendResponse](t, registry, "session.fanout", AgentMessageSendRequest{AgentMessageID: "fanout-1", SourceSessionID: "run-1", TargetSessionID: "reviewer-run", Body: "fan out"})
	if got.AgentMessage.AgentMessageID != "fanout-1" || got.AgentMessage.TargetAgentID != "reviewer" || got.AgentMessage.TargetSessionID != "reviewer-run" || got.AgentMessage.Status != "delivered" {
		t.Fatalf("fanout response = %#v, want delivered fanout message", got.AgentMessage)
	}
}

func TestSessionMessageSendRejectsUnknownTargetAgentWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-missing", SourceSessionID: "run-1", TargetAgentID: "missing-agent", Body: "review this", StartSessionID: "run-2"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "unknown_target_agent" || data["target_agent_id"] != "missing-agent" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown_target_agent details", data)
	}
}

func TestSessionMessageSendRejectsUnknownSourceSessionWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-missing-source", SourceSessionID: "missing-source", TargetAgentID: "agent-2", Body: "review this", StartSessionID: "run-2"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "unknown_source_session" || data["source_session_id"] != "missing-source" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown_source_session details", data)
	}
}

func TestSessionMessageSendRejectsMissingRequiredFieldsWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-missing-body", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "", StartSessionID: "run-2"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "missing_required_fields" || data["missing_field"] != "body" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want missing required field details", data)
	}
}

func TestSessionMessageSendRejectsMismatchedContextExcerptWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-3", WorkspaceID: "ws-1", Name: "other", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig other returned error: %v", err)
	}
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "plan"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	excerpt, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-3", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err = callMethodError(registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-bad-excerpt", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "review this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, StartSessionID: "run-2"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "context_excerpt_mismatch" || data["context_excerpt_id"] != "excerpt-1" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want context excerpt mismatch details", data)
	}
}

func TestSessionMessageSendRejectsTargetSessionOnlyContextExcerptMismatchWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-3", WorkspaceID: "ws-1", Name: "other", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig other returned error: %v", err)
	}
	if err := store.CreateAgentSession(ctx, globaldb.AgentSession{SessionID: "run-2", WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "opencode", Status: "waiting", Usage: "durable"}); err != nil {
		t.Fatalf("CreateAgentSession target returned error: %v", err)
	}
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "plan"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	excerpt, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-3", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err = callMethodError(registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-bad-excerpt", SourceSessionID: "run-1", TargetSessionID: "run-2", Body: "review this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "context_excerpt_mismatch" || data["context_excerpt_id"] != "excerpt-1" || data["target_session_id"] != "run-2" || data["target_agent_id"] != "agent-2" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want target-session-only context excerpt mismatch details", data)
	}
}

func TestSessionMessageSendRejectsTargetSessionAgentMismatchWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-3", WorkspaceID: "ws-1", Name: "other", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig other returned error: %v", err)
	}
	if err := store.CreateAgentSession(ctx, globaldb.AgentSession{SessionID: "run-2", WorkspaceID: "ws-1", AgentID: "agent-3", Harness: "opencode", Status: "waiting", Usage: "durable"}); err != nil {
		t.Fatalf("CreateAgentSession target returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-session-mismatch", SourceSessionID: "run-1", TargetAgentID: "agent-2", TargetSessionID: "run-2", Body: "review this"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "target_session_mismatch" || data["target_session_id"] != "run-2" || data["target_agent_id"] != "agent-2" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want target session mismatch details", data)
	}
}

func TestSessionMessageSendRejectsUnknownTargetSessionWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-missing-target-session", SourceSessionID: "run-1", TargetSessionID: "missing-target", Body: "review this"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "unknown_target_session" || data["target_session_id"] != "missing-target" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown target session details", data)
	}
}

func TestSessionMessageSendRejectsUnknownStartSessionWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-missing-start-session", SourceSessionID: "run-1", StartSessionID: "missing-start", Body: "review this"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "unknown_target_session" || data["target_session_id"] != "missing-start" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown start session target details", data)
	}
}

func TestSessionMessageSendRejectsCrossWorkspaceTargetSessionWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateSession(ctx, "ws-2", "workspace-2", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession ws-2 returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-3", WorkspaceID: "ws-2", Name: "reviewer-2", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig ws-2 returned error: %v", err)
	}
	if err := store.CreateAgentSession(ctx, globaldb.AgentSession{SessionID: "run-2", WorkspaceID: "ws-2", AgentID: "agent-3", Harness: "opencode", Status: "waiting", Usage: "durable"}); err != nil {
		t.Fatalf("CreateAgentSession ws-2 target returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-cross-ws", SourceSessionID: "run-1", TargetSessionID: "run-2", Body: "review this"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "target_workspace_mismatch" || data["target_session_id"] != "run-2" || data["source_workspace_id"] != "ws-1" || data["target_workspace_id"] != "ws-2" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want cross-workspace target session details", data)
	}
}

func TestSessionMessageSendRejectsCrossWorkspaceTargetAgentWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateSession(ctx, "ws-2", "workspace-2", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession ws-2 returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-cross", WorkspaceID: "ws-2", Name: "reviewer-2", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig ws-2 returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-cross-agent", SourceSessionID: "run-1", TargetAgentID: "agent-cross", Body: "review this"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "target_workspace_mismatch" || data["target_agent_id"] != "agent-cross" || data["source_workspace_id"] != "ws-1" || data["target_workspace_id"] != "ws-2" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want cross-workspace target agent details", data)
	}
}

func TestSessionMessageSendRejectsDuplicateAgentMessageIDWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	first := callMethod[AgentMessageSendResponse](t, registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-dup", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "first", StartSessionID: "run-2"})
	if first.AgentMessage.TargetSessionID == "" {
		t.Fatalf("first response = %#v, want target session id", first)
	}

	err := callMethodError(registry, "session.message.send", AgentMessageSendRequest{AgentMessageID: "dm-dup", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "second", StartSessionID: "run-3"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "agent_message_id_conflict" || data["agent_message_id"] != "dm-dup" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want duplicate agent_message_id details", data)
	}
}

func TestEphemeralAgentCallRejectsConflictingSessionIDWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	if err := store.CreateAgentSession(ctx, globaldb.AgentSession{SessionID: "existing-run", WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "opencode", Status: "waiting", Usage: "durable"}); err != nil {
		t.Fatalf("CreateAgentSession existing returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-conflict", SessionID: "existing-run", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "Research this"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "session_id_conflict" || data["session_id"] != "existing-run" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want session conflict details", data)
	}
}

func TestEphemeralAgentCallRejectsMismatchedContextExcerptWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-3", WorkspaceID: "ws-1", Name: "other", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig other returned error: %v", err)
	}
	if err := store.AppendRunLogMessage(ctx, globaldb.RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []globaldb.RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "plan"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	excerpt, err := store.CreateContextExcerptFromTail(ctx, globaldb.CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-3", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err = callMethodError(registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-bad-excerpt", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "Research this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "context_excerpt_mismatch" || data["context_excerpt_id"] != "excerpt-1" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want context excerpt mismatch details", data)
	}
	runs, listErr := store.ListAgentSessions(ctx, "ws-1")
	if listErr != nil {
		t.Fatalf("ListAgentSessions returned error: %v", listErr)
	}
	for _, run := range runs {
		if run.SessionID == "call-bad-excerpt-run" {
			t.Fatalf("sessions = %#v, want rejected call not to leave an ephemeral session", runs)
		}
	}
}

func TestEphemeralAgentCallRejectsUnknownContextExcerptWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-missing-excerpt", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "Research this", ContextExcerptIDs: []string{"excerpt-missing"}})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "unknown_context_excerpt" || data["context_excerpt_id"] != "excerpt-missing" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown context excerpt details", data)
	}
}

func TestEphemeralAgentCallRejectsReplyTargetAgentMissingConfigWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "workspace", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-missing", WorkspaceID: "ws-1", Name: "source", Harness: "codex"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig source returned error: %v", err)
	}
	if err := store.CreateAgentSession(ctx, globaldb.AgentSession{SessionID: "run-missing-agent", WorkspaceID: "ws-1", AgentID: "agent-missing", Harness: "codex", Status: "running", Usage: "durable", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateAgentSession source returned error: %v", err)
	}
	if err := store.DeleteAgentSessionConfig(ctx, "agent-missing"); err != nil {
		t.Fatalf("DeleteAgentSessionConfig source returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "test-harness", Model: "model-1", Prompt: "research"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}

	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("test-harness", []TimelineItem{{Kind: "agent_text", Text: "reply"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	err := callMethodError(registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-missing-reply-target", SourceSessionID: "run-missing-agent", TargetAgentID: "agent-2", Body: "Research this"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "unknown_reply_target_agent" || data["target_agent_id"] != "agent-missing" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want unknown reply target agent details", data)
	}
}

func TestEphemeralAgentCallRejectsDuplicateCallIDRequestMessageConflictWithStructuredError(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	seedRunLogMessageMethodData(t, store, ctx)
	if err := store.CreateAgentSessionConfig(ctx, globaldb.AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "test-harness", Model: "model-1", Prompt: "research"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig target returned error: %v", err)
	}
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	d.setHarnessFactoryForTest("test-harness", func(req AgentSessionStartRequest, primaryFolder string, sink func(string, []TimelineItem)) (Executor, error) {
		_ = req
		_ = primaryFolder
		_ = sink
		return newFakeHarness("test-harness", []TimelineItem{{Kind: "agent_text", Text: "reply"}}), nil
	})
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}

	_ = callMethod[EphemeralAgentCallResponse](t, registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-dup", SessionID: "call-dup-run-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "Research this"})
	err := callMethodError(registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-dup", SessionID: "call-dup-run-2", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "Research this again"})
	handlerErr, ok := err.(*rpc.HandlerError)
	if !ok || handlerErr.Code != rpc.InvalidParams {
		t.Fatalf("error = %T %[1]v, want InvalidParams handler error", err)
	}
	data := requireHandlerErrorData(t, err)
	if data["reason"] != "request_agent_message_id_conflict" || data["agent_message_id"] != "call-dup-request" || data["start_invoked"] != false {
		t.Fatalf("error data = %#v, want duplicate request message details", data)
	}
	runs, listErr := store.ListAgentSessions(ctx, "ws-1")
	if listErr != nil {
		t.Fatalf("ListAgentSessions returned error: %v", listErr)
	}
	for _, run := range runs {
		if run.SessionID == "call-dup-run-2" {
			t.Fatalf("sessions = %#v, want rejected duplicate request not to leave an ephemeral session", runs)
		}
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

	executorCall := callMethod[EphemeralAgentCallResponse](t, registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-executor", SourceSessionID: "run-1", TargetAgentID: "executor", Body: "execute plan", ContextExcerptIDs: []string{firstPlan.ContextExcerptID}})
	reviewerCall := callMethod[EphemeralAgentCallResponse](t, registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-reviewer", SourceSessionID: "run-1", TargetAgentID: "reviewer", Body: "review last updates", ContextExcerptIDs: []string{lastTwo.ContextExcerptID}})
	plannerCall := callMethod[EphemeralAgentCallResponse](t, registry, "session.call.ephemeral", EphemeralAgentCallRequest{CallID: "call-planner", SourceSessionID: executorCall.Run.SessionID, TargetAgentID: "planner", Body: "plan follow-up"})
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

func TestProfileCreateMethodDoesNotCreateRun(t *testing.T) {
	store := newCommandMethodTestStore(t)
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	got := callMethod[AgentProfileResponse](t, registry, "profile.create", AgentProfileCreateRequest{WorkspaceID: "ws-1", Name: "planner", Harness: "codex"})
	if got.Name != "planner" {
		t.Fatalf("profile = %#v, want planner", got)
	}
	runs, err := store.ListAgentSessions(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("ListAgentSessions returned error: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs = %#v, want no run from agent create", runs)
	}
}

func TestProfileRosterMethodsAndSessionStart(t *testing.T) {
	store := newCommandMethodTestStore(t)
	seedSessionWithPrimaryFolder(t, store, "ws-1", t.TempDir())
	registry := rpc.NewMethodRegistry()
	d := New("/tmp/daemon.sock", "/tmp/ari.db", "/tmp/daemon.pid", "defaults", "defaults", "test-version")
	if err := d.registerMethods(registry, store); err != nil {
		t.Fatalf("registerMethods returned error: %v", err)
	}
	created := callMethod[AgentProfileResponse](t, registry, "profile.create", AgentProfileCreateRequest{WorkspaceID: "ws-1", Name: "planner", Harness: "pty", Model: "gpt-5", Prompt: "plan"})
	listed := callMethod[AgentProfileListResponse](t, registry, "profile.list", AgentProfileListRequest{WorkspaceID: "ws-1"})
	if len(listed.Profiles) != 1 || listed.Profiles[0].Name != "planner" {
		t.Fatalf("listed = %#v, want planner", listed)
	}
	got := callMethod[AgentProfileResponse](t, registry, "profile.get", AgentProfileGetRequest{WorkspaceID: "ws-1", Name: "planner"})
	if got.ProfileID != created.ProfileID || got.Harness != "pty" || got.Prompt != "plan" {
		t.Fatalf("got profile = %#v, want created planner", got)
	}
	run := callMethod[AgentSessionStartResponse](t, registry, "session.start", AgentSessionStartRequest{WorkspaceID: "ws-1", Profile: "planner", SessionID: "run-1", Command: "/bin/sh", Args: []string{"-c", "printf done"}})
	if run.Run.AgentSessionID != "run-1" || run.Run.Executor != "pty" || (run.Run.Status != "running" && run.Run.Status != "completed") {
		t.Fatalf("run = %#v, want pty session from profile", run.Run)
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
