# ADR 0010: Workspace event history and subscriptions

Status: accepted

Date: 2026-06-04

## Context

Ari is moving from turn-shaped helper calls toward durable cross-harness orchestration. A sticky orchestrator session may fan out to several ephemeral worker calls using different profiles and harnesses, receive one worker's result while others continue, ask follow-up questions, wait for all workers, or move on after a timeout.

The current implementation has several overlapping communication and signaling paths: agent messages, run-log messages, sticky inbox items, daemon events, fanout group/member records, final responses, session status, attention, and timeline projections. Those pieces are useful, but without a single root model Ari risks adding another side system whenever it needs idle detection, timeouts, partial worker results, notifications, acknowledgements, or cross-harness message routing.

This decision is about workspace-scoped runtime communication and coordination. It does not add cross-workspace message routing.

## Decision

Workspace event history is the root primitive for durable Ari coordination and communication.

- Ari records durable runtime facts as append-only workspace events. Relevant facts include session lifecycle changes, worker lifecycle changes, agent messages, result availability, context sharing, needs-input signals, idle signals, timers firing, cancellations, timeouts, and attention-worthy notices.
- Messages, inbox items, attention, notifications, status, timeline, fanout status, and wait results are projections over workspace event history, not separate sources of truth.
- Workspace events carry enough Ari-owned identity to correlate work: workspace ID, event type, subject identity, producing session/call when applicable, correlation ID, causation ID, timestamps, and typed payload or typed payload reference.
- Communication remains workspace-scoped. A message, result, subscription, or wait condition targets sessions/calls/profiles within one workspace unless a later ADR explicitly adds cross-workspace routing.
- Subscriptions are Ari-owned objects over workspace event history. A durable subscription is owned by a session, stores its filter, cursor/read position, acknowledgement state, and optional timeout or completion condition.
- Subscription filters may select event types, subjects, fanout groups/members, sessions/calls, correlation IDs, and attention/result categories.
- Subscriptions are streams first. An orchestrator can consume each matching worker event as it arrives while other workers continue. `wait any`, `wait all`, bounded wait, timeout, and cancellation are completion conditions over the same event stream, not separate mechanisms.
- Subscription delivery is a policy over matched events, not the meaning of the subscription. No single delivery channel owns the semantics.
- Delivery policies may include pull/read from the durable cursor, push/wake to the owning session as new work, human notification, MCP stream, daemon RPC stream, CLI/TUI stream, or adapter-native resume/control when a harness can accept it safely.
- Every delivery policy uses the same subscription event, filter, cursor, acknowledgement, timeout, and completion semantics. Delivery should prefer event IDs and artifact links over duplicating large payloads inline. If one delivery channel is unavailable, the event remains durable and consumable through another channel.
- Ari may expose the same subscription/filter/delivery model through Ari tools for harnesses, future MCP, daemon RPC, CLI/TUI, or other clients. Different transports must not create different semantics.
- Ari should build MCP as a first-class daemon-backed client surface for tools, resources, and subscriptions. MCP projects Ari's workspace event/subscription model; it is not a separate orchestration runtime and must not become a second source of truth.
- Ari should remain Ari-native rather than embedding or sidecar-running Temporal as the orchestration source of truth. Ari may borrow Temporal-style primitives such as durable timers, signals, queries, updates, activity-style retries, cancellation, pending deliveries, and cursor/ack state, but those primitives are modeled over workspace events and daemon-owned operations.
- The initial Ari-native orchestration primitive set is:
  - **Durable timers:** daemon-owned scheduled conditions that append timer events when due. Timers survive daemon restart and can drive waits, retries, reminders, idle checks, and subscription delivery deadlines.
  - **Signals:** asynchronous, fire-and-forget workspace-scoped inputs addressed to a session, call, subscription, fanout group, or other Ari subject. Signals append events and may trigger delivery policies, but they do not require an immediate response.
  - **Queries:** read-only daemon/tool/MCP operations over workspace events and projections. Queries do not advance orchestration state except for explicit read/ack operations.
  - **Updates:** accepted or rejected mutating daemon operations that append events and may create sessions, calls, timers, subscriptions, cancellations, or deliveries.
  - **Activity-style retries:** retry policy for non-deterministic side effects such as harness calls, adapter delivery attempts, commands, and external probes. Attempts, failures, backoff, terminal success, and terminal failure are observable Ari facts.
  - **Cancellation:** cooperative cancellation requests and outcomes for sessions, calls, timers, subscriptions, fanout members, and delivery attempts. Cancellation is recorded and propagated through daemon-owned scopes.
  - **Pending deliveries:** durable work items derived from subscription matches. A pending delivery records the target, delivery policy, event references, attempts, errors, deadline, and terminal outcome.
  - **Ack/cursor state:** durable per-subscription or per-consumer read position and acknowledgement state so consumers can resume, deduplicate, and avoid repeated delivery after reconnect.
- Per-session run logs remain the normalized transcript/evidence surface for harness content. Workspace events should link to run-log items, final responses, context excerpts, and other artifacts rather than duplicating every large payload inline.

## Consequences

- New lifecycle or communication behavior should first ask: what workspace event is recorded, and what projections/subscriptions consume it?
- Existing fanout, inbox, daemon event, attention, status, final response, and timeline code should converge toward event-backed projections. They may remain materialized tables or caches for query speed, but should not become independent sources of truth.
- Orchestrator-style workflows can process partial results without blocking on all workers, while still supporting explicit `wait all` or timeout policies.
- Timeouts and idle/needs-input signals become durable facts visible to humans and agents, not transient tool-call return values only.
- Ari needs an event type catalog and payload contracts before expanding the schema broadly.
- Ari needs acknowledgement/read-position behavior for durable subscriptions so agents do not repeatedly consume the same events after reconnect.
- The event catalog and subscription contract should be broad enough to cover the primitive set above before Ari grows MCP push, richer fanout composition, idle detection, or cross-harness wake/resume behavior.
- Adapters need an explicit capability contract for which delivery policies they support, including whether and how they can wake or resume a session with a subscription event.
- MCP tool/resource/schema design should be derived from daemon operations and workspace event contracts, not provider-specific harness behavior.
- Workflow-engine-inspired behavior must not introduce a second durable execution history, independent scheduler authority, or separate product protocol.
- Exact database schema, retention/compaction policy, concrete transport, wake/resume mechanics, event replay/rebuild mechanics, and cross-workspace notification aggregation remain future decisions.

## Alternatives considered

- **Keep separate message, inbox, event, wait, and notification systems:** matches the current incremental implementation, but keeps adding fragmentation and makes new lifecycle facts such as idle or timeout hard to expose consistently.
- **Make inbox/messages the source of truth:** good for addressed communication, but weak for lifecycle events, timers, idle detection, fanout wait conditions, and workspace timeline/status projections.
- **Make workflow/fanout records the source of truth:** good for fanout status queries, but does not naturally cover ordinary session messages, attention, idle, notifications, or non-fanout workers.
- **Use only harness-native async/agent features:** useful as adapter capabilities, but it prevents Ari from composing work across different harnesses with one durable coordination model.
- **Use waits that only return aggregate results:** simpler for `wait all`, but prevents orchestrators from handling worker results as they arrive while other workers continue.
- **Embed or sidecar-run Temporal as the orchestration source of truth:** provides mature timers, signals, workflows, retries, and visibility, but adds a second event history, a separate server/protocol/persistence model, deterministic workflow constraints, and operational weight that conflicts with Ari's daemon-owned workspace runtime model.
