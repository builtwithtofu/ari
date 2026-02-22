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

The runtime is headless by design. The CLI is just the first client. Visualization, rendering, and UI are client concerns. Ari works in CI, in Docker, over SSH, piped between processes, or driven by other agents.

---

## How it works

```bash
ari init          # start a new world, or discover an existing one
ari plan          # explore the idea space before committing to a path
ari build         # execute against a validated plan
ari review        # understand what changed and why
ari ask           # interrogate the world - what exists, what was decided
```

Ari asks before she acts. She plans before she builds. She surfaces what she doesn't know rather than guessing. The result is a guide you can trust, and a world you can navigate.

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
