# Plan Schema (Ariadne v0)

This document defines the DAG-based plan structure for Ariadne v0. When project storage mode is enabled, plans are stored as JSON artifacts in the project-local `.ari/` directory; otherwise the global store is used.

## Core Structure

A plan is a directed acyclic graph (DAG) of steps. Each step has a type, dependencies, status, and typed payload.

### Top-Level Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `plan_id` | string | yes | Unique identifier (UUID or content hash) |
| `goal` | string | yes | Human-readable goal statement |
| `steps` | array | yes | Ordered list of step objects |
| `status` | string | yes | Overall plan status (see Status Values) |
| `created_at` | string | yes | ISO 8601 timestamp |
| `updated_at` | string | yes | ISO 8601 timestamp |
| `metadata` | object | no | Arbitrary key-value pairs |

### Example

```json
{
  "plan_id": "plan-abc123",
  "goal": "Add user authentication with JWT",
  "status": "planned",
  "created_at": "2026-02-22T10:00:00Z",
  "updated_at": "2026-02-22T10:05:00Z",
  "steps": [
    {
      "step_id": "step-001",
      "type": "REASONING",
      "description": "Analyze existing auth patterns",
      "depends_on": [],
      "status": "completed"
    },
    {
      "step_id": "step-002",
      "type": "CODE",
      "description": "Implement JWT middleware",
      "depends_on": ["step-001"],
      "status": "planned"
    }
  ]
}
```

## Step Schema

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `step_id` | string | Unique identifier within the plan |
| `type` | string | Step type (see Step Types) |
| `description` | string | Human-readable description |
| `depends_on` | array | List of step IDs this step depends on |
| `status` | string | Current status (see Status Values) |

### Optional Fields

| Field | Type | Description |
|-------|------|-------------|
| `payload` | object | Type-specific data (code, tool config, etc.) |
| `validation` | object | Validation criteria and results |
| `error` | string | Error message if status is `failed` |
| `started_at` | string | ISO 8601 timestamp when execution began |
| `completed_at` | string | ISO 8601 timestamp when execution finished |

## Step Types

| Type | Description | Payload Fields |
|------|-------------|----------------|
| `CODE` | File modifications, implementations | `files`, `diff`, `language` |
| `TOOL_CALL` | External tool or command invocation | `tool`, `args`, `expected_output` |
| `REASONING` | Analysis, research, planning outputs | `findings`, `decisions`, `questions` |
| `HUMAN_INPUT` | Requires explicit approval or feedback | `prompt`, `options`, `feedback_field` |

### CODE Step Example

```json
{
  "step_id": "step-002",
  "type": "CODE",
  "description": "Implement JWT middleware",
  "depends_on": ["step-001"],
  "status": "planned",
  "payload": {
    "files": ["internal/middleware/auth.go"],
    "language": "go"
  }
}
```

### TOOL_CALL Step Example

```json
{
  "step_id": "step-003",
  "type": "TOOL_CALL",
  "description": "Run unit tests",
  "depends_on": ["step-002"],
  "status": "planned",
  "payload": {
    "tool": "go",
    "args": ["test", "./internal/middleware/..."],
    "expected_output": "PASS"
  }
}
```

### HUMAN_INPUT Step Example

```json
{
  "step_id": "step-004",
  "type": "HUMAN_INPUT",
  "description": "Approve auth implementation",
  "depends_on": ["step-003"],
  "status": "waiting_approval",
  "payload": {
    "prompt": "Review the auth middleware changes before proceeding",
    "options": ["approve", "reject"]
  }
}
```

## Status Values

Aligned with the v0 execution contract state machine.

### Plan Status

| Status | Description |
|--------|-------------|
| `planned` | Plan created, awaiting approval to execute |
| `waiting_approval` | One or more steps require human approval |
| `approved` | Plan approved, execution can proceed |
| `executing` | Steps are being executed |
| `completed` | All steps completed successfully |
| `rejected` | Plan rejected by human |
| `failed` | Execution failed, may be resumable |

### Step Status

| Status | Description |
|--------|-------------|
| `planned` | Step not yet started |
| `waiting_approval` | Awaiting explicit approval |
| `approved` | Approved, ready to execute |
| `rejected` | Rejected with feedback |
| `executing` | Currently executing |
| `completed` | Finished successfully |
| `failed` | Failed, may be resumable |
| `failed_non_resumable` | Failed, requires manual intervention |

## State Transitions

```
planned -> waiting_approval -> approved -> executing -> completed
                |                |
                v                v
            rejected          failed -> resumed -> executing
                                     |
                                     v
                              failed_non_resumable
```

## Validation Fields

Steps may include validation criteria that are checked before marking as complete.

| Field | Type | Description |
|-------|------|-------------|
| `criteria` | array | List of validation criteria |
| `criteria[].type` | string | `file_exists`, `output_contains`, `exit_code`, `test_pass` |
| `criteria[].expected` | any | Expected value |
| `criteria[].actual` | any | Actual value (populated after execution) |
| `criteria[].passed` | boolean | Whether criterion passed |

### Validation Example

```json
{
  "validation": {
    "criteria": [
      {
        "type": "file_exists",
        "expected": "internal/middleware/auth.go",
        "actual": "internal/middleware/auth.go",
        "passed": true
      },
      {
        "type": "test_pass",
        "expected": "PASS",
        "actual": "FAIL: TestAuthMiddleware",
        "passed": false
      }
    ]
  }
}
```

## Storage Boundary

Project-local plan artifacts are stored as JSON files under `.ari/plans/`. Daemon runtime state is moving to `~/.ari/` in the daemon-first reset.

The legacy storage-mode toggles below are historical context and are not active in the daemon-first runtime.

```bash
# CLI flag
ari --storage-mode project ...

# Environment variable
ARI_STORAGE_MODE=project

# Daemon config file (~/.ari/config.json)
{
  "daemon": {
    "socket_path": "~/.ari/daemon.sock"
  },
  "log_level": "info"
}
```

```
.ari/
├── plans/
│   ├── plan-abc123.json
│   └── plan-def456.json
├── ops/                      # Operation DAG for recovery
├── project.json
└── agents.json
```

Plans are never stored in SQLite. This keeps project state git-syncable and human-readable.

## Database Migrations (Global DB)

The global SQLite database path is `~/.ari/ari.db`. Atlas is the **required** migration tool for v0.

### Migration Workflow

Migrations are applied automatically during Ari startup. To apply migrations manually:

```bash
atlas migrate apply --env globaldb --config tools/ari-cli/atlas.hcl
```

Migrations are **idempotent**: running apply twice results in a no-op success. Atlas tracks applied migrations in the `atlas_schema_revisions` table.

### Migration Safety Policy

- **Non-breaking by default**: Schema changes prioritize backward compatibility
- **Rollback path**: Each migration includes a reversible counterpart where feasible
- **Version coexistence**: During transition windows, v1 and v2 file formats may coexist with explicit migration handling
- **Manual intervention**: If a migration fails, Ari reports the error and does not proceed with startup

### Downgrade Guidance

If you need to roll back to a previous schema version:

```bash
# Check current schema state
atlas migrate status --env globaldb --config tools/ari-cli/atlas.hcl

# Roll back to specific version (if reversible)
atlas migrate down --env globaldb --config tools/ari-cli/atlas.hcl --to-version <version>
```

For irreversible migrations, restore from backup:

```bash
# Backup before migration
cp ~/.ari/ari.db ~/.ari/ari.db.backup

# Restore if needed
cp ~/.ari/ari.db.backup ~/.ari/ari.db
```
