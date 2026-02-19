# Project GAIA TODO

This backlog groups active work by delivery horizon.

## MVP Remaining (Current Phase)

- [ ] Finalize HITL supervision progression behavior:
  - `full_review` (approve each material action)
  - `checkpoint` (approve at defined boundaries)
  - `agentic` (minimal interruption with policy gates)
- [ ] Define and implement rejection rationale loop:
  - capture rejection
  - ask why
  - persist rationale
  - re-plan with explicit delta
- [ ] Implement interrupt/re-plan policy when user guidance arrives mid-execution.
- [x] Add recoverability runtime journal format (`.gaia/runtime/{session}/{work_unit}.ndjson`).
- [x] Add reducer that reconstructs current orchestration state from runtime journal events.
- [x] Define DEMETER status report contract for cross-subagent visibility.
- [ ] Integrate SDK v2 surfaces needed for permission/question/session/event flow.
- [ ] Add harness scenarios for rejection, interruption, and recovery/resume.
- [ ] Tune GAIA-to-HEPHAESTUS delegation scope so implementation tasks stay atomic and bounded.
- [x] Reduce GAIA permission-denied noise by tightening prompt behavior to avoid forbidden write/edit
      attempts.
- [ ] Run sandbox tests in a fresh isolated workspace for each run to keep evaluations reproducible.
- [ ] Add lean orchestration quality harness track:
  - deterministic lean subagent wiring smoke (`gaia` + hidden specialists)
  - repeatable prompt-quality checks for GAIA delegation decisions
  - lightweight regression corpus for GAIA orchestration outcomes
- [ ] Finalize operation profile behavior for agent selection:
  - keep `lean` default (GAIA + ATHENA + HEPHAESTUS + DEMETER)
  - keep `full` mode for full roster orchestration
  - add user-defined custom agent combinations where GAIA remains mandatory
  - validate custom profile inputs and fallback behavior
  - keep specialist visibility hidden by default and project-configurable
- [ ] Enforce opt-in orchestration behavior end-to-end:
  - GAIA mode must activate only when explicitly selected
  - native OpenCode `plan` and `build` flows stay unchanged by default
  - when GAIA is not selected, plugin behavior should be effectively out of the way

## Research Follow-up Sprint (Commit-Sliced)

- [x] Wave 1: local developer onboarding ergonomics
  - add one-command happy path in harness for first pull-and-run validation
  - add preflight checks for CLI/runtime/dependency readiness
  - document expected output and failure recovery in setup docs
  - commit boundary: one JJ commit for harness + docs onboarding flow
- [x] Wave 2: instruction-adherence hardening for GAIA roles
  - add runtime nudges that keep orchestration behavior in-lane during long sessions
  - add explicit corrective guidance when GAIA attempts blocked edit/write paths
  - add harness checks that validate reduced permission-denied churn
  - commit boundary: one JJ commit for behavior guardrails + tests
- [ ] Wave 3: plan artifact and gate progression
  - add a structured plan template for objective, constraints, done criteria, and risks
  - add plan-to-build gate checks for low/medium/high-risk work units
  - add resumable checkpoint recording shape for plan continuity
  - add deterministic GAIA command flow for plan lifecycle:
    - `gaia-start-plan` (intake + plan bootstrap)
    - `gaia-execute-plan` (gated execution entry)
    - `gaia-continue-work` (crash-safe resume)
  - keep companion CLI query-first for `.gaia/` state navigation by users and GAIA agents
  - make DEMETER status docs the projection of runtime journals (not primary source of truth)
  - record one-time session policy for human-loop mode and risk posture
  - open decision to finalize: does low-risk execution require explicit human confirmation?
  - lock protocol north star before deeper implementation:
    - `.gaia/plans/gaia-hitl-coding-protocol-north-star.md`
  - commit boundary: one JJ commit for plan/gate primitives + tests
- [ ] Wave 4: orchestration evaluation and regression corpus
  - add repeatable orchestration quality scenarios to harness
  - track role adherence, delegation quality, and resume success signals
  - add a lightweight baseline corpus for regression replay
  - commit boundary: one JJ commit for evaluation track + harness wiring

## Post-MVP (Near-Term)

- [ ] Add stronger collaboration profile controls (`/profile`, cadence, review depth).
- [ ] Add richer policy controls for checkpoint thresholds and escalation.
- [ ] Add model preset strategy (budget/balanced/premium) based on observed usage.
- [ ] Expand DEMETER summaries with compact trend and risk snapshots.

## Later Revisions

- [ ] Expand from lean to fuller pantheon coverage.
- [ ] Support pantheon expansion with additional Greek gods where role boundaries remain clear and
      testable.
- [ ] Add user-defined named specialist agents beyond built-in roster while keeping GAIA mandatory
      as the orchestrator.
- [ ] Add advanced parallel orchestration manager.
- [ ] Add deeper replay/debug tooling over runtime journals.
- [ ] Add evaluation benchmarks for orchestration quality and HITL outcomes.

## Guardrails and Architecture Notes

- [ ] Keep plugin core portable and host-agnostic.
- [ ] Keep project-specific docs in `doc/` and operational artifacts in `.gaia/`.
- [ ] Keep plugin defaults project-agnostic. Do not hardcode this repository's
      specification path as a plugin default.
- [ ] Keep plugin self-contained: no required dependency on repository-local docs for core behavior.
