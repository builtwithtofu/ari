package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

const daemonOperationTypeRollbackApplied = "rollback_applied"

type RollbackApplyRequest struct {
	RollbackPointID string `json:"rollback_point_id"`
}

type RollbackApplyResponse struct {
	Status              string `json:"status"`
	RollbackOperationID string `json:"rollback_operation_id"`
	TargetOperationID   string `json:"target_operation_id"`
}

func (d *Daemon) registerRollbackMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if err := rpc.RegisterMethod(registry, rpc.Method[RollbackApplyRequest, RollbackApplyResponse]{
		Name:        "rollback.apply",
		Description: "Apply an Ari-owned rollback point",
		Handler: func(ctx context.Context, req RollbackApplyRequest) (RollbackApplyResponse, error) {
			return d.applyRollback(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register rollback.apply: %w", err)
	}
	return nil
}

func (d *Daemon) applyRollback(ctx context.Context, store *globaldb.Store, req RollbackApplyRequest) (RollbackApplyResponse, error) {
	rollbackPointID := strings.TrimSpace(req.RollbackPointID)
	if rollbackPointID == "" {
		return RollbackApplyResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "rollback_point_id is required", nil)
	}
	target, err := findRollbackTarget(ctx, store, rollbackPointID)
	if err != nil {
		return RollbackApplyResponse{}, err
	}
	if target.OperationType != daemonOperationTypeInitApplied && target.OperationType != "workspace_project_setup" {
		return RollbackApplyResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "rollback point is not supported yet", map[string]any{"operation_type": target.OperationType})
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(target.PayloadSnapshotJSON), &payload); err != nil {
		return RollbackApplyResponse{}, err
	}
	var response RollbackApplyResponse
	summary := "rollback Ari init choices"
	if target.OperationType == "workspace_project_setup" {
		summary = "rollback project workspace setup"
	}
	record, err := recordDaemonOperation(ctx, store, daemonOperationRecordOptions{OperationType: daemonOperationTypeRollbackApplied, OperationKind: daemonOperationKindMutating, Actor: "user", Source: daemonOperationSourceDaemon, Scope: globaldb.OperationScopeGlobal, RequestSummary: summary, ParentOperationID: target.OperationID, CheckpointOperationID: rollbackPointID, RollbackPointID: rollbackPointID, RollbackData: map[string]string{"scope": "ari_owned_state_only"}, PayloadSnapshot: map[string]string{"target_operation_id": target.OperationID, "rollback_point_id": rollbackPointID, "target_operation_type": target.OperationType, "target_workspace_id": target.WorkspaceID}}, func(ctx context.Context) error {
		if target.OperationType == daemonOperationTypeInitApplied {
			if err := d.rollbackInitState(ctx, store, payload); err != nil {
				return err
			}
		} else if err := d.rollbackProjectWorkspaceSetup(ctx, store, payload); err != nil {
			return err
		}
		response = RollbackApplyResponse{Status: daemonOperationResultSucceeded, TargetOperationID: target.OperationID}
		return nil
	})
	if err != nil {
		return RollbackApplyResponse{}, err
	}
	response.RollbackOperationID = record.OperationID
	return response, nil
}

func (d *Daemon) rollbackProjectWorkspaceSetup(ctx context.Context, store *globaldb.Store, payload map[string]string) error {
	workspaceID := strings.TrimSpace(payload["workspace_id"])
	previousWorkspaceID := strings.TrimSpace(payload["previous_workspace_id"])
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if session.ID != workspaceID {
			continue
		}
		current, err := readActiveWorkspaceContext(ctx, store)
		if err != nil {
			return err
		}
		if current.WorkspaceID == session.ID {
			if previousWorkspaceID != "" {
				if _, err := setActiveWorkspaceContext(ctx, store, ContextSetRequest{WorkspaceID: previousWorkspaceID}); err != nil {
					return err
				}
				if err := patchJSONConfigStrings(d.configPath, map[string]string{"active_workspace": previousWorkspaceID}); err != nil {
					return err
				}
			} else {
				if err := store.SetMeta(ctx, activeContextMetaKey, `{}`); err != nil {
					return err
				}
				if err := patchJSONConfigStrings(d.configPath, map[string]string{"active_workspace": ""}); err != nil {
					return err
				}
			}
		}
		return store.DeleteSession(ctx, session.ID)
	}
	return nil
}

func findRollbackTarget(ctx context.Context, store *globaldb.Store, rollbackPointID string) (globaldb.OperationRecord, error) {
	records, err := store.ListOperationRecords(ctx, "")
	if err != nil {
		return globaldb.OperationRecord{}, err
	}
	for _, record := range records {
		if record.RollbackPointID != rollbackPointID || record.OperationType == daemonOperationTypeRollbackCheckpoint {
			continue
		}
		if record.OperationType == daemonOperationTypeRollbackApplied && record.Result == daemonOperationResultSucceeded {
			return globaldb.OperationRecord{}, rpc.NewHandlerError(rpc.InvalidParams, "rollback point has already been applied", map[string]any{"rollback_point_id": rollbackPointID, "rollback_operation_id": record.OperationID})
		}
		if record.OperationType == daemonOperationTypeRollbackApplied {
			continue
		}
		return record, nil
	}
	return globaldb.OperationRecord{}, rpc.NewHandlerError(rpc.InvalidParams, "rollback point target not found", map[string]any{"rollback_point_id": rollbackPointID})
}

func (d *Daemon) rollbackInitState(ctx context.Context, store *globaldb.Store, payload map[string]string) error {
	previousWorkspaceID := strings.TrimSpace(payload["previous_workspace_id"])
	if err := patchJSONConfigStrings(d.configPath, map[string]string{"default_harness": payload["previous_default_harness"], "preferred_model": payload["previous_preferred_model"], "default_workspace_root": payload["previous_default_workspace_root"]}); err != nil {
		return err
	}
	root := strings.TrimSpace(payload["root"])
	homeWorkspaceID := strings.TrimSpace(payload["home_workspace_id"])
	homeCreated := strings.TrimSpace(payload["home_workspace_created"]) == "true"
	if root == "" || !homeCreated || homeWorkspaceID == "" {
		return nil
	}
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if session.ID != homeWorkspaceID || session.OriginRoot != root {
			continue
		}
		helperSessions, err := store.ListAgentSessions(ctx, session.ID)
		if err != nil {
			return err
		}
		for _, helperSession := range helperSessions {
			if helperSession.Status == "running" {
				if err := store.UpdateAgentSessionStatus(ctx, helperSession.SessionID, "stopped"); err != nil {
					return err
				}
			}
		}
		current, err := readActiveWorkspaceContext(ctx, store)
		if err != nil {
			return err
		}
		if current.WorkspaceID == session.ID {
			if previousWorkspaceID != "" {
				if _, err := setActiveWorkspaceContext(ctx, store, ContextSetRequest{WorkspaceID: previousWorkspaceID}); err != nil {
					return err
				}
				if err := patchJSONConfigStrings(d.configPath, map[string]string{"active_workspace": previousWorkspaceID}); err != nil {
					return err
				}
			} else if err := store.SetMeta(ctx, activeContextMetaKey, `{}`); err != nil {
				return err
			} else if err := patchJSONConfigStrings(d.configPath, map[string]string{"active_workspace": ""}); err != nil {
				return err
			}
		}
		if err := store.DeleteSession(ctx, session.ID); err != nil {
			return err
		}
	}
	return nil
}
