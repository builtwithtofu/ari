# ADR 0006: Enhance existing harnesses

Status: accepted

Date: 2026-05-18

## Context

Ari runs LLM work through external harnesses such as Claude Code, Codex, OpenCode, and future adapters. These harnesses already own model interaction, provider authentication, native tools, permissions, project memory, transcripts, and interaction quality.

Ari's value is the durable runtime around those harnesses: workspaces, persistence, attach/inspect surfaces, attention, context movement, messages, profiles, and coordination. If Ari tries to become the harness, it loses the benefit of native harness behavior and becomes another provider-shaped agent framework.

## Decision

Ari enhances existing harnesses; it does not replace them.

- Harnesses remain the LLM interaction engines.
- Ari treats supported harnesses as peers.
- Ari preserves native harness behavior where possible, including provider auth, subscription/API mode, project guidance, tools, and transcripts.
- Ari may add persistence, observation, context excerpts, agent messages, fan-out, MCP/todo/review tooling, attention, and notifications around harnesses.
- Ari models bounded worker work as ephemeral calls, not provider-specific subagent hierarchies.
- Ari-owned concepts stay stable even when adapters use provider-native mechanisms internally.

## Consequences

- Public docs, APIs, and UI should prefer workspace, profile, harness session, sticky session, ephemeral call, context excerpt, and agent message language.
- Adapter details may expose provider metadata for traceability, debugging, attach, and resume, but provider-native concepts must not become Ari's product ontology without a later decision.
- Harness integration work should prove that Ari can enhance the harness without mutating global/user/project harness configuration unexpectedly.
- A future harness-neutral call envelope must preserve native harness differences instead of pretending every harness supports identical behavior.

## Alternatives considered

- **Ari as agent framework:** would give Ari full control, but would replace native harness strengths and force Ari to rebuild model loops, tool policy, auth, and provider-specific behavior.
- **Expose provider concepts directly:** fastest per adapter, but makes Ari unstable and provider-shaped.
- **Lowest-common-denominator harness abstraction:** hides useful native behavior and weakens the reason to use multiple harnesses.
