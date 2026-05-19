# ADR 0001: Headless daemon API authority

Status: accepted

Date: 2026-05-03

## Context

Ari needs more than one interface. The CLI is the first client, but future GUI, TUI, MCP, remote, and automation clients should all operate the same underlying product. If behavior lives only in a UI, other clients cannot reuse it safely and Ari becomes difficult to run in the background or attach to later.

The daemon is also the mechanism that lets work continue after a client disconnects. It is the runtime for persistent LLM harness sessions, commands, process output, attention state, and workspace runtime facts.

## Decision

Every operation Ari can perform must exist first in the headless daemon API, exposed through the daemon's service/JSON-RPC boundary. Headless daemon operations are Ari's core product surface; every other surface is UI, integration, automation, or tooling over those operations.

Clients build on top of that boundary:

- CLI, GUI, TUI, MCP, remote, and automation clients call daemon APIs.
- Product behavior must not exist only inside a client.
- Clients do not need to expose every API method.
- Clients may compose multiple API methods into a smaller, friendlier user workflow.
- Client-specific responsibilities are rendering, prompts, layout, formatting, exit-code mapping, and local UX affordances.

The CLI is a workflow UI over daemon operations, not a requirement that every curated command map one-to-one to an RPC method. Curated CLI commands should gather meaningful user operations into coherent command flows. `ari api` is the CLI escape hatch for fine-grained daemon access, similar to `gh api`; it can expose raw or near-raw daemon operations for debugging, scripting, integration, and advanced users without making the curated CLI shape mirror the daemon API.

## Consequences

- New product behavior should be designed as daemon-owned operations before client commands or screens are added.
- UI polish cannot be the only implementation of a workflow.
- The CLI remains a client, not the runtime authority.
- Curated CLI commands may be reshaped around user workflows even when daemon RPC methods remain finer-grained or differently grouped.
- `ari api` can preserve direct API access while the curated CLI remains user-task oriented.
- Future remote clients can reuse the same conceptual boundary, but remote transport and security require their own decision before implementation.
- Tests should prefer daemon/API behavior for product semantics and client tests for rendering/composition behavior.

## Alternatives considered

- **CLI-first behavior:** faster for early development, but would make future GUI/remote clients reimplement product logic.
- **GUI-owned behavior:** can optimize user experience quickly, but conflicts with background operation and attachable runtime goals.
- **Provider/harness-owned behavior:** delegates too much of Ari's product state to external tools and weakens Ari's ability to preserve workspace context.
