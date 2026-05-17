# ADR 0004: Harness semantics and normalization

Status: accepted

Date: 2026-05-04

## Context

Ari supports multiple harnesses such as Claude Code, Codex, and OpenCode. Each harness has different concepts for sessions, prompts, system/developer instructions, messages, runs, events, usage, and provider IDs. If Ari exposes those differences directly, the CLI, daemon API, and future frontend will become provider-shaped instead of Ari-shaped.

ADR 0001 makes the daemon API the product authority. ADR 0002 makes workspace the runtime unit. ADR 0003 defines profiles, agent sessions, messages, context excerpts, sticky sessions, and ephemeral calls as Ari product concepts. This ADR refines those decisions at the harness boundary.

The immediate workspace orchestration plan needs provider-specific prompt/session behavior, especially for profile prompts. Research found:

- Claude Code supports native replacement and append system prompt flags in headless mode.
- Claude Code background agents (`claude --bg`) are interactive/sessioned Claude Code sessions that consume normal Claude Code subscription usage, while `claude -p` and Agent SDK usage consume Agent SDK credits from June 15, 2026.
- Claude Code background agents load normal Claude Code project/user behavior unless Ari explicitly overrides it; this makes preserving native Claude Code behavior more important for subscription-backed workspace sessions than for deterministic headless calls.
- Codex app-server supports thread-level instruction fields; `codex exec` does not provide a clean separate session system-prompt channel.
- OpenCode CLI lacks a per-run system prompt flag, while OpenCode server/message APIs and agent configuration provide stronger behavior-prompt paths.

## Decision

Ari normalizes harness behavior into Ari-owned structures and semantics. Harness adapters translate provider details into stable Ari concepts; callers and frontends interact with Ari workspaces, profiles, sessions, messages, calls, context excerpts, run-log items, status, and timeline projections rather than provider-native objects.

### Stable Ari surface

Ari owns these concepts at the daemon/API boundary:

- workspace
- profile
- agent session
- sticky session
- ephemeral call
- agent message
- context excerpt
- run-log message/item
- final response/result
- status/timeline projection

Provider session IDs, thread IDs, run IDs, item IDs, event names, token fields, and launch flags are adapter metadata. They may be stored for traceability and resume support, but they are not the primary product interface.

### Prompt and instruction semantics

A profile prompt is Ari's reusable base behavior contract for a harness-backed session. Ari should map it to the strongest native base/system/developer instruction mechanism each harness supports.

Default behavior is **replacement base behavior**, not append to the provider's default agent instructions, for deterministic headless or API-style invocations where Ari owns the complete run contract. Ari may use additive behavior for native interactive/sessioned modes when preserving the harness's vanilla behavior is part of the product contract.

When Ari launches a harness, Ari may inject profile guidance, workspace context, settings, tools, or MCP configuration. Those injections are Ari-scoped launch behavior. They must not require mutating global/user/project harness configuration in a way that changes the harness's standalone vanilla behavior.

Harness-specific launch details should enter adapters through typed harness options created from Ari settings. Ari settings may contain normalized intent plus namespaced native harness settings; generic call sites should not scatter raw provider CLI flags.

Session start may accept `--prompt <text>` or `--prompt-file <path>` as a session-specific replacement for the profile prompt. Task messages and context excerpts remain visible user/context payloads and must not be hidden in system prompts.

### Harness mappings

Claude Code:

- Keep `claude -p` as an explicit headless/API-credit invocation mode, not the default subscription-backed workspace-session path.
- Prefer `claude --bg` as the first subscription-backed Claude Code workspace-session shape because it is a native sessioned mode and consumes normal Claude Code subscription usage.
- For headless `claude -p` invocations, use native replacement system prompt support for Ari profile/session behavior, such as `--system-prompt` or `--system-prompt-file`.
- For background/sessioned `claude --bg` invocations, append Ari profile guidance with `--append-system-prompt` or `--append-system-prompt-file` so Claude Code keeps its native interactive behavior, project memory, and tool guidance unless Ari explicitly chooses a replacement mode.
- Treat background initial task/context as the positional prompt to `claude --bg`; treat headless task/context as the visible stdin/user payload to `claude -p`.
- Avoid making Claude subagents Ari's product ontology; `--agent` is a provider-specific adapter option only.
- Store native Claude background session IDs and related metadata for traceability, logs, attach, and resume support, but keep Ari's public model centered on workspace sessions and normalized lifecycle/status concepts.

Codex:

- Prefer app-server for orchestration because it can separate thread/session instructions from user turns.
- Map Ari profile/session behavior to replacement/base thread instructions at session/thread creation when supported.
- Use additive `developer_instructions` only for explicit additive behavior or non-replacement guardrails after base behavior is set.
- Do not use `codex exec` as the primary orchestration path when a separate session behavior prompt is required.
- Keep AGENTS.md and project/user instructions distinct from Ari profile behavior.

OpenCode:

- Prefer direct OpenCode server/API integration over `opencode run` CLI for orchestration.
- Set base behavior through the server/API path where possible.
- If session creation cannot carry system behavior, create the session and apply the profile prompt through the native message/system channel before user task/context delivery.
- Use CLI `--agent` plus generated agent configuration only as a fallback adapter strategy, not the primary orchestration path.

Future harnesses:

- A harness should declare how it maps Ari base behavior, user task messages, context excerpts, provider sessions, final responses, and usage telemetry.
- A harness should declare which invocation modes it supports, such as headless call, background session, or interactive attach, and which typed options configure those modes.
- If no native system/developer instruction channel exists, adding the harness requires a separate decision about whether Ari should support a weaker visible-input fallback.

### Normalization rules

- Adapters convert provider output into Ari run-log items with stable kinds and status values.
- Status and timeline projections derive from Ari records, not provider-specific event names.
- Context excerpts are explicit bounded selections from Ari run-log messages.
- Handoffs, replies, reviews, research requests, and fan-out children are ordinary Ari messages or ephemeral calls unless a later ADR introduces a durable workflow object.
- Provider-native hierarchy, such as subagents or thread/task objects, must not leak into public Ari terminology unless a later ADR adopts it intentionally.
- Provider-native passthrough data may be exposed when needed to achieve equivalent user functionality, but it must remain clearly secondary to Ari's normalized session/lifecycle model.

## Consequences

- CLI, API, and future frontend can stay stable while adapters evolve per provider.
- Harness-specific details remain available for traceability, debugging, resume, and capability checks, but product behavior is Ari-owned.
- Adapter tests must prove prompt behavior reaches the provider through the intended native channel for the selected invocation mode and that task/context payloads remain visible user/context content.
- The workspace orchestration implementation should prefer server/API integrations over weaker CLI-only paths when the server/API is needed to preserve Ari semantics.
- Some harnesses may have weaker semantics. Ari should surface capability limitations rather than silently pretending all providers support the same behavior.
- Ari can preserve vanilla standalone harness behavior while still injecting Ari-specific behavior during Ari-launched sessions, because injected settings/options are scoped to the launch path.

## Alternatives considered

- **Expose provider-native concepts directly:** fastest per adapter, but creates unstable provider-shaped UX and blocks a stable frontend.
- **Always prepend profile prompts as user text:** simple and portable, but weakens profiles and hides the difference between behavior instructions and task/context content.
- **Always append to provider defaults:** preserves provider behavior, but conflicts with Ari profiles as the base behavior contract and makes behavior less predictable.
- **Use only CLI harness integrations:** simpler process model, but some providers only expose the needed prompt/session semantics through server/API paths.
- **Store raw provider flags in profiles:** maximally flexible, but leaks provider CLI details into Ari's product model and makes future UI/API stability harder. Typed harness options created from Ari settings preserve native control without scattering raw flags through generic call sites.
