# Project GAIA

Project GAIA is an OpenCode orchestration plugin focused on practical human-in-the-loop development.

It adds an optional `gaia` mode that coordinates specialist agents while keeping native OpenCode
`plan` and `build` behavior unchanged unless you opt in.

This repository is pre-alpha. Interfaces and behavior can change quickly while the core model hardens.

## Why this exists

Most agent systems break down at handoffs, not code generation. GAIA focuses on that gap:

- turn user intent into small, explicit work units,
- route work to specialists with clear contracts,
- keep decisions, rationale, and outcomes recoverable under `.gaia/`.

## Quickstart (new contributors)

From a fresh clone, run this once:

```bash
nix develop
bun install --cwd tools/opencode-gaia-plugin
bun install --cwd tools/opencode-gaia-harness
bun run --cwd tools/opencode-gaia-harness cli quickstart
```

`quickstart` runs a full onboarding confidence flow:

- preflight checks (`doctor`) for template + CLI readiness,
- sandbox bootstrap,
- lean-subagent wiring smoke,
- `gaia_init` tool smoke,
- locked-mode mutation guard smoke.

If it passes, you have a local sandboxed setup ready for development and experimentation.

## Common workflows

Start an isolated OpenCode web server:

```bash
bun run --cwd tools/opencode-gaia-harness cli serve-web
```

Launch OpenCode Web in a fresh temporary sandbox workspace (recommended manual web flow):

```bash
bun run --cwd tools/opencode-gaia-harness cli manual-web "critical bug" --model opencode/glm-5-free --port 4096
```

Run bug-repro harness (reproducer-first flow):

```bash
bun run --cwd tools/opencode-gaia-harness cli bug doc/bug-report.example.md
```

Run all harness stages in one command:

```bash
bun run --cwd tools/opencode-gaia-harness cli suite full doc/bug-report.example.md
```

Launch a temporary sandbox workspace in OpenCode TUI (best manual feel test):

```bash
bun run --cwd tools/opencode-gaia-harness cli manual-tui "critical bug"
```

This creates `.sandbox/workspaces/<timestamp>-critical-bug/` and starts TUI in that workspace
with the GAIA plugin loaded from sandbox config.

Each manual workspace is seeded with scenario projects:
- `go-hello-planning/`
- `planning-challenge/`
- `refactor-sandbox/`
- `bug-hunt/`

To force a specific model (useful when bringing your own provider keys):

```bash
bun run --cwd tools/opencode-gaia-harness cli manual-tui "critical bug" --model opencode/glm-5-free
```

This sets both the top-level OpenCode session model and GAIA lean subagent model override
(`OPENCODE_GAIA_AGENT_MODEL`) for that manual run.

Run GAIA prompt guardrail checks only:

```bash
bun run --cwd tools/opencode-gaia-harness cli prompt-quality-smoke
```

## Ariadne companion CLI (experimental)

Ariadne is the user-facing command surface: `ari`.

Project naming split:

- project name stays `Project GAIA`,
- protocol/CLI command surface is `ari` (Ariadne), with `gaia` retained as a compatibility alias.

Primary role right now:

- query and navigate `.gaia/` state for both human operators and GAIA agents,
- run deterministic flow transitions that can be resumed after interruption.

Canonical command contracts:

- `ari flow start|iterate|execute|continue`
- `ari query all|sessions|session|lifecycle|surfaces`

Query-first examples:

```bash
ari query all --json
ari query sessions --json
ari query session --session s1 --json
ari query lifecycle --session s1 --json
ari query surfaces --json
```

Deterministic flow examples:

```bash
ari status
ari flow start --session s1 --stream feature-x --scope "Add hello flag"
ari flow iterate --session s1 --note "tighten acceptance checks"
ari flow execute --session s1
ari flow continue --session s1
```

Legacy lifecycle helpers remain available during transition:

```bash
ari plan start --session s1 --stream feature-x --scope "Add hello flag"
ari plan execute --session s1
ari work continue --session s1
```

Sandbox navigation shortcuts:

```bash
ari sandbox list
ari sandbox tui "critical bug" --model opencode/glm-5-free
ari sandbox web "critical bug" --model opencode/glm-5-free --port 4096
```

## How GAIA mode behaves

- GAIA is opt-in.
- Native OpenCode flows remain the default when GAIA is not selected.
- Slim default mode uses `gaia` plus hidden specialists (`athena`, `hephaestus`, `demeter`).
- Full pantheon remains optional and configurable; default UX stays slim.
- Work-unit artifacts are written under `.gaia/<work-unit>/` and older work units are archived under
  `.gaia/archive/work-units/`.

## Repository map

- `tools/opencode-gaia-plugin/` - portable GAIA plugin core
- `tools/opencode-gaia-harness/` - sandbox CLI and confidence flows
- `doc/` - project-facing docs and specifications
- `.gaia/` - runtime artifacts and operational plans

## Development checks

```bash
bun run --cwd tools/opencode-gaia-plugin check
bun run --cwd tools/opencode-gaia-harness check
```

## Learn more

- Product direction and behavior model: `doc/SPECIFICATION.md`
- Information architecture policy: `doc/INFORMATION_ARCHITECTURE.md`
- Compatibility and migration policy: `doc/COMPATIBILITY_POLICY.md`
- Planning-first maturity policy: `doc/HITL_Protocol_Maturity.md`
- Sandbox setup and full command reference: `doc/Sandbox_Harness.md`
- Active MVP boundary: `.gaia/plans/project-gaia-plugin-mvp-cut.md`
- GAIA initialization requirements: `.gaia/plans/gaia-init-spec.md`
