# Ari

> **Attachable headless workspace runtime for LLM-assisted development.**

---

## What is Ari?

Ari is a local control plane for LLM workspaces. It keeps agents, commands, process output, context, proofs, and attention state alive behind a daemon so clients can detach, reattach, inspect, and compose workflows.

The product model is closer to a Docker daemon plus tmux for LLM-assisted development than to a single chat UI. The CLI is the first client; future GUI, TUI, MCP, remote, and automation clients should build on the same daemon API.

Core concepts:

- **Headless first**: every product operation belongs behind the daemon API before it appears in a UI.
- **Workspace runtime**: work is organized around one-or-more-folder workspaces, not legacy plan DAGs.
- **Background persistence**: agents and commands can keep running after a client exits.
- **Attachable clients**: clients render, prompt, format, and compose daemon operations for users.
- **Attention state**: idle agents, blockers, approvals, failed commands, and completions should bubble up from runtime facts.

See `docs/ep/ari-workspace-runtime.md` for the durable direction and `docs/adr/` for accepted architecture decisions.

---

## Development and verification

Run all project tooling through Nix so local behavior matches CI:

```bash
# Full verification gate
nix develop -c just verify

# Targeted Go test runs from tools/ari-cli/
nix develop -c go test ./...
```

For migration-related checks, run them from `nix develop` as well so Atlas and SQLite tool versions are consistent.

### Agent harness smoke checks

Default verification never requires provider credentials, network access, or billable model calls. To check locally installed harness command assumptions, run:

```bash
nix develop -c just agent-smoke
```

The smoke target only runs metadata probes:

- `codex --version`
- `claude --version`
- `opencode --version`

Ari resolves these command names at runtime unless you set explicit overrides:

```bash
ARI_CODEX_EXECUTABLE=/path/to/codex \
ARI_CLAUDE_EXECUTABLE=/path/to/claude \
ARI_OPENCODE_EXECUTABLE=/path/to/opencode \
  nix develop -c just agent-smoke
```

Fixture tests are the default adapter contract tests. `agent-smoke` is a credential-free local binary check. Authenticated model-call integration tests are intentionally separate and must stay opt-in.

---

## CLI workflow surfaces

The current Go runtime exposes workflow commands and a direct daemon JSON-RPC escape hatch under `tools/ari-cli/`.

- Ari is headless first: the daemon/API owns product behavior and state; CLI and future UI surfaces are clients.
- Onboarding: `ari init` renders the daemon-owned `init.state`, `init.options`, and `init.apply` flow. The only day-one choice is the default harness.
- Dashboard: `ari` and `ari status` render the daemon-owned active workspace dashboard, including attention, resume actions, and cwd workspace memberships without auto-switching context.
- API escape hatch: `ari api <method> --params '<json>'` calls daemon JSON-RPC directly for scripts, debugging, and low-level operations that are not part of the normal workflow surface.
- Workspaces: the daemon owns workspace creation, folder membership, and active context. Use `ari workspace use <id-or-name>` to set the daemon active workspace. A workspace contains one or more folders, and a folder can belong to multiple workspaces.
- Helpers: each workspace can have an ordinary profile named `helper`. Home and project helpers share one helper contract; scope comes from workspace context, not from a profile role/type field.
- Ari tools: helper-visible settings/profile/self-check/run-forensics actions are daemon-owned tool calls with scoped metadata. Writes require explicit, single-use approval markers.
- Low-level operation mirrors such as agent/process/profile/final-response/telemetry commands are hidden from normal help while the workflow surface is refined. Use `ari api` for direct method access when automation needs the underlying daemon operation.

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                        Clients                          │
│    ┌──────┐  ┌──────┐  ┌─────────┐  ┌───────────────┐  │
│    │ CLI  │  │ TUI  │  │ GUI/IDE │  │ Remote/Agents │  │
│    └──┬───┘  └──┬───┘  └────┬────┘  └───────┬───────┘  │
└───────┼─────────┼───────────┼───────────────┼──────────┘
        │         │           │               │
        ▼         ▼           ▼               ▼
┌─────────────────────────────────────────────────────────┐
│             Daemon API / JSON-RPC boundary              │
│        product operations, projections, attention       │
└───────────────────────────┬─────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                       Ari Daemon                        │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  │
│  │ Workspaces  │  │ Agents/PTYs │  │ Runtime Store   │  │
│  └─────────────┘  └─────────────┘  └─────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

The runtime is headless by design. Product behavior belongs behind daemon service methods first. Clients may expose a subset of methods and compose them into better workflows, but they do not own Ari runtime state.

---

## How it works

```bash
ari init                                  # choose a default harness and ensure a normal home workspace/helper
ari workspace create my-app               # create a project workspace with a project helper when defaults exist
ari workspace use my-app                  # make daemon active context explicit
ari status                                # show active workspace attention, resume actions, and cwd memberships
ari api workspace.list --params '{}'      # direct JSON-RPC escape hatch for scripts/debugging
```

Ari asks before she acts. Helpers teach, explain, diagnose, draft, route, and request approval; they do not write project source. Coding work belongs to explicitly configured specialist profiles such as builders, reviewers, or test writers. Ari does not install or authenticate external harnesses, poll provider model catalogs, or turn natural language into every CLI command.

---

## Current documentation

- `docs/ep/ari-workspace-runtime.md` — product direction.
- `docs/adr/0001-headless-daemon-api-authority.md` — daemon API authority.
- `docs/adr/0002-workspace-as-runtime-unit.md` — workspace runtime unit.
- `docs/protocol-spec.md` — daemon API and attach boundary.
- `docs/workspace-lifecycle.md` — workspace lifecycle and folder membership.
- `docs/plan-schema.md` — task/context concepts replacing legacy plan-DAG framing.
- `docs/headless-runtime.md` — headless runtime and helper model.
- `docs/tool-projection.md` — projection contract.
