package daemon

import (
	"context"
	"fmt"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func (d *Daemon) registerProfileSessionMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if err := rpc.RegisterMethod(registry, rpc.Method[HarnessSessionStartRequest, HarnessSessionStartResponse]{
		Name:        "session.start",
		Description: "Start a sticky session from a named Ari profile",
		Handler: func(ctx context.Context, req HarnessSessionStartRequest) (HarnessSessionStartResponse, error) {
			if agentSessionStartUsesProfile(req) {
				return startProfileSession(d, ctx, store, req)
			}
			return d.startHarnessSession(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register session.start: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[ProfileCreateRequest, ProfileResponse]{
		Name:        "profile.create",
		Description: "Create or update a durable Ari profile",
		Handler: func(ctx context.Context, req ProfileCreateRequest) (ProfileResponse, error) {
			return createStoredProfile(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register profile.create: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[ProfileGetRequest, ProfileResponse]{
		Name:        "profile.get",
		Description: "Get a durable Ari profile by name",
		Handler: func(ctx context.Context, req ProfileGetRequest) (ProfileResponse, error) {
			return getStoredProfile(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register profile.get: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[ProfileListRequest, ProfileListResponse]{
		Name:        "profile.list",
		Description: "List durable Ari profiles",
		Handler: func(ctx context.Context, req ProfileListRequest) (ProfileListResponse, error) {
			return listStoredProfiles(ctx, store, req)
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
	if err := rpc.RegisterMethod(registry, rpc.Method[SessionLogsRequest, SessionLogsResponse]{
		Name:        "session.logs",
		Description: "Fetch native harness session logs when supported",
		Handler: func(ctx context.Context, req SessionLogsRequest) (SessionLogsResponse, error) {
			return claudeSessionLogs(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register session.logs: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[SessionAttachRequest, SessionAttachResponse]{
		Name:        "session.attach",
		Description: "Return the native harness attach command when supported",
		Handler: func(ctx context.Context, req SessionAttachRequest) (SessionAttachResponse, error) {
			return claudeSessionAttach(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register session.attach: %w", err)
	}
	return nil
}
