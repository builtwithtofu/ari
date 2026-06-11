# ADR 0012: Harness peer-parity onboarding contract

Status: accepted

Date: 2026-06-12

## Context

Ari now supports five harnesses (Claude Code, Codex, OpenCode, pi, Grok CLI) behind the normalized contract from ADR 0004. Adding pi and grok exposed which integration facts were previously implicit or Claude-shaped: invocation modes were a Claude-only option, session persistence/resume facts were hardcoded rather than adapter-reported, and there was no checklist for what "officially supported" means. Without a contract, each new harness risks landing at a different, undocumented level of support.

## Decision

A harness is officially supported only when all of the following hold. Registration in `NewDefaultHarnessRegistry` is the last step, not the first.

- **Descriptor-declared invocation modes.** The adapter descriptor lists the `HarnessInvocationMode` values it supports (headless, background, server). The call envelope rejects undeclared modes before `Start` is invoked; adapters validate again at `Start`.
- **Adapter-reported session facts.** `Start` returns persistence, resume mode, resume cursor, and provider thread id on `ExecutorRun`. Unknown is a valid honest answer only for executors outside the official set; official adapters must report real facts.
- **Auth descriptor and slots.** The descriptor declares status/login/logout/named-slot support levels, slot scope, login methods, non-empty risk labels, and non-empty caveats. Named slots either project a per-slot native config root (`CODEX_HOME`, `CLAUDE_CONFIG_DIR`, `GROK_HOME` pattern) or require an Ari-owned projection from the secrets boundary (ADR 0007); HOME-wide projection is not acceptable. A default auth slot is seeded by migration.
- **At least one delivery channel or explicit unsupported.** Delivery capabilities are declared only when the adapter genuinely implements them; dead conditional wiring is not allowed. Admission and completion signals follow ADR 0004 (attempted vs terminal).
- **Capability honesty.** Adapters declare only capabilities they meet. A harness may omit a shared capability (grok omits measured token telemetry because headless output has no usage counters); callers see the gap through capability negotiation instead of fabricated data.
- **Fanout eligibility.** The harness resolves through the registry for profile-backed ephemeral worker calls without harness-specific changes to fanout code.
- **Fake persona.** The fake harness has a persona that mirrors the real CLI's flags, output formats, session/resume semantics (including stateful turn counting under `ARI_FAKE_HARNESS_STATE_DIR`), auth flows, and failure shapes. Persona facts are validated against the real CLI before they are frozen.
- **Contract-suite row.** One row in the harness adapter contract table covering capabilities, invocation modes, observation/delivery capabilities, auth facts, and an injected-runner `Start` that proves the adapter-reported session ref.
- **Proof coverage.** `auth-proof-fake` exercises doctor, status, applicable login flows, and a session start for the harness through the full CLI → daemon → adapter → process boundary.

## Consequences

- Adding a harness is a predictable checklist: adapter file, registry entry, name constant, executable env var, options dispatch, migration, fake persona, contract row, proof coverage, ADR 0004 mapping section.
- Support levels are visible in code (descriptors) and tests (contract table) rather than tribal knowledge; capability gaps are explicit.
- The fake harness doubles as the executable spec of each integration; drift between fake and real CLIs is a bug, and `auth-live-smoke` remains the opt-in reconciliation path.
- Cost: onboarding a harness requires touching the fake harness and proof packages even for experiments. Experimental adapters can exist behind `ReplaceForTest`/custom registries without official registration.

## Alternatives considered

- **Best-effort onboarding per harness:** faster initially, but produced the Claude-only invocation mode and dead OpenCode delivery wiring this contract removes.
- **Plugin-style external adapters:** decouples harness work from the core, but Ari's secrets boundary, delivery dispatcher, and proof packages need in-tree integration; premature until adapter count grows.
- **Capability parity enforcement (all harnesses declare identical capabilities):** simpler tables, but forces fabricated data (grok token usage) and contradicts ADR 0004's capability-limitation surfacing.
