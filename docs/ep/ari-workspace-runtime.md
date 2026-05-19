# EP: Ari workspace runtime

Status: proposed

## Intent

Ari is a durable, headless workspace runtime for LLM harnesses. It gives existing harnesses a persistent workspace home: users can switch between workspaces, keep harness work running, inspect what happened, continue human-facing sessions, and receive attention signals when work elsewhere needs them.

Ari is closer to a headless-first Solo-like runtime than to an AI IDE, agent framework, or single chat UI. The tmux/cmux analogy applies to durable workspaces: Ari keeps multiple workspaces available and switchable, while each workspace preserves its own folders, harness sessions, output, context, messages, and attention state.

## Product thesis

LLM harness work becomes hard to manage when it only lives in terminal tabs, provider transcripts, one-off process output, or a single chat thread. Users need a runtime that can:

- keep workspaces and harness sessions durable after a client exits;
- run multiple peer harness sessions in the same workspace;
- let humans interact with sticky sessions while ephemeral workers run;
- show idle, blocked, waiting, completed, failed, auth-required, and review-ready work across workspaces;
- preserve logs, messages, final responses, context excerpts, and coordination history for later inspection;
- support future remote clients without moving product behavior into a UI.

Ari should make serious LLM-assisted work persistent, inspectable, resumable, and coordinatable without replacing the harnesses that do the LLM interaction.

## Durable direction

- Ari is headless first. Product operations live behind daemon APIs before they appear in any UI.
- The daemon owns durable runtime state. Clients render, prompt, format, compose, and notify.
- CLI is the current control/story surface. `ari api` is the fine-grained daemon escape hatch.
- Workspaces are durable switchable units of work. A workspace contains one or more folders and may represent multi-folder systems such as microservice projects.
- A workspace can host multiple peer harness sessions, including Claude Code, Codex, OpenCode, and future harnesses.
- Sticky sessions are persistent human-facing harness sessions attached to a workspace.
- Ephemeral calls are inspectable bounded worker invocations. They are Ari's alternative to provider-specific subagents and support fan-out, review, research, implementation slices, comparison, and follow-on worker work.
- Profiles are reusable behavior contracts passed into harnesses. Ari does not hardcode planner, orchestrator, reviewer, worker, helper, or researcher as product roles.
- Ari enhances harnesses by adding persistence, observation, context movement, coordination, attention, and optional tooling around them. Ari does not replace harness interaction models.
- Attention should bubble up from runtime facts, including work in other workspaces while the user is focused elsewhere.
- Remote clients are part of the long-term product shape, but remote transport, authentication, authorization, and audit require later decisions.

## Non-goals for this EP

- Do not make a specific CLI, GUI, TUI, MCP, or remote client the product source of truth.
- Do not define Ari as an AI IDE, model runtime, or replacement harness.
- Do not freeze a normalized harness-call envelope or final database schema here.
- Do not decide remote transport, authentication, authorization, notification delivery, telemetry, or analytics architecture here.
- Do not preserve legacy Ariadne planning-engine, plan-DAG, or session-first language as current product direction.

## Revisit triggers

- Remote access moves from aspiration to implementation.
- Cross-workspace notifications need platform-specific or cross-device delivery.
- The first full sticky-orchestrator plus ephemeral-worker flow is implemented.
- Harness integration needs a durable mode-selection rule that affects billing, subscription use, attach behavior, or native harness semantics.
- Workspace-level concurrency creates unsafe or confusing mutations.

## Related decisions

- ADR 0001: daemon API authority.
- ADR 0002: workspace as runtime unit.
- ADR 0003: harness session and profile terminology.
- ADR 0004: harness semantics and normalization.
- ADR 0005: helper tool control surface.
- ADR 0006: enhance existing harnesses.
