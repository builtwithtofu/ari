# OpenCode Sandbox Harness

This document describes a clean, reproducible OpenCode harness setup for GAIA development.

## Goals

- Run OpenCode in a sandbox that does not inherit host config.
- Support local smoke testing and agent-driven bug reproduction flows.
- Provide a devcontainer and Docker path for reproducible execution.

## Isolation Model

All harness commands route OpenCode through `tools/opencode-gaia-harness/src/` runtime setup, which sets:

- `HOME=.sandbox/home`
- `XDG_CONFIG_HOME=.sandbox/home/.config`
- `XDG_CACHE_HOME=.sandbox/home/.cache`
- `XDG_DATA_HOME=.sandbox/home/.local/share`
- `OPENCODE_CONFIG_DIR=.sandbox/opencode`
- `OPENCODE_CONFIG=.sandbox/opencode/opencode.jsonc`
- `OPENCODE_DISABLE_CLAUDE_CODE=1`

This avoids accidental use of host-level OpenCode or Claude config.

## Default Sandbox Config

`bun run --cwd tools/opencode-gaia-harness cli bootstrap` creates
`.sandbox/opencode/opencode.jsonc` if missing.

It also syncs the GAIA plugin template from
`tools/opencode-gaia-harness/templates/gaia-plugin.ts` into
`.sandbox/opencode/plugins/gaia-plugin.ts`, and installs `@opencode-ai/plugin`
for local tool registration.

Default profile:

- `model`: `opencode/glm-5-free`
- `small_model`: `opencode/glm-5-free`
- `permission`: ask for `bash`, `edit`, `write`
- `server`: host `0.0.0.0`, port `4096`

## Commands

- Bootstrap sandbox:

```bash
bun run --cwd tools/opencode-gaia-harness cli bootstrap
```

- Run preflight checks (template + `bun` + `opencode`):

```bash
bun run --cwd tools/opencode-gaia-harness cli doctor
```

- Run onboarding quickstart (recommended first run):

```bash
bun run --cwd tools/opencode-gaia-harness cli quickstart
```

This runs:

- `doctor`
- `bootstrap`
- `lean-subagents-smoke`
- `locked-smoke`

- Run OpenCode with sandbox env:

```bash
bun run --cwd tools/opencode-gaia-harness cli opencode
```

- Launch OpenCode TUI in a fresh temporary manual-testing workspace:

```bash
bun run --cwd tools/opencode-gaia-harness cli manual-tui "critical bug"
```

This creates `.sandbox/workspaces/<timestamp>-critical-bug/` and launches TUI in that workspace
while keeping plugin/config sandboxed under `.sandbox/opencode/`.

Use `--model provider/model` to force both the top-level model and GAIA subagent model override:

```bash
bun run --cwd tools/opencode-gaia-harness cli manual-tui "critical bug" --model opencode/glm-5-free
```

- Start web server:

```bash
bun run --cwd tools/opencode-gaia-harness cli serve-web
```

- Launch OpenCode web UI in a fresh temporary manual-testing workspace:

```bash
bun run --cwd tools/opencode-gaia-harness cli manual-web "critical bug" --model opencode/glm-5-free --port 4096
```

This mirrors `manual-tui` behavior, but serves web from a new workspace under
`.sandbox/workspaces/<timestamp>-critical-bug/`.

Each manual workspace is seeded with scenario projects to exercise GAIA behavior:

- `go-hello-planning/` (simple plan-first flow)
- `planning-challenge/` (question-first planning depth)
- `research-ops-planning/` (non-coding research and operations planning)
- `refactor-sandbox/` (behavior-preserving refactor)
- `bug-hunt/` (reproducer-first bug triage and fix)

Companion CLI shortcuts (Cobra, experimental):

- Installed binary command identity is `ari` (with `gaia` alias).

```bash
ari sandbox list
ari sandbox tui "critical bug" --model opencode/glm-5-free
ari sandbox web "critical bug" --model opencode/glm-5-free --port 4096
```

Companion CLI query shortcuts (`.gaia/` context for users and agents):

```bash
ari query all --json
ari query sessions --json
ari query session --session s1 --json
ari query lifecycle --session s1 --json
ari query surfaces --json
```

Companion CLI deterministic flow shortcuts:

```bash
ari flow start --session s1 --stream feature-x --scope "Add hello flag"
ari flow iterate --session s1 --note "tighten acceptance checks"
ari flow execute --session s1
ari flow continue --session s1
```

- Start API server:

```bash
bun run --cwd tools/opencode-gaia-harness cli serve-api
```

- Run smoke prompt:

```bash
bun run --cwd tools/opencode-gaia-harness cli smoke
```

`cli smoke` defaults to a non-interactive safe permission profile:

- allow: `bash`, `read`
- deny: `edit`, `write`

Override with `OPENCODE_PERMISSION` when needed.

Timeout defaults are built in. Override with env vars when needed:

- `OPENCODE_HEARTBEAT_MS`
- `OPENCODE_SMOKE_HEARTBEAT_MS`
- `OPENCODE_PLUGIN_TIMEOUT_MS`
- `OPENCODE_BUG_HEARTBEAT_MS`
- `OPENCODE_LIST_TIMEOUT_MS`
- `OPENCODE_SMOKE_TIMEOUT_MS`
- `OPENCODE_SMOKE_IDLE_TIMEOUT_MS`
- `OPENCODE_BUG_TIMEOUT_MS`
- `OPENCODE_BUG_IDLE_TIMEOUT_MS`

Idle timeout vars are optional and disabled by default. Set them when you want faster
stuck detection for interactive runs.

Heartbeat defaults to 10 seconds and prints `[harness] still running ...` while long commands run.

- List currently available free models:

```bash
bun run --cwd tools/opencode-gaia-harness cli list-free-models
```

- Run bug repro harness with attached report:

```bash
bun run --cwd tools/opencode-gaia-harness cli bug doc/bug-report.example.md
```

`cli bug` defaults to an implementation profile:

- allow: `bash`, `read`, `edit`, `write`

Override with `OPENCODE_PERMISSION` if you want stricter behavior.

- Run lean subagent wiring smoke test:

```bash
bun run --cwd tools/opencode-gaia-harness cli lean-subagents-smoke
```

This confirms GAIA is primary and lean specialists are configured as hidden subagents by default.

- Run GAIA prompt-quality smoke test:

```bash
bun run --cwd tools/opencode-gaia-harness cli prompt-quality-smoke
```

This checks the GAIA prompt for required guardrails around blocked mutation behavior and delegation.

- Run locked-mode mutation guard smoke test:

```bash
bun run --cwd tools/opencode-gaia-harness cli locked-smoke
```

This validates that `mode: locked` blocks `.gaia` mutation paths.

- Run harness suite modes:

```bash
bun run --cwd tools/opencode-gaia-harness cli suite basic
bun run --cwd tools/opencode-gaia-harness cli suite plugin
bun run --cwd tools/opencode-gaia-harness cli suite quality
bun run --cwd tools/opencode-gaia-harness cli suite locked
bun run --cwd tools/opencode-gaia-harness cli suite bug doc/bug-report.example.md
bun run --cwd tools/opencode-gaia-harness cli suite full doc/bug-report.example.md
```

The bug harness prompt enforces reproducer-first TDD, low-mock tests, and exact assertions.

## Devcontainer

Use `.devcontainer/devcontainer.json`.

- Image base: Node 22
- Installs: Bun and OpenCode CLI (`opencode-ai@1.2.6`)
- Forwards port `4096`
- Post-create: bootstrap sandbox, install plugin deps, run checks

## Docker Compose

Headless, browser-accessible sandbox server:

```bash
docker compose -f docker-compose.sandbox.yml up --build
```

Then open the served OpenCode web UI on port `4096`.

## Notes

- Current harness validates environment and workflows first.
- GAIA tool/plugin integration in OpenCode runtime should be layered on top of this sandbox.
- GAIA runtime archives older `.gaia/<work-unit>/` directories under
  `.gaia/archive/work-units/` as new work units are written.

## Growth roadmap

Grow this harness in small stages so confidence increases with each unit:

1. `L0` Environment confidence
   - bootstrap sandbox
   - list free models
   - run smoke prompt
2. `L1` Runtime confidence
   - call `runGaiaWorkUnit` and `runDelegateGaiaTool` in automated tests
   - validate `.gaia/<unit>` artifacts and parse metadata
3. `L2` Plugin loading confidence
   - load GAIA plugin from `.opencode/plugins/`
   - verify custom tool registration in a real OpenCode session (`delegate_gaia`)
4. `L3` Agentic workflow confidence
   - run bug-repro harness end-to-end on sample bug reports
   - ensure reproducer-first TDD and exact assertions
5. `L4` Regression confidence
   - maintain a `doc/bug-reports/` corpus
   - replay corpus in CI/nightly container runs
