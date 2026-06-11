# Prior art: Solo (soloterm.com)

Researched 2026-06-10. Solo, by Aaron Francis, is a native (Tauri) desktop "agentic metaharness": one workspace app above coding-agent harnesses (Claude Code, Codex, Gemini CLI, Aider, …) plus the dev stack around them. Several Ari workspace concepts — especially messages, timers, idle-wake, and process supervision — were shaped by looking at how Solo works. Solo is prior art only: no Solo coupling exists or is planned, and Solo concepts never become Ari ontology.

## What Solo does

- **Metaharness positioning**: "A harness turns a model into a coding agent. A metaharness gives all of your harnesses a place to live." Solo supervises agents + dev servers per project, declared in a repo-committed `solo.yml` (auto-restart, file-watch restarts, ports/CPU/memory, crash notifications).
- **MCP tool surface**: 40+ tools exposing logs, process status, ports, resource usage, project context, scratchpads, todos, comments, blockers, shared key-value state, lease-based locks, timers/reminders, and coordination.
- **Idle detection & wake**: a lead agent can pause without burning tokens and ask Solo to wake it "when one or more watched agents become idle", then integrate results.
- **Human role**: supervisor, not relay operator — selective desktop notifications, visible terminals/logs.
- **Lifetime**: everything dies when the app closes; explicitly local-GUI-first.

## Mapping to Ari

| Solo concept | Ari answer |
|---|---|
| Metaharness over harnesses | Same philosophy: ADR 0006 (enhance existing harnesses) + adapter contracts |
| App-lifetime processes | **Ari's differentiator: durable headless daemon.** Workspace state, event history, timers, and deliveries survive; clients (CLI/TUI/future MCP) attach and detach |
| Timers/reminders | Durable workspace timers, fired by a daemon worker loop (ADR 0010) |
| Wake-when-idle | `session.idle` / `session.needs_input` workspace events + durable subscriptions + server-side bounded waits (`workspace.events.next` with `min_events`/`timeout_ms`) + pending deliveries |
| Messages between agents | Agent messages recording `message.sent` workspace events atomically |
| MCP tool surface | `ari.*` tools are daemon-backed; MCP projection of the same registry is deferred (`workspace-event-orchestration-mcp-projection-afq`). Solo validates MCP as the right projection surface |
| Crash/attention notifications | `attention_required` events + inbox projection exist; a human-notification delivery channel (ADR 0010 delivery policy) is backlog |
| Process supervision of dev stack | Partial: Ari supervises harness sessions, commands, process output, telemetry. Dev-stack supervision (file watchers, auto-restart of servers) is not a current goal |
| `solo.yml` declarative project config | Workspace/profile state lives in the daemon DB; a declarative repo-committed import is an open product question (backlog) |

## Deliberately not adopted

Scratchpads, todos/blockers, shared KV, and lease locks (decision 2026-06-11):

- **Scratchpads/agent memory**: Solo needs them because harness context dies with each chat window. Ari already persists context excerpts, agent messages, run logs, and final responses workspace-scoped; a generic mutable scratchpad would be a second untyped state surface — exactly the fragmentation ADR 0010 exists to prevent.
- **Todos/blockers**: human planning lives in the issue graph; runtime facts live in events/inbox/timeline. Agent task-claim coordination is an explicit PRD non-goal pending its own decision.
- **Lease locks / shared KV**: only needed when parallel agents mutate one resource without coordination — not a current journey (orchestrator → isolated ephemeral workers). If ever built, these become event-backed projections over workspace event history, never a mutable bag beside it.

Secrets are unrelated to these: Ari's secret store is credential material with grants and a dedicated global audit trail (`secret_audit_events`), not coordination state.
