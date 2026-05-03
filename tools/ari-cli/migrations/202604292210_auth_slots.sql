CREATE TABLE auth_slots (
    auth_slot_id TEXT PRIMARY KEY,
    harness TEXT NOT NULL,
    label TEXT NOT NULL,
    provider_label TEXT,
    credential_owner TEXT NOT NULL DEFAULT 'provider',
    status TEXT NOT NULL DEFAULT 'unknown',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX auth_slots_harness_label_idx
    ON auth_slots(harness, label, auth_slot_id);

INSERT INTO auth_slots (
    auth_slot_id,
    harness,
    label,
    credential_owner,
    status,
    metadata_json,
    created_at,
    updated_at
)
VALUES
    ('codex-default', 'codex', 'Codex default', 'provider', 'unknown', '{}', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('claude-default', 'claude', 'Claude default', 'provider', 'unknown', '{}', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('opencode-default', 'opencode', 'OpenCode default', 'provider', 'unknown', '{}', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
