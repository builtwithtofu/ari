package globaldb

import (
	"context"
	"testing"
)

// Secret grants leave an inspectable, redacted audit trail in the dedicated
// daemon-global table (not workspace event history — grants have no
// workspace identity).
func TestSecretAuditEventsRecordGrantLifecycle(t *testing.T) {
	store := newGlobalDBTestStore(t, "secret-audit-events")
	ctx := context.Background()
	value := []byte("ari-audit-secret-sentinel")
	metadata, err := store.UpsertSecretMetadata(ctx, SecretMetadata{Name: "claude-work", Purpose: SecretPurposeHarnessAuth, Scope: SecretScopeHarness, BackendKind: SecretBackendKindMemory, Fingerprint: SecretFingerprint(value), RedactedDescription: "claude work auth", MetadataJSON: `{}`})
	if err != nil {
		t.Fatalf("UpsertSecretMetadata returned error: %v", err)
	}
	grant, err := store.GrantSecretAccess(ctx, SecretGrant{SecretID: metadata.SecretID, SubjectType: SecretGrantSubjectHarness, SubjectID: "claude:work", Purpose: SecretGrantPurposeProject})
	if err != nil {
		t.Fatalf("GrantSecretAccess returned error: %v", err)
	}

	events, err := store.ListSecretAuditEvents(ctx, 10)
	if err != nil {
		t.Fatalf("ListSecretAuditEvents returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("audit events = %#v, want one grant event", events)
	}
	event := events[0]
	if event.EventType != SecretAuditEventGranted || event.SubjectType != "secret" || event.SubjectID != metadata.SecretID || event.EventID == "" || event.CreatedAt.IsZero() {
		t.Fatalf("audit event = %#v, want granted event for %q", event, metadata.SecretID)
	}
	payload := workspaceEventStringPayloadForTest(t, event.PayloadJSON)
	if payload["grant_id"] != grant.GrantID || payload["redacted"] != "true" {
		t.Fatalf("audit payload = %#v, want redacted grant linkage", payload)
	}
}
