package globaldb

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestAgentRuntimeSchemaUsesADRTerminology(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-sessiontime-terminology")
	ctx := context.Background()

	for _, table := range []string{"workspace_agents", "agent_runs", "run_messages", "run_message_parts", "message_shares", "message_share_items", "direct_messages", "direct_message_shares"} {
		var count int
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatalf("query old table %s returned error: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("old table %s exists; want ADR-aligned agent_session/run_log/context_excerpt/agent_message storage names", table)
		}
	}

	for _, table := range []string{"agent_session_configs", "agent_sessions", "run_log_messages", "run_log_message_parts", "context_excerpts", "context_excerpt_items", "agent_messages", "agent_message_context_excerpts"} {
		var count int
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatalf("query table %s returned error: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("table %s exists = %d, want 1", table, count)
		}
	}
}

func TestRunLogMessagesTailReturnsLastNInRunOrder(t *testing.T) {
	store := newGlobalDBTestStore(t, "run-message-tail")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)

	for _, msg := range []RunLogMessage{
		{MessageID: "msg-1", WorkspaceID: "ws-1", SessionID: "run-1", AgentID: "agent-1", Sequence: 1, Role: "user", Status: "completed", Parts: []RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "one"}}},
		{MessageID: "msg-2", WorkspaceID: "ws-1", SessionID: "run-1", AgentID: "agent-1", Sequence: 2, Role: "assistant", Status: "completed", Parts: []RunLogMessagePart{{PartID: "part-2", Sequence: 1, Kind: "text", Text: "two"}}},
		{MessageID: "msg-3", WorkspaceID: "ws-1", SessionID: "run-1", AgentID: "agent-1", Sequence: 3, Role: "user", Status: "completed", Parts: []RunLogMessagePart{{PartID: "part-3", Sequence: 1, Kind: "text", Text: "three"}}},
	} {
		if err := store.AppendRunLogMessage(ctx, msg); err != nil {
			t.Fatalf("AppendRunLogMessage(%s) returned error: %v", msg.MessageID, err)
		}
	}

	tail, err := store.TailRunLogMessages(ctx, "run-1", 2)
	if err != nil {
		t.Fatalf("TailRunLogMessages returned error: %v", err)
	}
	if len(tail) != 2 || tail[0].MessageID != "msg-2" || tail[1].MessageID != "msg-3" {
		t.Fatalf("tail = %#v, want msg-2,msg-3 in ascending run order", tail)
	}
	if len(tail[0].Parts) != 1 || tail[0].Parts[0].Text != "two" {
		t.Fatalf("tail parts = %#v, want copied message parts", tail[0].Parts)
	}
}

func TestRunLogMessagesTailRejectsUnknownRun(t *testing.T) {
	store := newGlobalDBTestStore(t, "run-message-tail-unknown-run")
	ctx := context.Background()

	_, err := store.TailRunLogMessages(ctx, "missing-run", 1)
	if err != ErrNotFound {
		t.Fatalf("TailRunLogMessages error = %v, want ErrNotFound", err)
	}
}

func TestRunLogMessagesListReturnsCursorLimitedPageInRunOrder(t *testing.T) {
	store := newGlobalDBTestStore(t, "run-message-list")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	for _, msg := range []RunLogMessage{
		{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "user", Parts: []RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "one"}}},
		{MessageID: "msg-2", SessionID: "run-1", Sequence: 2, Role: "assistant", Parts: []RunLogMessagePart{{PartID: "part-2", Sequence: 1, Kind: "text", Text: "two"}}},
		{MessageID: "msg-3", SessionID: "run-1", Sequence: 3, Role: "tool", ProviderMessageID: "provider-msg-3", ProviderItemID: "item-3", ProviderTurnID: "turn-1", ProviderResponseID: "response-1", ProviderCallID: "call-1", ProviderChannel: "analysis", ProviderKind: "function_call_output", RawMetadataJSON: `{"provider":"codex"}`, Parts: []RunLogMessagePart{{PartID: "part-3", Sequence: 1, Kind: "tool_result", Text: "three", ToolCallID: "call-1", RawJSON: `{"ok":true}`}}},
	} {
		if err := store.AppendRunLogMessage(ctx, msg); err != nil {
			t.Fatalf("AppendRunLogMessage(%s) returned error: %v", msg.MessageID, err)
		}
	}

	page, err := store.ListRunLogMessages(ctx, "run-1", 1, 2)
	if err != nil {
		t.Fatalf("ListRunLogMessages returned error: %v", err)
	}
	if len(page) != 2 || page[0].MessageID != "msg-2" || page[1].MessageID != "msg-3" || page[1].ProviderMessageID != "provider-msg-3" || page[1].ProviderChannel != "analysis" || page[1].RawMetadataJSON != `{"provider":"codex"}` || page[1].Parts[0].ToolCallID != "call-1" || page[1].Parts[0].RawJSON != `{"ok":true}` {
		t.Fatalf("page = %#v, want messages after sequence 1 with parts in run order", page)
	}
}

func TestAgentSessionMetadataRoundTrips(t *testing.T) {
	store := newGlobalDBTestStore(t, "run-metadata")
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "workspace", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "executor", Harness: "codex"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig returned error: %v", err)
	}
	if err := store.CreateAgentSession(ctx, AgentSession{SessionID: "run-1", WorkspaceID: "ws-1", AgentID: "agent-1", Harness: "codex", Model: "gpt-5", ProviderSessionID: "thread-1", CWD: t.TempDir(), FolderScopeJSON: `["/repo"]`, Status: "running", SourceSessionID: "source-run", SourceAgentID: "source-agent", PromptHash: "sha256:prompt", ContextPayloadIDsJSON: `["ctx-1"]`, PermissionMode: "ask", SandboxMode: "workspace-write", ToolScopeJSON: `{"shell":true}`, ProviderMetadataJSON: `{"native":"yes"}`}); err != nil {
		t.Fatalf("CreateAgentSession returned error: %v", err)
	}
	runs, err := store.ListAgentSessions(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListAgentSessions returned error: %v", err)
	}
	if len(runs) != 1 || runs[0].FolderScopeJSON != `["/repo"]` || runs[0].ContextPayloadIDsJSON != `["ctx-1"]` || runs[0].PermissionMode != "ask" || runs[0].SandboxMode != "workspace-write" || runs[0].ProviderMetadataJSON != `{"native":"yes"}` {
		t.Fatalf("runs = %#v, want run metadata round-tripped", runs)
	}
}

func TestRunLogMessagesListRejectsUnknownRun(t *testing.T) {
	store := newGlobalDBTestStore(t, "run-message-list-unknown-run")
	ctx := context.Background()

	_, err := store.ListRunLogMessages(ctx, "missing-run", 0, 1)
	if err != ErrNotFound {
		t.Fatalf("ListRunLogMessages error = %v, want ErrNotFound", err)
	}
}

func TestRunLogMessagesRejectUnknownRun(t *testing.T) {
	store := newGlobalDBTestStore(t, "run-message-unknown-run")
	ctx := context.Background()

	if _, err := store.TailRunLogMessages(ctx, "missing-run", 1); err != ErrNotFound {
		t.Fatalf("TailRunLogMessages error = %v, want ErrNotFound", err)
	}
	if _, err := store.ListRunLogMessages(ctx, "missing-run", 0, 10); err != ErrNotFound {
		t.Fatalf("ListRunLogMessages error = %v, want ErrNotFound", err)
	}
}

func TestContextExcerptCopiesOrderedExcerptAndIsImmutable(t *testing.T) {
	store := newGlobalDBTestStore(t, "context-excerpt")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	for _, msg := range []RunLogMessage{
		{MessageID: "msg-1", WorkspaceID: "ws-1", SessionID: "run-1", AgentID: "agent-1", Sequence: 1, Role: "user", Status: "completed", Parts: []RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "question"}}},
		{MessageID: "msg-2", WorkspaceID: "ws-1", SessionID: "run-1", AgentID: "agent-1", Sequence: 2, Role: "assistant", Status: "completed", Parts: []RunLogMessagePart{{PartID: "part-2", Sequence: 1, Kind: "text", Text: "answer"}}},
	} {
		if err := store.AppendRunLogMessage(ctx, msg); err != nil {
			t.Fatalf("AppendRunLogMessage(%s) returned error: %v", msg.MessageID, err)
		}
	}

	excerpt, err := store.CreateContextExcerptFromTail(ctx, CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", WorkspaceID: "ws-1", SourceSessionID: "run-1", SourceAgentID: "agent-1", TargetAgentID: "agent-2", Count: 2, AppendedMessage: "continue from here"})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}
	if len(excerpt.Items) != 2 || excerpt.Items[0].CopiedText != "question" || excerpt.Items[1].CopiedText != "answer" {
		t.Fatalf("excerpt items = %#v, want copied ordered excerpt", excerpt.Items)
	}
	if excerpt.SelectorType != "last_n" || excerpt.SelectorJSON == "" || excerpt.Visibility != "visible_context" {
		t.Fatalf("excerpt selector = %#v, want durable last_n visible_context selector", excerpt)
	}
	if len(excerpt.Items[1].CopiedParts) != 1 || excerpt.Items[1].CopiedParts[0].Kind != "text" || excerpt.Items[1].CopiedParts[0].Text != "answer" {
		t.Fatalf("excerpt item parts = %#v, want copied normalized parts", excerpt.Items[1].CopiedParts)
	}

	if err := store.AppendRunLogMessage(ctx, RunLogMessage{MessageID: "msg-3", WorkspaceID: "ws-1", SessionID: "run-1", AgentID: "agent-1", Sequence: 3, Role: "assistant", Status: "completed", Parts: []RunLogMessagePart{{PartID: "part-3", Sequence: 1, Kind: "text", Text: "new"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage msg-3 returned error: %v", err)
	}
	got, err := store.GetContextExcerpt(ctx, "excerpt-1")
	if err != nil {
		t.Fatalf("GetContextExcerpt returned error: %v", err)
	}
	if len(got.Items) != 2 || got.Items[1].CopiedText != "answer" {
		t.Fatalf("excerpt mutated after source changed: %#v", got.Items)
	}
	if got.SelectorType != "last_n" || got.SelectorJSON != excerpt.SelectorJSON || got.Visibility != "visible_context" {
		t.Fatalf("stored excerpt selector = %#v, want selector and visibility round tripped", got)
	}
	if len(got.Items[1].CopiedParts) != 1 || got.Items[1].CopiedParts[0].Text != "answer" {
		t.Fatalf("excerpt parts mutated or missing: %#v", got.Items[1].CopiedParts)
	}
}

func TestContextExcerptFromRangeCopiesInclusiveOrderedExcerpt(t *testing.T) {
	store := newGlobalDBTestStore(t, "message-excerpt-range")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	for _, msg := range []RunLogMessage{
		{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "user", Parts: []RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "one"}}},
		{MessageID: "msg-2", SessionID: "run-1", Sequence: 2, Role: "assistant", Parts: []RunLogMessagePart{{PartID: "part-2", Sequence: 1, Kind: "text", Text: "two"}}},
		{MessageID: "msg-3", SessionID: "run-1", Sequence: 3, Role: "tool", Parts: []RunLogMessagePart{{PartID: "part-3", Sequence: 1, Kind: "tool_result", Text: "three"}}},
		{MessageID: "msg-4", SessionID: "run-1", Sequence: 4, Role: "assistant", Parts: []RunLogMessagePart{{PartID: "part-4", Sequence: 1, Kind: "text", Text: "four"}}},
	} {
		if err := store.AppendRunLogMessage(ctx, msg); err != nil {
			t.Fatalf("AppendRunLogMessage(%s) returned error: %v", msg.MessageID, err)
		}
	}

	excerpt, err := store.CreateContextExcerptFromRange(ctx, CreateContextExcerptFromRangeParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", StartSequence: 2, EndSequence: 3, AppendedMessage: "use selected range"})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromRange returned error: %v", err)
	}
	if excerpt.SelectorType != "range" || !strings.Contains(excerpt.SelectorJSON, `"start_sequence":2`) || !strings.Contains(excerpt.SelectorJSON, `"end_sequence":3`) {
		t.Fatalf("excerpt selector = %#v, want range selector", excerpt)
	}
	if len(excerpt.Items) != 2 || excerpt.Items[0].SourceMessageID != "msg-2" || excerpt.Items[1].SourceMessageID != "msg-3" || excerpt.Items[1].CopiedRole != "tool" {
		t.Fatalf("excerpt items = %#v, want msg-2,msg-3 in run order", excerpt.Items)
	}
}

func TestContextExcerptFromExplicitIDsCopiesMessagesInRequestedOrder(t *testing.T) {
	store := newGlobalDBTestStore(t, "message-excerpt-explicit")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	for _, msg := range []RunLogMessage{
		{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "user", Parts: []RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "one"}}},
		{MessageID: "msg-2", SessionID: "run-1", Sequence: 2, Role: "assistant", Parts: []RunLogMessagePart{{PartID: "part-2", Sequence: 1, Kind: "text", Text: "two"}}},
		{MessageID: "msg-3", SessionID: "run-1", Sequence: 3, Role: "assistant", Parts: []RunLogMessagePart{{PartID: "part-3", Sequence: 1, Kind: "text", Text: "three"}}},
	} {
		if err := store.AppendRunLogMessage(ctx, msg); err != nil {
			t.Fatalf("AppendRunLogMessage(%s) returned error: %v", msg.MessageID, err)
		}
	}

	excerpt, err := store.CreateContextExcerptFromExplicitIDs(ctx, CreateContextExcerptFromExplicitIDsParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", MessageIDs: []string{"msg-3", "msg-1"}})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromExplicitIDs returned error: %v", err)
	}
	if excerpt.SelectorType != "explicit_ids" || !strings.Contains(excerpt.SelectorJSON, `"message_ids":["msg-3","msg-1"]`) {
		t.Fatalf("excerpt selector = %#v, want explicit_ids selector preserving requested ids", excerpt)
	}
	if len(excerpt.Items) != 2 || excerpt.Items[0].SourceMessageID != "msg-3" || excerpt.Items[1].SourceMessageID != "msg-1" {
		t.Fatalf("excerpt items = %#v, want requested explicit ID order", excerpt.Items)
	}
}

func TestContextExcerptPreservesNonTextParts(t *testing.T) {
	store := newGlobalDBTestStore(t, "message-excerpt-parts")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.AppendRunLogMessage(ctx, RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "tool", Parts: []RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "tool_result", Text: "tests passed"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}

	excerpt, err := store.CreateContextExcerptFromTail(ctx, CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}
	if len(excerpt.Items) != 1 || len(excerpt.Items[0].CopiedParts) != 1 || excerpt.Items[0].CopiedParts[0].Kind != "tool_result" || excerpt.Items[0].CopiedParts[0].Text != "tests passed" {
		t.Fatalf("excerpt item = %#v, want non-text part preserved", excerpt.Items)
	}
}

func TestContextExcerptPreservesPartMetadata(t *testing.T) {
	store := newGlobalDBTestStore(t, "message-excerpt-part-metadata")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.AppendRunLogMessage(ctx, RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "tool_call", Text: "search", ToolName: "web.search", ToolCallID: "call-1", RawJSON: `{"query":"ari"}`}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}

	excerpt, err := store.CreateContextExcerptFromTail(ctx, CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}
	if len(excerpt.Items) != 1 || len(excerpt.Items[0].CopiedParts) != 1 || excerpt.Items[0].CopiedParts[0].ToolCallID != "call-1" || excerpt.Items[0].CopiedParts[0].RawJSON != `{"query":"ari"}` {
		t.Fatalf("excerpt item = %#v, want copied part metadata", excerpt.Items)
	}

	if _, err := store.SendAgentMessage(ctx, AgentMessageSendParams{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, Body: "inspect", StartSessionID: "run-2"}); err != nil {
		t.Fatalf("SendAgentMessage returned error: %v", err)
	}
	tail, err := store.TailRunLogMessages(ctx, "run-2", 2)
	if err != nil {
		t.Fatalf("TailRunLogMessages target returned error: %v", err)
	}
	if len(tail) != 2 || len(tail[0].Parts) != 1 || tail[0].Parts[0].ToolName != "web.search" || tail[0].Parts[0].ToolCallID != "call-1" {
		t.Fatalf("delivered tail = %#v, want delivered part metadata", tail)
	}
}

func TestContextExcerptPersistsCopiedPartsWithNormalizedJSONKeys(t *testing.T) {
	store := newGlobalDBTestStore(t, "message-excerpt-part-json-shape")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.AppendRunLogMessage(ctx, RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "tool_call", ToolName: "web.search", ToolCallID: "call-1", RawJSON: `{"query":"ari"}`}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	if _, err := store.CreateContextExcerptFromTail(ctx, CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1}); err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}

	var raw string
	if err := store.db.QueryRowContext(ctx, `SELECT copied_parts_json FROM context_excerpt_items WHERE context_excerpt_id = ? AND sequence = ?`, "excerpt-1", 1).Scan(&raw); err != nil {
		t.Fatalf("query copied parts json returned error: %v", err)
	}
	if !strings.Contains(raw, `"tool_call_id":"call-1"`) || !strings.Contains(raw, `"raw_json":"{\"query\":\"ari\"}"`) {
		t.Fatalf("copied_parts_json = %s, want normalized snake_case part metadata keys", raw)
	}
	if strings.Contains(raw, `"ToolCallID"`) || strings.Contains(raw, `"RawJSON"`) {
		t.Fatalf("copied_parts_json = %s, want no Go field-name keys", raw)
	}
}

func TestContextExcerptCopiesOrderedPartsAndIsImmutable(t *testing.T) {
	store := newGlobalDBTestStore(t, "message-excerpt-parts")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.AppendRunLogMessage(ctx, RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []RunLogMessagePart{
		{PartID: "part-1", Sequence: 1, Kind: "text", Text: "result"},
		{PartID: "part-2", Sequence: 2, Kind: "tool_call", Text: "call search"},
	}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}

	excerpt, err := store.CreateContextExcerptFromTail(ctx, CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}
	if len(excerpt.Items) != 1 || len(excerpt.Items[0].CopiedParts) != 2 || excerpt.Items[0].CopiedParts[1].Kind != "tool_call" {
		t.Fatalf("excerpt items = %#v, want copied ordered part structure", excerpt.Items)
	}

	if err := store.AppendRunLogMessage(ctx, RunLogMessage{MessageID: "msg-2", SessionID: "run-1", Sequence: 2, Role: "assistant", Parts: []RunLogMessagePart{{PartID: "part-3", Sequence: 1, Kind: "text", Text: "new"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage msg-2 returned error: %v", err)
	}
	got, err := store.GetContextExcerpt(ctx, "excerpt-1")
	if err != nil {
		t.Fatalf("GetContextExcerpt returned error: %v", err)
	}
	if len(got.Items) != 1 || len(got.Items[0].CopiedParts) != 2 || got.Items[0].CopiedParts[1].Kind != "tool_call" {
		t.Fatalf("excerpt copied parts mutated or were lost: %#v", got.Items)
	}
}

func TestMessageWritesDeriveWorkspaceAndAgentFromRun(t *testing.T) {
	store := newGlobalDBTestStore(t, "message-identity")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)

	if err := store.AppendRunLogMessage(ctx, RunLogMessage{MessageID: "msg-1", WorkspaceID: "wrong-workspace", SessionID: "run-1", AgentID: "agent-2", Sequence: 1, Role: "user"}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	tail, err := store.TailRunLogMessages(ctx, "run-1", 1)
	if err != nil {
		t.Fatalf("TailRunLogMessages returned error: %v", err)
	}
	if len(tail) != 1 || tail[0].WorkspaceID != "ws-1" || tail[0].AgentID != "agent-1" {
		t.Fatalf("tail identity = %#v, want workspace and agent derived from run", tail)
	}
}

func TestContextExcerptDerivesSourceIdentityFromRun(t *testing.T) {
	store := newGlobalDBTestStore(t, "excerpt-identity")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.AppendRunLogMessage(ctx, RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "user", Parts: []RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "question"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}

	excerpt, err := store.CreateContextExcerptFromTail(ctx, CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", WorkspaceID: "wrong-workspace", SourceSessionID: "run-1", SourceAgentID: "agent-2", TargetAgentID: "agent-2", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}
	if excerpt.WorkspaceID != "ws-1" || excerpt.SourceAgentID != "agent-1" {
		t.Fatalf("excerpt identity = %#v, want source identity derived from run", excerpt)
	}
}

func TestContextExcerptContentHashIncludesAppendedMessage(t *testing.T) {
	store := newGlobalDBTestStore(t, "excerpt-hash-appended")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.AppendRunLogMessage(ctx, RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "plan"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}

	first, err := store.CreateContextExcerptFromTail(ctx, CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1, AppendedMessage: "review this"})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail first returned error: %v", err)
	}
	second, err := store.CreateContextExcerptFromTail(ctx, CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-2", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1, AppendedMessage: "implement this"})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail second returned error: %v", err)
	}
	if first.ContentHash == second.ContentHash {
		t.Fatalf("content hashes both = %q, want appended message to affect packet hash", first.ContentHash)
	}
}

func TestAgentMessageAttachesExcerptAndStartsTargetRunWhenNeeded(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-message")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.AppendRunLogMessage(ctx, RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "plan"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	excerpt, err := store.CreateContextExcerptFromTail(ctx, CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}

	dm, err := store.SendAgentMessage(ctx, AgentMessageSendParams{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "review this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, StartSessionID: "run-2"})
	if err != nil {
		t.Fatalf("SendAgentMessage returned error: %v", err)
	}
	if dm.WorkspaceID != "ws-1" || dm.SourceAgentID != "agent-1" || dm.TargetSessionID != "run-2" || dm.Status != "delivered" {
		t.Fatalf("direct message = %#v, want delivered message with derived source and started target run", dm)
	}
	targetRun, err := store.GetAgentSession(ctx, "run-2")
	if err != nil {
		t.Fatalf("GetAgentSession target returned error: %v", err)
	}
	if targetRun.Status != "waiting" {
		t.Fatalf("target run status = %q, want waiting until a harness is started", targetRun.Status)
	}
	tail, err := store.TailRunLogMessages(ctx, "run-2", 2)
	if err != nil {
		t.Fatalf("TailRunLogMessages target returned error: %v", err)
	}
	if len(tail) != 2 || tail[0].Role != "assistant" || tail[0].Parts[0].Text != "plan" || tail[1].Role != "user" || tail[1].Parts[0].Text != "review this" {
		t.Fatalf("target tail = %#v, want attached excerpt then visible direct message in target run", tail)
	}
	storedExcerpt, err := store.GetContextExcerpt(ctx, excerpt.ContextExcerptID)
	if err != nil {
		t.Fatalf("GetContextExcerpt returned error: %v", err)
	}
	if storedExcerpt.TargetSessionID != "" || storedExcerpt.ContentHash != excerpt.ContentHash {
		t.Fatalf("stored excerpt = %#v, want immutable excerpt not bound to delivery target", storedExcerpt)
	}
}

func TestAgentMessageDeliversExcerptAppendedMessageBeforeBody(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-message-appended-excerpt-message")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.AppendRunLogMessage(ctx, RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "plan"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	excerpt, err := store.CreateContextExcerptFromTail(ctx, CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1, AppendedMessage: "continue from this plan"})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}

	if _, err := store.SendAgentMessage(ctx, AgentMessageSendParams{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "start now", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, StartSessionID: "run-2"}); err != nil {
		t.Fatalf("SendAgentMessage returned error: %v", err)
	}
	tail, err := store.TailRunLogMessages(ctx, "run-2", 3)
	if err != nil {
		t.Fatalf("TailRunLogMessages target returned error: %v", err)
	}
	if len(tail) != 3 || tail[0].Parts[0].Text != "plan" || tail[1].Role != "user" || tail[1].Parts[0].Text != "continue from this plan" || tail[2].Parts[0].Text != "start now" {
		t.Fatalf("target tail = %#v, want excerpt context, appended excerpt message, direct body", tail)
	}
}

func TestAgentMessageCanDeliverSameImmutableExcerptToMultipleRuns(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-message-excerpt-multi-delivery")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.AppendRunLogMessage(ctx, RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "plan"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	excerpt, err := store.CreateContextExcerptFromTail(ctx, CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}

	if _, err := store.SendAgentMessage(ctx, AgentMessageSendParams{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "first", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, StartSessionID: "run-2"}); err != nil {
		t.Fatalf("SendAgentMessage first returned error: %v", err)
	}
	if _, err := store.SendAgentMessage(ctx, AgentMessageSendParams{AgentMessageID: "dm-2", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "second", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, StartSessionID: "run-3"}); err != nil {
		t.Fatalf("SendAgentMessage second returned error: %v", err)
	}
	got, err := store.GetContextExcerpt(ctx, excerpt.ContextExcerptID)
	if err != nil {
		t.Fatalf("GetContextExcerpt returned error: %v", err)
	}
	if got.TargetSessionID != "" || got.ContentHash != excerpt.ContentHash || len(got.Items) != 1 || got.Items[0].CopiedText != "plan" {
		t.Fatalf("excerpt = %#v, want same immutable copied excerpt after multiple deliveries", got)
	}
}

func TestAgentMessageDeliversExcerptAppendedMessageAfterCopiedExcerpt(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-message-excerpt-appended")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.AppendRunLogMessage(ctx, RunLogMessage{MessageID: "msg-1", SessionID: "run-1", Sequence: 1, Role: "assistant", Parts: []RunLogMessagePart{{PartID: "part-1", Sequence: 1, Kind: "text", Text: "plan"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage returned error: %v", err)
	}
	excerpt, err := store.CreateContextExcerptFromTail(ctx, CreateContextExcerptFromTailParams{ContextExcerptID: "excerpt-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Count: 1, AppendedMessage: "use this plan"})
	if err != nil {
		t.Fatalf("CreateContextExcerptFromTail returned error: %v", err)
	}

	if _, err := store.SendAgentMessage(ctx, AgentMessageSendParams{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", Body: "review this", ContextExcerptIDs: []string{excerpt.ContextExcerptID}, StartSessionID: "run-2"}); err != nil {
		t.Fatalf("SendAgentMessage returned error: %v", err)
	}
	tail, err := store.TailRunLogMessages(ctx, "run-2", 3)
	if err != nil {
		t.Fatalf("TailRunLogMessages target returned error: %v", err)
	}
	if len(tail) != 3 || tail[0].Parts[0].Text != "plan" || tail[1].Role != "user" || tail[1].Parts[0].Text != "use this plan" || tail[2].Parts[0].Text != "review this" {
		t.Fatalf("target tail = %#v, want copied excerpt, appended excerpt message, direct body", tail)
	}
}

func TestAgentMessageToExistingRunAppendsAfterCurrentTail(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-message-existing-run")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.CreateAgentSession(ctx, AgentSession{SessionID: "run-2", WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "opencode", Status: "running", Usage: "durable", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateAgentSession target returned error: %v", err)
	}
	if err := store.AppendRunLogMessage(ctx, RunLogMessage{MessageID: "existing-msg", SessionID: "run-2", Sequence: 1, Role: "assistant", Parts: []RunLogMessagePart{{PartID: "existing-part", Sequence: 1, Kind: "text", Text: "ready"}}}); err != nil {
		t.Fatalf("AppendRunLogMessage existing returned error: %v", err)
	}

	dm, err := store.SendAgentMessage(ctx, AgentMessageSendParams{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", TargetSessionID: "run-2", Body: "continue"})
	if err != nil {
		t.Fatalf("SendAgentMessage returned error: %v", err)
	}
	if dm.TargetSessionID != "run-2" || dm.DeliveredSessionID != "run-2" {
		t.Fatalf("direct message = %#v, want delivered to existing run", dm)
	}
	tail, err := store.TailRunLogMessages(ctx, "run-2", 2)
	if err != nil {
		t.Fatalf("TailRunLogMessages target returned error: %v", err)
	}
	if len(tail) != 2 || tail[0].MessageID != "existing-msg" || tail[1].Sequence != 2 || tail[1].Parts[0].Text != "continue" {
		t.Fatalf("target tail = %#v, want direct message appended after existing message", tail)
	}
}

func TestAgentMessageResolvesTargetAgentFromTargetSession(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-message-resolve-target-session")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.CreateAgentSession(ctx, AgentSession{SessionID: "run-2", WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "opencode", Status: "running", Usage: "durable", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateAgentSession target returned error: %v", err)
	}

	dm, err := store.SendAgentMessage(ctx, AgentMessageSendParams{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetSessionID: "run-2", Body: "continue"})
	if err != nil {
		t.Fatalf("SendAgentMessage returned error: %v", err)
	}
	if dm.TargetAgentID != "agent-2" || dm.TargetSessionID != "run-2" || dm.DeliveredSessionID != "run-2" {
		t.Fatalf("direct message = %#v, want target agent resolved from target session", dm)
	}
}

func TestAgentMessageAppendsToExistingTargetRun(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-message-existing-run")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.CreateAgentSession(ctx, AgentSession{SessionID: "run-2", WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "opencode", Status: "running", Usage: "durable", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateAgentSession target returned error: %v", err)
	}
	if _, err := store.SendAgentMessage(ctx, AgentMessageSendParams{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "agent-2", TargetSessionID: "run-2", Body: "first"}); err != nil {
		t.Fatalf("SendAgentMessage first returned error: %v", err)
	}
	if _, err := store.SendAgentMessage(ctx, AgentMessageSendParams{AgentMessageID: "dm-2", SourceSessionID: "run-1", TargetAgentID: "agent-2", TargetSessionID: "run-2", Body: "second"}); err != nil {
		t.Fatalf("SendAgentMessage second returned error: %v", err)
	}
	tail, err := store.TailRunLogMessages(ctx, "run-2", 2)
	if err != nil {
		t.Fatalf("TailRunLogMessages returned error: %v", err)
	}
	if len(tail) != 2 || tail[0].Sequence != 1 || tail[0].Parts[0].Text != "first" || tail[1].Sequence != 2 || tail[1].Parts[0].Text != "second" {
		t.Fatalf("tail = %#v, want two appended direct messages", tail)
	}
}

func TestAgentMessageConcurrentSendsToExistingRunAppendContiguousMessages(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-message-concurrent-existing-run")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.CreateAgentSession(ctx, AgentSession{SessionID: "run-2", WorkspaceID: "ws-1", AgentID: "agent-2", Harness: "opencode", Status: "running", Usage: "durable", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateAgentSession target returned error: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, spec := range []struct{ id, body string }{{"dm-1", "first"}, {"dm-2", "second"}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.SendAgentMessage(ctx, AgentMessageSendParams{AgentMessageID: spec.id, SourceSessionID: "run-1", TargetAgentID: "agent-2", TargetSessionID: "run-2", Body: spec.body})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("SendAgentMessage concurrent returned error: %v", err)
		}
	}
	tail, err := store.TailRunLogMessages(ctx, "run-2", 2)
	if err != nil {
		t.Fatalf("TailRunLogMessages returned error: %v", err)
	}
	if len(tail) != 2 || tail[0].Sequence != 1 || tail[1].Sequence != 2 || tail[0].Parts[0].Text == tail[1].Parts[0].Text {
		t.Fatalf("tail = %#v, want two contiguous delivered messages", tail)
	}
}

func TestAgentMessageRejectsTargetAgentFromDifferentWorkspace(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-message-cross-workspace")
	ctx := context.Background()
	seedAgentSessionConfigSession(t, store, ctx)
	if err := store.CreateSession(ctx, "ws-2", "other workspace", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession ws-2 returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-ws-2", WorkspaceID: "ws-2", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig cross-workspace target returned error: %v", err)
	}

	if _, err := store.SendAgentMessage(ctx, AgentMessageSendParams{AgentMessageID: "dm-1", SourceSessionID: "run-1", TargetAgentID: "agent-ws-2", Body: "wrong workspace", StartSessionID: "run-2"}); err != ErrInvalidInput {
		t.Fatalf("SendAgentMessage error = %v, want ErrInvalidInput", err)
	}
}

func TestAgentSessionConfigCreateDoesNotCreateRun(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-create")
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "workspace", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "planner", Harness: "codex"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig returned error: %v", err)
	}
	runs, err := store.ListAgentSessions(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListAgentSessions returned error: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs = %#v, want creating an agent to create no runs", runs)
	}
}

func TestAgentSessionConfigRosterListGetUpdateDelete(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-session-config-roster")
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "workspace", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "planner", Harness: "codex", Model: "gpt-5", Prompt: "plan"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig returned error: %v", err)
	}

	listed, err := store.ListAgentSessionConfigs(ctx, "ws-1")
	if err != nil {
		t.Fatalf("ListAgentSessionConfigs returned error: %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "planner" {
		t.Fatalf("listed = %#v, want planner", listed)
	}
	got, err := store.GetAgentSessionConfig(ctx, "agent-1")
	if err != nil {
		t.Fatalf("GetAgentSessionConfig returned error: %v", err)
	}
	if got.Prompt != "plan" || got.Model != "gpt-5" {
		t.Fatalf("got = %#v, want persisted prompt/model", got)
	}
	updated, err := store.UpdateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "planner", Harness: "claude", Model: "sonnet", Prompt: "revise"})
	if err != nil {
		t.Fatalf("UpdateAgentSessionConfig returned error: %v", err)
	}
	if updated.Harness != "claude" || updated.Prompt != "revise" {
		t.Fatalf("updated = %#v, want new harness/prompt", updated)
	}
	if err := store.DeleteAgentSessionConfig(ctx, "agent-1"); err != nil {
		t.Fatalf("DeleteAgentSessionConfig returned error: %v", err)
	}
	if _, err := store.GetAgentSessionConfig(ctx, "agent-1"); err != ErrNotFound {
		t.Fatalf("GetAgentSessionConfig after delete error = %v, want ErrNotFound", err)
	}
}

func TestAgentSessionConfigSessionCreatesRunFromAgentDefaults(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-session-config-run")
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "workspace", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "planner", Harness: "codex", Model: "gpt-5", Prompt: "plan"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig returned error: %v", err)
	}
	run, err := store.CreateSessionFromAgentSessionConfig(ctx, "run-1", "agent-1", t.TempDir())
	if err != nil {
		t.Fatalf("CreateSessionFromAgentSessionConfig returned error: %v", err)
	}
	if run.SessionID != "run-1" || run.AgentID != "agent-1" || run.Harness != "codex" || run.Model != "gpt-5" || run.Status != "waiting" {
		t.Fatalf("run = %#v, want run from agent defaults", run)
	}
}

func TestAgentSessionConfigRosterRejectsDuplicateNamesWithinWorkspace(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-session-config-duplicate-name")
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "workspace", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession ws-1 returned error: %v", err)
	}
	if err := store.CreateSession(ctx, "ws-2", "workspace-2", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession ws-2 returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "planner", Harness: "codex"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig first returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "planner", Harness: "claude"}); err == nil {
		t.Fatal("CreateAgentSessionConfig duplicate returned nil error")
	}
	if err := store.CreateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-3", WorkspaceID: "ws-2", Name: "planner", Harness: "claude"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig same name different workspace returned error: %v", err)
	}
}

func TestAgentSessionConfigUpdatePreservesWorkspaceScope(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-session-config-update-scope")
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "workspace", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession ws-1 returned error: %v", err)
	}
	if err := store.CreateSession(ctx, "ws-2", "workspace-2", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession ws-2 returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "planner", Harness: "codex"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig returned error: %v", err)
	}
	if _, err := store.UpdateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-2", Name: "planner", Harness: "claude"}); err != ErrNotFound {
		t.Fatalf("UpdateAgentSessionConfig wrong workspace error = %v, want ErrNotFound", err)
	}
}

func TestAgentSessionConfigCreateRejectsMissingWorkspace(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-create-missing-workspace")
	ctx := context.Background()

	err := store.CreateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-1", WorkspaceID: "missing-workspace", Name: "planner", Harness: "codex"})
	if err != ErrInvalidInput {
		t.Fatalf("CreateAgentSessionConfig error = %v, want ErrInvalidInput", err)
	}
}

func TestCreateAgentSessionRejectsAgentSessionConfigFromDifferentWorkspace(t *testing.T) {
	store := newGlobalDBTestStore(t, "agent-session-cross-workspace")
	ctx := context.Background()
	if err := store.CreateSession(ctx, "ws-1", "workspace one", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession ws-1 returned error: %v", err)
	}
	if err := store.CreateSession(ctx, "ws-2", "workspace two", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession ws-2 returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-ws-2", WorkspaceID: "ws-2", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig returned error: %v", err)
	}

	err := store.CreateAgentSession(ctx, AgentSession{SessionID: "run-1", WorkspaceID: "ws-1", AgentID: "agent-ws-2", Harness: "opencode", Status: "running"})
	if err != ErrInvalidInput {
		t.Fatalf("CreateAgentSession error = %v, want ErrInvalidInput", err)
	}
}

func seedAgentSessionConfigSession(t *testing.T, store *Store, ctx context.Context) {
	t.Helper()
	if err := store.CreateSession(ctx, "ws-1", "workspace", t.TempDir(), "manual", "auto"); err != nil {
		t.Fatalf("CreateSession returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-1", WorkspaceID: "ws-1", Name: "executor", Harness: "codex", Model: "gpt-5", Prompt: "do work"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig agent-1 returned error: %v", err)
	}
	if err := store.CreateAgentSessionConfig(ctx, AgentSessionConfig{AgentID: "agent-2", WorkspaceID: "ws-1", Name: "reviewer", Harness: "opencode"}); err != nil {
		t.Fatalf("CreateAgentSessionConfig agent-2 returned error: %v", err)
	}
	if err := store.CreateAgentSession(ctx, AgentSession{SessionID: "run-1", WorkspaceID: "ws-1", AgentID: "agent-1", Harness: "codex", Model: "gpt-5", Status: "running", Usage: "durable", ProviderSessionID: "thread-1", CWD: t.TempDir()}); err != nil {
		t.Fatalf("CreateAgentSession returned error: %v", err)
	}
}
