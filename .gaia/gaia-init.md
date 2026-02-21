# GAIA Init

## Mission
- Build a dependable base GAIA orchestrator and a strong runtime planning context foundation before subsystem
  expansion.

## Product Context
- System style: human-in-the-loop orchestration.
- Problem: specialist agents are useful but imperfect; GAIA must coordinate them into coherent
  product execution.
- Scope: not code-only; includes product planning and decision flow.

## Roles and Decision Rights
- Operator: interactive human steering session-level decisions.
- Owner: accountable human making final ship decisions.
- Decision handoff shape: Context -> Options -> Recommendation -> Action needed.

## Constraints
- Keep native `plan` and `build` behavior unchanged unless GAIA mode is explicitly selected.
- Keep plugin core portable and host-agnostic.
- Keep changes small, typed, and easy to verify.

## Non-Goals (Current Phase)
- Do not implement the full pantheon yet.
- Do not optimize for full autonomy ahead of reliability.

## Working Style
- Keep units small, actionable, and test-backed.
- Require reproducer-first tests for bug reports.
- Prefer exact assertions with low-mock tests.

## Risk Tolerance
- Default risk tolerance: low.
- Medium/high-risk actions require explicit checkpoint approval.

## Communication Contract (Baseline)
- Work unit handoff fields: `work_unit`, `objective`, `inputs`, `constraints`, `done_when`,
  `open_questions`.
- Result fields: `status`, `summary`, `evidence`, `risks`, `next_actions`.

## Notes
- Keep this file concise and durable.
- Promote stable learnings from runtime artifacts (`.gaia/runtime/<session>/`) into this file when useful.
