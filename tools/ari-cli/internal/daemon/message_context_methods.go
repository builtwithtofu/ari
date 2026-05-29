package daemon

import (
	"context"
	"fmt"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func (d *Daemon) registerMessageContextMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if err := rpc.RegisterMethod(registry, rpc.Method[RunLogMessagesTailRequest, RunLogMessagesTailResponse]{
		Name:        "run.messages.tail",
		Description: "Select the last N normalized messages from a run in deterministic run order",
		Handler: func(ctx context.Context, req RunLogMessagesTailRequest) (RunLogMessagesTailResponse, error) {
			return tailRunLogMessages(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register run.messages.tail: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[RunLogMessagesListRequest, RunLogMessagesListResponse]{
		Name:        "run.messages.list",
		Description: "List normalized run messages after a sequence cursor with a limit",
		Handler: func(ctx context.Context, req RunLogMessagesListRequest) (RunLogMessagesListResponse, error) {
			return listRunLogMessages(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register run.messages.list: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[ContextExcerptCreateFromTailRequest, ContextExcerptResponse]{
		Name:        "context.excerpt.create_from_tail",
		Description: "Create an immutable visible context excerpt from the last N run messages",
		Handler: func(ctx context.Context, req ContextExcerptCreateFromTailRequest) (ContextExcerptResponse, error) {
			return createContextExcerptFromTail(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register context.excerpt.create_from_tail: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[ContextExcerptCreateFromRangeRequest, ContextExcerptResponse]{
		Name:        "context.excerpt.create_from_range",
		Description: "Create an immutable visible context excerpt from an inclusive run message sequence range",
		Handler: func(ctx context.Context, req ContextExcerptCreateFromRangeRequest) (ContextExcerptResponse, error) {
			return createContextExcerptFromRange(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register context.excerpt.create_from_range: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[ContextExcerptCreateFromExplicitIDsRequest, ContextExcerptResponse]{
		Name:        "context.excerpt.create_from_explicit_ids",
		Description: "Create an immutable visible context excerpt from explicit run message IDs",
		Handler: func(ctx context.Context, req ContextExcerptCreateFromExplicitIDsRequest) (ContextExcerptResponse, error) {
			return createContextExcerptFromExplicitIDs(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register context.excerpt.create_from_explicit_ids: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[ContextExcerptGetRequest, ContextExcerptResponse]{
		Name:        "context.excerpt.get",
		Description: "Get an immutable context excerpt and copied message items",
		Handler: func(ctx context.Context, req ContextExcerptGetRequest) (ContextExcerptResponse, error) {
			return getContextExcerpt(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register context.excerpt.get: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentMessageSendRequest, AgentMessageSendResponse]{
		Name:        "session.message.send",
		Description: "Send a visible message between workspace sessions",
		Handler: func(ctx context.Context, req AgentMessageSendRequest) (AgentMessageSendResponse, error) {
			return sendAgentMessage(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register session.message.send: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentMessageSendRequest, AgentMessageSendResponse]{
		Name:        "session.fanout",
		Description: "Fan out a visible session message to multiple sessions or profiles",
		Handler: func(ctx context.Context, req AgentMessageSendRequest) (AgentMessageSendResponse, error) {
			return d.fanoutSession(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register session.fanout: %w", err)
	}
	return nil
}
