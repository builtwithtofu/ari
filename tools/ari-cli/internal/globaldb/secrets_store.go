package globaldb

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

const (
	SecretPurposeHarnessAuth = "harness_auth"
	SecretPurposeTool        = "tool"

	SecretScopeGlobal    = "global"
	SecretScopeWorkspace = "workspace"
	SecretScopeSession   = "session"
	SecretScopeHarness   = "harness"

	SecretBackendKindMemory = "memory"
	SecretBackendKindOS     = "os_keychain"
	SecretBackendKindAge    = "age_file"

	SecretGrantSubjectWorkspace = "workspace"
	SecretGrantSubjectSession   = "session"
	SecretGrantSubjectHarness   = "harness"
	SecretGrantSubjectTool      = "tool"

	SecretGrantPurposeRead    = "read"
	SecretGrantPurposeProject = "project"

	SecretAuditEventGranted          = "secret.grant.created"
	SecretAuditEventProjectionDenied = "secret.projection.denied"
	SecretAuditEventProjected        = "secret.projected"
)

type SecretMetadata struct {
	SecretID            string
	Name                string
	Purpose             string
	Scope               string
	BackendKind         string
	Fingerprint         string
	RedactedDescription string
	MetadataJSON        string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type SecretGrant struct {
	GrantID     string
	SecretID    string
	SubjectType string
	SubjectID   string
	Purpose     string
	CreatedAt   time.Time
	ExpiresAt   *time.Time
}

type SecretBackend interface {
	PutSecret(context.Context, string, []byte) error
	GetSecret(context.Context, string) ([]byte, error)
	DeleteSecret(context.Context, string) error
}

type MemorySecretBackend struct {
	mu      sync.Mutex
	secrets map[string][]byte
}

func NewMemorySecretBackend() *MemorySecretBackend {
	return &MemorySecretBackend{secrets: map[string][]byte{}}
}

func (b *MemorySecretBackend) PutSecret(ctx context.Context, secretID string, value []byte) error {
	_ = ctx
	secretID = strings.TrimSpace(secretID)
	if secretID == "" {
		return fmt.Errorf("%w: secret id is required", ErrInvalidInput)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.secrets[secretID] = append([]byte(nil), value...)
	return nil
}

func (b *MemorySecretBackend) GetSecret(ctx context.Context, secretID string) ([]byte, error) {
	_ = ctx
	secretID = strings.TrimSpace(secretID)
	b.mu.Lock()
	defer b.mu.Unlock()
	value, ok := b.secrets[secretID]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

func (b *MemorySecretBackend) DeleteSecret(ctx context.Context, secretID string) error {
	_ = ctx
	secretID = strings.TrimSpace(secretID)
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.secrets, secretID)
	return nil
}

func (s *Store) UpsertSecretMetadata(ctx context.Context, metadata SecretMetadata) (SecretMetadata, error) {
	metadata = normalizeSecretMetadata(metadata)
	if metadata.SecretID == "" {
		var err error
		metadata.SecretID, err = newSecretID()
		if err != nil {
			return SecretMetadata{}, err
		}
	}
	if err := validateSecretMetadata(metadata); err != nil {
		return SecretMetadata{}, err
	}
	now := time.Now().UTC()
	if existing, err := s.GetSecretMetadata(ctx, metadata.SecretID); err == nil && !existing.CreatedAt.IsZero() {
		metadata.CreatedAt = existing.CreatedAt
	} else if metadata.CreatedAt.IsZero() {
		metadata.CreatedAt = now
	}
	if metadata.UpdatedAt.IsZero() {
		metadata.UpdatedAt = now
	}

	if err := s.sqlcQueries().UpsertSecretMetadata(ctx, dbsqlc.UpsertSecretMetadataParams{SecretID: metadata.SecretID, Name: metadata.Name, Purpose: metadata.Purpose, Scope: metadata.Scope, BackendKind: metadata.BackendKind, Fingerprint: metadata.Fingerprint, RedactedDescription: metadata.RedactedDescription, MetadataJson: metadata.MetadataJSON, CreatedAt: metadata.CreatedAt.Format(time.RFC3339Nano), UpdatedAt: metadata.UpdatedAt.Format(time.RFC3339Nano)}); err != nil {
		return SecretMetadata{}, fmt.Errorf("upsert secret metadata %q: %w", metadata.SecretID, err)
	}
	return metadata, nil
}

func (s *Store) GetSecretMetadata(ctx context.Context, secretID string) (SecretMetadata, error) {
	secretID = strings.TrimSpace(secretID)
	if secretID == "" {
		return SecretMetadata{}, fmt.Errorf("%w: secret id is required", ErrInvalidInput)
	}
	row, err := s.sqlcQueries().GetSecretMetadata(ctx, dbsqlc.GetSecretMetadataParams{SecretID: secretID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SecretMetadata{}, ErrNotFound
		}
		return SecretMetadata{}, fmt.Errorf("get secret metadata %q: %w", secretID, err)
	}
	return secretMetadataFromSQLC(row), nil
}

func (s *Store) ListSecretMetadata(ctx context.Context, scope string) ([]SecretMetadata, error) {
	scope = strings.TrimSpace(scope)
	var rows []dbsqlc.AriSecret
	var err error
	if scope == "" {
		rows, err = s.sqlcQueries().ListSecretMetadata(ctx)
	} else {
		rows, err = s.sqlcQueries().ListSecretMetadataByScope(ctx, dbsqlc.ListSecretMetadataByScopeParams{Scope: scope})
	}
	if err != nil {
		return nil, fmt.Errorf("list secret metadata: %w", err)
	}
	metadatas := make([]SecretMetadata, 0, len(rows))
	for _, row := range rows {
		metadatas = append(metadatas, secretMetadataFromSQLC(row))
	}
	return metadatas, nil
}

func (s *Store) DeleteSecretMetadata(ctx context.Context, secretID string) error {
	secretID = strings.TrimSpace(secretID)
	if secretID == "" {
		return fmt.Errorf("%w: secret id is required", ErrInvalidInput)
	}
	rows, err := s.sqlcQueries().DeleteSecretMetadata(ctx, dbsqlc.DeleteSecretMetadataParams{SecretID: secretID})
	if err != nil {
		return fmt.Errorf("delete secret metadata %q: %w", secretID, err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteSecret(ctx context.Context, backend SecretBackend, secretID string) error {
	secretID = strings.TrimSpace(secretID)
	if secretID == "" {
		return fmt.Errorf("%w: secret id is required", ErrInvalidInput)
	}
	if backend == nil {
		return fmt.Errorf("%w: secret backend is required", ErrInvalidInput)
	}
	if err := backend.DeleteSecret(ctx, secretID); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete backend secret %q: %w", secretID, err)
	}
	if _, err := s.sqlcQueries().DeleteSecretGrantsBySecret(ctx, dbsqlc.DeleteSecretGrantsBySecretParams{SecretID: secretID}); err != nil {
		return fmt.Errorf("delete secret grants %q: %w", secretID, err)
	}
	return s.DeleteSecretMetadata(ctx, secretID)
}

func (s *Store) GrantSecretAccess(ctx context.Context, grant SecretGrant) (SecretGrant, error) {
	grant = normalizeSecretGrant(grant)
	if grant.GrantID == "" {
		var err error
		grant.GrantID, err = newSecretGrantID()
		if err != nil {
			return SecretGrant{}, err
		}
	}
	if err := validateSecretGrant(grant); err != nil {
		return SecretGrant{}, err
	}
	if _, err := s.GetSecretMetadata(ctx, grant.SecretID); err != nil {
		return SecretGrant{}, err
	}
	if grant.CreatedAt.IsZero() {
		grant.CreatedAt = time.Now().UTC()
	}
	var expiresAt *string
	if grant.ExpiresAt != nil {
		formatted := grant.ExpiresAt.UTC().Format(time.RFC3339Nano)
		expiresAt = &formatted
	}
	if err := s.sqlcQueries().UpsertSecretGrant(ctx, dbsqlc.UpsertSecretGrantParams{GrantID: grant.GrantID, SecretID: grant.SecretID, SubjectType: grant.SubjectType, SubjectID: grant.SubjectID, Purpose: grant.Purpose, CreatedAt: grant.CreatedAt.Format(time.RFC3339Nano), ExpiresAt: expiresAt}); err != nil {
		return SecretGrant{}, fmt.Errorf("upsert secret grant %q: %w", grant.GrantID, err)
	}
	existing, err := s.GetSecretGrantBySubject(ctx, grant.SecretID, grant.SubjectType, grant.SubjectID, grant.Purpose)
	if err != nil {
		return SecretGrant{}, fmt.Errorf("reload secret grant %q: %w", grant.GrantID, err)
	}
	grant.GrantID = existing.GrantID
	grant.CreatedAt = existing.CreatedAt
	if err := s.appendSecretAuditEvent(ctx, SecretAuditEventGranted, grant, false); err != nil {
		return SecretGrant{}, err
	}
	return grant, nil
}

func (s *Store) GetSecretGrantBySubject(ctx context.Context, secretID, subjectType, subjectID, purpose string) (SecretGrant, error) {
	grants, err := s.ListSecretGrantsBySubject(ctx, subjectType, subjectID)
	if err != nil {
		return SecretGrant{}, err
	}
	for _, grant := range grants {
		if grant.SecretID == strings.TrimSpace(secretID) && grant.Purpose == strings.TrimSpace(purpose) {
			return grant, nil
		}
	}
	return SecretGrant{}, ErrNotFound
}

func (s *Store) ListSecretGrantsBySubject(ctx context.Context, subjectType, subjectID string) ([]SecretGrant, error) {
	subjectType = strings.TrimSpace(subjectType)
	subjectID = strings.TrimSpace(subjectID)
	if subjectType == "" || subjectID == "" {
		return nil, fmt.Errorf("%w: secret grant subject is required", ErrInvalidInput)
	}
	rows, err := s.sqlcQueries().ListSecretGrantsBySubject(ctx, dbsqlc.ListSecretGrantsBySubjectParams{SubjectType: subjectType, SubjectID: subjectID})
	if err != nil {
		return nil, fmt.Errorf("list secret grants: %w", err)
	}
	grants := make([]SecretGrant, 0, len(rows))
	for _, row := range rows {
		grants = append(grants, secretGrantFromSQLC(row))
	}
	return grants, nil
}

func (s *Store) CheckSecretAccess(ctx context.Context, secretID, subjectType, subjectID, purpose string) (bool, error) {
	secretID = strings.TrimSpace(secretID)
	subjectType = strings.TrimSpace(subjectType)
	subjectID = strings.TrimSpace(subjectID)
	purpose = strings.TrimSpace(purpose)
	if secretID == "" || subjectType == "" || subjectID == "" || purpose == "" {
		return false, fmt.Errorf("%w: secret access required field is missing", ErrInvalidInput)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	count, err := s.sqlcQueries().CountActiveSecretGrants(ctx, dbsqlc.CountActiveSecretGrantsParams{SecretID: secretID, SubjectType: subjectType, SubjectID: subjectID, Purpose: purpose, ExpiresAt: &now})
	if err != nil {
		return false, fmt.Errorf("check secret grant: %w", err)
	}
	return count > 0, nil
}

func (s *Store) ProjectSecretWithGrant(ctx context.Context, backend SecretBackend, secretID, subjectType, subjectID string) ([]byte, error) {
	allowed, err := s.CheckSecretAccess(ctx, secretID, subjectType, subjectID, SecretGrantPurposeProject)
	grant := SecretGrant{SecretID: strings.TrimSpace(secretID), SubjectType: strings.TrimSpace(subjectType), SubjectID: strings.TrimSpace(subjectID), Purpose: SecretGrantPurposeProject}
	if err != nil {
		_ = s.appendSecretAuditEvent(ctx, SecretAuditEventProjectionDenied, grant, true)
		return nil, err
	}
	if !allowed {
		_ = s.appendSecretAuditEvent(ctx, SecretAuditEventProjectionDenied, grant, true)
		return nil, fmt.Errorf("%w: secret projection grant is required", ErrPermissionDenied)
	}
	if backend == nil {
		_ = s.appendSecretAuditEvent(ctx, SecretAuditEventProjectionDenied, grant, true)
		return nil, fmt.Errorf("%w: secret backend is required", ErrInvalidInput)
	}
	value, err := backend.GetSecret(ctx, secretID)
	if err != nil {
		_ = s.appendSecretAuditEvent(ctx, SecretAuditEventProjectionDenied, grant, true)
		return nil, err
	}
	if err := s.appendSecretAuditEvent(ctx, SecretAuditEventProjected, grant, false); err != nil {
		return nil, err
	}
	return value, nil
}

func normalizeSecretMetadata(metadata SecretMetadata) SecretMetadata {
	metadata.SecretID = strings.TrimSpace(metadata.SecretID)
	metadata.Name = strings.TrimSpace(metadata.Name)
	metadata.Purpose = strings.TrimSpace(metadata.Purpose)
	metadata.Scope = strings.TrimSpace(metadata.Scope)
	metadata.BackendKind = strings.TrimSpace(metadata.BackendKind)
	metadata.Fingerprint = strings.TrimSpace(metadata.Fingerprint)
	metadata.RedactedDescription = strings.TrimSpace(metadata.RedactedDescription)
	if strings.TrimSpace(metadata.MetadataJSON) == "" {
		metadata.MetadataJSON = "{}"
	}
	return metadata
}

func validateSecretMetadata(metadata SecretMetadata) error {
	if metadata.SecretID == "" || metadata.Name == "" || metadata.Purpose == "" || metadata.Scope == "" || metadata.BackendKind == "" || metadata.Fingerprint == "" || metadata.RedactedDescription == "" {
		return fmt.Errorf("%w: secret metadata required field is missing", ErrInvalidInput)
	}
	if metadata.Purpose != SecretPurposeHarnessAuth && metadata.Purpose != SecretPurposeTool {
		return fmt.Errorf("%w: secret purpose is invalid", ErrInvalidInput)
	}
	if metadata.Scope != SecretScopeGlobal && metadata.Scope != SecretScopeWorkspace && metadata.Scope != SecretScopeSession && metadata.Scope != SecretScopeHarness {
		return fmt.Errorf("%w: secret scope is invalid", ErrInvalidInput)
	}
	if metadata.BackendKind != SecretBackendKindMemory && metadata.BackendKind != SecretBackendKindOS && metadata.BackendKind != SecretBackendKindAge {
		return fmt.Errorf("%w: secret backend kind is invalid", ErrInvalidInput)
	}
	if !json.Valid([]byte(metadata.MetadataJSON)) {
		return fmt.Errorf("%w: secret metadata json is invalid", ErrInvalidInput)
	}
	if jsonContainsSecretLikeFields(metadata.MetadataJSON) {
		return fmt.Errorf("%w: secret metadata json must not include secret-like fields", ErrInvalidInput)
	}
	return nil
}

func normalizeSecretGrant(grant SecretGrant) SecretGrant {
	grant.GrantID = strings.TrimSpace(grant.GrantID)
	grant.SecretID = strings.TrimSpace(grant.SecretID)
	grant.SubjectType = strings.TrimSpace(grant.SubjectType)
	grant.SubjectID = strings.TrimSpace(grant.SubjectID)
	grant.Purpose = strings.TrimSpace(grant.Purpose)
	return grant
}

func validateSecretGrant(grant SecretGrant) error {
	if grant.SecretID == "" || grant.SubjectType == "" || grant.SubjectID == "" || grant.Purpose == "" {
		return fmt.Errorf("%w: secret grant required field is missing", ErrInvalidInput)
	}
	if grant.SubjectType != SecretGrantSubjectWorkspace && grant.SubjectType != SecretGrantSubjectSession && grant.SubjectType != SecretGrantSubjectHarness && grant.SubjectType != SecretGrantSubjectTool {
		return fmt.Errorf("%w: secret grant subject type is invalid", ErrInvalidInput)
	}
	if grant.Purpose != SecretGrantPurposeRead && grant.Purpose != SecretGrantPurposeProject {
		return fmt.Errorf("%w: secret grant purpose is invalid", ErrInvalidInput)
	}
	return nil
}

func (s *Store) appendSecretAuditEvent(ctx context.Context, eventType string, grant SecretGrant, attention bool) error {
	payload, err := json.Marshal(map[string]string{"grant_id": grant.GrantID, "secret_id": grant.SecretID, "subject_type": grant.SubjectType, "subject_id": grant.SubjectID, "purpose": grant.Purpose, "redacted": "true"})
	if err != nil {
		return err
	}
	_, err = s.AppendDaemonEvent(ctx, DaemonEvent{EventType: eventType, SubjectType: "secret", SubjectID: grant.SecretID, PayloadJSON: string(payload), AttentionRequired: attention})
	return err
}

func secretMetadataFromSQLC(row dbsqlc.AriSecret) SecretMetadata {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	return SecretMetadata{SecretID: row.SecretID, Name: row.Name, Purpose: row.Purpose, Scope: row.Scope, BackendKind: row.BackendKind, Fingerprint: row.Fingerprint, RedactedDescription: row.RedactedDescription, MetadataJSON: row.MetadataJson, CreatedAt: createdAt, UpdatedAt: updatedAt}
}

func secretGrantFromSQLC(row dbsqlc.AriSecretGrant) SecretGrant {
	createdAt, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
	var expiresAt *time.Time
	if row.ExpiresAt != nil {
		if parsed, err := time.Parse(time.RFC3339Nano, *row.ExpiresAt); err == nil {
			expiresAt = &parsed
		}
	}
	return SecretGrant{GrantID: row.GrantID, SecretID: row.SecretID, SubjectType: row.SubjectType, SubjectID: row.SubjectID, Purpose: row.Purpose, CreatedAt: createdAt, ExpiresAt: expiresAt}
}

func newSecretID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("generate secret id: %w", err)
	}
	return "sec_" + hex.EncodeToString(data[:]), nil
}

func newSecretGrantID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("generate secret grant id: %w", err)
	}
	return "sgrant_" + hex.EncodeToString(data[:]), nil
}

func SecretFingerprint(value []byte) string {
	hash := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(hash[:])
}
