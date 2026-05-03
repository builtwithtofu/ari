# ADR 0003: Workspace agent session terminology

Status: accepted

Date: 2026-05-04

## Context

Ari's early code and planning material used “agent” for several different things: a live process record, a behavior profile, a harness session, and a user-facing collaborator. That ambiguity made it hard to remove legacy runtime-agent APIs without also losing the user journeys they enabled.

The intended model is closer to an attachable workspace runtime for agentic work. A user creates a workspace, returns to it later, sees the run log where work was left, talks to ongoing sessions, and can let those sessions fan out work to short-lived helper invocations. Ari should preserve journeys such as attach/return/resume, planner/executor collaboration, bounded message sharing, review/research helper work, fan-out, and dashboard attention, but implement them through workspace/session/profile/message primitives rather than legacy process-agent commands.

ADR 0002 establishes workspace as Ari's primary durable runtime unit. In this ADR, “workflow” is only informal shorthand for the work happening in a workspace; it is not a separate durable object.

## Decision

Ari uses the following product terminology.

### Workspace

A **workspace** is the durable runtime container users return to. It owns or references folders, context, commands, agent sessions, messages, shares, calls, output, and attention state.

A workspace can be created empty: no agents or sessions are required at creation time. Ari has no mandatory workspace template, but users may create templates with their preferred setup as bootstrapping. Agents can be attached to the workspace later, using profiles where appropriate.

Workspaces do not “close”. A workspace with no active work is **idle** and remains inspectable where the user left off until explicitly deleted. Ari may derive active, idle, waiting, blocked, failed, completed, auth-required, or resumable presentation from runtime facts, but those are not a closed-workspace lifecycle.

### Profile

A **profile** is a reusable behavior prompt, ideally a system prompt, that programs how a harness-backed agent should behave. Examples include planner, executor, reviewer, librarian, explorer, worker, or orchestrator-style behavior.

A profile is not itself a running agent. Profiles may eventually include or reference defaults such as model, cost tier, reasoning/effort, tools, permissions, or context policy, but this ADR does not decide that boundary. Those settings can also be per-session or per-call overrides until a later decision settles the configuration model.

### Agent and agent session

An **agent** is a harness invocation created in a workspace. It has a session identity, harness-specific settings, and may be created with a profile. The harness may be Claude Code, Codex, OpenCode, PTY, or another adapter.

An **agent session** is the persisted session/conversation identity for an agent invocation. It stores or references messages, provider session IDs, status, settings, resume metadata where supported, and other run-log facts.

The session history is the **run log**: a chronological record of prompts, responses, shares, follow-ups, status-relevant output, returned results, and other normalized messages. Ari does not define special “continuation message” objects. If a topic needs more research, exploration, review, or follow-up work, Ari sends ordinary messages to an agent session.

### Sticky sessions and ephemeral calls

A **sticky agent session** is an agent session intentionally attached to the workspace as ongoing work, such as a planner, executor, reviewer, or orchestrator-style session. Sticky sessions are the normal direct user interaction surface: users can return to them, inspect them, and continue the conversation.

An **ephemeral agent call** is a non-sticky harness invocation linked to the workspace for traceability. It may use a profile to do focused work, fetch information, review a change, or implement a bounded slice. Ephemeral calls are displayed as short-lived/background work by default rather than primary user chat surfaces. They may still have one or a few follow-on messages when the task needs clarification, review iteration, or more exploration.

Sticky and ephemeral are lifecycle/presentation intent, not different harness capabilities or different persistence paths. Both are agent sessions with message history. Stickiness can change over time: an ephemeral call can be promoted to a sticky agent session when the helper becomes ongoing workspace work, and a sticky session can be detached from the primary workspace view while preserving its run log.

Sticky sessions can perform work directly and can also coordinate other agent calls. Coordination is behavior of a session/profile, not a separate product object. For example, an orchestrator-style sticky session may do work itself, delegate focused slices to ten ephemeral worker calls using a worker profile, choose cheaper or more expensive models for different slices, collect results, and continue.

### Agent messages

An **agent message** is a normal visible message from one agent/session to another. Handoffs, questions, replies, blockers, review requests, research requests, fan-out tasks, returned findings, and replanning prompts are all ordinary agent messages. Ari should not add special durable workflow objects for those labels until a behavior requires it.

An agent message may include enough visible context for the receiver to act, such as a planning summary, selected prior messages, file references, task slices, or returned findings. Implementation may store selected excerpts, attachments, or provenance internally, but the product concept is still one agent messaging another.

Examples are illustrative, not exhaustive. A user may ask a planner-style session to message an executor-style session after planning is done. An executor may message several explorer sessions or ephemeral explorer calls in parallel asking which files need to change, receive messages back with findings, and then continue. Those explorers might use cheaper models, needle-in-haystack models, higher reasoning settings, or different harnesses.

## Legacy terminology and behavior

Legacy runtime-agent product terminology and APIs such as `agent.spawn`, `agent.send`, `agent.output`, `agent.attach`, and legacy CLI attach flows should be removed, renamed, or replaced.

Removing those names does not mean removing the journeys. Equivalent capabilities must map to workspace, profile, agent session, agent message, and ephemeral call behavior unless a later decision explicitly rejects a journey.

## Consequences

- Existing EP language that says “agent is a durable named behavior profile” is superseded by this ADR. The durable behavior object is a profile; an agent is a harness-backed invocation/session that can use a profile.
- Workspace creation must not require a profile or agent session.
- Workspace lifecycle should prefer idle/active presentation derived from runtime facts and explicit deletion. “Closed workspace” should not be product terminology.
- Public API, CLI, docs, and tests should avoid the old process-agent model. Remaining internal harness/process seams may exist only as implementation details, not stable product surfaces.
- Dashboard/status projections should preserve user-facing visibility by deriving running, idle, waiting, blocked, failed, completed, auth-required, resumable, sticky-session, and ephemeral-call states from workspace/session/message facts.
- UI and CLI may display sticky and ephemeral sessions differently, but both should use the same underlying session/message/run-log concepts.
- This ADR does not decide final database table names, exact RPC names, CLI syntax, harness-neutral call envelopes, profile configuration schema, model/cost/reasoning policy, fan-out limits, result aggregation policy, retention policy, budgeting, authorization, or remote transport.

## Alternatives considered

- **Keep agent as profile:** matches part of the previous EP but keeps overloading “agent” and conflicts with the harness-invocation mental model.
- **Use workflow as a durable child of workspace:** could support future task lanes, but it conflicts with the current workspace-first ADR and the current intent where workflow and workspace are being used interchangeably.
- **Keep closed/archive state:** less aligned with the desired Docker-like runtime model. Idle plus explicit delete is simpler for now.
- **Make ephemeral calls single-shot only:** simpler, but too restrictive for review or exploration loops that need a few follow-ups. Ephemeral should mean non-sticky/background by default, not necessarily one message forever.
