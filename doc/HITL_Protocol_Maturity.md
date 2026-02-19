# GAIA Planning-First Policy and Stability Lanes

> Status: Active policy
> Purpose: keep planning as the primary protocol while applying version-aware stability rules

## Core stance

Planning is always first.

GAIA should begin with problem framing, pathway discovery, and decision clarity before technology
commitments. Maturity influences guardrails, but does not replace planning.

## Planning-first protocol

Every meaningful task starts in a planning loop:

1. discover context and constraints,
2. identify gaps and branching pathways,
3. capture assumptions and risks,
4. document and review with the operator,
5. iterate planning rounds until execution is explicitly approved.

Further planning rounds are expected when:

- gap analysis finds unresolved risk,
- the operator asks to expand/refine a branch,
- execution diverges and re-plan is required.

## Why maturity still matters

Maturity changes strictness, not intent.

- very early: optimize for problem understanding and north star definition,
- pre-alpha/alpha: allow aggressive structural changes,
- beta: bias toward stability while still permitting controlled breaks,
- released/public surfaces: compatibility-first defaults.

## Stability is version-scoped, not one project flag

A project can hold stable and unstable surfaces at the same time.

Use stability lanes per version and per surface:

- `stable`: protected user-facing surface,
- `experimental`: allowed to change quickly,
- `deprecated`: still supported, requires migration guidance,
- `scheduled_removal`: removal planned for next major.

Example lifecycle:

- v1: stable API
- v2: keep v1 behavior, mark deprecated, provide migration path
- v3: remove deprecated path

This model avoids treating the entire repository as uniformly stable or unstable.

## Required decisions before execution on stable surfaces

- define what is public vs internal,
- define compatibility contract boundaries,
- define migration/deprecation timeline,
- define test/verification gates for user-space safety.

## Documentation placement policy

Durable protocol policy belongs in `doc/`.

Operational execution state belongs in `.gaia/`.

### Keep in `doc/`

- north star and protocol policy,
- planning process and decision framework,
- maturity/stability lane definitions,
- public contract and migration policy.

### Keep in `.gaia/`

- active implementation plans,
- current session/runtime state,
- in-flight decision logs and progress snapshots,
- temporary execution artifacts that can be regenerated.

## Meta case for this repository

This repository builds GAIA itself, so it is both product and protocol lab.

Rule:

- promote long-lived policy to `doc/`,
- keep `.gaia/` focused on active execution state.
