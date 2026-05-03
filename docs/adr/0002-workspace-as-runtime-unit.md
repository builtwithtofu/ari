# ADR 0002: Workspace as runtime unit

Status: accepted

Date: 2026-05-03

## Context

Users think about software work through projects and workspaces: one or more folders, the commands running inside them, the agents working on them, and the outputs or decisions tied to that place. LLM-assisted development amplifies that need because multiple agents, commands, prompts, proofs, and diffs can accumulate around the same body of work.

A workspace is not the same thing as exactly one repository or exactly one folder. Ari supports workspaces made from multiple folders for microsessions and related work. A folder may also appear in multiple workspaces when users need different runtime contexts over overlapping files.

Older Ariadne-era docs used session-first and plan-DAG language. That framing is no longer the product model.

## Decision

Workspace is Ari's primary durable runtime unit.

A workspace anchors:

- one or more folders, with folders allowed to belong to more than one workspace;
- folder and project identity;
- active and historical agent runs;
- commands and process output;
- context and projection results;
- approvals, blockers, completions, and other attention state;
- final responses and shareable artifacts when they are tied to work in that workspace.

Sessions, runs, commands, profiles, and tasks may exist as runtime concepts, but they are subordinate to or resolved through workspace context when they affect user work.

## Consequences

- User-facing docs and APIs should prefer Ari/workspace language over Ariadne/session/plan language.
- Workspace identity must be stable enough for clients to detach, reattach, list, inspect, and resume work.
- Names are display/search conveniences; durable targeting should use stable IDs or explicit workspace resolution.
- Runtime activity should be explainable from the workspace view first, even when individual agents or commands have their own detail views.
- Legacy plan-DAG and session-first docs should be replaced or clearly marked historical.

## Alternatives considered

- **Session as the primary unit:** useful for attach/detach mechanics, but too narrow for project-level LLM work that spans agents, commands, proofs, and context.
- **Task as the primary unit:** useful for planning and acceptance, but not all useful runtime state begins as a task.
- **Provider conversation as the primary unit:** easy to map to chat tools, but gives Ari too little control over workspace, process, and attention state.
