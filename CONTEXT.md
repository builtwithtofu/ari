# Ari Glossary

## Ari

A durable, headless workspace runtime for LLM harnesses.

## Workspace

A durable switchable unit of work, similar to an IDE project window. A workspace can exist before folders are attached, and may contain folders plus the harness sessions, commands, logs, context, messages, outputs, attention state, and coordination history for that work.

## Folder

A filesystem root referenced by a workspace. A workspace may contain multiple folders, and a folder may belong to multiple workspaces.

## Daemon

The headless Ari runtime. The daemon owns workspace runtime state and exposes daemon operations to clients.

## Daemon operation

A product capability exposed through the daemon API. Daemon operations are Ari's source of product behavior.

## Client

A surface over daemon operations, such as CLI, TUI, GUI, MCP, remote, automation, or helper tools. Clients render, compose, prompt, and format; they do not own runtime state.

## CLI workflow

A curated command-line flow over daemon operations. CLI workflows do not need to mirror RPC methods one-to-one.

## `ari api`

The CLI escape hatch for fine-grained daemon operations, similar to `gh api`.

## Harness

An external LLM interaction runtime that Ari launches, observes, or coordinates, such as Claude Code, Codex, OpenCode, or a future adapter.

## Fake harness

A test double executable that impersonates a harness at Ari's process boundary. A fake harness is used to prove Ari's adapter, auth, projection, and runtime behavior without invoking a real provider CLI, network, or credentials.

## Harness session

A harness invocation in a workspace with persisted Ari identity, provider resume metadata, normalized messages, and run log. Supported harnesses are peers.

## Auth slot

A named Ari reference to a harness authentication context. Auth slots prefer provider-owned credentials and may have harness-specific capabilities or limits.

## Auth diagnostic

A daemon-owned read-only summary of harness authentication readiness. An auth diagnostic combines live provider readiness, configured auth slot metadata, declared harness auth capability limits, and remediation guidance without reading or exposing credentials.

## Harness auth capability

A read-only adapter declaration of what Ari can safely do with a harness's provider-owned authentication, such as status checks, login methods, logout, named slot status, and named slot execution. Capability declarations describe limits before Ari attempts a harness run or auth flow.

## Ari secrets store

A general daemon-owned capability for storing and injecting secrets Ari must provide to external tools or LLM-run commands. The Ari secrets store is not the default owner of harness credentials.

## Profile

A reusable behavior contract passed to a harness, usually as system/developer prompt plus related harness defaults. Planner, orchestrator, reviewer, worker, helper, and researcher are examples users may create; they are not built-in Ari roles.

## Sticky session

A persistent human-facing harness session attached to a workspace. Sticky sessions are the sessions a user normally returns to, observes, and continues interacting with.

## Ephemeral call

An inspectable bounded worker harness invocation using a profile. Ephemeral calls are Ari's alternative to provider-specific subagents and are useful for fan-out, research, review, comparison, implementation slices, and follow-on worker work.

## Run log

The chronological normalized message and event history for a harness session.

## Timeline

A workspace-level projection of relevant runtime activity across harness sessions, commands, outputs, attention, and artifacts.

## Attention

Runtime state that should surface to a user or client, such as idle, blocked, waiting for input, auth required, failed, completed, or ready for review.

## Notification

A client-visible signal derived from attention state, including cross-workspace signals when the user is focused elsewhere.

## Context excerpt

An explicit bounded selection of prior messages or context shared with another harness session or call.

## Agent message

A visible message from one harness session or call to another. Handoffs, questions, blockers, review requests, research requests, and returned findings are agent messages unless a later decision creates a more specific primitive.

## Operation record

An auditable record of an Ari-owned configuration or runtime mutation, including rollback metadata when rollback is supported.
