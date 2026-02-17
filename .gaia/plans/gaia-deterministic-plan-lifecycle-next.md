# Next Focus: Deterministic Plan Lifecycle

> Status: Active next focus
> Date: 2026-02-18
> Scope anchor: `.gaia/plans/project-gaia-plugin-mvp-cut.md`

## Why this is next

Recent manual testing showed GAIA can skip into execution behavior without reliably entering a
deterministic planning flow first. Prompt guidance alone is not enough for consistent UX.

The next checkpoint is to move planning lifecycle control into explicit command/tool-driven state
transitions.

## Current Context

- Runtime journal and reducers exist (`.gaia/runtime/{session}/{work_unit}.ndjson`, state reducers).
- Stream/session groundwork exists for pivot/fork/stack flows.
- Manual TUI sandbox workflow exists and is now the preferred way to feel-test behavior.
- GAIA orchestration loop is currently defined mostly by prompt instruction, not by a strict
  lifecycle command protocol.

## Target Outcome

Add a deterministic plan lifecycle with explicit entry points:

1. `gaia-start-plan`
2. `gaia-execute-plan`
3. `gaia-continue-work`

These commands should drive state transitions and user checkpoints, not rely on implied behavior.

## Proposed Lifecycle States

- `planning_in_progress`
- `planning_needs_input`
- `planning_ready`
- `executing`
- `blocked_waiting_human`
- `paused`
- `completed`

Execution should only begin from `planning_ready` (or explicit continue paths that are already in
`executing`/`paused`).

## Deterministic Session Intake

`gaia-start-plan` should capture once per session:

- stream/intent name (or selected existing stream)
- collaboration mode for this session (`supervised`, `checkpoint`, `agentic`)
- risk posture (`low`, `medium`, `high`)
- current focus scope (what we are looking at now)
- unresolved questions to clear before execution

## Human-in-the-Loop Behavior to Decide

Open decision intentionally deferred for next checkpoint:

- Should `gaia-execute-plan` require explicit confirmation for **all** risk levels,
  or only medium/high?

## Source-of-Truth Rule

- Runtime journals remain the source of truth.
- DEMETER docs (`status.md`, plan summaries) are deterministic projections from journal/state.
- Crash recovery must reconstruct active state from journal replay.

## Immediate Acceptance Checks

- Running `gaia-start-plan` always yields one of: `needs_input`, `ready`, or `resumed`.
- Running `gaia-execute-plan` without ready state fails with clear reason and next action.
- Running `gaia-continue-work` after interruption reconstructs session/stream state and resumes
  deterministically.
- Manual TUI testing in sandbox can walk through start -> execute -> continue without ambiguous
  flow.
