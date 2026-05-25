package globaldb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSecretMetadataPersistsWithoutPlaintext(t *testing.T) {
	store := newGlobalDBTestStore(t, "secrets")
	ctx := context.Background()
	sentinel := []byte("ari-secret-sentinel-token")

	metadata, err := store.UpsertSecretMetadata(ctx, SecretMetadata{Name: "opencode-work", Purpose: SecretPurposeHarnessAuth, Scope: SecretScopeHarness, BackendKind: SecretBackendKindMemory, Fingerprint: SecretFingerprint(sentinel), RedactedDescription: "opencode work auth token", MetadataJSON: `{"harness":"opencode","slot":"work"}`})
	if err != nil {
		t.Fatalf("UpsertSecretMetadata returned error: %v", err)
	}
	if metadata.SecretID == "" || metadata.CreatedAt.IsZero() || metadata.UpdatedAt.IsZero() {
		t.Fatalf("metadata = %#v, want generated id and timestamps", metadata)
	}

	stored, err := store.GetSecretMetadata(ctx, metadata.SecretID)
	if err != nil {
		t.Fatalf("GetSecretMetadata returned error: %v", err)
	}
	if stored.Fingerprint != SecretFingerprint(sentinel) || stored.RedactedDescription != "opencode work auth token" {
		t.Fatalf("stored = %#v, want metadata only", stored)
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if bytes.Contains(encoded, sentinel) {
		t.Fatalf("metadata JSON leaked sentinel: %s", encoded)
	}
	listed, err := store.ListSecretMetadata(ctx, SecretScopeHarness)
	if err != nil {
		t.Fatalf("ListSecretMetadata returned error: %v", err)
	}
	if len(listed) != 1 || listed[0].SecretID != metadata.SecretID {
		t.Fatalf("listed = %#v, want one stored secret metadata row", listed)
	}
	if err := store.DeleteSecretMetadata(ctx, metadata.SecretID); err != nil {
		t.Fatalf("DeleteSecretMetadata returned error: %v", err)
	}
	if _, err := store.GetSecretMetadata(ctx, metadata.SecretID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSecretMetadata after delete error = %v, want ErrNotFound", err)
	}
}

func TestSecretMetadataRejectsPlaintextLikeMetadata(t *testing.T) {
	store := newGlobalDBTestStore(t, "secrets-invalid")
	_, err := store.UpsertSecretMetadata(context.Background(), SecretMetadata{Name: "bad", Purpose: SecretPurposeHarnessAuth, Scope: SecretScopeHarness, BackendKind: SecretBackendKindMemory, Fingerprint: "sha256:test", RedactedDescription: "bad", MetadataJSON: `{"api_key":"sk-test"}`})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpsertSecretMetadata error = %v, want ErrInvalidInput", err)
	}
}

func TestMemorySecretBackendStoresValuesOutsideSQLite(t *testing.T) {
	store := newGlobalDBTestStore(t, "secrets-backend")
	backend := NewMemorySecretBackend()
	ctx := context.Background()
	value := []byte("ari-memory-secret-sentinel")
	metadata, err := store.UpsertSecretMetadata(ctx, SecretMetadata{Name: "claude-work", Purpose: SecretPurposeHarnessAuth, Scope: SecretScopeHarness, BackendKind: SecretBackendKindMemory, Fingerprint: SecretFingerprint(value), RedactedDescription: "claude work token", MetadataJSON: `{}`})
	if err != nil {
		t.Fatalf("UpsertSecretMetadata returned error: %v", err)
	}
	if err := backend.PutSecret(ctx, metadata.SecretID, value); err != nil {
		t.Fatalf("PutSecret returned error: %v", err)
	}
	got, err := backend.GetSecret(ctx, metadata.SecretID)
	if err != nil {
		t.Fatalf("GetSecret returned error: %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Fatalf("secret value = %q, want stored sentinel", got)
	}
	got[0] = 'X'
	again, err := backend.GetSecret(ctx, metadata.SecretID)
	if err != nil {
		t.Fatalf("GetSecret second read returned error: %v", err)
	}
	if bytes.Equal(got, again) {
		t.Fatalf("backend returned mutable secret slice")
	}
	stored, err := store.GetSecretMetadata(ctx, metadata.SecretID)
	if err != nil {
		t.Fatalf("GetSecretMetadata returned error: %v", err)
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if bytes.Contains(encoded, value) {
		t.Fatalf("metadata JSON leaked backend value: %s", encoded)
	}
	if err := backend.DeleteSecret(ctx, metadata.SecretID); err != nil {
		t.Fatalf("DeleteSecret returned error: %v", err)
	}
	if _, err := backend.GetSecret(ctx, metadata.SecretID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSecret after delete error = %v, want ErrNotFound", err)
	}
}

func TestDeleteSecretRemovesMetadataGrantsAndBackendValue(t *testing.T) {
	store := newGlobalDBTestStore(t, "secrets-delete-owned")
	backend := NewMemorySecretBackend()
	ctx := context.Background()
	value := []byte("ari-delete-secret-sentinel")
	metadata, err := store.UpsertSecretMetadata(ctx, SecretMetadata{Name: "opencode-work", Purpose: SecretPurposeHarnessAuth, Scope: SecretScopeWorkspace, BackendKind: SecretBackendKindMemory, Fingerprint: SecretFingerprint(value), RedactedDescription: "opencode work auth", MetadataJSON: `{}`})
	if err != nil {
		t.Fatalf("UpsertSecretMetadata returned error: %v", err)
	}
	if err := backend.PutSecret(ctx, metadata.SecretID, value); err != nil {
		t.Fatalf("PutSecret returned error: %v", err)
	}
	if _, err := store.GrantSecretAccess(ctx, SecretGrant{SecretID: metadata.SecretID, SubjectType: SecretGrantSubjectWorkspace, SubjectID: "ws-1", Purpose: SecretGrantPurposeProject}); err != nil {
		t.Fatalf("GrantSecretAccess returned error: %v", err)
	}

	if err := store.DeleteSecret(ctx, backend, metadata.SecretID); err != nil {
		t.Fatalf("DeleteSecret returned error: %v", err)
	}
	if _, err := store.GetSecretMetadata(ctx, metadata.SecretID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSecretMetadata after delete error = %v, want ErrNotFound", err)
	}
	if _, err := backend.GetSecret(ctx, metadata.SecretID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSecret after delete error = %v, want ErrNotFound", err)
	}
	allowed, err := store.CheckSecretAccess(ctx, metadata.SecretID, SecretGrantSubjectWorkspace, "ws-1", SecretGrantPurposeProject)
	if err != nil {
		t.Fatalf("CheckSecretAccess returned error: %v", err)
	}
	if allowed {
		t.Fatalf("deleted secret grant still allows projection")
	}
}

func TestSecretProjectionRequiresGrantAndAuditsDeniedAccess(t *testing.T) {
	store := newGlobalDBTestStore(t, "secrets-grant-denied")
	backend := NewMemorySecretBackend()
	ctx := context.Background()
	value := []byte("ari-denied-secret-sentinel-access_token")
	metadata, err := store.UpsertSecretMetadata(ctx, SecretMetadata{Name: "opencode-work", Purpose: SecretPurposeHarnessAuth, Scope: SecretScopeHarness, BackendKind: SecretBackendKindMemory, Fingerprint: SecretFingerprint(value), RedactedDescription: "opencode work auth", MetadataJSON: `{}`})
	if err != nil {
		t.Fatalf("UpsertSecretMetadata returned error: %v", err)
	}
	if err := backend.PutSecret(ctx, metadata.SecretID, value); err != nil {
		t.Fatalf("PutSecret returned error: %v", err)
	}

	_, err = store.ProjectSecretWithGrant(ctx, backend, metadata.SecretID, SecretGrantSubjectHarness, "opencode:work")
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("ProjectSecretWithGrant error = %v, want ErrPermissionDenied", err)
	}
	assertLatestSecretAuditEventRedacted(t, store, SecretAuditEventProjectionDenied, value)
}

func TestSecretProjectionWithGrantReturnsValueAndAuditsRedactedSuccess(t *testing.T) {
	store := newGlobalDBTestStore(t, "secrets-grant-success")
	backend := NewMemorySecretBackend()
	ctx := context.Background()
	value := []byte("ari-projected-secret-sentinel-refresh_token")
	metadata, err := store.UpsertSecretMetadata(ctx, SecretMetadata{Name: "claude-work", Purpose: SecretPurposeHarnessAuth, Scope: SecretScopeHarness, BackendKind: SecretBackendKindMemory, Fingerprint: SecretFingerprint(value), RedactedDescription: "claude work auth", MetadataJSON: `{}`})
	if err != nil {
		t.Fatalf("UpsertSecretMetadata returned error: %v", err)
	}
	if err := backend.PutSecret(ctx, metadata.SecretID, value); err != nil {
		t.Fatalf("PutSecret returned error: %v", err)
	}
	grant, err := store.GrantSecretAccess(ctx, SecretGrant{SecretID: metadata.SecretID, SubjectType: SecretGrantSubjectHarness, SubjectID: "claude:work", Purpose: SecretGrantPurposeProject})
	if err != nil {
		t.Fatalf("GrantSecretAccess returned error: %v", err)
	}
	if grant.GrantID == "" || grant.CreatedAt.IsZero() {
		t.Fatalf("grant = %#v, want generated id and timestamp", grant)
	}

	got, err := store.ProjectSecretWithGrant(ctx, backend, metadata.SecretID, SecretGrantSubjectHarness, "claude:work")
	if err != nil {
		t.Fatalf("ProjectSecretWithGrant returned error: %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Fatalf("projected secret = %q, want backend value", got)
	}
	assertLatestSecretAuditEventRedacted(t, store, SecretAuditEventProjected, value)
}

func TestSecretGrantUpsertPreservesGrantIdentity(t *testing.T) {
	store := newGlobalDBTestStore(t, "secrets-grant-idempotent")
	ctx := context.Background()
	value := []byte("ari-grant-stability")
	metadata, err := store.UpsertSecretMetadata(ctx, SecretMetadata{Name: "opencode-work", Purpose: SecretPurposeHarnessAuth, Scope: SecretScopeWorkspace, BackendKind: SecretBackendKindMemory, Fingerprint: SecretFingerprint(value), RedactedDescription: "opencode work auth", MetadataJSON: `{}`})
	if err != nil {
		t.Fatalf("UpsertSecretMetadata returned error: %v", err)
	}
	first, err := store.GrantSecretAccess(ctx, SecretGrant{SecretID: metadata.SecretID, SubjectType: SecretGrantSubjectWorkspace, SubjectID: "ws-1", Purpose: SecretGrantPurposeProject})
	if err != nil {
		t.Fatalf("GrantSecretAccess first returned error: %v", err)
	}
	expiresAt := time.Now().Add(time.Hour)
	second, err := store.GrantSecretAccess(ctx, SecretGrant{SecretID: metadata.SecretID, SubjectType: SecretGrantSubjectWorkspace, SubjectID: "ws-1", Purpose: SecretGrantPurposeProject, ExpiresAt: &expiresAt})
	if err != nil {
		t.Fatalf("GrantSecretAccess second returned error: %v", err)
	}
	if second.GrantID != first.GrantID || !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("second grant = %#v, want stable id/timestamp from %#v", second, first)
	}
}

func TestExpiredSecretGrantDeniesProjectionAndAuditsRedactedDenial(t *testing.T) {
	store := newGlobalDBTestStore(t, "secrets-expired-grant")
	backend := NewMemorySecretBackend()
	ctx := context.Background()
	value := []byte("ari-expired-secret-sentinel-api_key")
	metadata, err := store.UpsertSecretMetadata(ctx, SecretMetadata{Name: "opencode-expired", Purpose: SecretPurposeHarnessAuth, Scope: SecretScopeHarness, BackendKind: SecretBackendKindMemory, Fingerprint: SecretFingerprint(value), RedactedDescription: "expired opencode auth", MetadataJSON: `{}`})
	if err != nil {
		t.Fatalf("UpsertSecretMetadata returned error: %v", err)
	}
	if err := backend.PutSecret(ctx, metadata.SecretID, value); err != nil {
		t.Fatalf("PutSecret returned error: %v", err)
	}
	expiresAt := time.Now().Add(-time.Minute)
	if _, err := store.GrantSecretAccess(ctx, SecretGrant{SecretID: metadata.SecretID, SubjectType: SecretGrantSubjectHarness, SubjectID: "opencode-expired", Purpose: SecretGrantPurposeProject, ExpiresAt: &expiresAt}); err != nil {
		t.Fatalf("GrantSecretAccess returned error: %v", err)
	}

	_, err = store.ProjectSecretWithGrant(ctx, backend, metadata.SecretID, SecretGrantSubjectHarness, "opencode-expired")
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("ProjectSecretWithGrant error = %v, want ErrPermissionDenied for expired grant", err)
	}
	assertLatestSecretAuditEventRedacted(t, store, SecretAuditEventProjectionDenied, value)
}

func assertLatestSecretAuditEventRedacted(t *testing.T, store *Store, eventType string, forbidden []byte) {
	t.Helper()
	events, err := store.ListDaemonEventsAfter(context.Background(), "", 20)
	if err != nil {
		t.Fatalf("ListDaemonEventsAfter returned error: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("no daemon events recorded")
	}
	event := events[len(events)-1]
	if event.EventType != eventType || event.SubjectType != "secret" {
		t.Fatalf("event = %#v, want %s secret event", event, eventType)
	}
	payload := []byte(event.PayloadJSON)
	if bytes.Contains(payload, forbidden) {
		t.Fatalf("audit payload leaked sentinel: %s", payload)
	}
	for _, tokenField := range []string{"access_token", "refresh_token", "api_key"} {
		if strings.Contains(event.PayloadJSON, tokenField) {
			t.Fatalf("audit payload leaked token-like field %q: %s", tokenField, event.PayloadJSON)
		}
	}
}
