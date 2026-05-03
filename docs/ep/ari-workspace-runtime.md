# EP: Ari workspace runtime

Status: proposed

Source: extracted from `.ari/roadmaps/ari-direction-reset/ROADMAP.md` and current documentation cleanup.

## Intent

Ari is an attachable, headless workspace runtime for LLM-assisted development. The daemon keeps work alive in the background, lets clients detach and reattach, and preserves enough state for users to understand what agents did, what is still running, and what needs attention.

The product analogy is closer to Docker daemon plus tmux for LLM work than to a single chat UI. Ari owns the runtime around agents: workspaces, processes, agent runs, command output, projections, context, approvals, and attention state. CLI, GUI, TUI, MCP, remote, and other clients render or compose that runtime for humans and automation.

## Product thesis

LLM coding work becomes hard to manage when it lives only in terminals, provider chats, or one-off process output. Users need a local control plane that can:

- keep agents, commands, and workspace activity alive after a client exits;
- show which workspaces and agents are active, idle, blocked, completed, or waiting for input;
- preserve outputs, final responses, proofs, and process state for later inspection;
- let users switch between workspaces without rebuilding context by hand;
- allow future clients to attach locally or remotely without changing where product behavior lives.

Ari should make serious agent-assisted work feel persistent, inspectable, and resumable.

## Durable direction

- Ari is headless first. Product operations live behind daemon APIs before they appear in any UI.
- The daemon owns durable runtime state. Clients render, prompt, format, and compose workflows.
- Workspaces are the primary unit users return to and reason about. A workspace contains one or more folders, supports multi-folder work such as microsessions, and does not claim exclusive ownership of a folder; the same folder may appear in multiple workspaces.
- Agents, commands, process output, context packets, approvals, notifications, and final responses are workspace-scoped runtime facts where possible.
- Attention should bubble up from runtime facts such as idle agents, blocked runs, approval requests, failed commands, completed work, and waiting input.
- External harnesses such as Codex, Claude Code, OpenCode, or local PTY processes are pragmatic execution backends today. Ari may add different or more native execution later; this EP does not decide that permanently.
- UIs should be user-oriented. They do not need to expose every daemon method, and they may compose multiple API calls into one better workflow.

## Non-goals for this EP

- Do not make a specific GUI, TUI, or remote client the product source of truth.
- Do not decide that Ari will always delegate LLM execution to external harnesses.
- Do not freeze a normalized harness-call schema as a durable architecture decision yet.
- Do not decide remote transport, authentication, or authorization details here.
- Do not decide telemetry or analytics architecture here.
- Do not preserve legacy Ariadne planning-engine, plan-DAG, or session-first language as current product direction.

## Revisit triggers

- Remote access moves from aspiration to implementation; write a dedicated decision for transport, authentication, authorization, and audit boundaries.
- Agent execution moves beyond adapter-backed harnesses; decide what Ari owns in the execution loop.
- Notifications require platform-specific behavior or cross-device delivery; decide attention and notification architecture.
- Telemetry becomes more than local status/diagnostics; decide privacy, retention, export, and observability boundaries.
- Workspace-level concurrency creates unsafe or confusing mutations; decide locking, isolation, and conflict rules.

## Superseded framing

Older roadmap material described Ariadne/session/plan-DAG flows and UI-specific roadmaps. Those documents are historical context only. Current guidance is Ari/workspace/runtime/API first.
