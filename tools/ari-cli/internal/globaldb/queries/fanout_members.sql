-- name: UpsertFanoutMember :exec
INSERT INTO fanout_members (
  fanout_member_id,
  fanout_group_id,
  workspace_id,
  worker_session_id,
  target_profile_id,
  request_agent_message_id,
  reply_agent_message_id,
  final_response_id,
  status,
  created_at,
  updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(fanout_member_id) DO UPDATE SET
  worker_session_id = COALESCE(NULLIF(excluded.worker_session_id, ''), fanout_members.worker_session_id),
  target_profile_id = COALESCE(NULLIF(excluded.target_profile_id, ''), fanout_members.target_profile_id),
  request_agent_message_id = COALESCE(NULLIF(excluded.request_agent_message_id, ''), fanout_members.request_agent_message_id),
  reply_agent_message_id = COALESCE(NULLIF(excluded.reply_agent_message_id, ''), fanout_members.reply_agent_message_id),
  final_response_id = COALESCE(NULLIF(excluded.final_response_id, ''), fanout_members.final_response_id),
  status = excluded.status,
  updated_at = excluded.updated_at;
