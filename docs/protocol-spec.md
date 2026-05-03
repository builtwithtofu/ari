# Daemon API and attach protocol

Ari is headless first. Product operations belong behind the daemon API before they appear in a CLI command, GUI screen, TUI view, MCP surface, or remote client.

See also:

- `docs/adr/0001-headless-daemon-api-authority.md`
- `docs/ep/ari-workspace-runtime.md`

## API authority

The daemon service/JSON-RPC boundary is Ari's product authority.

- Every operation Ari can perform must be available through the daemon API first.
- Clients call daemon APIs and may compose multiple calls into user-friendly workflows.
- Clients do not need to expose every method.
- Product behavior must not exist only in a UI client.

## Client responsibilities

Clients own presentation and interaction:

- command-line flags and exit codes;
- terminal prompts and human-readable formatting;
- screens, layout, navigation, and notifications;
- composition of multiple daemon calls into one UX action.

Clients must not become independent state owners for workspace runtime behavior.

## Daemon responsibilities

The daemon owns durable runtime behavior and state, including:

- workspace creation, resolution, lifecycle, and folder membership;
- background agent and command lifecycles;
- process output and attachable terminal state;
- context, proof, activity, timeline, and other projections;
- approvals, blockers, completions, idle/waiting state, and other attention facts;
- profile, helper, settings, and final-response operations when present.

## Attach transport

Control-plane operations use the daemon API. Terminal and process I/O use the attach transport.

Attach is intentionally scoped to live terminal/process streams. It should not become the general product event bus for agent timelines, workspace activity, approvals, or context inspection. Those remain daemon API projections so every client can query the same state.

## Remote clients

Future remote clients should reuse the same authority boundary: clients render and compose, daemon owns runtime behavior. Remote transport, authentication, authorization, and audit rules are not decided by this document and require a dedicated decision before implementation.
