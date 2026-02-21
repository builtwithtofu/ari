# Protocol Spec (Ariadne v0)

This document defines the event-based protocol for headless communication between Ari CLI and external clients. The protocol uses JSONL framing over stdio.

## Event Envelope

All events share a common envelope structure. Each line is a complete JSON object.

```json
{
  "type": "<event_type>",
  "timestamp": "<ISO8601>",
  "data": { ... }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | yes | Event type identifier (see Event Types) |
| `timestamp` | string | yes | ISO 8601 timestamp of event emission |
| `data` | object | no | Type-specific payload |

## JSONL Framing

- Each event is a single line of valid JSON
- Lines are newline-delimited (`\n`)
- Clients parse line by line, never buffer across newlines
- Empty lines should be ignored
- Protocol streams over stdout; stderr reserved for logs/errors

### Streaming Example

```jsonl
{"type":"session_start","timestamp":"2026-02-22T14:30:00Z","data":{"session_id":"sess-7f3a9b2c","command":"plan"}}
{"type":"state_change","timestamp":"2026-02-22T14:30:01Z","data":{"from":"idle","to":"discovery"}}
{"type":"thought","timestamp":"2026-02-22T14:30:05Z","data":{"content":"Analyzing existing auth patterns..."}}
{"type":"plan_created","timestamp":"2026-02-22T14:31:22Z","data":{"id":"plan-abc123","title":"Add JWT authentication"}}
{"type":"session_end","timestamp":"2026-02-22T14:31:30Z","data":{"session_id":"sess-7f3a9b2c","status":"success"}}
```

## Event Types

Event types are defined in `internal/protocol/events.go`. The following types are supported in v0.

### Lifecycle Events

| Type | Description | Data Fields |
|------|-------------|-------------|
| `session_start` | Session initialized | `session_id`, `command` |
| `session_end` | Session terminated | `session_id`, `status` |

### State Events

| Type | Description | Data Fields |
|------|-------------|-------------|
| `state_change` | Agent state transition | `from`, `to` |

### Thought Events

| Type | Description | Data Fields |
|------|-------------|-------------|
| `thought` | Agent reasoning output | `content` |

### Tool Events

| Type | Description | Data Fields |
|------|-------------|-------------|
| `tool_call` | Tool invocation started | `id`, `tool`, `args` |
| `tool_result` | Tool execution completed | `id`, `result`, `error?` |

### Human-in-the-Loop Events

| Type | Description | Data Fields |
|------|-------------|-------------|
| `question` | Prompt for human input | `id`, `prompt`, `options[]`, `allows_custom` |
| `answer` | Human response submitted | `question_id`, `answer`, `custom?` |
| `answer_received` | Answer acknowledged by agent | (none) |

### World Events

| Type | Description | Data Fields |
|------|-------------|-------------|
| `decision_created` | Decision recorded | `id`, `title` |
| `plan_created` | Plan generated | `id`, `title` |
| `plan_progress` | Step completion update | `id`, `completed`, `total` |

## Data Payloads

### SessionStartEvent

```json
{
  "type": "session_start",
  "timestamp": "2026-02-22T14:30:00Z",
  "data": {
    "session_id": "sess-7f3a9b2c",
    "command": "plan"
  }
}
```

### SessionEndEvent

```json
{
  "type": "session_end",
  "timestamp": "2026-02-22T14:31:30Z",
  "data": {
    "session_id": "sess-7f3a9b2c",
    "status": "success"
  }
}
```

Status values: `success`, `error`, `cancelled`

### StateChangeEvent

```json
{
  "type": "state_change",
  "timestamp": "2026-02-22T14:30:01Z",
  "data": {
    "from": "idle",
    "to": "discovery"
  }
}
```

Common states: `idle`, `discovery`, `planning`, `validation`, `executing`, `waiting_approval`

### ThoughtEvent

```json
{
  "type": "thought",
  "timestamp": "2026-02-22T14:30:05Z",
  "data": {
    "content": "Analyzing existing auth patterns in the codebase..."
  }
}
```

### ToolCallEvent

```json
{
  "type": "tool_call",
  "timestamp": "2026-02-22T14:30:10Z",
  "data": {
    "id": "call-001",
    "tool": "read",
    "args": {
      "path": "internal/middleware/auth.go"
    }
  }
}
```

### ToolResultEvent

```json
{
  "type": "tool_result",
  "timestamp": "2026-02-22T14:30:11Z",
  "data": {
    "id": "call-001",
    "result": {"lines": 150, "content": "..."},
    "error": ""
  }
}
```

The `error` field is omitted or empty on success.

### QuestionEvent

```json
{
  "type": "question",
  "timestamp": "2026-02-22T14:35:00Z",
  "data": {
    "id": "q-001",
    "prompt": "Which authentication provider should we use?",
    "options": [
      {"id": "jwt", "label": "JWT", "description": "Stateless token-based auth"},
      {"id": "oauth", "label": "OAuth 2.0", "description": "Delegated authorization"}
    ],
    "allows_custom": true
  }
}
```

### AnswerEvent

```json
{
  "type": "answer",
  "timestamp": "2026-02-22T14:36:00Z",
  "data": {
    "question_id": "q-001",
    "answer": "jwt",
    "custom": ""
  }
}
```

### PlanCreatedEvent

```json
{
  "type": "plan_created",
  "timestamp": "2026-02-22T14:31:22Z",
  "data": {
    "id": "plan-abc123",
    "title": "Add JWT authentication"
  }
}
```

### PlanProgressEvent

```json
{
  "type": "plan_progress",
  "timestamp": "2026-02-22T14:32:00Z",
  "data": {
    "id": 2,
    "completed": 2,
    "total": 5
  }
}
```

## Deterministic Ordering

Events are emitted in strict chronological order within a session. Clients can rely on:

1. **Timestamp ordering**: Events are sorted by `timestamp` within a session
2. **Causal ordering**: `tool_result` always follows its corresponding `tool_call` (matched by `id`)
3. **State consistency**: `state_change` events reflect actual state machine transitions

No event reordering is required by clients. Parse and process in received order.

## Versioning

v0 protocol does not include an explicit version field in the envelope. Protocol version is implicit to the Ari CLI release. Future versions may add:

- `version` field in envelope for backward-compatible evolution
- `capabilities` negotiation on session start
- Event schema registry for validation

Clients should ignore unknown event types to support forward compatibility.

## CI/Non-Interactive Behavior

When running in CI or with `--non-interactive` flag:

- No blocking prompts are emitted
- Unresolved approval gates cause CLI to exit with **code 3** (approval required)
- Clients can poll `ari status --format json` to detect `waiting_approval` state
- Human approval is handled via explicit commands: `ari approve <op-id>` or `ari reject <op-id> --feedback "..."`

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Validation/user input error |
| 3 | Approval required (gated state) |
| 4 | Conflict detected |
| 5 | Provider/network failure |
| 6 | State/recovery corruption |

### CI Detection Pattern

```bash
ari build --non-interactive
exit_code=$?

if [ $exit_code -eq 3 ]; then
  echo "Approval required. Polling for human action..."
  while true; do
    status=$(ari status --format json | jq -r '.status')
    if [ "$status" != "waiting_approval" ]; then
      break
    fi
    sleep 5
  done
  ari resume
fi
```

## Session Flow Example

Complete session from init to completion:

```jsonl
{"type":"session_start","timestamp":"2026-02-22T14:30:00Z","data":{"session_id":"sess-7f3a9b2c","command":"plan"}}
{"type":"state_change","timestamp":"2026-02-22T14:30:01Z","data":{"from":"idle","to":"discovery"}}
{"type":"thought","timestamp":"2026-02-22T14:30:05Z","data":{"content":"Scanning codebase for auth patterns..."}}
{"type":"tool_call","timestamp":"2026-02-22T14:30:10Z","data":{"id":"call-001","tool":"glob","args":{"pattern":"**/*auth*"}}}
{"type":"tool_result","timestamp":"2026-02-22T14:30:11Z","data":{"id":"call-001","result":{"files":["internal/auth/middleware.go","internal/auth/token.go"]}}}
{"type":"state_change","timestamp":"2026-02-22T14:30:30Z","data":{"from":"discovery","to":"planning"}}
{"type":"plan_created","timestamp":"2026-02-22T14:31:22Z","data":{"id":"plan-abc123","title":"Add JWT authentication"}}
{"type":"plan_progress","timestamp":"2026-02-22T14:31:23Z","data":{"id":1,"completed":0,"total":5}}
{"type":"state_change","timestamp":"2026-02-22T14:31:24Z","data":{"from":"planning","to":"validation"}}
{"type":"thought","timestamp":"2026-02-22T14:31:30Z","data":{"content":"Plan validated. Ready for approval."}}
{"type":"state_change","timestamp":"2026-02-22T14:31:31Z","data":{"from":"validation","to":"waiting_approval"}}
{"type":"session_end","timestamp":"2026-02-22T14:31:32Z","data":{"session_id":"sess-7f3a9b2c","status":"success"}}
```

After approval:

```jsonl
{"type":"session_start","timestamp":"2026-02-22T14:35:00Z","data":{"session_id":"sess-7f3a9b2c","command":"build"}}
{"type":"state_change","timestamp":"2026-02-22T14:35:01Z","data":{"from":"waiting_approval","to":"executing"}}
{"type":"plan_progress","timestamp":"2026-02-22T14:35:10Z","data":{"id":1,"completed":1,"total":5}}
{"type":"plan_progress","timestamp":"2026-02-22T14:35:45Z","data":{"id":2,"completed":2,"total":5}}
{"type":"plan_progress","timestamp":"2026-02-22T14:36:20Z","data":{"id":3,"completed":3,"total":5}}
{"type":"plan_progress","timestamp":"2026-02-22T14:36:55Z","data":{"id":4,"completed":4,"total":5}}
{"type":"plan_progress","timestamp":"2026-02-22T14:37:30Z","data":{"id":5,"completed":5,"total":5}}
{"type":"state_change","timestamp":"2026-02-22T14:37:31Z","data":{"from":"executing","to":"completed"}}
{"type":"session_end","timestamp":"2026-02-22T14:37:32Z","data":{"session_id":"sess-7f3a9b2c","status":"success"}}
```

## Capabilities (Future)

v0 does not implement a capabilities negotiation mechanism. Future versions may introduce:

```json
{
  "type": "capabilities",
  "timestamp": "...",
  "data": {
    "protocol_version": "1.0",
    "features": ["streaming", "questions", "progress"],
    "output_formats": ["json", "jsonl", "markdown"]
  }
}
```

Clients should be prepared to handle unknown event types gracefully for forward compatibility.

## Implementation Reference

Event types and payloads are defined in:
- `tools/ari-cli/internal/protocol/events.go`

Session state machine is documented in:
- `docs/session-lifecycle.md`

Plan schema is documented in:
- `docs/plan-schema.md`
