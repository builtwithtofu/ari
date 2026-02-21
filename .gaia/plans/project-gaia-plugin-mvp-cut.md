# MVP Cut: Project GAIA Plugin

> **Status**: Draft (execution baseline)
> **Created**: 2026-02-06
> **Canonical product spec**: `doc/SPECIFICATION.md`

---

## Purpose

Define the strict P1 implementation boundary so we can ship a usable GAIA orchestration slice
without scope creep.

This cut is the execution contract for the first release candidate.

## North Star Alignment

This MVP is aligned to one core product direction:

- GAIA is a human-in-the-loop orchestrator, not a fully autonomous replacement for human judgment.
- GAIA should support broader product work, not only code edits.
- Specialist agents are expected to be imperfect; GAIA's role is to coordinate, validate, and
  escalate decisions clearly.

Phase note:

- This phase prioritizes a strong base GAIA and deterministic runtime planning context model.
- Subsystem expansion is intentionally deferred until this base behavior is reliable.

---

## MVP Outcome

Ship one complete orchestrated workflow that is demonstrably better than single-agent execution:

1. User selects `gaia` mode.
2. GAIA delegates to specialist agents.
3. Results are parsed through a strict JSON contract.
4. Work artifacts are written under `.gaia/` only.
5. Native `plan` and `build` remain unchanged.
6. The collaboration model supports both human-in-the-loop and agentic execution at a basic level.
7. Human-facing role terminology is consistent: **Operator** (interactive) and **Owner** (accountable).

If any item above fails, MVP is not complete.

---

## In Scope (P1)

## 1) Core package boundary
- Create portable plugin package at `tools/opencode-gaia-plugin/`.
- Keep runtime free of imports from `modules/editors/**` or other dotfiles-only paths.

## 2) Config and model resolution foundation
- `src/config/schema.ts` with `GaiaConfig` and `AgentOverride`.
- `src/config/defaults.ts` with `AGENT_DEFAULTS` for all declared agents.
- `src/config/loader.ts` loading `.gaia/config.jsonc` and global override file.
- `src/shared/models.ts` with deterministic model fallback resolution.

## 3) MVP agent set only
- `gaia` (primary orchestrator).
- `athena` (recon/routing).
- `hephaestus` (implementation).
- `demeter` (historian).

This P1 agent profile is also referred to as the **lean** mode.

All other agents are deferred.

## 4) Tooling surface
- `delegate_gaia` with sync path as the required path.
- `collect_results` with minimal metadata handling (no complex background manager).
- Shared contract parsing with one retry when JSON is invalid.
- `plan_gaia` constrained to `.gaia/` operations only.

## 5) Hooks (minimum viable)
- `decision-capture` for question and rejection logging.
- `rejection-feedback` prompt prefill behavior.
- `harvest-reminder` to trigger DEMETER after implementation/verification waves.

Decision hand-offs to the human should use a stable structure:

- `Context`
- `Options`
- `Recommendation`
- `Action needed`

## 6) Commands (minimum viable)
- Runtime-first planning commands via `ari flow start|iterate|execute|continue`.
- Minimal mode controls needed to exercise both collaboration styles.
- `/locked` behavior is required for safety checks.

## 7) Host adapter and integration checks
- Wire plugin into host layer without breaking native `plan` or `build`.
- Apply `smallModel` provider prefix fix (`zhipuai-coding-plan`).

---

## Explicitly Out Of Scope (P1)

- Full 9-agent roster (APOLLO, EILEITHYIA, ARTEMIS, AETHER, POSEIDON, HADES).
- Rich collaboration profile matrix (`/profile`, `/cadence`, `/review`, `/pair`, `/next`).
- Full autopilot guardrail engine and complex pause UX.
- Background orchestration manager and advanced parallel scheduling.
- Model preset system (`budget`, `balanced`, `premium`, `local`).
- A/B dual implementation runs (HEPHAESTUS_A / HEPHAESTUS_B).

Any out-of-scope item blocks this cut if pulled into active implementation.

---

## Default Decisions For This Cut

These close open questions for P1 execution:

1. **Background result collection**: use simple polling path; no event-driven manager in P1.
2. **Context-aid nudging**: low-noise nudges only when directly relevant to current task.
3. **GAIA model selection**: default to configured GAIA model; respect explicit user model change.
4. **Plan approval**: require approval for medium/large tasks; auto-proceed for trivial tasks.
5. **Runtime context sourcing**: read planning context from deterministic runtime artifacts.
6. **Default collaboration profile**: `standard` for MVP predictability.
7. **A/B implementation strategy**: defer to post-MVP.
8. **Test runner**: use Bun's built-in test runner for MVP (no Vitest in P1).
9. **Operation profile naming**: treat MVP profile as `lean`.
10. **Future custom operation profile**: allow subsystem combinations while GAIA remains mandatory.

---

## Implementation Waves

## Wave 1 - Foundation
- Package scaffold and build setup.
- Config schema/defaults/loader.
- Model resolver and shared permission constants.

Exit: plugin builds, config resolves, and model fallback logic is testable.

## Wave 2 - Core agents and contracts
- Add agent contract types and parser utilities.
- Implement prompts for GAIA, ATHENA, HEPHAESTUS, DEMETER.
- Implement agent registry and config override merge.

Exit: each MVP agent can be invoked with contract-compliant output.

## Wave 3 - Tools and hooks
- Implement `delegate_gaia`, `collect_results`, `plan_gaia`.
- Implement JSON parse retry behavior.
- Add MVP hooks (`decision-capture`, `rejection-feedback`, `harvest-reminder`).

Exit: end-to-end delegation and `.gaia/` write flow runs in one session.

## Wave 4 - Integration
- Wire plugin entrypoint and host adapter.
- Add locked-mode enforcement and runtime context visibility needed for MVP checks.
- Ensure native `plan`/`build` behavior does not regress.

Exit: GAIA works as optional mode, native modes remain intact.

## Wave 5 - Verification
- Validate non-overlap contract.
- Validate invalid JSON retry and parse failure metadata.
- Validate rejection capture flow.
- Validate extraction smoke test in external repo.

Exit: all MVP acceptance gates pass.

---

## Acceptance Gates

## Gate A - Functional
- GAIA appears as optional primary mode.
- `delegate_gaia` returns `session_id`, `model_used`, `parsed_json`, `parse_error`, `status`.
- `plan_gaia` creates and reads `.gaia/{work-unit}/plan.md`, `log.md`, `decisions.md`.
- Runtime context is persisted under `.gaia/runtime/<session>/` and queryable from Ari surfaces.

## Gate B - Safety and behavior
- Invalid JSON triggers one retry and then safe fallback metadata.
- Rejected permission flow pre-fills `Rejected <tool> because:`.
- `/locked` blocks mutation paths.
- Dangerous ops remain denied.

## Gate C - Compatibility and portability
- Native `plan` and `build` still work without GAIA orchestration side effects.
- Core plugin runs when copied outside this dotfiles repository.
- Runtime context artifacts remain non-fatal when absent before first session start.

---

## Cut Failure Conditions

Stop and re-scope immediately if any of the following occur:

- New features are added from out-of-scope list before Gate B passes.
- Native `plan`/`build` behavior changes in order to support GAIA.
- Plugin core takes dependency on host-specific module paths.
- Contract parsing remains flaky after retry path is implemented.

---

## Post-MVP Backlog Entry Point

After this cut is accepted, expand in this order:

1. Add remaining six sub-agents.
2. Add background parallel orchestration manager.
3. Add full collaboration command matrix.
4. Add richer pair-loop checkpoints and review depth controls.
5. Tune model assignments and cost presets from real usage data.

Roster note:

- Future full-pantheon expansion is not constrained to the current names; additional Greek-god
  specialists and user-defined specialist names are allowed as long as GAIA remains the primary
  orchestrator.

---

## Sign-off Checklist

- [ ] In-scope items implemented.
- [ ] Out-of-scope boundary respected.
- [ ] Acceptance Gates A, B, C passed.
- [ ] Companion doc reflects actual implementation mechanics.
- [ ] Main plan updated for any MVP deviations.
