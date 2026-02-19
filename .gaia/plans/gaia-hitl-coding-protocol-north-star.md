# GAIA North Star: Human-in-the-Loop Coding Protocol

> Status: Active vision (pre-implementation lock)
> Date: 2026-02-19
> Scope anchor: `.gaia/plans/project-gaia-plugin-mvp-cut.md`

## Why this exists

GAIA should behave like a deterministic human-in-the-loop coding protocol, not only a prompt style.

The operator must be able to control GAIA in two equivalent surfaces:

1. a companion CLI (primary operator control plane), and
2. in OpenCode sessions (TUI/web) with the same lifecycle semantics.

## North Star Outcome

Build a dual-surface protocol where planning, execution, rejection handling, and resume behavior
are explicit, auditable, and crash-safe.

The same workflow should be available whether the operator starts from CLI or directly in OpenCode.

## Core Principles

- Deterministic state transitions over implied prompt behavior.
- Runtime journal as source of truth; docs/status are projections.
- GAIA owns rejection feedback loops; specialists pause on rejection.
- Human decisions are explicit at checkpoints and risk gates.
- Sandbox runs are inspectable but gitignored and disposable.
- Keep naming explicit: Project GAIA is the repo/product identity; Ariadne (`ari`) is the protocol CLI identity.

## CLI as protocol engine + query surface

The Ariadne CLI is both:

1. protocol engine for deterministic lifecycle transitions, and
2. query surface for reading canonical `.gaia/` state from both human and agent loops.

OpenCode remains the execution backend for agentic loops, but command semantics are defined by
the deterministic flow layer first.

## Control Surfaces

### Companion CLI (Cobra)

The CLI is the operator control plane for deterministic lifecycle control and context navigation.

Target command families:

- `ari flow start|iterate|execute|continue`
- `ari query all|sessions|session|lifecycle|surfaces`
- `ari status [--json]`
- `ari sandbox list|tui|web`

Legacy helpers remain available during transition:

- `ari plan start|execute`
- `ari work continue`

## Command contract (deterministic)

### `ari flow`

- `start`: initialize/refresh one lifecycle policy and flow snapshot.
- `iterate`: advance planning loop with new scope/feedback while preserving session identity.
- `execute`: move into execution only when lifecycle state and approval gates allow it.
- `continue`: resume deterministic execution from persisted state after interruption.

### `ari query`

- `all`: canonical envelope for automation (`runtime`, `lifecycle`, `flow`, `surfaces`).
- `sessions`: list runtime sessions with active/completed/blocked counts.
- `session`: show one runtime state payload.
- `lifecycle`: show deterministic lifecycle policy payload.
- `surfaces`: show public surface registry and stability lanes.

## Slim-by-default pantheon policy

- Default execution profile is slim and stable for planning loops.
- Current runtime default: `gaia`, `athena`, `hephaestus`, `demeter`.
- Extended/full pantheon remains opt-in and should not change default operator UX.
- Full roster experimentation is tracked as optional surfaces while MVP boundaries stay lean.

### OpenCode plugin/runtime

The plugin owns orchestration behavior and policy enforcement inside OpenCode.

Target capabilities:

- enforce lifecycle gates and state checks,
- route rejection feedback ownership to GAIA,
- produce deterministic runtime events and state artifacts,
- preserve native OpenCode defaults unless GAIA mode is selected.

## Protocol States (shared model)

- `planning_in_progress`
- `planning_needs_input`
- `planning_ready`
- `executing`
- `blocked_waiting_human`
- `paused`
- `completed`

Execution enters only from allowed states (`planning_ready`, `paused`, guarded resume paths).

## Session Intake Contract

`flow start` captures once per session:

- stream/intent
- collaboration mode (`supervised`, `checkpoint`, `agentic`)
- risk posture (`low`, `medium`, `high`)
- active scope for this session
- unresolved questions before execution

## Rejection Ownership Contract

When a specialist output is rejected:

1. specialist pauses,
2. GAIA requests feedback from the operator,
3. feedback is captured as a decision artifact,
4. GAIA replans and re-delegates.

Specialists do not run direct rejection follow-up loops with the operator.

## Runtime and Artifact Contract

Source-of-truth artifacts:

- `.gaia/runtime/{session}/{work_unit}.ndjson`
- `.gaia/runtime/{session}/state.json`
- `.gaia/lifecycle/{session}.json`

Projection artifacts:

- `.gaia/plans/session-<session>-status.md`
- `.gaia/streams/<stream>/status.md`
- `.gaia/streams/index.json`

## Sandbox Verification Contract

Sandbox workspaces remain gitignored and operator-inspectable under `.sandbox/workspaces/`.

Manual scenario set (seeded):

- `go-hello-planning/`
- `planning-challenge/`
- `refactor-sandbox/`
- `bug-hunt/`

Use these to validate protocol behavior, not only code generation quality.

## Testing contract

Confidence should come from deterministic state and contract tests before UI depth:

- state-machine tests for lifecycle and flow transitions,
- adapter contract tests at the OpenCode boundary,
- replay fixtures for regression,
- 3-6 smoke E2E scenarios for TUI/web validation only.

## Success Criteria

- Operator can run the same lifecycle from CLI or OpenCode session with consistent outcomes.
- `status --json` is stable for automation and human-readable for manual operation.
- Crash/restart replay reconstructs current stream/session and next command deterministically.
- Rejection flow consistently routes through GAIA-owned feedback capture.

## Open Decision (kept explicit)

- Should low-risk `flow execute` require explicit human confirmation, or only medium/high?

## Immediate Build Direction

1. Lock command semantics and output schema for `start`, `execute`, `continue`, `status`.
2. Complete stream/reject/recover command families in CLI.
3. Bind plugin hooks to the same protocol transitions.
4. Validate end-to-end in seeded sandbox scenarios via TUI/web and automated checks.
