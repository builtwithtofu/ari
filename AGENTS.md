# Ari Agent Rules

This repository is pre-alpha. Keep changes small, typed, and easy to verify.

## Alpha Evolution

- This project is greenfield/pre-alpha.
- Breaking changes are acceptable when they improve the core design.
- Do not add compatibility shims or legacy aliases unless explicitly requested.

## Version Control

- This is a JJ project. Use JJ-first workflows for local version control operations.
- Create frequent JJ checkpoints during active work by committing logical WIP slices.
- Keep a moving WIP bookmark for visibility while iterating.

### JJ Checkpoints

- Check state before checkpoint: `jj st --no-pager --color=never --quiet` and `jj diff --summary --no-pager --color=never --quiet`.
- Create checkpoint commit: `jj commit -m "wip: <short-description>"`.
- Move/update bookmark to latest checkpoint: `jj bookmark set wip/<topic> -r @-`.
- Use dated milestone bookmarks for stable pauses: `jj bookmark set checkpoint/<yyyy-mm-dd>-<topic> -r @-`.
- Create checkpoints after each completed task/wave, before risky refactors, and before rebases.

### JJ Safety Boundaries

- JJ checkpoint commits are safe to rewrite/replace while they are local-only (not published).
- It is acceptable to override local WIP history by creating a new checkpoint and moving the `wip/<topic>` bookmark.
- `main` (trunk) is the sacred boundary: do not rewrite commits on `main`.
- Published downstream stacks may be rewritten only when clearly safe; if safety is uncertain, ask the user first.
- Before cleanup or history rewrites, preview local-only mutable commits with: `jj log -r 'mutable() & ~ancestors(remote_bookmarks(remote=origin)) & ~@' --no-pager --color=never --quiet`.

## Development Environment

- Enter the environment with `nix develop`.
- Use Go tooling for the Ari CLI baseline.
- Prefer `go run`, `go test`, and `go fmt` when needed.

## Language and Typing

- Use Go for CLI/runtime code.
- Keep code explicit and easy to test.

## Comments and Documentation

- Keep comments in present tense.
- Do not reference deleted or replaced code in comments.
- Prefer comments that explain current intent, not history.

## Critical Constraints (MUST)

### Test-Driven Development

- RED first: write failing test before implementation.
- GREEN second: write minimum code to pass.
- REFACTOR third: clean up while keeping tests green.
- NEVER modify tests to make them pass; fix the code.
- NEVER use `assert.Contains`; assert on complete expected values.
- Prefer real database tests over mocked tests.
- TESTS MUST NOT write inline migration or table-creation logic; use Atlas-backed migrations/helpers as the schema source.
- Test behavior, not implementation details.
- For bug reports (stack traces, logs, or repro steps), add a reproducer test before fixing.

### Tiger Style

- At least 2 assertions per function (input validation, return validation).
- Validate all arguments at function entry.
- Validate all returns before returning.
- Fail-fast: panic on nil, error on invalid state.
- No silent failures: all errors handled or propagated.
- No hidden behavior: explicit over implicit.

### Daemon Foundations (Go)

- Build daemon lifecycle around `context.Context` and `signal.NotifyContext`.
- Use synchronous startup: initialize dependencies, bind socket, then serve.
- Use explicit shutdown sequencing: cancel context, close listener, wait for goroutines, clean socket/pid artifacts.
- Tie every goroutine lifetime to context cancellation or channel close.
- Use Unix socket `PlainObjectCodec` framing consistently for local RPC.
- Remove stale Unix socket files before bind and unlink socket file on close.
- Report daemon status with at least version, pid, uptime, and socket path.

### Invariant Policy

- Use `panic` only for programmer errors and impossible internal states.
- Use `error` returns for recoverable runtime failures (I/O, RPC, config, user/system state).
- Keep invariant helpers minimal and internal (`Must`, `Must1`, `Invariant`).
- Do not hide control flow with broad recover wrappers in normal runtime paths.
- Test signal handling through injectable seams or subprocess tests; do not send SIGTERM to the test runner process.

### Migration Safety

- Preserve existing user databases during normal bootstrap and upgrades.
- Never rewrite or edit already-applied migration files; add new forward migrations instead.
- Keep migrations forward-only by default and use Atlas revision history as source of truth.
- Use explicit backup + restore or a corrective forward migration for recovery; do not use destructive reset behavior in upgrade paths.

## Scope Discipline

- Keep active implementation focused on `tools/ari-cli/`.
- Ari is the project and interface name.
- Treat historical plugin and harness artifacts as archived context, not active scope.

## Validation

- Before finishing a change, run at least:
  - `nix develop -c just verify`
- Run project tooling through Nix instead of calling tools directly from the host shell.
- Preferred validation commands:
  - `nix develop -c go test ./...`
  - `nix develop -c just verify`
- If migration checks are needed, run them from the Nix shell so Atlas and SQLite toolchain versions match CI.
