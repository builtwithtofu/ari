package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

func TestRecordDaemonOperationAppendsSucceededAndFailedSemanticRecords(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()

	succeeded, err := recordDaemonOperation(ctx, store, globalOperationOptions("workspace_created", "create workspace alpha"), func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("recordDaemonOperation succeeded returned error: %v", err)
	}
	if succeeded.OperationType != "workspace_created" || succeeded.Source != daemonOperationSourceCLI || succeeded.Result != daemonOperationResultSucceeded {
		t.Fatalf("succeeded record = %#v, want semantic cli success", succeeded)
	}

	mutationErr := errors.New("disk full")
	failed, err := recordDaemonOperation(ctx, store, globalOperationOptions("init_applied", "apply Ari init choices"), func(context.Context) error {
		return mutationErr
	})
	if !errors.Is(err, mutationErr) {
		t.Fatalf("recordDaemonOperation failed error = %v, want mutation error", err)
	}
	if failed.OperationType != "init_applied" || failed.Source != daemonOperationSourceCLI || failed.Result != "failed: disk full" {
		t.Fatalf("failed record = %#v, want semantic cli failure", failed)
	}
	if strings.Contains(failed.OperationType, "daemon.") || strings.Contains(failed.OperationType, "rpc") {
		t.Fatalf("operation type %q exposes raw daemon/RPC naming", failed.OperationType)
	}
}

func TestDaemonOperationCheckpointCanGroupVisibleChildOperations(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()

	checkpoint, err := createDaemonOperationCheckpoint(ctx, store, daemonOperationCheckpointOptions{Actor: "user", Source: daemonOperationSourceTool, Scope: globaldb.OperationScopeGlobal, RequestSummary: "create workspace with defaults", PayloadSnapshot: map[string]string{"steps": "2"}})
	if err != nil {
		t.Fatalf("createDaemonOperationCheckpoint returned error: %v", err)
	}

	first, err := recordDaemonOperation(ctx, store, childOptions(checkpoint.OperationID, "workspace_created", "create workspace"), func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("record first child returned error: %v", err)
	}
	second, err := recordDaemonOperation(ctx, store, childOptions(checkpoint.OperationID, "profile_configured", "configure default helper profile"), func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("record second child returned error: %v", err)
	}

	if first.RollbackPointID != checkpoint.OperationID || second.RollbackPointID != checkpoint.OperationID || first.CheckpointOperationID != checkpoint.OperationID || second.CheckpointOperationID != checkpoint.OperationID {
		t.Fatalf("children rollback links = (%#v, %#v), want shared checkpoint %q", first, second, checkpoint.OperationID)
	}
}

func TestDaemonOperationSnapshotsKeepOnlySmallSummaries(t *testing.T) {
	store := newCommandMethodTestStore(t)
	ctx := context.Background()
	rawSecret := "token=super-secret-value"

	record, err := recordDaemonOperation(ctx, store, globalOperationOptions("workspace_created", "create workspace alpha"), func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("recordDaemonOperation returned error: %v", err)
	}
	if strings.Contains(record.RequestSummary, rawSecret) || strings.Contains(record.PayloadSnapshotJSON, rawSecret) || strings.Contains(record.RollbackDataJSON, rawSecret) {
		t.Fatalf("record copied sensitive raw payload: %#v", record)
	}
	if record.PayloadSnapshotJSON != `{"name":"alpha","operation_kind":"mutating","request_sha256":"f97a346b","trust_choice_scope":"operation_type"}` {
		t.Fatalf("payload snapshot = %s, want minimal summary", record.PayloadSnapshotJSON)
	}
	if !strings.HasPrefix(record.PayloadHash, "sha256:") || strings.Contains(record.PayloadHash, rawSecret) {
		t.Fatalf("payload hash = %q, want hash without raw secret", record.PayloadHash)
	}
}

func TestDaemonOperationRecordsDistinguishReadOnlyAndMutatingKinds(t *testing.T) {
	mutatingJSON, _, err := operationPayloadSnapshot(daemonOperationKindMutating, map[string]string{"operation_type": "workspace_created"})
	if err != nil {
		t.Fatalf("operationPayloadSnapshot mutating returned error: %v", err)
	}
	readOnlyJSON, _, err := operationPayloadSnapshot(daemonOperationKindReadOnly, map[string]string{"operation_type": "self_check"})
	if err != nil {
		t.Fatalf("operationPayloadSnapshot read-only returned error: %v", err)
	}
	if !strings.Contains(mutatingJSON, `"operation_kind":"mutating"`) || !strings.Contains(readOnlyJSON, `"operation_kind":"read_only"`) {
		t.Fatalf("operation kind metadata missing: mutating=%s read_only=%s", mutatingJSON, readOnlyJSON)
	}
	if !strings.Contains(mutatingJSON, `"trust_choice_scope":"operation_type"`) || !strings.Contains(readOnlyJSON, `"trust_choice_scope":"operation_type"`) {
		t.Fatalf("trust choice scope metadata missing: mutating=%s read_only=%s", mutatingJSON, readOnlyJSON)
	}
}

func globalOperationOptions(operationType, summary string) daemonOperationRecordOptions {
	return daemonOperationRecordOptions{OperationType: operationType, OperationKind: daemonOperationKindMutating, Actor: "user", Source: daemonOperationSource(daemonOperationSourceCLI), Scope: globaldb.OperationScopeGlobal, RequestSummary: summary, RollbackData: map[string]string{"undo": "not_supported_yet"}, PayloadSnapshot: map[string]string{"name": "alpha", "request_sha256": "f97a346b"}}
}

func childOptions(checkpointID, operationType, summary string) daemonOperationRecordOptions {
	opts := globalOperationOptions(operationType, summary)
	opts.Source = daemonOperationSourceTool
	opts.ParentOperationID = checkpointID
	opts.CheckpointOperationID = checkpointID
	opts.RollbackPointID = checkpointID
	return opts
}
