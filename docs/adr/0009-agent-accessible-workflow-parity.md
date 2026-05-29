# ADR 0009: Agent-accessible workflow parity

Status: accepted

Date: 2026-05-29

## Context

Ari is a durable, headless workspace runtime. ADR 0001 makes daemon operations the source of product behavior. ADR 0005 decides that helpers use an Ari-owned `ari.tool.*` control surface over pruned daemon operations rather than CLI parsing or raw RPC access. ADR 0008 keeps public run and auth surfaces harness-neutral.

The next orchestration work clarified an important product boundary: fan-out and other coordination flows should not be primarily human CLI workflows. A sticky orchestrator agent should be able to ask Ari to start ephemeral workers, wait boundedly, inspect status, and read durable inbox/attention state. Humans and agents should see the same durable workspace facts.

Without a durable rule, Ari could accidentally grow two product surfaces: human workflows in CLI/RPC and separate, smaller helper tools for agents. That would make agents second-class users of Ari workflows, push product behavior back into clients, and make orchestration features hard to expose consistently through future MCP, TUI, GUI, remote, or automation clients.

This decision does not settle the full permission, authorization, or trust model. Those need later design as the tool catalog expands.

## Decision

Any durable Ari workflow that is exposed to humans through CLI or daemon RPC should also be accessible to agents through an Ari-owned agent control surface, unless there is an explicit safety, capability, or product reason to withhold it.

- The daemon remains the product authority. Human clients and agent tools are clients over daemon behavior.
- CLI workflows may be curated and human-friendly, but they must compile down to daemon operations that can be exposed to agents without parsing CLI text.
- Raw daemon RPCs are not automatically exposed to agents. Agent access is through pruned, Ari-shaped tools or future equivalent catalogs.
- Agent-accessible tools should preserve the same durable state, validation, audit, rollback, projections, inbox, attention, and evidence links as human workflows.
- If a workflow is deliberately not agent-accessible, the reason should be recorded near the planning or implementation surface. Examples include unresolved permissions, sensitive data exposure, irreversible external effects, unsafe concurrency, missing auditability, or an intentionally human-only interaction.
- Agent-accessible surfaces must stay harness-neutral and use Ari concepts such as workspace, sticky session, ephemeral call, profile, context excerpt, inbox, attention, operation record, and provider option.
- Permission, trust, approval, scoping, revocation, and access-control policy remain future work. Until that work is settled, implementations may expose a narrower tool set, but should shape daemon operations so later agent exposure does not require redefining product behavior.

## Consequences

- New human-facing workflows need an agent-accessibility check during planning: what daemon operation backs this, and how would an agent call or inspect it later?
- Orchestration work should expose fan-out, bounded wait, fanout status, and inbox listing through `ari.tool.*` or a successor Ari-owned catalog, not only through CLI/RPC.
- CLI polish must not become the only implementation of a workflow. Product behavior belongs in daemon operations and reusable response shapes.
- Tests should prefer daemon/tool/API proof for product behavior. CLI tests should cover composition and formatting, not be the only proof that a workflow exists.
- Agent tools remain curated. This decision does not mean every internal RPC is safe or useful for agents.
- Some workflows may ship in CLI/RPC before agent exposure when permissions or safety are unsettled, but the implementation should avoid client-only logic that would block later exposure.
- Future permission design must account for parity: access should be controlled by scope, operation kind, trust, approval, and audit rather than by hiding agent access as a default.

## Alternatives considered

- **Expose only selected helper features to agents:** simpler initially, but creates a second-class agent surface and risks rebuilding human workflows separately for agents later.
- **Expose every raw RPC to agents:** maximizes parity but leaks internals, bypasses pruning, and conflicts with ADR 0005's Ari-owned tool catalog boundary.
- **Require agents to use CLI commands:** reuses existing human workflows but moves product coupling into CLI parsing and makes structured evidence, scope validation, audit, and permissions harder.
- **Delay the decision until permissions are designed:** avoids premature exposure risk, but lets current workflow design drift away from agent accessibility. This ADR records parity as the desired architecture while leaving permission policy for later.
