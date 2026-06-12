-- Seed the default auth slot for the grok harness so auth status/doctor list
-- it alongside the other officially supported harnesses.
INSERT INTO auth_slots (auth_slot_id, harness, label, provider_label, credential_owner, status, metadata_json, created_at, updated_at)
VALUES
  ('grok-default', 'grok', 'Default', NULL, 'provider', 'unknown', '{}', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
