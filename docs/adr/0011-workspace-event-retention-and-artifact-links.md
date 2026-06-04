# ADR 0011: Workspace event retention and artifact links

Status: accepted

Date: 2026-06-10

## Context

ADR 0010 made workspace event history the root primitive for durable Ari coordination and explicitly deferred retention/compaction policy and event replay/rebuild mechanics. The event store, durable subscriptions, timers, signals, pending deliveries, and event-backed projections are now implemented. Before the event table grows in real workspaces, Ari needs an explicit answer to: what keeps event rows bounded, where do large payloads live, and what may be rebuilt versus what must never be lost.

## Decision

Payload references are the retention mechanism. Workspace events record facts; artifact tables hold bodies.

- A workspace event row carries two JSON columns: `payload_json` for small, typed, queryable fact metadata, and `payload_ref_json` for a `{kind, id}` link to the artifact that holds the body.
- Large content — final responses, run-log transcripts, context excerpts, command output, run logs — lives in its artifact table (`final_responses`, run-log messages, `context_excerpts`, `commands`) and is referenced from events, never duplicated into them. Terminal worker and session events link results as `{kind: "final_response", id}`; consumers resolve refs through the existing artifact read paths (`final_response.get`, run-log/timeline reads).
- Workspace events are append-only and are not pruned at this stage. There is no size pressure yet, and pruning without a consumer would be speculative policy. Introducing compaction or deletion later requires its own decision that addresses subscription cursors, causation chains, and projection rebuild.
- Projections materialized from events (`fanout_members`, `inbox_items`) are rebuildable caches, not durable state. The rebuild expectation is replay equivalence: replaying the workspace event history must reproduce the materialized rows. `fanoutMembersFromWorkspaceEvents` is the rebuild/repair primitive for fanout members and is held to that contract by tests; it is not a serving read path. Inbox re-projection must preserve consumer read state while refreshing event evidence.
- Artifact tables referenced by events are durable state, not caches. A final response, run log, or context excerpt referenced from event history must not be deleted while events reference it; artifact retention follows event retention.

### Known exception: harness runtime events

`harness.event.<kind>` workspace events currently embed the normalized harness runtime payload inline (under `payload.payload`) in addition to the `{kind: "harness_runtime_event", id}` ref. This is accepted for now because normalized runtime payloads are bounded by the adapter contract, but it is the first compaction target: if event rows grow, the inline payload moves behind the ref and consumers must resolve it like any other artifact.

## Consequences

- New event emission must follow the ref pattern: facts in `payload_json`, bodies behind `payload_ref_json`. Tests prove that terminal events do not embed final response bodies (`TestTerminalWorkspaceEventsReferenceFinalResponsesNotBodies`) and that replay reproduces materialized projections (`TestFanoutMemberRebuildFromWorkspaceEventsMatchesMaterializedRows`, journey proof rebuild step).
- Projection schema changes stay cheap: a materialized projection can be dropped and rebuilt from history, so migrations on projection tables do not need data preservation guarantees beyond consumer read state.
- Anything that cannot be rebuilt from events plus artifact tables must be stored as an artifact or event, not in a projection.
- A future compaction/pruning decision has a defined starting point: harness runtime event inline payloads first, then time- or workspace-lifecycle-based event archival, never silent deletion under live subscription cursors.

## Alternatives considered

- **Embed payloads inline and prune aggressively:** makes events self-contained but duplicates large bodies, inflates the table immediately, and forces pruning decisions before there is operational evidence.
- **Time-based retention now:** simple to state, but arbitrary without size data, and deleting events breaks replay equivalence and causation chains for long-lived workspaces.
- **External blob/artifact store:** unnecessary indirection while artifact tables in the same SQLite database serve all consumers; can be revisited if artifact size outgrows the database.
