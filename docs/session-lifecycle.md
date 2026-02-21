# Session Lifecycle (Ariadne v0)

This document defines session operations, state transitions, and human-in-the-loop behavior for Ariadne v0. Sessions represent active work contexts within a project.

## Session Operations

Sessions are tmux-like work contexts that persist across CLI invocations. Each session tracks an operation DAG and current execution state.

| Command | Description |
|---------|-------------|
| `ari sessions` | List all active sessions with status |
| `ari attach <session-id>` | Attach to a running session's context |
| `ari status` | Show overview of all sessions across projects |
| `ari kill <session-id>` | Terminate an active session |
| `ari resume <session-id>` | Resume from last completed step boundary |

### Operation Descriptions

- **list**: Query the global registry for sessions matching current project or all projects with `--all`. Output includes session ID, status, elapsed time, and current operation.
- **attach**: Switch CLI context to a running session. Subsequent commands operate within that session's operation DAG until detached.
- **detach**: Implicit when running commands without `--session` flag or when switching projects. Sessions remain active in background.
- **kill**: Mark session as terminated. Operation DAG is preserved for recovery. Active operations are cancelled gracefully.
- **resume**: Continue execution from the last completed step. Used after failures or explicit pauses.

## State Transitions

Sessions follow the v0 execution contract state machine. Each session has a status that drives available operations.

### Session Status Values

| Status | Description | Next States |
|--------|-------------|-------------|
| `running` | Actively executing steps | `waiting`, `completed`, `failed` |
| `waiting_approval` | Blocked on human approval | `running`, `rejected` |
| `completed` | All steps finished successfully | Terminal |
| `rejected` | Human rejected the work | Terminal |
| `failed` | Execution error, resumable | `running` (via resume) |
| `killed` | Explicitly terminated by user | Terminal |

### State Transition Diagram

```
running ──────────────────> waiting_approval ──[approve]──> running
   │                              │
   │                         [reject]
   │                              │
   │                              v
   │                          rejected
   │
   ├──[error]──> failed ──[resume]──> running
   │                  │
   │                  └──[non-resumable]──> killed
   │
   ├──[kill]───────────────────────> killed
   │
   └──[complete]───────────────────> completed
```

### Step-Level Status Alignment

Within a session, individual steps follow their own status progression aligned with plan schema:

```
planned -> waiting_approval -> approved -> executing -> completed
                 |                |
                 v                v
             rejected          failed -> resumed -> executing
```

## Deterministic HITL Behavior

Human-in-the-loop interactions are **non-blocking** and command-driven. No interactive prompts are used.

### Approval Commands

| Command | Description |
|---------|-------------|
| `ari approve <op-id>` | Approve a gated operation |
| `ari reject <op-id> --feedback "..."` | Reject with feedback |

### CI/Non-Interactive Mode

In CI or when `--non-interactive` is set:
- Unresolved approval gates return **exit code 3** (approval required)
- No blocking wait for user input
- Scripts can poll `ari status --format json` to detect `waiting_approval` state

### Approval Flow

```
1. Agent reaches HUMAN_INPUT step
2. Session status -> waiting_approval
3. CLI exits with code 3 (if non-interactive)
4. Human runs: ari approve <op-id> OR ari reject <op-id> --feedback "..."
5. Session status -> running (approve) or rejected (reject)
6. Agent continues or halts based on decision
```

### Feedback Handling

When rejected, the feedback string is stored in the operation record and made available to agents:

```bash
ari reject op-abc123 --feedback "Missing rate limiting on auth endpoint"
```

Agents query feedback via `ari op show <op-id> --feedback` to understand rejection reasons.

## Storage Boundary

Session state is split between global and project-local storage. The **default** is global-only; project-local `.ari/` requires explicit opt-in.

### Storage Mode Policy

- **Default**: Global storage at `~/.config/ari`
- **Project-local**: `.ari/` directory is **opt-in only** via explicit configuration
- **Precedence**: CLI flag > environment variable > config file > default

Ari never creates `.ari/` implicitly. To enable project-local storage, set:

```bash
# Via CLI flag
ari --storage-mode project ...

# Via environment
ARI_STORAGE_MODE=project ari ...

# Via config (in ~/.config/ari/config.yaml)
storage_mode: project
```

### Global State (SQLite)

Path: `~/.config/ari/ari.db`

The global registry tracks sessions across all projects for cross-project visibility and telemetry.

```sql
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    project_id TEXT REFERENCES projects(id),
    op_id TEXT,
    status TEXT,
    started_at TEXT,
    ended_at TEXT
);
```

### Project-Local State (JSON)

Path: `<project>/.ari/` (opt-in only)

Project-local state is file-based, never SQLite. This keeps project artifacts git-syncable and human-readable.

```
.ari/
├── ops/                      # Operation DAG (jj-style recovery)
│   ├── op-{hash}.json
│   └── ...
├── views/                    # Content-addressed snapshots
├── current-op                # Current operation ID
├── plans/                    # Markdown plans
├── project.json              # Project config
├── agents.json               # Agent definitions
└── providers.json            # Provider config
```

### Session Record Example

```json
{
  "session_id": "sess-7f3a9b2c",
  "project_id": "proj-auth-service",
  "project_path": "/home/user/projects/auth-service",
  "op_id": "op-e8d4f1a2",
  "status": "waiting_approval",
  "current_step": "step-004",
  "started_at": "2026-02-22T14:30:00Z",
  "updated_at": "2026-02-22T14:45:22Z",
  "agent": "theseus",
  "goal": "Add rate limiting to authentication endpoints"
}
```

## Recovery Contract

Sessions support full recovery from any interruption point.

### Recovery Guarantees

1. **Operation log is append-only**: No data loss on crash
2. **Resume from step boundary**: `ari resume <session-id>` continues from last completed step
3. **Non-resumable marking**: Steps with external side effects that cannot be retried are marked `failed_non_resumable`

### Resume Behavior

```bash
# Session failed at step-003
ari sessions
#> sess-7f3a9b2c  failed  23m ago  "Add rate limiting"

# Resume from last checkpoint
ari resume sess-7f3a9b2c

# Execution continues from step-003
# If step-003 is marked failed_non_resumable, user is informed
```

## v0/v1 Boundaries

### Included in v0

- Session list, attach, kill, resume
- Deterministic approve/reject commands
- Global SQLite session registry
- Operation DAG recovery model

### Deferred to v1+

- `ari projects` command family for multi-project management
- Cross-project dependency tracking
- Session migration between projects
- Overlay filesystem isolation
