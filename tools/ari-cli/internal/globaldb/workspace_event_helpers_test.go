package globaldb

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFanoutWorkerWorkspaceEventContractRoundTrips(t *testing.T) {
	createdAt := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	event := NewFanoutWorkerWorkspaceEvent(FanoutWorkerWorkspaceEventParams{
		WorkspaceID:           " ws-1 ",
		EventType:             WorkspaceEventWorkerCompleted,
		WorkerSessionID:       " worker-1 ",
		ProducerID:            " worker-1 ",
		CausationID:           " reply-1 ",
		FinalResponseID:       " fr-1 ",
		AttentionRequired:     true,
		FanoutGroupID:         " fg-1 ",
		FanoutMemberID:        " fm-1 ",
		SourceSessionID:       " run-1 ",
		SourceAgentID:         " agent-1 ",
		TargetProfileID:       " profile-2 ",
		RequestAgentMessageID: " request-1 ",
	})
	event.EventID = "we-1"
	event.CreatedAt = createdAt

	if event.WorkspaceID != "ws-1" || event.SubjectType != WorkspaceEventSubjectHarnessSession || event.SubjectID != "worker-1" || event.ProducerType != WorkspaceEventProducerSession || event.CorrelationID != "fg-1" || event.CausationID != "reply-1" || !event.AttentionRequired {
		t.Fatalf("fanout worker event = %#v", event)
	}
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	if payload["status"] != "completed" || payload["fanout_member_id"] != "fm-1" || payload["reply_agent_message_id"] != "reply-1" || payload["request_agent_message_id"] != "request-1" {
		t.Fatalf("fanout worker payload = %#v", payload)
	}
	if FinalResponseIDFromWorkspaceEventRef(event.PayloadRefJSON) != "fr-1" {
		t.Fatalf("payload ref = %s, want final_response fr-1", event.PayloadRefJSON)
	}

	decoded, ok, err := DecodeFanoutWorkerWorkspaceEvent(event)
	if err != nil || !ok {
		t.Fatalf("DecodeFanoutWorkerWorkspaceEvent ok=%v err=%v", ok, err)
	}
	if decoded.FanoutMemberID != "fm-1" || decoded.FanoutGroupID != "fg-1" || decoded.WorkerSessionID != "worker-1" || decoded.RequestAgentMessageID != "request-1" || decoded.ReplyAgentMessageID != "reply-1" || decoded.FinalResponseID != "fr-1" || !decoded.CreatedAt.Equal(createdAt) {
		t.Fatalf("decoded fanout worker event = %#v", decoded)
	}
}

func TestHarnessRuntimeWorkspaceEventContract(t *testing.T) {
	createdAt := time.Date(2026, 6, 26, 12, 1, 0, 0, time.UTC)
	event, err := NewHarnessRuntimeWorkspaceEvent(HarnessRuntimeWorkspaceEventParams{EventID: "hre-1", WorkspaceID: "ws-1", SessionID: "run-1", RootSessionID: "root-1", Kind: "error", Sequence: 7, Payload: json.RawMessage(`{"message":"boom"}`), RunID: "provider-run", ProviderKind: "provider-error", CreatedAt: createdAt})
	if err != nil {
		t.Fatalf("NewHarnessRuntimeWorkspaceEvent returned error: %v", err)
	}
	if event.EventType != WorkspaceEventHarnessError || event.SubjectType != WorkspaceEventSubjectHarnessSession || event.ProducerType != WorkspaceEventProducerSession || event.CorrelationID != "root-1" || !event.AttentionRequired || !event.CreatedAt.Equal(createdAt) {
		t.Fatalf("harness runtime event = %#v", event)
	}
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	if payload["harness_event_id"] != "hre-1" || payload["kind"] != "error" || payload["sequence"] != "7" || payload["run_id"] != "provider-run" || payload["provider_kind"] != "provider-error" {
		t.Fatalf("harness runtime payload = %#v", payload)
	}
	ref := WorkspaceEventStringPayload(event.PayloadRefJSON)
	if ref["kind"] != WorkspaceEventPayloadRefHarnessRuntimeEvent || ref["id"] != "hre-1" || ref["sequence"] != "7" {
		t.Fatalf("harness runtime ref = %#v", ref)
	}

	if _, err := NewHarnessRuntimeWorkspaceEvent(HarnessRuntimeWorkspaceEventParams{EventID: "bad", Kind: "debug", Payload: json.RawMessage(`{bad`)}); err == nil {
		t.Fatal("NewHarnessRuntimeWorkspaceEvent with invalid payload returned nil error")
	}
}

func TestOperationWorkspaceEventContract(t *testing.T) {
	event := NewOperationWorkspaceEvent(OperationWorkspaceEventParams{WorkspaceID: "ws-1", OperationID: "op-1", OperationType: "workspace_project_setup", Source: "cli", Scope: OperationScopeWorkspace, Result: "succeeded", RequestSummary: "set up project", RollbackPointID: "rb-1"})
	if event.EventType != OperationWorkspaceEventType("workspace_project_setup") || event.SubjectType != WorkspaceEventSubjectOperation || event.SubjectID != "op-1" || event.ProducerType != WorkspaceEventProducerDaemon || event.ProducerID != "cli" || event.CorrelationID != "op-1" {
		t.Fatalf("operation event = %#v", event)
	}
	payload := WorkspaceEventStringPayload(event.PayloadJSON)
	if payload["operation_id"] != "op-1" || payload["operation_type"] != "workspace_project_setup" || payload["scope"] != OperationScopeWorkspace || payload["rollback_point_id"] != "rb-1" {
		t.Fatalf("operation payload = %#v", payload)
	}
	ref := WorkspaceEventStringPayload(event.PayloadRefJSON)
	if ref["kind"] != WorkspaceEventPayloadRefOperationRecord || ref["id"] != "op-1" {
		t.Fatalf("operation ref = %#v", ref)
	}
	decoded, ok := DecodeOperationWorkspaceEvent(event)
	if !ok || decoded.OperationType != "workspace_project_setup" || decoded.Source != "cli" || decoded.Scope != OperationScopeWorkspace || decoded.Result != "succeeded" || decoded.RollbackPointID != "rb-1" {
		t.Fatalf("decoded operation event ok=%v decoded=%#v", ok, decoded)
	}
}

func TestHarnessSessionWorkspaceEventContractRoundTrips(t *testing.T) {
	event := NewHarnessSessionWorkspaceEvent(HarnessSessionWorkspaceEventParams{WorkspaceID: "ws-1", SessionID: "run-1", Harness: "codex", TaskID: "task-1", EventType: WorkspaceEventSessionCompleted, Status: "completed", FinalResponseID: "fr-1", AttentionRequired: true})
	if event.EventType != WorkspaceEventSessionCompleted || event.SubjectType != WorkspaceEventSubjectHarnessSession || event.SubjectID != "run-1" || event.ProducerType != WorkspaceEventProducerDaemon || event.ProducerID != WorkspaceEventProducerHarnessLifecycle || event.CorrelationID != "run-1" || !event.AttentionRequired {
		t.Fatalf("harness session event = %#v", event)
	}
	decoded, ok := DecodeHarnessSessionWorkspaceEvent(event)
	if !ok || decoded.SessionID != "run-1" || decoded.Harness != "codex" || decoded.Status != "completed" || decoded.FinalResponseID != "fr-1" {
		t.Fatalf("decoded harness session event ok=%v decoded=%#v", ok, decoded)
	}
}

func TestCommandWorkspaceEventContractRoundTrips(t *testing.T) {
	failingExit := 2
	failed := NewCommandWorkspaceEvent(CommandWorkspaceEventParams{WorkspaceID: "ws-1", CommandID: "cmd-1", Command: "go", Args: `["test","./..."]`, Status: "exited", ExitCode: &failingExit})
	if failed.EventType != WorkspaceEventCommandFailed || failed.SubjectType != WorkspaceEventSubjectCommand || failed.ProducerType != WorkspaceEventProducerDaemon || failed.ProducerID != WorkspaceEventProducerCommand || !failed.AttentionRequired {
		t.Fatalf("failed command event = %#v", failed)
	}
	decoded, ok := DecodeCommandWorkspaceEvent(failed)
	if !ok || decoded.Command != "go" || decoded.Status != "exited" {
		t.Fatalf("decoded command event ok=%v decoded=%#v", ok, decoded)
	}

	success := NewCommandWorkspaceEvent(CommandWorkspaceEventParams{WorkspaceID: "ws-1", CommandID: "cmd-2", Command: "true", Status: "exited"})
	if success.EventType != WorkspaceEventCommandCompleted || success.AttentionRequired {
		t.Fatalf("successful command event = %#v", success)
	}
}

func TestDeliveryWorkspaceEventContractRoundTrips(t *testing.T) {
	nextAttempt := time.Date(2026, 6, 26, 12, 2, 0, 0, time.UTC)
	event := NewDeliveryWorkspaceEvent(DeliveryWorkspaceEventParams{WorkspaceID: "ws-1", DeliveryID: "pd-1", SubscriptionID: "sub-1", TargetType: WorkspaceEventSubjectHarnessSession, TargetID: "run-1", EventIDs: []string{"we-1", "we-2"}, EventType: WorkspaceEventDeliveryFailed, Status: "failed", Attempts: 3, LastError: "boom", NextAttemptAt: &nextAttempt})
	if event.EventType != WorkspaceEventDeliveryFailed || event.SubjectType != WorkspaceEventSubjectPendingDelivery || event.SubjectID != "pd-1" || event.ProducerType != WorkspaceEventProducerDaemon || event.ProducerID != WorkspaceEventProducerWorkspaceDelivery || event.CorrelationID != "sub-1" || event.CausationID != "we-1" || !event.AttentionRequired {
		t.Fatalf("delivery event = %#v", event)
	}
	decoded, ok := DecodeDeliveryWorkspaceEvent(event)
	if !ok || decoded.DeliveryID != "pd-1" || decoded.SubscriptionID != "sub-1" || decoded.TargetType != WorkspaceEventSubjectHarnessSession || decoded.TargetID != "run-1" || decoded.Status != "failed" || decoded.LastError != "boom" {
		t.Fatalf("decoded delivery event ok=%v decoded=%#v", ok, decoded)
	}
}

func TestSignalWorkspaceEventContractRoundTrips(t *testing.T) {
	event := NewSignalWorkspaceEvent(SignalWorkspaceEventParams{EventID: "sig-1", WorkspaceID: "ws-1", TargetType: WorkspaceEventSubjectHarnessSession, TargetID: "run-1", ProducerType: WorkspaceEventProducerSession, ProducerID: "owner-1", CorrelationID: "fg-1", PayloadJSON: `{"source_session_id":"owner-1","target_session_id":"run-1","action":"continue"}`})
	if event.EventType != WorkspaceEventSignalSent || event.SubjectType != WorkspaceEventSubjectHarnessSession || event.SubjectID != "run-1" || event.ProducerType != WorkspaceEventProducerSession || event.ProducerID != "owner-1" || event.CorrelationID != "fg-1" || !event.AttentionRequired {
		t.Fatalf("signal event = %#v", event)
	}
	decoded, ok := DecodeSignalWorkspaceEvent(event)
	if !ok || decoded.SourceSessionID != "owner-1" || decoded.TargetSessionID != "run-1" || decoded.Action != "continue" {
		t.Fatalf("decoded signal event ok=%v decoded=%#v", ok, decoded)
	}
}
