# EP: Ari cross-harness coordination

Status: proposed

## Intent

Ari's core product is the durable workspace runtime. Cross-harness coordination is a capability built on that runtime: sticky sessions and ephemeral calls can pass visible context, messages, blockers, questions, and results between peer harnesses.

A profile describes how a harness session or call should behave. Names such as planner, orchestrator, reviewer, researcher, and worker are user conventions, not built-in Ari roles. A Claude Code planner sticky session can message a Codex worker ephemeral call, an orchestrator sticky session can fan out to several workers, and a reviewer can receive selected context from another harness session.

## Product thesis

Provider-native “subagent” models are too hierarchical and provider-specific for Ari's product model. Ari should let users compose peer harness sessions and inspectable ephemeral calls across harnesses without adopting provider-specific hierarchy.

Ari should make coordination explicit and inspectable by moving bounded context excerpts and visible messages between sessions/calls rather than treating the whole workspace as one shared conversation.

## Durable direction

- A **profile** is the durable named behavior prompt/configuration object, not a running process.
- A **harness session** is a harness invocation in a workspace with persisted Ari identity and run log.
- A **harness** is an external LLM interaction runtime such as Claude Code, Codex, OpenCode, or a future adapter.
- “Ephemeral” describes lifecycle/presentation intent for a task-scoped worker call; it is not a separate or lesser persistence path.
- Ari should avoid user-facing “subagent” framing except when discussing prior art it intentionally rejects.
- Workspaces should support user-managed profiles and harness sessions that users can add to, remove from, configure, start, resume, inspect, or detach.
- Profile/session setups may have defaults, but Ari should not hardcode one universal layout such as planner/executor for every workspace.
- Ari should support cross-harness composition while preserving identity, context ownership, and traceability.

## Context movement primitives

Ari should treat context movement between harness sessions and calls as explicit workflow state:

- **Context excerpt:** the primary bounded-context primitive. It is a user-approved transcript excerpt, such as “share the last 10 messages with executor.” Excerpts should preserve order and role as much as possible and should be visible as transcript/context, not hidden instructions or system prompt. The user or source session may optionally append a new user/human message at the end to frame what the target session should do with the excerpt.
- **Agent message:** a harness session or call may send a normal visible message to another harness session or call through a daemon operation/tool call. For example, the planner can write to the orchestrator: “These tasks are groomed; start with X.” What people might call a handoff is just this agent message, optionally accompanied by context excerpts.
- **Ephemeral call:** a bounded worker harness invocation, usually from a profile, for focused work; it is inspectable and recallable when continuity matters.

Ari should avoid creating special workflow object types until a real behavior requires them. Terms such as “handoff”, “notify”, “review request”, or “replan” can describe user workflows, but they should initially compile down to agent messages, context excerpts, and ephemeral calls rather than separate durable primitives.

These primitives should compose into loops such as:

```text
planner grooms tasks → message orchestrator
orchestrator fans out to workers
worker hits blocker → notify orchestrator or planner
orchestrator continues → reviewer gets targeted evidence or last N messages
```

In this EP's terminology, those workflow labels mean:

- “message orchestrator” = planner sends an agent message to orchestrator, optionally with context excerpts
- “notify planner” = worker or orchestrator sends an agent message to planner describing the blocker, optionally with context excerpts
- “reviewer gets targeted evidence” = orchestrator or worker calls/messages reviewer with selected messages/evidence

Another example:

```text
orchestrator notices a task needs external evidence
orchestrator starts an ephemeral researcher call: “Help me understand X in Spring Boot 4.”
orchestrator may wait while researcher works
researcher replies directly to orchestrator: “I found X; use Y/Z for that functionality.”
orchestrator asks a follow-up question, merges the result, or sends work to another worker
```

No special “research request” or “handoff response” object is required at first. This is ordinary agent messaging plus an ephemeral call. The workflow label is useful for humans, but the core model remains messages between peer harness sessions and calls.

This message-first model also enables true waiting. A harness session can wait while another session or call works, and Ari can append only the eventual response messages or selected shared context back into the waiting session. The waiting session does not need to inherit the other session's full working context.

It also supports orchestrator-style workflows without changing the ontology. A workspace may have an `orchestrator` sticky session/profile that sends work to several sessions and harnesses in parallel, waits for replies, and then decides the next action. Those parallel tasks are still agent messages, ephemeral calls, and context excerpts between peer sessions/calls.

Example:

```text
orchestrator receives user goal
orchestrator starts researcher for external research
orchestrator starts explorer for repo evidence
orchestrator starts worker for a narrow code spike
orchestrator waits
responses arrive as messages
orchestrator asks follow-ups, merges results, or sends work to another session/call
```

The same primitives cover planner/orchestrator, orchestrator/worker, reviewer/author, and research/execution flows.

## Normalized message model direction

Last-message and last-N sharing requires Ari to normalize conversation messages instead of only storing provider output snapshots. Ari should model messages as workspace/session-scoped records with provider-native IDs preserved as metadata.

At the EP level, the desired shape is:

```text
Workspace
  Profile        durable behavior prompt/configuration
  HarnessSession persisted harness invocation/session identity
  Message        normalized conversation item in a harness session or call
  MessagePart    text/tool-call/tool-result/file/image/etc. content
  ContextExcerpt immutable bounded ordered excerpt selected from messages, optionally with an appended framing message
```

Ari should normalize roles to a harness-neutral superset:

- `system`
- `developer`
- `user`
- `assistant`
- `tool`

Harness-specific details such as Codex channels (`analysis`, `commentary`, `final`, `summary`), OpenCode part metadata, Claude transcript paths, provider item IDs, tool call IDs, and response IDs should be preserved as facets or raw metadata, not treated as Ari roles.

A context excerpt should be an explicit immutable object:

```text
context_excerpt:
  workspace_id
  source_session_id
  selector: last_message | last_n | range | explicit_ids
  messages[]  # preserved order and normalized roles
  appended_message?  # optional user/source-agent framing message delivered after the shared excerpt
  target_session_id?
  visibility: visible_context
  created_at
  content_hash
```

An agent message should be similarly ordinary:

```text
agent_message:
  workspace_id
  source_session_id?
  target_session_id
  message
  attached_context_excerpt_ids[]
  created_at
```

The default sharing path should use `visibility: visible_context`: the receiving session sees the excerpt as shared transcript/context, not as hidden system prompt or durable project guidance. Ari should avoid transforming shared messages into a summary unless the user or source session explicitly requests summarization. The baseline behavior is ordered message transfer plus an optional final framing message.

T3 Chat/T3Code prior art appears to use a simple thread/session projection: `OrchestrationThread` has normalized `messages[]`; each `OrchestrationMessage` has `id`, role (`user|assistant|system`), text, attachments, turn ID, streaming flag, and timestamps; session state is stored separately. Ari needs a broader role/part model because harnesses expose tool calls, tool results, channels, and provider-native IDs, but the thread + messages + separate session projection is a useful shape.

## Harness implications

Claude Code, Codex CLI, and OpenCode are all sessioned local runtimes rather than simple prompt APIs. Ari should persist enough harness-specific state to make runs inspectable and resumable:

- harness session ID
- workspace ID and folder set
- launch cwd and extra folders
- profile/version used
- instruction injection mode
- tool/MCP scope
- sandbox or permission mode
- context packet IDs and output/final summary references

Harness-specific features such as Claude teams, OpenCode subagents, or MCP tools may be useful implementation details, but Ari should not let them define the product ontology. Agent messages and context excerpts should be Ari-native records rendered into harness-specific prompts or visible context payloads.

Current Ari persistence is run/artifact oriented and does not yet have a durable normalized message table. The closest existing projection is timeline output plus final-response artifacts. Message normalization should be introduced before implementing “share last N messages” as a durable feature.

## Feature-to-harness mapping

This EP does not require every harness to expose the same native primitive. Ari should define the feature once, then adapt it to the closest safe harness mechanism.

| Ari feature | Claude Code | Codex CLI | OpenCode |
| --- | --- | --- | --- |
| Minimal handoff | No dedicated handoff object. Best fit is a new prompt/session with Ari's curated packet injected through system prompt/context; Claude hook metadata can expose session ID, transcript path, cwd, permission mode, and agent fields. | Closest native fit is `spawn_agent` with a concise `message`/`items` payload and task name. | Closest fit is `session.prompt(..., noReply: true)` or an explicit prompt/message into a target session with Ari's packet; session/message records carry agent/model/path metadata. |
| Share last N messages | No documented native “last N messages” share selector. Ari should read its own stored transcript/output or harness transcript, slice explicitly, and inject as visible shared context. | Stronger native fit: Codex agent tooling has `fork_turns` (`none`/`all`/`N`) in newer flow; older flow has full-context fork. Ari should still preserve an explicit context-excerpt record. | Good fit through server/SDK message APIs such as session messages with `limit=N`, then inject the bounded excerpt into another session. |
| Notify/update blocker | Hooks and notifications can surface permission prompts, task completion, idle teammates, stops, or denied permissions. Ari should convert these into typed notify/update packets. | `wait_agent`/mailbox updates and `send_input` can route status or redirection. Blocker is operational, not a native durable type. | Toasts/events/permission APIs and session status can surface blockers. Ari should own the typed blocker/update record. |
| Ephemeral call use | Claude subagent/agent invocation may be an adapter mechanism, but Ari should treat it as an ephemeral call, not as a product subagent hierarchy. `claude -p --bare` may fit clean deterministic ephemeral calls. | Strong fit with `spawn_agent`, `wait_agent`, `close_agent`, and `codex exec --ephemeral` depending on whether the ephemeral call is in-process or CLI-level. | Can use Task/subagent invocation or create a separate session/message targeting an agent/model. Ari should avoid leaking OpenCode's subagent hierarchy into its ontology. |
| Resume/recall same agent | Use session IDs and `--resume`/`--continue`; native teammate/subagent resume has limitations and preserves its own history. | `resume_agent` and `codex exec resume` map to recalling the same task/thread. | Persistent sessions, child sessions, `/session/:id`, and message history support recall. Agent identity appears on messages. |
| Workspace folders/context | `cwd`, `CLAUDE.md`, `--add-dir`, MCP scope, and permission/sandbox config. Extra dirs do not load all `.claude` config. | `cwd`, AGENTS.md layering, sandbox/approval mode, MCP config, and session metadata. No broad multi-folder primitive beyond cwd/config. | Project directory/worktree, server path/project APIs, `external_directory` permissions, and visible context injection. |

Harness research sources include Claude Code session/memory/CLI/headless/permissions/sandboxing/MCP/agent-team docs, OpenAI Codex CLI/non-interactive/prompting/AGENTS/MCP/sandboxing docs and source, and `anomalyco/opencode` agent/CLI/TUI/server/SDK/plugin/MCP/permission docs.

## Ari-owned adapter contract direction

Ari should eventually define one harness-neutral call envelope that can express all of these features without adopting provider-specific terms:

```text
ephemeral_call / harness_call:
  workspace_id
  source_session_id?
  target_profile_id?
  harness + model
  usage: sticky | agent_message | context_excerpt | ephemeral
  context_payloads:
    - type: context_excerpt | user_prompt | evidence
      visibility: visible_context | system_prompt | project_guidance
  folder_scope
  tool_scope / mcp_scope
  permission_or_sandbox_mode
  resume_policy: fresh | resume_run_id | recall_ephemeral_id
```

The exact schema belongs in a later ADR. The EP-level direction is that Ari owns this envelope and each harness adapter maps it to native flags, config, API calls, or prompt injection.

## Prior art

SoloTerm/Solo shows useful prior art: configured agent tools, project-level launching/removal, multiple running agents, and local MCP coordination for spawning/binding agents, scratchpads, todos, locks, timers, projects, processes, and output.

Ari should go further by making durable workspaces, typed context movement, and cross-harness identity first-class runtime concepts rather than only configured CLIs plus MCP tools.

## Non-goals for this EP

- Do not decide the final database schema for profiles, harness sessions, handoffs, or context excerpts.
- Do not decide exact daemon RPC names or CLI command names.
- Do not decide the full harness-neutral launch contract yet.
- Do not require every harness to support identical capabilities natively.
- Do not require Ari to implement provider-native subagent hierarchies.
- Do not decide MCP versus Ari daemon RPC as the internal coordination transport.

## Revisit triggers

- Profile/runtime vocabulary blocks implementation or user-facing CLI design; write an ADR for naming and schema boundaries.
- The first cross-harness workflow is implemented; decide the harness-neutral run contract.
- Handoffs or message sharing need persistence; decide packet schema, visibility, retention, and audit behavior.
- Ephemeral calls need budgeting, model selection, or lifecycle policy; decide defaults and override rules.
- MCP becomes a product surface rather than an adapter detail; decide its boundary with Ari daemon APIs.

## Superseded framing

Older code and docs sometimes use “agent” for live runtime process records or for durable behavior profiles. ADR 0003 supersedes that framing: profiles are durable behavior prompts/configuration; harness sessions and ephemeral calls are the runtime invocation identities. Implementation may need migration steps and narrower ADRs before the vocabulary is fully reflected in code.
