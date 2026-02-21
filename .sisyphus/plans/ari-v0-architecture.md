# Ari v0 Architecture Plan

> Every project is a world. Ari helps you navigate it.

## Overview

Ari is a headless CLI for agentic orchestration. It provides:
- A **world** — persistent project state (decisions, plans, architecture understanding)
- A **guide** — the agent presence that navigates the world with you
- A **protocol** — LSP-style structured output for UI plugins to consume

**Design Principle**: Ari optimizes for *understanding*, not output speed.

---

## 1. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         UI Layer (External)                      │
│    OpenCode Plugin │ Neovim Plugin │ Web UI │ Raw CLI Output    │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼ LSP-style Protocol (JSON events)
┌─────────────────────────────────────────────────────────────────┐
│                         Ari CLI (This Repo)                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │   Command    │  │    Agent     │  │    World Manager     │  │
│  │   Surface    │  │    Loop      │  │                      │  │
│  │              │  │              │  │  ┌────────────────┐  │  │
│  │  init        │  │  ┌────────┐  │  │  │    SQLite      │  │  │
│  │  plan        │──│  │ LLM    │  │  │  │    (queries)   │  │  │
│  │  build       │  │  │ Client │  │  │  └────────────────┘  │  │
│  │  review      │  │  └────────┘  │  │  ┌────────────────┐  │  │
│  │  ask         │  │      │       │  │  │    JSON        │  │  │
│  │              │  │      ▼       │  │  │    (git sync)  │  │  │
│  │              │  │  Provider    │  │  └────────────────┘  │  │
│  │              │  │  Interface   │  │                      │  │
│  └──────────────┘  └──────────────┘  └──────────────────────┘  │
│                              │                                   │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │                    VCS Detector                           │  │
│  │         Git │ JJ │ None (graceful degradation)           │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │   .ari/         │
                    │   World State   │
                    └─────────────────┘
```

---

## 2. Core Components

### 2.1 Command Surface (v0 Contract)

| Command | Purpose | Input | Output |
|---------|---------|-------|--------|
| `ari init` | Start/discover a world | Path (default: `.`) | World manifest |
| `ari plan` | Explore idea space | Prompt or file | Plan document |
| `ari build` | Execute against plan | Plan ID or `--latest` | Execution log |
| `ari review` | Understand what changed | Revision range | Change summary |
| `ari ask` | Interrogate the world | Question | Answer + sources |

### 2.2 Agent Loop

The agent loop is the core state machine:

```
┌─────────┐     ┌──────────┐     ┌───────────┐     ┌─────────┐
│  IDLE   │────▶│ THINKING │────▶│  ACTING   │────▶│ WAITING │
└─────────┘     └──────────┘     └───────────┘     └─────────┘
     ▲                                                 │
     └─────────────────────────────────────────────────┘
```

**States**:
- `IDLE` — No active operation, waiting for command
- `THINKING` — LLM is processing, emitting thought events
- `ACTING` — Executing tool calls, file operations
- `WAITING` — Human-in-the-loop, awaiting user input

**Events emitted** (LSP-style):
```json
{"type": "state_change", "state": "thinking", "timestamp": "..."}
{"type": "thought", "content": "...", "timestamp": "..."}
{"type": "tool_call", "tool": "read_file", "args": {...}}
{"type": "tool_result", "result": "..."}
{"type": "question", "id": "q1", "prompt": "...", "options": [...]}
{"type": "answer_received", "question_id": "q1", "answer": "..."}
{"type": "state_change", "state": "idle", "timestamp": "..."}
```

### 2.3 World Manager

The world is the persistent artifact. Structure:

```
.ari/
├── world.json          # Manifest: project name, created, version
├── world.db            # SQLite: decisions, plans, artifacts, knowledge
├── decisions/          # JSON mirror of decisions (git-friendly)
│   ├── 001-use-go.md
│   └── 002-sqlite-state.md
├── plans/              # JSON mirror of plans
│   └── active/
│       └── v0-architecture.json
└── sessions/           # Session logs (optional, configurable)
    └── 2026-02-21-init.jsonl
```

**SQLite Schema** (v0):

```sql
-- Core world info
CREATE TABLE world (
    key TEXT PRIMARY KEY,
    value TEXT
);

-- Decisions made (architectural, design, etc.)
CREATE TABLE decisions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    context TEXT,           -- What led to this decision
    consequences TEXT,      -- What this implies
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Plans (executable checklists)
CREATE TABLE plans (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    status TEXT NOT NULL,   -- draft, active, completed, abandoned
    content TEXT NOT NULL,  -- Markdown with checkboxes
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Knowledge graph (entities and relationships)
CREATE TABLE knowledge (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,     -- file, concept, person, dependency
    name TEXT NOT NULL,
    content TEXT,
    metadata TEXT,          -- JSON blob for type-specific data
    created_at TEXT NOT NULL
);

CREATE TABLE knowledge_relations (
    from_id TEXT NOT NULL,
    to_id TEXT NOT NULL,
    relation TEXT NOT NULL, -- depends_on, implements, references
    PRIMARY KEY (from_id, to_id, relation)
);

-- Session history
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    command TEXT NOT NULL,
    started_at TEXT NOT NULL,
    ended_at TEXT,
    events TEXT             -- JSONL of events
);
```

### 2.4 VCS Detector

Graceful degradation model:

```go
type VCSBackend interface {
    // Detection
    Name() string           // "git", "jj", "none"
    IsAvailable() bool
    
    // Context (for world enrichment)
    CurrentBranch() string
    RecentCommits(n int) []Commit
    ChangedFiles() []string
    
    // Optional operations (may return ErrNotSupported)
    CreateCommit(message string) error
    CreateBranch(name string) error
}
```

### 2.5 LLM Provider Interface

```go
type Provider interface {
    Name() string
    Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
    Stream(ctx context.Context, req CompletionRequest) (<-chan StreamEvent, error)
}

type CompletionRequest struct {
    SystemPrompt string
    Messages     []Message
    Tools        []Tool
    MaxTokens    int
}

type Message struct {
    Role    string // "user", "assistant", "system"
    Content string
}

type StreamEvent struct {
    Type    string // "delta", "tool_call", "done", "error"
    Content string
    ToolCall *ToolCall
}
```

**Initial providers**: OpenAI, Anthropic (implement later)

---

## 3. Protocol Specification

### 3.1 Event Types

All output goes to stdout as line-delimited JSON. Stderr is for logs/errors.

**Lifecycle Events**:
```json
{"type": "session_start", "session_id": "ses_...", "command": "plan"}
{"type": "session_end", "session_id": "ses_...", "status": "success|error|cancelled"}
```

**State Events**:
```json
{"type": "state_change", "from": "idle", "to": "thinking"}
```

**Thought Events**:
```json
{"type": "thought", "content": "Let me analyze the existing structure..."}
```

**Tool Events**:
```json
{"type": "tool_call", "id": "tc_1", "tool": "read_file", "args": {"path": "README.md"}}
{"type": "tool_result", "id": "tc_1", "result": "...", "error": null}
```

**Human-in-the-Loop**:
```json
{
  "type": "question",
  "id": "q_1",
  "prompt": "Which approach do you prefer?",
  "options": [
    {"id": "a", "label": "SQLite only", "description": "..."},
    {"id": "b", "label": "SQLite + JSON", "description": "..."}
  ],
  "allows_custom": true
}
```
```json
{"type": "answer", "question_id": "q_1", "answer": "b", "custom": null}
```

**World Events**:
```json
{"type": "decision_created", "id": "dec_1", "title": "Use Go for CLI"}
{"type": "plan_created", "id": "plan_1", "title": "v0 Architecture"}
{"type": "plan_progress", "id": "plan_1", "completed": 3, "total": 10}
```

### 3.2 CLI Flags

```bash
# Output format
ari plan --format=json        # LSP-style events (default)
ari plan --format=markdown    # Human-readable markdown
ari plan --format=silent      # Only errors to stderr

# Session control
ari plan --session=ses_abc    # Resume session
ari plan --non-interactive    # Fail on questions instead of prompting

# Provider config
ari plan --provider=openai --model=gpt-4
ari plan --provider=anthropic --model=claude-3

# World location
ari plan --world=./.ari       # Default
ari plan --world=/tmp/test-world
```

---

## 4. Implementation Phases

### Phase 1: Foundation
- [ ] Define Go module structure and packages
- [ ] Implement protocol types (events, messages)
- [ ] Implement world manager (SQLite + JSON mirror)
- [ ] Implement VCS detector
- [ ] Add basic tests for each component

### Phase 2: Command Surface
- [ ] Implement `ari init` (create world, detect VCS, emit events)
- [ ] Implement `ari ask` (query world without LLM)
- [ ] Add command tests

### Phase 3: Agent Loop
- [ ] Define provider interface
- [ ] Implement mock provider for testing
- [ ] Implement agent state machine
- [ ] Wire `ari plan` to agent loop
- [ ] Add agent tests

### Phase 4: Full Commands
- [ ] Complete `ari plan` with real LLM integration
- [ ] Implement `ari build`
- [ ] Implement `ari review`
- [ ] Implement `ari ask` with LLM

### Phase 5: Providers & Polish
- [ ] Add OpenAI provider
- [ ] Add Anthropic provider
- [ ] Error handling and edge cases
- [ ] Documentation

---

## 5. Package Structure

```
tools/ari-cli/
├── main.go
├── go.mod
├── go.sum
├── cmd/
│   ├── root.go
│   ├── init.go
│   ├── plan.go
│   ├── build.go
│   ├── review.go
│   └── ask.go
├── internal/
│   ├── agent/
│   │   ├── loop.go         # State machine
│   │   ├── loop_test.go
│   │   └── state.go
│   ├── protocol/
│   │   ├── events.go       # Event types
│   │   ├── emitter.go      # JSON output
│   │   └── events_test.go
│   ├── world/
│   │   ├── manager.go      # CRUD operations
│   │   ├── sqlite.go       # SQLite implementation
│   │   ├── mirror.go       # JSON sync
│   │   ├── schema.go       # SQL schema
│   │   └── manager_test.go
│   ├── vcs/
│   │   ├── detector.go     # Interface + factory
│   │   ├── git.go
│   │   ├── jj.go
│   │   ├── none.go
│   │   └── detector_test.go
│   └── provider/
│       ├── interface.go
│       ├── mock.go
│       ├── openai.go       # Phase 5
│       └── anthropic.go    # Phase 5
└── README.md
```

---

## 6. Testing Strategy

- **Unit tests**: Each package in isolation with mocks
- **Integration tests**: Commands against test worlds
- **Golden tests**: Protocol event sequences

```go
// Example: testing init command
func TestInitCreatesWorld(t *testing.T) {
    tmp := t.TempDir()
    cmd := exec.Command("ari", "init", tmp)
    output, _ := cmd.StdoutPipe()
    
    cmd.Start()
    
    // Parse JSON events
    decoder := json.NewDecoder(output)
    var events []Event
    for decoder.More() {
        var e Event
        decoder.Decode(&e)
        events = append(events, e)
    }
    
    // Verify event sequence
    assert.Equal(t, "session_start", events[0].Type)
    assert.Equal(t, "decision_created", events[1].Type)
    
    // Verify world was created
    assert.FileExists(t, filepath.Join(tmp, ".ari", "world.db"))
}
```

---

## 7. Dependencies

```go
// go.mod
require (
    github.com/spf13/cobra v1.8.1      // CLI framework (already in)
    github.com/mattn/go-sqlite3 v1.14. // SQLite driver
)
```

---

## 8. Open Questions (resolve during implementation)

- [ ] How to handle concurrent CLI invocations? (Lock file in .ari/?)
- [ ] Session persistence: always on, or opt-in?
- [ ] Knowledge graph population: automatic or manual?
- [ ] World migrations: how to handle schema changes?

---

## 9. Success Criteria for v0

1. `ari init` creates a valid world with VCS detection
2. `ari ask "what is this project?"` returns useful answer from world state
3. `ari plan "add auth"` produces a plan document
4. All events parse as valid JSON
5. `go test ./...` passes
6. Works without any LLM provider configured (read-only mode)
