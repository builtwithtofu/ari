# Execution Plan: Research Follow-up Sprint

> Status: Draft (ready for build execution)
> Created: 2026-02-17
> Scope anchor: `.gaia/plans/project-gaia-plugin-mvp-cut.md`

## Purpose

Convert recent research findings into a small, verifiable execution sequence that improves:

1. local pull-and-run developer experience,
2. agent instruction adherence in practice,
3. plan quality and plan-to-build gating behavior.

This plan uses one logical JJ commit per wave.

## Scope Guard

- Keep GAIA as optional orchestration mode.
- Keep native `plan` and `build` unchanged when GAIA mode is not selected.
- Keep portable plugin core separate from host wiring.
- Avoid out-of-scope full-pantheon expansion in this sprint.

## Wave 0 - Planning + Backlog Alignment

Objective:
- Align active backlog with research findings and define commit-sliced execution order.

Deliverables:
- Add wave items to `TODO.md`.
- Add this execution plan document.

Acceptance checks:
- Backlog has explicit wave entries for onboarding, adherence, planning, and evaluation.
- Plan references MVP boundary and commit cadence.

Commit boundary:
- Commit after docs/backlog updates are complete.
- Suggested message: `docs: add research follow-up execution waves`

## Wave 1 - Local Dev Onboarding Ergonomics

Objective:
- Make first local validation easy and reproducible for new contributors.

Deliverables:
- Add one-command harness happy path for bootstrap + confidence checks.
- Add preflight checks for required runtime/tooling signals.
- Update docs with expected outputs and recovery guidance.

Acceptance checks:
- A new contributor can run one command and get a clear pass/fail signal.
- Failures include actionable remediation guidance.

Commit boundary:
- One JJ commit for harness and docs changes in this wave.
- Suggested message: `feat(harness): streamline local onboarding flow`

## Wave 2 - Instruction-Adherence Hardening

Objective:
- Reduce drift between role instructions and runtime behavior.

Deliverables:
- Add runtime reminder/nudge mechanisms for orchestration flow.
- Add explicit correction messages for blocked mutation attempts.
- Add harness coverage for adherence behavior under realistic prompts.

Acceptance checks:
- GAIA permission-denied noise is lower in harness runs.
- Delegation/role behavior stays within expected boundaries in repeated runs.

Commit boundary:
- One JJ commit for guardrails and tests.
- Suggested message: `feat(gaia): add runtime adherence guardrails`

## Wave 3 - Plan Artifact + Gate Progression

Objective:
- Make planning artifacts actionable, auditable, and resumable.

Deliverables:
- Add structured plan template (objective, constraints, done criteria, risks).
- Add explicit plan-to-build gate checks by risk level.
- Add checkpoint shape for resume continuity.
- Add deterministic lifecycle commands for start/execute/continue planning flow.

Active design note:
- See `.gaia/plans/gaia-deterministic-plan-lifecycle-next.md` for current checkpoint context.
- See `.gaia/plans/gaia-hitl-coding-protocol-north-star.md` for operator-facing protocol north
  star and control-surface goals.

Acceptance checks:
- Plan outputs are consistently structured.
- Gate decisions are explicit and reproducible.
- Resume behavior can reconstruct current state from saved artifacts.

Commit boundary:
- One JJ commit for plan/gate primitives and tests.
- Suggested message: `feat(gaia): introduce plan gates and resumable checkpoints`

## Wave 4 - Orchestration Evaluation Track

Objective:
- Add repeatable regression confidence for orchestration quality.

Deliverables:
- Add harness scenarios for role adherence, delegation quality, and resume reliability.
- Add lightweight regression corpus for replay.
- Define metrics for pass/fail trend tracking.

Acceptance checks:
- Harness exposes deterministic scenarios for the new quality track.
- Regression corpus runs can detect behavior drift.

Commit boundary:
- One JJ commit for evaluation track and harness integration.
- Suggested message: `test(harness): add orchestration regression track`

## Execution Rhythm

- Work waves in order.
- Run validation at the end of each wave:
  - `bun run typecheck`
  - `bun test`
- Commit once per wave when that wave's acceptance checks pass.

## Out-of-Scope For This Sprint

- Full nine-agent roster enablement.
- Advanced parallel orchestration manager.
- Collaboration command matrix beyond MVP needs.
