package daemon

import (
	"context"
	"fmt"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func (d *Daemon) registerProfileSessionMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentSessionStartRequest, AgentSessionStartResponse]{
		Name:        "session.start",
		Description: "Start a sticky session from a named Ari profile",
		Handler: func(ctx context.Context, req AgentSessionStartRequest) (AgentSessionStartResponse, error) {
			if agentSessionStartUsesProfile(req) {
				return startProfileSession(d, ctx, store, req)
			}
			return d.startAgentSession(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register session.start: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentProfileCreateRequest, AgentProfileResponse]{
		Name:        "profile.create",
		Description: "Create or update a durable Ari profile",
		Handler: func(ctx context.Context, req AgentProfileCreateRequest) (AgentProfileResponse, error) {
			return createStoredAgentProfile(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register profile.create: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentProfileGetRequest, AgentProfileResponse]{
		Name:        "profile.get",
		Description: "Get a durable Ari profile by name",
		Handler: func(ctx context.Context, req AgentProfileGetRequest) (AgentProfileResponse, error) {
			return getStoredAgentProfile(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register profile.get: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentProfileListRequest, AgentProfileListResponse]{
		Name:        "profile.list",
		Description: "List durable Ari profiles",
		Handler: func(ctx context.Context, req AgentProfileListRequest) (AgentProfileListResponse, error) {
			return listStoredAgentProfiles(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register profile.list: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[SessionGetRequest, SessionGetResponse]{
		Name:        "session.get",
		Description: "Get a durable workspace session by id",
		Handler: func(ctx context.Context, req SessionGetRequest) (SessionGetResponse, error) {
			session, err := getWorkspaceSession(ctx, store, req)
			if err != nil {
				return SessionGetResponse{}, err
			}
			return SessionGetResponse{Session: session}, nil
		},
	}); err != nil {
		return fmt.Errorf("register session.get: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[SessionListRequest, SessionListResponse]{
		Name:        "session.list",
		Description: "List durable workspace sessions",
		Handler: func(ctx context.Context, req SessionListRequest) (SessionListResponse, error) {
			sessions, err := listWorkspaceSessions(ctx, store, req)
			if err != nil {
				return SessionListResponse{}, err
			}
			return SessionListResponse{Sessions: sessions}, nil
		},
	}); err != nil {
		return fmt.Errorf("register session.list: %w", err)
	}
	return nil
}
