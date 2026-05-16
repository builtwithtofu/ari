# Ari Domain Context

## Ari

Ari is the single pane of glass for configurable, continuous, agentic workflows. It lets humans connect a project, define profiles for agent behavior and harness defaults, start and observe agent work, and keep that work visible from front-view clients while execution continues in the background.

Ari does not prescribe one planning or execution ceremony. It provides the runtime primitives that let users define flows such as ongoing planning, story/task shaping, execution, review, research, and fan-out according to the needs of a workspace.

## Daemon

The daemon exists so agentic workflows can keep running when the human is focused elsewhere or when a client disconnects. It is the runtime authority behind CLI, GUI, TUI, MCP, remote, and automation clients.

Long-term, Ari operations that can be performed or configured through clients should also be performable through an agent control surface. The default general helper is expected to become that broad Ari control surface. The initial trust model is trust-after-init: after the user configures the helper during onboarding, Ari focuses on visible operations, first-use confirmation, auditability, and rollback for Ari-owned configuration/runtime state rather than a heavy permission system. First-use confirmation offers trust once, trust always, or do not perform; trust-always decisions are remembered by Ari operation type.

Ari may use existing local-agent tools as view and capability references, but they are not the destination architecture. Ari-owned helper tools map to pruned daemon operations first; MCP can later expose the same tool catalog without becoming the source of product behavior.

Helper-driven Ari mutations should be atomic, auditable, and rollbackable. Each mutation either fully applies with a change record or does not apply. The first implementation can keep rollback simple, but change records should be shaped with parent links or checkpoints so Ari can later grow toward branching rollback across Ari-owned configuration/runtime state. Helper shell commands are audited, but their filesystem effects are not rolled back by Ari in the first implementation.

## Workspace

A workspace is the unit of work an LLM is pointed at. It represents one or more project folders, including multi-folder microservice projects, plus the agents, commands, context, messages, outputs, and attention state tied to that work. Multiple sessions may be active in the same workspace at the same time.

First-run onboarding should create or select a home workspace and offer the user's home directory (`~/`) as the default root rather than silently assuming it. Users may choose another root such as `~/Projects` or a custom folder. The home workspace can host a general helper agent session using the default helper profile, acting as a general system AI surface for Ari setup, workspace/profile creation, and questions about the user's environment or codebases. The helper and home workspace are optional runtime conveniences, not mandatory product dependencies; users may remove the helper agent/session or the home workspace. The helper may later create project workspaces from user requests such as pulling a repository and setting up a workspace for it.

Ari is daemon-first at the product boundary: users return to workspaces through daemon APIs and clients. Within a workspace, the desired user-facing capability set is SoloTerm-like: configure agents, start and observe multiple running agents, attach and detach, coordinate work, and inspect state without making the client the runtime authority.

When a user returns to Ari, the default restore target is the last accessed workspace, not necessarily the home workspace. If the last accessed workspace no longer exists, Ari should show a workspace picker rather than silently recreating or switching workspaces.

Another useful analogy is a Neovim session: Ari should preserve and restore the state of an interactive workspace session, including active workspace context, agents/sessions, profile/config state, visible outputs, timeline, and pending attention. Unlike Neovim, the persisted session state is daemon-owned and headless so multiple clients can attach or render it.

## Profile

A profile is a user-configured reusable agent behavior contract. Ari should not assume built-in role profiles such as planner, executor, reviewer, explorer, or librarian; those names are examples users may create for their own workspace. The expected default profile is a general helper profile for onboarding, Ari setup, and questions about the current workspace. A profile may include harness defaults such as model, permissions, auth, tool scope, and context policy, but the final ownership of those settings is a separate configuration decision. Users set up or select profiles for a connected project before or while running agentic workflows.

## Agent And Session

An agent is a harness invocation in a workspace. An agent session is the persisted identity and run log for that invocation, including provider resume metadata and normalized messages.

## Continuous Workflow

A continuous workflow means the human can interact with one session while other sessions continue working. For example, an executor can keep implementing a feature while the human keeps talking to a planner and asks it to add or reshape a user story. Ari should preserve both activities as workspace runtime state rather than forcing the work into a single turn-based conversation.
