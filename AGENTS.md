# Ari Agent Rules

## Scope

- Work primarily in `tools/ari-cli/`.
- Treat old plugin/harness artifacts as archived context.
- Use Go for CLI/runtime code.
- Use JJ for local VCS operations.
- Use Nix for project tooling.

## Product constraints

- Ari is the project and interface name.
- Read `CONTEXT.md` before product planning or implementation; it is the glossary.
- Read `docs/ep/ari-workspace-runtime.md` before product-shaping work.
- Read `docs/adr/*` before implementation work. Accepted ADRs are hard constraints unless the user asks to change them.
- Read other `docs/ep/*` only for the touched topic. EPs guide product intent; they may drift as evidence changes.
- Ari is a durable, headless workspace runtime for LLM harnesses.
- Workspaces are durable switchable units of work. A workspace may contain one or more folders and multiple peer harness sessions.
- Ari enhances existing harnesses; it does not replace Claude Code, Codex, OpenCode, or future harnesses.
- Sticky sessions are persistent human-facing harness sessions.
- Ephemeral calls are inspectable bounded worker harness invocations, not provider-specific subagents.
- Daemon operations are the product authority. CLI is the current control/story surface. `ari api` is the fine-grained escape hatch.
- Breaking changes are allowed when they improve the core design.
- No compatibility shims or legacy aliases unless requested.
- For OpenCode harness auth research/integration, use `github.com/anomalyco/opencode`; do not use `opencode-ai/opencode` as evidence.

## Documentation discipline

- Keep `CONTEXT.md` as glossary only.
- Use ADRs for accepted durable decisions.
- Use EPs for product direction and intended outcomes.
- Surface doc/code contradictions before changing governed code.

## Default validation

- Before finishing code changes, run `nix develop -c just verify` from repo root.
- For narrow Go checks, run `nix develop -c go test ./...` from `tools/ari-cli/`.
- Use `go fmt`/`gofumpt`, `go test`, and `go build` only through Nix.
- If a CI job fails, reproduce that exact `just` or Nix command before changing code.

## Formatting and CI gates

- Root Nix format: `nix run nixpkgs#nixpkgs-fmt -- --check .`
- Go format: `nix develop -c just fmt-check`
- Lint: `nix develop -c just lint`
- Build: `nix develop -c just build`
- Test: `nix develop -c just test`
- Full gate: `nix develop -c just verify`

Avoid:

- host-installed Go/lint/tool versions;
- missing `gofumpt` after Go edits;
- unordered map/filesystem/Git output assumptions in tests;
- timing-sensitive PTY/daemon tests without generous synchronization.

## Daemon

- Build lifecycle around `context.Context` and `signal.NotifyContext`.
- Start synchronously: initialize dependencies, bind socket, then serve.
- Shut down explicitly: cancel context, close listener, wait for goroutines, clean socket/pid artifacts.
- Tie goroutine lifetime to context cancellation or channel close.
- Use Unix socket `PlainObjectCodec` framing for local RPC.
- Remove stale Unix socket files before bind; unlink socket file on close.
- Report daemon status with at least version, pid, uptime, and socket path.
- Test signal handling through injectable seams or subprocess tests; do not signal the test runner process.

## Database and migrations

- Ari is pre-alpha; migrations may be rewritten when it keeps the schema clean.
- Preserve existing user databases only when the task explicitly targets upgrade/preservation behavior.
- Use sqlc for database queries.
- Use Atlas revision history as source of truth when migrations are present.
- Run migration checks through Nix so Atlas and SQLite versions match CI.
