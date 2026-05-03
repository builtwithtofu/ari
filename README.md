# Ari

> **Headless agentic runtime for software development.**

---

## What is Ari?

Ari is an agentic runtime, an engine that powers intelligent software development workflows.

**Core concepts:**
- **Work isolation**: Multiple agents can work on the same project without collision
- **DAG of branching work**: Not just linear tasks, fork, merge, and parallelize
- **Protocol-based**: JSON event stream for any client to consume
- **CLI is the first client**: Proving the engine works

Every project you touch with Ari becomes a **world**. A living, structured representation of what exists, what was decided, what is planned, and what remains unknown. The world grows as you work. It persists between sessions. It can be handed to a new collaborator, picked up after months away, or interrogated when you've forgotten why something was built the way it was.

---

## Current State (v0)

**What's working:**
- Event-based protocol (17 event types)
- Agent loop (research, question, refine)
- DAG-based plan execution
- World persistence (SQLite)
- CLI commands: `init`, `plan`, `build`, `review`, `ask`
- Headless mode (`--headless` flag for machine consumption)

**What's planned:**
- Work isolation for parallel agent tasks
- Multiple clients (TUI, IDE plugins, web)
- Full stdin/JSON protocol mode
- Vector-based semantic search

---

## Development and verification

Run all project tooling through Nix so local behavior matches CI:

```bash
# Full verification gate
nix develop -c just verify

# Targeted Go test runs
nix develop -c go test ./...
```

For migration-related checks, run them from `nix develop` as well so Atlas and SQLite tool versions are consistent.

### Agent harness smoke checks

Default verification never requires provider credentials, network access, or billable model calls. To check locally installed harness command assumptions, run:

```bash
nix develop -c just agent-smoke
```

The smoke target only runs metadata probes:

- `codex --version`
- `claude --version`
- `opencode --version`

Ari resolves these command names at runtime unless you set explicit overrides:

```bash
ARI_CODEX_EXECUTABLE=/path/to/codex \
ARI_CLAUDE_EXECUTABLE=/path/to/claude \
ARI_OPENCODE_EXECUTABLE=/path/to/opencode \
  nix develop -c just agent-smoke
```

Fixture tests are the default adapter contract tests. `agent-smoke` is a credential-free local binary check. Authenticated model-call integration tests are intentionally separate and must stay opt-in.

## Agent runtime surfaces

The current Go runtime exposes profile-driven local agent runs through JSON-RPC and CLI commands under `tools/ari-cli/`.

- Ari is headless first: the daemon/API owns product behavior and state; CLI and future UI surfaces are clients.
- Onboarding: `ari init` renders the daemon-owned `init.state`, `init.options`, and `init.apply` flow. The only day-one choice is the default harness.
- Workspaces: the daemon owns workspace creation and resolution. Init creates a normal `$HOME` workspace as a starter landing place when possible.
- Helpers: each workspace can have an ordinary profile named `helper`. Home and project helpers share one helper contract; scope comes from workspace context, not from a profile role/type field.
- Ari tools: helper-visible settings/profile/self-check/run-forensics actions are daemon-owned tool calls with scoped metadata. Writes require explicit, single-use approval markers.
- Profiles: `ari profile create|list|show|defaults` maps to `agent.profile.create|get|list`.
- Temporary visibility: `ari agent list` hides temporary agents; `ari agent list --show-temporary` includes them with a `CLASS` label.
- Final responses: `ari final-response show --run-id <run>` reads the first-class final-response artifact, while `ari final-response export --run-id <run>` prints only shareable final text without transcript, hidden context, or provider-private metadata.
- Telemetry: `ari telemetry rollup --workspace-id <workspace>` reports local run counts and known/unknown token, cost, duration, and process facts without guessing missing values.

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                        Clients                          │
│    ┌──────┐  ┌──────┐  ┌─────────┐  ┌───────────────┐  │
│    │ CLI  │  │ TUI  │  │ IDE     │  │ Web/Remote    │  │
│    └──┬───┘  └──┬───┘  └────┬────┘  └───────┬───────┘  │
└───────┼─────────┼───────────┼───────────────┼──────────┘
        │         │           │               │
        ▼         ▼           ▼               ▼
┌─────────────────────────────────────────────────────────┐
│              JSON Event Protocol (JSONL)                │
│         agent_start, plan_created, task_progress,       │
│         question_asked, file_written, build_complete... │
└───────────────────────────┬─────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                      Ari Runtime                        │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  │
│  │ Agent Loop  │  │ Plan DAG    │  │ World Store     │  │
│  └─────────────┘  └─────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

The runtime is headless by design. The CLI is just the first client. Visualization, rendering, prompts, and UI are client concerns; product behavior belongs behind daemon JSON-RPC/service methods first. Ari works in CI, in Docker, over SSH, piped between processes, or driven by other agents.

---

## How it works

```bash
ari init                                  # choose a default harness and ensure a normal home workspace/helper
ari agent spawn --workspace home -- \
  "Teach me how Ari profiles work."       # ask the home helper about Ari
ari workspace create my-app               # create a project workspace with a project helper when defaults exist
ari agent spawn --workspace my-app -- \
  "Tell me about this project."           # ask the project helper from project context
```

Ari asks before she acts. Helpers teach, explain, diagnose, draft, route, and request approval; they do not write project source. Coding work belongs to explicitly configured specialist profiles such as builders, reviewers, or test writers. Ari does not install or authenticate external harnesses, poll provider model catalogs, or turn natural language into every CLI command.

---

## Headless Mode

For machine-readable output, use `--headless`:

```bash
# Stream events as JSONL
ari build --plan <id> --headless > events.jsonl
```

**Note:** Only `build` supports headless mode currently. Other commands require interactive input.

---

## The north star

Most tools optimize for *output*, more code, faster, with less friction.

Ari optimizes for *understanding*.

The bet is that the bottleneck in building things isn't writing code. It's navigating the world of ideas well enough to write the right code, in the right place, for the right reasons. Ari exists to close that gap, not by automating away the thinking, but by giving you a guide who can hold the map while you explore the terrain.

A world well understood is a world well built.
