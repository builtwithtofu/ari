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

## Testing Approach

- Follow TDD: write/adjust a failing test first, then implement, then refactor.
- For bug reports (stack traces, logs, or repro steps), add a reproducer test first before fixing the bug.
- Use `go test` for this project unless a clear need appears.
- Prefer low-mock, low-orchestration tests with real values.
- Prefer exact assertions over partial-response checks.

## Scope Discipline

- Keep active implementation focused on `tools/ari-cli/`.
- Ari is the project and interface name.
- Treat historical plugin and harness artifacts as archived context, not active scope.

## Validation

- Before finishing a change, run at least:
  - `go test ./...` (from `tools/ari-cli`)
