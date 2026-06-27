# Daemon API and attach protocol

Ari is headless first. Product operations belong behind the daemon API before they appear in a CLI command, GUI screen, TUI view, MCP surface, or remote client.

See also:

- `docs/adr/0001-headless-daemon-api-authority.md`
- `docs/adr/0010-workspace-event-history-and-subscriptions.md`
- `docs/adr/0013-user-facing-presentation-and-copy.md`

## API authority

The daemon service/JSON-RPC boundary is Ari's product authority.

- Every operation Ari can perform must be available through the daemon API first.
- Clients call daemon APIs and may compose multiple calls into user-friendly workflows.
- Clients do not need to expose every method.
- Product behavior must not exist only in a UI client.

## Client responsibilities

Clients own rendering and interaction:

- command-line flags and exit codes;
- terminal prompts, layout, and formatting;
- screens, layout, navigation, and notifications;
- composition of multiple daemon calls into one UX action.

Clients must not become independent state owners for workspace runtime behavior, status semantics, or Ari product copy. The daemon API exposes normalized presentation facts and safe detail/raw fields; clients render those facts and may choose how much detail to show.

## Daemon responsibilities

The daemon owns durable runtime behavior and state, including:

- workspace creation, resolution, lifecycle, and folder membership;
- background agent and command lifecycles;
- process output and attachable terminal state;
- append-only workspace event history with durable subscriptions (filters, cursors, acknowledgements), signals, durable timers, and pending deliveries (ADR 0010, ADR 0011);
- context, proof, activity, timeline, inbox, fanout-status, and other projections derived from workspace event history;
- normalized presentation fields for workspace, harness, provider, model, auth, runtime, session, timeline, attention, and tool surfaces;
- approvals, blockers, completions, idle/waiting state, and other attention facts;
- profile, helper, settings, and final-response operations when present.

Agent-callable Ari tools (`ari.*` via `ari.tool.call`) and any future MCP surface project this same daemon authority; they do not own orchestration semantics or state.

## Attach transport

Control-plane operations use the daemon API. Terminal and process I/O use the attach transport.

Attach is intentionally scoped to live terminal/process streams. It should not become the general product event bus for agent timelines, workspace activity, approvals, or context inspection. Those remain daemon API projections so every client can query the same state.

## Remote clients

Future remote clients should reuse the same authority boundary: clients render and compose, daemon owns runtime behavior. Remote transport, authentication, authorization, and audit rules are not decided by this document and require a dedicated decision before implementation.
