# ADR 0001: Headless daemon API authority

Status: accepted

Date: 2026-05-03

## Context

Ari needs more than one interface. The CLI is the first client, but future GUI, TUI, MCP, remote, and automation clients should all operate the same underlying product. If behavior lives only in a UI, other clients cannot reuse it safely and Ari becomes difficult to run in the background or attach to later.

The daemon is also the mechanism that lets work continue after a client disconnects. It is the local control plane for persistent LLM-agent sessions, commands, process output, attention state, and workspace runtime facts.

## Decision

Every operation Ari can perform must exist first in the headless daemon API, exposed through the daemon's service/JSON-RPC boundary.

Clients build on top of that boundary:

- CLI, GUI, TUI, MCP, remote, and automation clients call daemon APIs.
- Product behavior must not exist only inside a client.
- Clients do not need to expose every API method.
- Clients may compose multiple API methods into a smaller, friendlier user workflow.
- Client-specific responsibilities are rendering, prompts, layout, formatting, exit-code mapping, and local UX affordances.

## Consequences

- New product behavior should be designed as daemon-owned operations before client commands or screens are added.
- UI polish cannot be the only implementation of a workflow.
- The CLI remains a client, not the runtime authority.
- Future remote clients can reuse the same conceptual boundary, but remote transport and security require their own decision before implementation.
- Tests should prefer daemon/API behavior for product semantics and client tests for rendering/composition behavior.

## Alternatives considered

- **CLI-first behavior:** faster for early development, but would make future GUI/remote clients reimplement product logic.
- **GUI-owned behavior:** can optimize user experience quickly, but conflicts with background operation and attachable runtime goals.
- **Provider/harness-owned behavior:** delegates too much of Ari's product state to external tools and weakens Ari's ability to preserve workspace context.
