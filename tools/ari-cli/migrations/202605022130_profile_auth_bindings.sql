ALTER TABLE agent_profiles
ADD COLUMN auth_slot_id TEXT;

ALTER TABLE agent_profiles
ADD COLUMN auth_pool_json TEXT NOT NULL DEFAULT '{}';
