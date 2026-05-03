# Workspace lifecycle

Workspace is Ari's primary durable runtime unit.

See also:

- `docs/adr/0002-workspace-as-runtime-unit.md`
- `docs/ep/ari-workspace-runtime.md`

## Definition

A workspace is a named runtime context over one or more folders.

- A workspace may contain a single project folder.
- A workspace may contain multiple folders for microsessions or related work.
- A folder may belong to multiple workspaces.
- Workspace identity is not the same thing as repository identity.

The workspace is where Ari gathers the facts a user returns to: agents, commands, processes, context, proofs, final responses, and attention state.

## Lifecycle

Workspace lifecycle operations are daemon-owned. Clients render and compose them.

Core lifecycle concepts:

- create a workspace;
- list workspaces;
- show workspace details;
- add or remove folders;
- close, suspend, or resume runtime activity where supported;
- resolve a workspace from explicit IDs, names, or current folder context according to daemon rules.

## Folder membership

Folder membership is many-to-many:

- one workspace can reference many folders;
- one folder can be referenced by many workspaces.

This supports different LLM work contexts over overlapping files. For example, a user may keep a broad project workspace and create a narrower microsession workspace that includes only folders relevant to a focused investigation.

## Runtime state

Workspace-scoped runtime state may include:

- active and historical agent runs;
- command and process records;
- retained process output;
- context packets and projection results;
- proof summaries and timeline items;
- approvals, blockers, idle state, completions, and other attention signals;
- final responses and shareable artifacts.

Not every runtime fact must be displayed by every client. A GUI may compose a few daemon calls into a dashboard, while the CLI may expose lower-level commands for inspection and automation.

## Legacy terminology

Older Ariadne documents described sessions and plan DAGs as primary concepts. That framing is historical. Current docs should use Ari/workspace/runtime language unless referring to legacy artifacts explicitly.
