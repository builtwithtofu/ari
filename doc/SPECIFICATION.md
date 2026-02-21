# Project GAIA Specification

> Status: Draft
> Scope: Product direction, human-in-the-loop orchestration model, and phased delivery plan
> Last updated: 2026-02-08

## Purpose

Define the product-level specification for Project GAIA so contributors can align on the north star,
the human-in-the-loop operating model, and the current execution plan.

This document is project-facing and durable.

Operational internals and run-specific details remain in `.gaia/`.

## North Star

GAIA is a human-in-the-loop orchestration system.

The goal is to turn imperfect specialist agents into a coherent pantheon that can deliver outcomes
across product work, not only code changes.

GAIA's primary responsibility is intent translation:

- convert user intent into clear outcomes,
- route work to the right specialists,
- maintain decision checkpoints,
- keep memory and rationale recoverable across sessions.

## Product Principles

- Human judgment remains central in supervised mode.
- Agentic mode can run with fewer interrupts but keeps the same safety and recovery rules.
- Specialists stay narrow and imperfect by design; orchestration quality is the value layer.
- Every meaningful decision should be traceable.
- Native OpenCode `plan` and `build` behavior must remain intact when GAIA mode is not active.
- GAIA is opt-in. If GAIA mode is not selected, orchestration behavior should stay out of the way.
- Portable plugin defaults must stay project-agnostic. Repository-specific documents are configured
  at project level, not hardcoded as global plugin defaults.
- This project is pre-alpha and greenfield; breaking changes are acceptable.

## Pantheon Direction

- The pantheon direction is Greek-first naming.
- GAIA remains the stable orchestrator identity.
- New specialist additions should prioritize clear role contracts over lore references.
- User-defined specialist names remain allowed as long as GAIA is the orchestrator.

## Pantheon Profile Policy

- Default operator profile is slim and should stay predictable.
- Current slim runtime profile is `gaia`, `athena`, `hephaestus`, `demeter`.
- Full roster behavior is opt-in and should not degrade slim defaults.
- Expansion beyond slim profile remains bounded by active MVP scope.

## Naming Contract

- Project identity: `Project GAIA`.
- Protocol identity: `Ariadne HITL Coding Protocol`.
- CLI identity: `ari` (with `gaia` as a compatibility alias during transition).
- Orchestrator identity in runtime remains `gaia`.

This keeps operator language, runtime behavior, and documentation aligned during pre-alpha.

## CLI Protocol Surface

The companion CLI is the deterministic protocol surface for both humans and agents.

GAIA flow semantics are defined in CLI/state contracts first, then executed through OpenCode runtime
adapters.

Canonical command families:

- `ari flow start|iterate|execute|continue`
- `ari query all|sessions|session|lifecycle|surfaces`
- `ari status --json` for automation-safe status projection

Legacy command families (`plan`, `work`) can remain as transition aliases while `flow` hardens.

## Core Roles

## GAIA (primary orchestrator)
- Intake and clarify user intent.
- Produce scoped work units with explicit done criteria.
- Decide delegation strategy (solo, parallel, chained).
- Drive checkpoints and approvals.
- Re-plan when user feedback changes direction.

## ATHENA (recon/routing)
- Map repository and context reality.
- Identify risk and routing recommendations.

## HEPHAESTUS (implementation)
- Execute implementation work units with strict quality expectations.

## DEMETER (memory/reporting)
- Record decisions, rationale, and impacts.
- Produce concise status updates across active subagents.
- Maintain recoverable state summaries for continuation.
- Favor fast/low-cost models when output quality remains factual and stable.

## Human Interface Model

- GAIA is the default human-facing entry point.
- Specialist gods are hidden from direct selection by default.
- Projects can explicitly configure specialist visibility when direct access is desired.
- Even when visible, specialist work should still route through GAIA policies.

## Collaboration Modes

## Supervised (default)
- Explicit checkpoints are required.
- Medium/high-risk actions require approval.
- Rejections must be captured and fed back into the next plan.

## Agentic
- Fewer interaction pauses.
- Same contracts and guardrails.
- Same recoverability and auditability requirements.

## Supervision Progression

Supervised collaboration should support a progression path as trust increases:

1. `full_review`: ask for approval on each material edit/action.
2. `checkpoint`: approve at defined boundaries rather than every step.
3. `agentic`: minimal interruption with policy-driven checkpoints.

GAIA should allow moving both directions between these levels at runtime.

## Human-in-the-Loop Feedback Loop

When the user rejects an action:

1. Record the rejection event.
2. GAIA asks a targeted follow-up question to understand why.
3. Store the answer as decision memory.
4. Generate a revised recommendation with explicit change rationale.

Decision handoff format:

- Context
- Options
- Recommendation
- Action needed

## Interrupt and Recovery Requirements

User updates arriving mid-execution are first-class signals.

GAIA should be able to:

- interpret new guidance while subagents are running,
- decide whether to continue, partially cancel, or re-plan,
- preserve continuity without losing history.

Recoverability requirements:

- retain per-work-unit artifacts under `.gaia/`,
- maintain compact runtime state records suitable for low-context resume,
- keep decision history machine-readable and human-reviewable.

## Testing Contract

Testing confidence prioritizes deterministic contracts over broad UI automation:

- state-machine tests for flow and lifecycle transitions,
- adapter contract tests for OpenCode integration boundaries,
- replay fixtures for regression,
- 3-6 smoke E2E scenarios for TUI/web verification.

## Runtime Planning Context Purpose

Runtime planning context captures where GAIA currently is so work can resume cleanly across sessions.

It should capture:

- current objective and active work unit,
- constraints and non-goals,
- risk tolerance and checkpoint expectations,
- communication contracts between orchestrator and specialists,
- quality expectations and evidence format,
- decision state and next command for deterministic continuation.

Runtime planning context does not replace `AGENTS.md`.

## Current Phase and Plan

Current build focus:

1. Strong base GAIA behavior.
2. Strong runtime context surface under `.gaia/runtime/<session>/`.
3. Reliable visibility and command wiring (`gaia` agent, Ari flow/query surfaces).
4. Recoverable human-in-the-loop behavior before wider pantheon expansion.

Pantheon note:

- The long-term roster is not limited to the initial set.
- Future revisions can add additional Greek-god specialists and user-defined named specialists,
  while GAIA remains the mandatory orchestrator.

MVP boundary and acceptance criteria are defined in:

- `.gaia/plans/project-gaia-plugin-mvp-cut.md`

Runtime context requirements are defined in runtime/session-state and Ari flow/query contracts.

## Documentation Ownership

- Project-facing product/spec docs live in `doc/`.
- GAIA operational state, execution plans, and run artifacts live in `.gaia/`.

This split keeps product documentation stable while allowing orchestration internals to evolve.

Reference docs:

- `doc/INFORMATION_ARCHITECTURE.md`
- `doc/COMPATIBILITY_POLICY.md`
- `doc/HITL_Protocol_Maturity.md`
