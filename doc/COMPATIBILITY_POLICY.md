# GAIA Compatibility Policy

> Status: Active policy
> Purpose: define version-scoped stability lanes and migration expectations

## Core rule

Compatibility is managed per surface and per version, not by one global project flag.

A single project can contain stable and unstable surfaces at the same time.

## Stability lanes

- `stable`: protected user-facing behavior; default is non-breaking.
- `experimental`: rapid iteration allowed; breaking changes are acceptable.
- `deprecated`: still supported; migration path is required.
- `scheduled_removal`: known removal target in a future major version.

## Versioned lifecycle model

Typical progression:

- `v1`: mark a surface `stable`.
- `v2`: keep behavior, mark old surface `deprecated`, publish migration path.
- `v3`: move deprecated surface to `scheduled_removal`/remove as planned.

## Required metadata for each surface

- `surface_id`
- `stability_lane`
- `since_version`
- `public_contract` summary
- `deprecates` (optional)
- `removes_in` (optional)
- `migration_path` (required for deprecated/scheduled_removal)

## Planning and execution gates

For `stable` surfaces, execution requires:

- explicit contract impact assessment,
- non-breaking verification plan,
- migration notes for any behavior change,
- operator approval for breaking intent.

For `experimental` surfaces, execution can be faster but should still include:

- clear scope,
- rollback path,
- documented rationale.

## Public vs internal boundary

Before execution on mature surfaces, classify touched components as:

- `public`: contract must be preserved or migrated.
- `internal`: can change freely within project-level constraints.

If classification is unclear, treat the surface as public until clarified.

## Registry source of truth

Surface-level compatibility metadata lives in:

- `.gaia/surfaces/registry.json`

This allows protocol tooling (CLI/plugin) to enforce lane-specific behavior consistently.
