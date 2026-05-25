# ADR 0008: Harness-neutral provider option surfaces

Status: accepted

Date: 2026-05-25

## Context

Ari is a durable, headless workspace runtime for LLM harnesses. It enhances Claude Code, Codex, OpenCode, and future harnesses without becoming a harness-specific wrapper UI.

Harness auth and provider setup sometimes require provider-specific inputs. For example, one harness may need a provider id, another may need a config root, and another may need an auth-content payload or provider method. Exposing those needs as one-off CLI flags such as `--opencode-auth-content-file` makes Ari's public surface provider-shaped instead of Ari-shaped. It also encourages bypassing the daemon-owned auth workflow and secrets boundary with ad hoc provider plumbing.

ADR 0001 makes daemon operations the source of product behavior. ADR 0006 says Ari enhances existing harnesses without replacing their semantics. ADR 0007 requires Ari-owned secrets to move through a daemon-owned store, grants, audit, redaction, and short-lived projection.

## Decision

Ari public auth and run surfaces must stay harness-neutral.

- Do not add provider-specific public CLI flags, RPC fields, config keys, or workflow branches for one harness unless the command itself is explicitly a provider/harness diagnostic or adapter-development escape hatch.
- Normal UX commands such as `ari auth login`, `ari auth logout`, `ari auth status`, `ari auth doctor`, session start, and ephemeral calls must use Ari concepts: harness, auth slot, workspace, profile, provider option, method, secret reference, grant, and projection summary.
- When a provider-specific input is unavoidable, expose it through a general provider-options mechanism rather than a dedicated flag. The option surface must be typed or namespaced enough for adapter validation, for example a generic map/list such as provider options, adapter options, or `ari api` payload fields that are validated by the daemon and adapter.
- Provider options may select provider behavior, but secret values still must not travel through ordinary metadata, profile defaults, operation records, logs, command history, or provider-specific flags. Secret values must enter only through the Ari secrets boundary accepted in ADR 0007.
- Adapter-specific interpretation belongs behind daemon operations and harness adapters. Clients may prompt and render, but they must not own provider-specific runtime state or bypass daemon validation.
- Escape hatches should be general and explicit, such as `ari api` or a future harness-neutral `--provider-option key=value` shape. They must be documented as advanced surfaces, not the primary auth workflow.

## Consequences

- Ari remains harness-neutral as new providers and harnesses are added.
- Guided auth work must model provider-specific requirements as daemon-owned options and adapter validation, not as one-off CLI affordances.
- OpenCode Ari-owned auth provisioning cannot be solved by adding an `opencode`-named flag. It needs either a guided generic provider-option/secret-reference workflow or a daemon/API escape hatch consistent with ADR 0007.
- Tests should assert the absence of provider-specific public flags for normal workflows when this boundary is at risk.
- This may require more design work up front, but prevents public contracts that are hard to remove and inconsistent across harnesses.

## Alternatives considered

- **Provider-specific CLI flags:** Simple for one immediate provider, but it leaks adapter details into Ari's public workflow and creates an uneven surface as more providers need special cases.
- **Only `ari api` for all provider-specific setup:** Keeps curated CLI neutral, but may be too low-level for normal auth setup. It remains acceptable as an advanced escape hatch while guided UX matures.
- **Provider-specific subcommands:** Clear for adapter debugging, but too close to replacing harness CLIs and not appropriate for normal Ari auth workflows.
