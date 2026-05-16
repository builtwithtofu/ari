# ADR 0005: Helper tool control surface

Status: accepted

Date: 2026-05-14

## Context

Ari is moving toward a daemon-first workspace runtime where each workspace contains the functions needed to configure, start, observe, coordinate, and manage local agentic work. The default general helper should support onboarding, Ari setup, workspace questions, and eventually broad Ari control. Users should be able to ask the helper to create workspaces, configure profiles, start or stop sessions, run commands, manage auth flows, and inspect workspace state.

Ari already has a daemon-owned helper tool surface through `ari.tool.list` and `ari.tool.call`, with tool schemas, read-only flags, approval requirements, and scoped approval markers. ADR 0001 says product behavior belongs behind daemon APIs, not in clients.

The main tradeoff is whether to let helpers call raw daemon RPCs, CLI-shaped commands, MCP tools, or Ari-owned tools. The control surface also needs a trust, audit, and rollback model before exposing broad daemon operations to the default helper.

## Decision

Ari helpers use an Ari-owned tool control surface over daemon operations.

- `ari.tool.*` is the first helper control surface.
- Helper-facing tools map to pruned, Ari-shaped daemon operations rather than exposing every raw RPC method blindly.
- MCP can later expose the same tool catalog, but MCP is not the first source of product behavior.
- CLI commands remain clients over daemon behavior; helpers should not depend on CLI parsing as their primary control layer.

The default helper follows a trust-after-init model:

- `ari init` configures the default helper and makes trust choices visible.
- First-use confirmation offers trust once, trust always, or do not perform.
- Trust-always decisions are remembered by operation type.
- The first tranche does not implement a heavy permission system.

Ari-owned configuration/runtime mutations must be atomic, auditable, and rollbackable:

- Each Ari-owned mutation, whether triggered by helper, CLI, or API, either fully applies with a change record or does not apply.
- Each atomic change records actor/source, operation type, request summary, result, trust decision when relevant, parent/checkpoint link, and rollback data when rollback is supported.
- Multi-step workflows are batches or graphs of atomic operations, not opaque state changes.
- User-facing rollback should feel like moving back through meaningful user-perceived actions, similar to browser Back, even when those actions are implemented as multiple atomic child operations.
- Every user-visible mutation step should automatically create a rollback point; rolling back a batch action undoes the whole visible step rather than only its last child operation.
- The target rollback model is a navigable change graph where Ari can move Ari-owned configuration/runtime state to any point in the graph. The first implementation may keep rollback simple along the current path, but records must be shaped for full graph navigation later.
- Rollback is non-destructive: moving back to a prior point appends a new rollback node that targets that prior state rather than deleting later history.
- Rollback scope is Ari-owned configuration/runtime state. Shell command and file/repository effects caused by helpers or harnesses are audited but not rolled back by Ari in the first implementation.
- User-relevant helper operations should appear in workspace timeline/status projections as well as the structured audit log.

## Consequences

- The helper can become a broad Ari control surface without coupling to raw daemon RPC names or CLI syntax.
- Existing daemon operations must be inventoried and pruned before helper exposure: keep operations that fit workspace setup, profile/session lifecycle, coordination, inspection, auth, and command execution; hide, merge, or defer internal leftovers.
- New helper actions should be designed as daemon-backed tools with operation-type trust metadata, audit records, and rollback behavior where applicable.
- Atomicity becomes part of the runtime contract for Ari-owned mutations.
- Rollback data increases storage and migration considerations, but keeps helper autonomy safer and more inspectable.
- A future MCP server can project Ari tools outward without redefining product semantics.

## Alternatives considered

- **Raw daemon RPC access:** simplest implementation, but exposes unpruned internals and ties helper behavior to API cleanup churn.
- **CLI-shaped helper commands:** matches human command syntax, but risks moving product behavior back into a client layer.
- **MCP-first control surface:** useful for external integration, but makes MCP a product authority before Ari's daemon-owned tool catalog is settled.
- **Heavy permission system first:** safer in theory, but conflicts with the desired desktop-assistant trust model and would delay the core helper-control workflow.
