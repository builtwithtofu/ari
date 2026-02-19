# Surface Registry

This directory tracks version-scoped stability metadata for public and internal surfaces.

Use `registry.json` as the machine-readable source for compatibility lane decisions.

Stability lanes:

- `stable`
- `experimental`
- `deprecated`
- `scheduled_removal`

The registry is consumed by planning/execution policy checks.

For CLI surfaces, prefer Ariadne command contracts (`ari flow`, `ari query`) while keeping GAIA
project naming for repository ownership.
