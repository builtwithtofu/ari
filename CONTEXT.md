# Ari Domain Context

## Ari

Ari is the single pane of glass for configurable, continuous, agentic workflows. It lets humans connect a project, define profiles for agent behavior and harness defaults, start and observe agent work, and keep that work visible from front-view clients while execution continues in the background.

Ari does not prescribe one planning or execution ceremony. It provides the runtime primitives that let users define flows such as ongoing planning, story/task shaping, execution, review, research, and fan-out according to the needs of a workspace.

## Daemon

The daemon exists so agentic workflows can keep running when the human is focused elsewhere or when a client disconnects. It is the runtime authority behind CLI, GUI, TUI, MCP, remote, and automation clients.

## Workspace

A workspace is the unit of work an LLM is pointed at. It represents one or more project folders, including multi-folder microservice projects, plus the agents, commands, context, messages, outputs, and attention state tied to that work. Multiple sessions may be active in the same workspace at the same time.

## Profile

A profile is a reusable agent behavior contract, such as planner, executor, reviewer, explorer, or librarian. It may include harness defaults such as model, permissions, auth, tool scope, and context policy, but the final ownership of those settings is a separate configuration decision. Users set up or select profiles for a connected project before or while running agentic workflows.

## Agent And Session

An agent is a harness invocation in a workspace. An agent session is the persisted identity and run log for that invocation, including provider resume metadata and normalized messages.

## Continuous Workflow

A continuous workflow means the human can interact with one session while other sessions continue working. For example, an executor can keep implementing a feature while the human keeps talking to a planner and asks it to add or reshape a user story. Ari should preserve both activities as workspace runtime state rather than forcing the work into a single turn-based conversation.
