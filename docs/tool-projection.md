# Ari Tool Projection Contract

This document defines source-of-truth and sync behavior for Ari-owned tools and harness-facing projections.

## Scope

- Ari owns one canonical tool model (`internal/tool`).
- Workspace command definitions are the first projected tool type.
- The first projection target is daemon runtime responses for `workspace.command.*`.

This slice does **not** define file-based harness artifact generation yet.

## Source of Truth

For workspace command definitions, the canonical record is stored in globaldb:

- Table: `workspace_command_definitions`
- Key fields: `command_id`, `workspace_id`, `name`, `command`, `args`, timestamps

Canonicalization rules:

1. Definitions are workspace-scoped.
2. `args` must be a JSON string array.
3. `command_id_or_name` must remain unambiguous within one workspace.

The daemon projects these records through the tool model (`tool.FromWorkspaceCommandDefinition`) before returning API responses.

## Projection Boundary

Projection is read-time adaptation, not duplicated persistence.

- Store layer persists canonical definition data only.
- Daemon layer projects canonical data into API response shapes.
- CLI renders daemon responses; it does not maintain a separate command-definition source.

Current command-definition projection path:

- `workspace.command.create` -> store canonical definition -> project for response
- `workspace.command.list` -> list canonical definitions -> project each for response
- `workspace.command.get` -> load canonical definition -> project for response

## Sync Behavior

Because projection is computed from canonical storage, sync behavior is immediate and one-way:

1. Update canonical data (create/remove).
2. Next read (`list`/`get`) reflects projected result with no background sync job.

There is currently no generated harness artifact file to reconcile.

## Conflict and Drift Rules

- If projection fails (for example malformed canonical payload), daemon returns invalid-params style errors.
- Projection helpers in daemon must not become parallel decoders; canonical parsing stays in `internal/tool`.
- New projection targets must consume canonical tool projection helpers, not copy field-mapping logic.

## Future Extension Points

When file-based or provider-native artifacts are introduced:

1. Canonical Ari storage remains authoritative.
2. Generated artifacts are derived outputs.
3. Regeneration should be deterministic from canonical records.
4. Drift is resolved by re-projecting from canonical records, not editing generated artifacts in place.
