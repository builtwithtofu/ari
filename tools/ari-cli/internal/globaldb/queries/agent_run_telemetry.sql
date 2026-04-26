-- name: UpsertAgentRunTelemetry :exec
INSERT INTO agent_run_telemetry (
  run_id, workspace_id, task_id, profile_id, profile_name, harness, model, invocation_class, status,
  input_tokens_known, input_tokens, output_tokens_known, output_tokens,
  estimated_cost_known, estimated_cost_micros, duration_ms_known, duration_ms,
  exit_code_known, exit_code, owned_by_ari, pid_known, pid,
  cpu_time_ms_known, cpu_time_ms, memory_rss_bytes_peak_known, memory_rss_bytes_peak,
  child_processes_peak_known, child_processes_peak, ports_json, orphan_state, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
  status = excluded.status,
  input_tokens_known = excluded.input_tokens_known,
  input_tokens = excluded.input_tokens,
  output_tokens_known = excluded.output_tokens_known,
  output_tokens = excluded.output_tokens,
  estimated_cost_known = excluded.estimated_cost_known,
  estimated_cost_micros = excluded.estimated_cost_micros,
  duration_ms_known = excluded.duration_ms_known,
  duration_ms = excluded.duration_ms,
  exit_code_known = excluded.exit_code_known,
  exit_code = excluded.exit_code,
  owned_by_ari = excluded.owned_by_ari,
  pid_known = excluded.pid_known,
  pid = excluded.pid,
  cpu_time_ms_known = excluded.cpu_time_ms_known,
  cpu_time_ms = excluded.cpu_time_ms,
  memory_rss_bytes_peak_known = excluded.memory_rss_bytes_peak_known,
  memory_rss_bytes_peak = excluded.memory_rss_bytes_peak,
  child_processes_peak_known = excluded.child_processes_peak_known,
  child_processes_peak = excluded.child_processes_peak,
  ports_json = excluded.ports_json,
  orphan_state = excluded.orphan_state,
  updated_at = excluded.updated_at;

-- name: ListAgentRunTelemetryByWorkspace :many
SELECT * FROM agent_run_telemetry
WHERE workspace_id = ?
ORDER BY created_at DESC, run_id ASC;
