# Ari Agent Rules

Ari is pre-alpha. Keep changes small, typed, and easy to verify.

## Critical Constraints (MUST)

- Work primarily in `tools/ari-cli/`; treat old plugin/harness artifacts as archived context.
- Use Go for CLI/runtime code.
- Use JJ for local version control; do not rewrite `main`/`trunk()`.
- Run project tooling through Nix, not host-installed tools.
- Before finishing code changes, run `nix develop -c just verify`.

## Development Environment

- Enter tools with `nix develop` or prefix commands with `nix develop -c`.
- Preferred checks:
  - `nix develop -c go test ./...` from `tools/ari-cli/` for quick Go validation.
  - `nix develop -c just verify` from repo root for the CI-equivalent gate.
- Use `go fmt`/`gofumpt`, `go test`, and `go build` only through the Nix shell.

## Pipeline Stability

CI splits the same repo contract into separate jobs, so a change can pass one local command and still fail another gate.

- Format can fail for either repo-root Nix files or Go files:
  - root: `nix run nixpkgs#nixpkgs-fmt -- --check .`
  - Go: `nix develop -c just fmt-check` (`gofumpt -l tools/ari-cli`)
- Lint/build/test run from the Nix dev shell and are scoped by `justfile`:
  - `nix develop -c just lint`
  - `nix develop -c just build`
  - `nix develop -c just test`
- Common breakages to prevent:
  - forgetting `gofumpt` after Go edits;
  - using host Go/lint versions instead of Nix versions;
  - editing migrations without updating Atlas revision state;
  - adding timing-sensitive PTY/daemon tests without generous synchronization;
  - relying on unordered maps, filesystem order, or Git output order in tests.
- If CI fails, inspect the named job first and reproduce that exact `just` or Nix command locally before changing code.

## Test-Driven Development

- RED first: write a meaningful failing test before behavior changes or bug fixes.
- GREEN second: implement the minimum code to pass.
- REFACTOR third: clean up while tests stay green.
- Never weaken or delete tests to make code pass.
- Prefer real database tests over mocks.
- Test behavior, not implementation details.
- Do not use `assert.Contains`; assert complete expected values when exact output is known.
- Tests must not create schema inline; use Atlas-backed migrations/helpers as the schema source.
- For bug reports with logs, stack traces, or repro steps, add the reproducer test first.

## Go Runtime Rules

- Validate arguments at function entry and validate important returns before returning.
- Return errors for recoverable runtime failures: I/O, RPC, config, user/system state.
- Use `panic` only for programmer errors and impossible internal states.
- Handle or propagate every error; avoid silent failures.
- Keep behavior explicit; avoid hidden defaults and broad recover wrappers.
- Keep comments present-tense and focused on current intent, not history.

## Daemon Foundations

- Build daemon lifecycle around `context.Context` and `signal.NotifyContext`.
- Start synchronously: initialize dependencies, bind socket, then serve.
- Shut down explicitly: cancel context, close listener, wait for goroutines, clean socket/pid artifacts.
- Tie every goroutine lifetime to context cancellation or channel close.
- Use Unix socket `PlainObjectCodec` framing for local RPC.
- Remove stale Unix socket files before bind and unlink socket file on close.
- Report daemon status with at least version, pid, uptime, and socket path.
- Test signal handling through injectable seams or subprocess tests; do not signal the test runner process.

## Database and Migrations

- Preserve existing user databases during bootstrap and upgrades.
- Never rewrite or edit already-applied migration files; add forward migrations instead.
- Keep migrations forward-only by default and use Atlas revision history as source of truth.
- Use explicit backup/restore or corrective forward migration for recovery; do not reset destructively in upgrade paths.
- Run migration-related checks from the Nix shell so Atlas and SQLite versions match CI.

## JJ Workflow

- Inspect before checkpoints: `jj st --no-pager --color=never --quiet` and `jj diff --summary --no-pager --color=never --quiet`.
- Commit logical WIP slices: `jj commit -m "wip: <short-description>"`.
- Move the active bookmark after committing: `jj bookmark set wip/<topic> -r @-`.
- Use dated pause bookmarks when helpful: `jj bookmark set checkpoint/<yyyy-mm-dd>-<topic> -r @-`.
- Preview mutable local-only commits before cleanup: `jj log -r 'mutable() & ~ancestors(remote_bookmarks(remote=origin)) & ~@' --no-pager --color=never --quiet`.

## Product Direction

- Ari is the project and interface name.
- Breaking changes are acceptable when they improve the core design.
- Do not add compatibility shims or legacy aliases unless explicitly requested.
