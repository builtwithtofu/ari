# ADR 0013: User-facing presentation and copy

Status: accepted

Date: 2026-06-27

## Context

Ari coordinates multiple harnesses, providers, models, sessions, auth mechanisms, runtime states, tools, timelines, attention signals, and workspace events. Each source has its own terms and edge cases. If those terms become the primary user interface, Ari becomes provider-shaped: users must learn every harness's native vocabulary before they can understand a workspace.

ADR 0004 already decides that Ari normalizes harness behavior into Ari-owned concepts. ADR 0006 says Ari enhances existing harnesses rather than replacing them. ADR 0008 keeps provider setup harness-neutral, and ADR 0012 requires harnesses to declare their capabilities and auth/session behavior. This ADR makes the user-facing presentation rule explicit: Ari owns the default language, copy, status names, and affordances shown to humans and agent-facing Ari tools.

Some provider facts do not translate cleanly. Ari still needs native detail for debugging, support, advanced configuration, resume/attach, adapter development, and capability explanation. The decision is not to hide native facts; it is to keep them secondary and intentional.

Prior art supports this split. T3 Code uses normalized provider/model display metadata while retaining provider/model identifiers. OpenRouter exposes one normalized API surface and transforms to provider-native calls, with provider routing and debug escape hatches. Vercel AI SDK and LiteLLM similarly provide normalized provider/model interfaces with provider-specific metadata or aliases. SoloTerm uses product-level runtime statuses while leaving raw process/tool output inspectable.

## Decision

Ari's default presentation is opinionated, user-facing, and affordance-rich.

Rules:

- **Ari language comes first.** Primary copy uses Ari product terms, not provider-native terms. Examples include Ready, Running, Needs auth, Blocked, Failed, Stopped, Sticky session, Ephemeral call, Workspace activity, Attention, Auth slot, and Harness session.
- **Top-level statuses stay small and actionable.** Ari should prefer a stable top-level vocabulary that tells the user what they can do next. Nuance belongs in detail, next-step, badge, and source fields, not in an ever-growing status enum.
- **Every unclear state needs an affordance.** If a status may surprise the user, the presentation should include a short explanation, next step, caveat, or detail pointer. Ari copy should answer "what is happening?" and "what can I do?" before exposing raw facts.
- **Adapters provide facts; Ari provides copy.** Harness adapters report factual identity, capability, provider/model/session/runtime metadata, and safe native details. Ari's presentation layer owns labels, badges, status copy, grouping, remediation text, and default/raw visibility policy.
- **Native/raw detail is an explicit escape hatch.** Provider-faithful terms, raw IDs, native payload fragments, transformed request bodies, and adapter diagnostics may be exposed only through deliberate detail/debug/advanced views, API options, or configuration. They must be clearly secondary to the Ari view.
- **Raw detail is safe by default.** Raw/native views must preserve the secrets boundary: no credentials, projected secret values, auth content, bearer tokens, device codes, or sensitive payloads. Redaction is part of the presentation contract.
- **Do not flatten away meaningful differences.** If a harness lacks a capability or has weaker semantics, Ari should present that limitation clearly instead of pretending all harnesses are equivalent.
- **Display/read normalization precedes control normalization.** Ari should prove the vocabulary in read/display surfaces before moving configuration and command inputs onto the same normalized concepts. The destination includes normalized control, with native provider options remaining explicit advanced escape hatches.
- **Tests protect presentation semantics.** Tests should assert the behavior and copy contract users depend on, not provider-internal raw strings unless the raw/detail escape hatch is the behavior under test.

## Consequences

- CLI, TUI, GUI, MCP, remote clients, and Ari tools can share one product language instead of inventing display rules independently.
- Daemon responses may need presentation-ready normalized fields plus safe source/detail fields so clients can render consistently without owning status semantics.
- Adapter descriptors and source facts become input to presentation, not user-facing copy policy.
- Documentation, help text, errors, diagnostics, and command output should be reviewed for Ari-first language when touched.
- Raw/native escape hatches remain necessary and valuable, but adding one requires explicit scope, labeling, and redaction.
- Some implementation work will move logic out of client formatting and provider-specific branches into a shared presentation/read-model layer.
- The stable top-level vocabulary may initially feel less precise than provider-native states. Ari must compensate with detail, next-step, source fields, and good explanations rather than by leaking provider terms into the primary view.

## Alternatives considered

- **Provider-faithful primary language:** preserves every native nuance, but makes Ari feel like a collection of wrappers and forces users to understand each harness before acting.
- **Lowest-common-denominator abstraction:** gives uniform labels, but hides real differences and weakens Ari's ability to explain capability limits.
- **Client-owned copy and status mapping:** lets each UI move quickly, but creates drift across CLI, TUI, GUI, MCP, and tools.
- **Raw-first expert interface:** useful for debugging, but wrong as the default product surface because it leaks implementation details and provider vocabulary into normal workflows.
