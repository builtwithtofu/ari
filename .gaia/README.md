# .gaia Directory Contract

This directory holds GAIA operational state and active execution plans.

Use `.gaia/` for artifacts that are tied to current runs and can evolve rapidly.

## Put here

- active execution plans and wave plans,
- runtime journals and reduced session state,
- stream/session status projections,
- in-flight decision traces,
- surface stability registry and collaboration metadata.

## Do not put here

- long-lived product policy,
- durable protocol guidance,
- public documentation intended as stable reference.

Those belong in `doc/`.

## Relationship to `doc/`

`doc/` stays curated and durable.

`.gaia/` can hold many collaboration artifacts over time, including planning rounds, notes, decision
logs, and implementation status snapshots.
