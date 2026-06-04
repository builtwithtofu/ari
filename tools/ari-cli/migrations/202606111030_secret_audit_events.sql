-- daemon_events was a parallel event system. Lifecycle and message facts now
-- live in workspace event history; the only remaining writer is the global
-- secret audit trail, so the table shrinks to exactly that: append-only
-- secret audit events without workspace/session/attention columns.
CREATE TABLE secret_audit_events (
  event_id TEXT PRIMARY KEY,
  event_type TEXT NOT NULL,
  subject_type TEXT NOT NULL,
  subject_id TEXT NOT NULL,
  payload_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

INSERT INTO secret_audit_events (event_id, event_type, subject_type, subject_id, payload_json, created_at)
SELECT event_id, event_type, subject_type, subject_id, payload_json, created_at
FROM daemon_events
WHERE subject_type = 'secret';

DROP INDEX IF EXISTS daemon_events_created_idx;
DROP INDEX IF EXISTS daemon_events_attention_idx;
DROP TABLE daemon_events;

CREATE INDEX secret_audit_events_created_idx
  ON secret_audit_events(created_at ASC, event_id ASC);
