package daemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

const (
	daemonOperationResultSucceeded = "succeeded"

	daemonOperationSourceCLI    = "cli"
	daemonOperationSourceDaemon = "daemon"
	daemonOperationSourceTool   = "tool"
	daemonOperationSourceRPC    = "rpc"

	daemonOperationTypeRollbackCheckpoint = "rollback_checkpoint"

	daemonOperationKindReadOnly = "read_only"
	daemonOperationKindMutating = "mutating"
)

type daemonOperationRecordOptions struct {
	WorkspaceID           string
	OperationType         string
	OperationKind         string
	Actor                 string
	Source                string
	Scope                 string
	RequestSummary        string
	TrustDecision         string
	ParentOperationID     string
	CheckpointOperationID string
	RollbackPointID       string
	RollbackData          map[string]string
	PayloadSnapshot       map[string]string
}

type daemonOperationCheckpointOptions struct {
	WorkspaceID     string
	Actor           string
	Source          string
	Scope           string
	RequestSummary  string
	PayloadSnapshot map[string]string
}

func recordDaemonOperation(ctx context.Context, store *globaldb.Store, opts daemonOperationRecordOptions, fn func(context.Context) error) (globaldb.OperationRecord, error) {
	err := fn(ctx)
	recordErr := err
	result := daemonOperationResultSucceeded
	if err != nil {
		result = "failed: " + err.Error()
	}

	record, appendErr := appendDaemonOperationRecord(ctx, store, opts, result)
	if appendErr != nil {
		if recordErr != nil {
			return globaldb.OperationRecord{}, fmt.Errorf("%w; append operation record: %v", recordErr, appendErr)
		}
		return globaldb.OperationRecord{}, appendErr
	}
	return record, err
}

func createDaemonOperationCheckpoint(ctx context.Context, store *globaldb.Store, opts daemonOperationCheckpointOptions) (globaldb.OperationRecord, error) {
	return appendDaemonOperationRecord(ctx, store, daemonOperationRecordOptions{WorkspaceID: opts.WorkspaceID, OperationType: daemonOperationTypeRollbackCheckpoint, OperationKind: daemonOperationKindReadOnly, Actor: opts.Actor, Source: opts.Source, Scope: opts.Scope, RequestSummary: opts.RequestSummary, PayloadSnapshot: opts.PayloadSnapshot}, daemonOperationResultSucceeded)
}

func appendDaemonOperationRecord(ctx context.Context, store *globaldb.Store, opts daemonOperationRecordOptions, result string) (globaldb.OperationRecord, error) {
	if store == nil {
		return globaldb.OperationRecord{}, fmt.Errorf("globaldb store is required")
	}
	snapshotJSON, payloadHash, err := operationPayloadSnapshot(opts.OperationKind, opts.PayloadSnapshot)
	if err != nil {
		return globaldb.OperationRecord{}, err
	}
	rollbackJSON, err := compactJSONObject(opts.RollbackData)
	if err != nil {
		return globaldb.OperationRecord{}, err
	}
	record := globaldb.OperationRecord{OperationID: newDaemonOperationID(), WorkspaceID: opts.WorkspaceID, OperationType: opts.OperationType, Actor: opts.Actor, Source: opts.Source, Scope: opts.Scope, RequestSummary: opts.RequestSummary, Result: result, TrustDecision: opts.TrustDecision, ParentOperationID: opts.ParentOperationID, CheckpointOperationID: opts.CheckpointOperationID, RollbackPointID: opts.RollbackPointID, RollbackDataJSON: rollbackJSON, PayloadHash: payloadHash, PayloadSnapshotJSON: snapshotJSON}
	event, emitEvent := operationWorkspaceEvent(record)
	if !emitEvent {
		return store.AppendOperationRecord(ctx, record)
	}
	return store.AppendOperationRecordWithWorkspaceEvent(ctx, record, event)
}

func operationWorkspaceEvent(record globaldb.OperationRecord) (globaldb.WorkspaceEvent, bool) {
	operationType := strings.TrimSpace(record.OperationType)
	if operationType == daemonOperationTypeRollbackCheckpoint {
		return globaldb.WorkspaceEvent{}, false
	}
	workspaceID := strings.TrimSpace(record.WorkspaceID)
	if workspaceID == "" {
		workspaceID = operationRecordHomeWorkspaceID(record)
	}
	if workspaceID == "" {
		return globaldb.WorkspaceEvent{}, false
	}
	payload := map[string]string{
		"operation_id":    strings.TrimSpace(record.OperationID),
		"operation_type":  operationType,
		"source":          strings.TrimSpace(record.Source),
		"scope":           strings.TrimSpace(record.Scope),
		"result":          operationRecordStatus(record),
		"request_summary": strings.TrimSpace(record.RequestSummary),
	}
	if rollbackPointID := strings.TrimSpace(record.RollbackPointID); rollbackPointID != "" {
		payload["rollback_point_id"] = rollbackPointID
	}
	operationID := strings.TrimSpace(record.OperationID)
	return globaldb.WorkspaceEvent{WorkspaceID: workspaceID, EventType: "operation." + operationType, SubjectType: "operation", SubjectID: operationID, ProducerType: workspaceEventProducerDaemon, ProducerID: daemonOperationSource(record.Source), CorrelationID: operationID, PayloadJSON: daemonEventPayload(payload), PayloadRefJSON: daemonEventPayload(map[string]string{"kind": "operation_record", "id": operationID}), AttentionRequired: strings.HasPrefix(record.Result, "failed:")}, true
}

func operationPayloadSnapshot(operationKind string, snapshot map[string]string) (string, string, error) {
	snapshot = operationSnapshotWithTrustMetadata(operationKind, snapshot)
	snapshotJSON, err := compactJSONObject(snapshot)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(snapshotJSON))
	return snapshotJSON, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func operationSnapshotWithTrustMetadata(operationKind string, snapshot map[string]string) map[string]string {
	metadata := make(map[string]string, len(snapshot)+2)
	for key, value := range snapshot {
		metadata[key] = value
	}
	if _, ok := metadata["operation_kind"]; !ok {
		metadata["operation_kind"] = daemonOperationKind(operationKind)
	}
	if _, ok := metadata["trust_choice_scope"]; !ok {
		metadata["trust_choice_scope"] = "operation_type"
	}
	return metadata
}

func compactJSONObject(values map[string]string) (string, error) {
	if len(values) == 0 {
		return "{}", nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func newDaemonOperationID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return "op_" + hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("op_%d", time.Now().UnixNano())
}

func daemonOperationSource(source string) string {
	source = strings.TrimSpace(source)
	switch source {
	case daemonOperationSourceCLI, daemonOperationSourceDaemon, daemonOperationSourceTool, daemonOperationSourceRPC:
		return source
	default:
		return daemonOperationSourceDaemon
	}
}

func daemonOperationKind(kind string) string {
	kind = strings.TrimSpace(kind)
	switch kind {
	case daemonOperationKindReadOnly, daemonOperationKindMutating:
		return kind
	default:
		return daemonOperationKindMutating
	}
}
