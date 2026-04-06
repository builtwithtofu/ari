ALTER TABLE agents ADD COLUMN harness TEXT;
ALTER TABLE agents ADD COLUMN harness_resumable_id TEXT;
ALTER TABLE agents ADD COLUMN harness_metadata TEXT NOT NULL DEFAULT '{}';
