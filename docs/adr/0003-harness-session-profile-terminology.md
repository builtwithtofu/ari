# ADR 0003: Workspace harness session terminology

Status: accepted

Date: 2026-05-04

## Context

Ari's early code and planning material used “agent” for several different things: a live process record, a behavior profile, a harness session, and a user-facing collaborator. That ambiguity made it hard to remove legacy runtime-agent APIs without also losing the user journeys they enabled.

The intended model is a durable workspace runtime for LLM harness work. A user creates a workspace, returns to it later, sees the run log where work was left, talks to ongoing sticky sessions, and can let those sessions fan out work to bounded ephemeral calls. Ari should preserve journeys such as attach/return/resume, planner/orchestrator collaboration, bounded message sharing, review/research worker work, fan-out, and dashboard attention, but implement them through workspace/harness-session/profile/message primitives rather than legacy process-agent commands.

ADR 0002 establishes workspace as Ari's primary durable runtime unit. In this ADR, “workflow” is only informal shorthand for the work happening in a workspace; it is not a separate durable object.

## Decision

Ari uses the following product terminology.

### Workspace

A **workspace** is the durable runtime container users return to. It owns or references folders, context, commands, harness sessions, messages, shares, calls, output, and attention state.

A workspace can be created empty: no harness sessions are required at creation time. Ari has no mandatory workspace template, but users may create templates with their preferred setup as bootstrapping. Harness sessions can be attached to the workspace later, using profiles where appropriate.

Workspaces do not “close”. A workspace with no active work is **idle** and remains inspectable where the user left off until explicitly deleted. Ari may derive active, idle, waiting, blocked, failed, completed, auth-required, or resumable presentation from runtime facts, but those are not a closed-workspace lifecycle.

### Profile

A **profile** is a reusable behavior prompt, ideally a system prompt, that programs how a harness-backed agent should behave. Examples include planner, executor, reviewer, librarian, explorer, worker, or orchestrator-style behavior.

A profile is not itself a running agent. Profiles may eventually include or reference defaults such as model, cost tier, reasoning/effort, tools, permissions, or context policy, but this ADR does not decide that boundary. Those settings can also be per-session or per-call overrides until a later decision settles the configuration model.

### Harness session

A **harness session** is a harness invocation created in a workspace. It has a session identity, harness-specific settings, and may be created with a profile. The harness may be Claude Code, Codex, OpenCode, PTY, or another adapter.

Ari treats supported harnesses as peers. Claude Code, Codex, OpenCode, and future harnesses should not become a primary/secondary hierarchy in the product model.

The session history is the **run log**: a chronological record of prompts, responses, shares, follow-ups, status-relevant output, returned results, and other normalized messages. Ari does not define special “continuation message” objects. If a topic needs more research, exploration, review, or follow-up work, Ari sends ordinary messages to a harness session or starts an ephemeral call.

### Sticky sessions and ephemeral calls

A **sticky session** is a harness session intentionally attached to the workspace as ongoing human-facing work, such as a planner, orchestrator, reviewer, or helper-style session. Sticky sessions are the normal direct user interaction surface: users can return to them, inspect them, and continue the conversation.

An **ephemeral call** is a non-sticky harness invocation linked to the workspace for traceability. It may use a profile to do focused work, fetch information, review a change, compare options, or implement a bounded slice. Ephemeral calls are displayed as short-lived/background work by default rather than primary user chat surfaces. Users should still be able to inspect what an ephemeral call is doing, and an ephemeral call may have follow-on messages when the task needs clarification, review iteration, or more exploration.

Sticky and ephemeral are lifecycle/presentation intent, not different harness capabilities or different persistence paths. Both have persisted identity and message history. Stickiness can change over time: an ephemeral call can be promoted to a sticky session when the worker becomes ongoing workspace work, and a sticky session can be detached from the primary workspace view while preserving its run log.

Sticky sessions can perform work directly and can also coordinate ephemeral calls. Coordination is behavior of a session/profile, not a separate product object. For example, an orchestrator-style sticky session may do work itself, delegate focused slices to ten ephemeral worker calls using a worker profile, choose cheaper or more expensive models for different slices, collect results, and continue.

### Agent messages

An **agent message** is a normal visible message from one harness session or call to another. Handoffs, questions, replies, blockers, review requests, research requests, fan-out tasks, returned findings, and replanning prompts are all ordinary agent messages. Ari should not add special durable workflow objects for those labels until a behavior requires it.

An agent message may include enough visible context for the receiver to act, such as a planning summary, selected prior messages, file references, task slices, or returned findings. Implementation may store selected excerpts, attachments, or provenance internally, but the product concept is still one harness session or call messaging another.

Examples are illustrative, not exhaustive. A user may ask a planner-style sticky session to message an orchestrator-style sticky session after grooming is done. An orchestrator may start several ephemeral worker calls in parallel, receive messages back with findings, and then continue. Those workers might use cheaper models, needle-in-haystack models, higher reasoning settings, or different harnesses.

## Legacy terminology and behavior

Legacy runtime-agent product terminology and APIs such as `agent.spawn`, `agent.send`, `agent.output`, `agent.attach`, and legacy CLI attach flows should be removed, renamed, or replaced.

Removing those names does not mean removing the journeys. Equivalent capabilities must map to workspace, profile, harness session, agent message, and ephemeral call behavior unless a later decision explicitly rejects a journey.

## Consequences

- Existing EP language that says “agent is a durable named behavior profile” is superseded by this ADR. The durable behavior object is a profile; the runtime object is a harness session or ephemeral call that can use a profile.
- Workspace creation must not require a profile or harness session.
- Workspace lifecycle should prefer idle/active presentation derived from runtime facts and explicit deletion. “Closed workspace” should not be product terminology.
- Public API, CLI, docs, and tests should avoid the old process-agent model. Remaining internal harness/process seams may exist only as implementation details, not stable product surfaces.
- Dashboard/status projections should preserve user-facing visibility by deriving running, idle, waiting, blocked, failed, completed, auth-required, resumable, sticky-session, and ephemeral-call states from workspace/session/message facts.
- UI and CLI may display sticky and ephemeral sessions differently, but both should use the same underlying identity/message/run-log concepts.
- This ADR does not decide final database table names, exact RPC names, CLI syntax, harness-neutral call envelopes, profile configuration schema, model/cost/reasoning policy, fan-out limits, result aggregation policy, retention policy, budgeting, authorization, or remote transport.

## Alternatives considered

- **Keep agent as profile:** matches part of the previous EP but keeps overloading “agent” and conflicts with the harness-session mental model.
- **Use workflow as a durable child of workspace:** could support future task lanes, but it conflicts with the current workspace-first ADR and the current intent where workflow and workspace are being used interchangeably.
- **Keep closed/archive state:** less aligned with the desired Docker-like runtime model. Idle plus explicit delete is simpler for now.
- **Make ephemeral calls single-shot only:** simpler, but too restrictive for review or exploration loops that need a few follow-ups. Ephemeral should mean non-sticky/background by default, not necessarily one message forever.
