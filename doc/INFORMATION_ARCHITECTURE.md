# GAIA Information Architecture

> Status: Active policy
> Purpose: keep durable project docs clear while enabling high-volume collaboration artifacts

## Design intent

Use a two-lane documentation model:

- `doc/` for curated, durable project documentation,
- `.gaia/` for active collaboration artifacts, decision trails, and operational state.

This keeps project docs recognizable for contributors while allowing GAIA workflows to generate many
planning and status documents without polluting durable docs.

## What belongs in `doc/`

`doc/` should look like a typical project documentation surface.

Expected durable categories:

- product and protocol north star,
- architecture and boundaries,
- compatibility and migration policy,
- stable operator guides and references.

`doc/` should stay intentionally small and curated.

## What belongs in `.gaia/`

`.gaia/` is the collaboration workspace and can contain high-volume artifacts.

Expected operational categories:

- active plans and planning rounds,
- runtime session logs and state snapshots,
- stream status and in-flight implementation notes,
- user decisions, rejection feedback records, and progress logs.

It is acceptable for `.gaia/` to contain many documents over time.

## Practical directory model

Recommended durable lane:

- `doc/SPECIFICATION.md`
- `doc/COMPATIBILITY_POLICY.md`
- `doc/HITL_Protocol_Maturity.md`
- `doc/Sandbox_Harness.md`
- `doc/INFORMATION_ARCHITECTURE.md`

Recommended operational lane:

- `.gaia/plans/`
- `.gaia/runtime/`
- `.gaia/streams/`
- `.gaia/decisions/`
- `.gaia/notes/`
- `.gaia/surfaces/`

## Promotion and archival rule

Promotion from `.gaia/` to `doc/` is optional and should happen only when an artifact becomes
general, durable project policy.

Default behavior:

- keep working collaboration artifacts in `.gaia/`,
- keep `doc/` focused on durable references.
