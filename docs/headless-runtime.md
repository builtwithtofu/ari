# Headless runtime and helper model

Ari is headless first. The daemon/API owns product behavior and state. CLI, UI, TUI, IDE, Electron, MCP, or other clients render and call those APIs; they must not become the only implementation of a product flow.

## Ownership boundary

Daemon-owned behavior includes:

- onboarding state and application through `init.state`, `init.options`, and `init.apply`;
- workspace creation and resolution, including the normal `$HOME` starter workspace created during init when possible;
- default-helper resolution by workspace-scoped profile name and config;
- helper launch request shape and workspace context injection;
- Ari tool registry/calls and approval validation;
- settings/profile writes;
- read-only teaching, self-check, run-forensics, and workflow-learning projections.

Client-owned behavior includes flags, terminal prompts, human-readable formatting, UI layout, and exit-code handling around daemon/API results.

Examples:

- `ari init` is CLI rendering over `init.state`, `init.options`, and `init.apply`.
- A future first-run UI should call the same init methods instead of shelling out to `ari init`.
- Workspace creation and target resolution are daemon-owned, so every client sees the same `system` and project workspace rules.
- Helper tools/settings writes are daemon-owned; CLI formatting is not the product contract.

## Workspaces

Ari workspaces are folder-backed. During `ari init`, Ari creates a normal workspace rooted at `$HOME` when possible. That home workspace is a starter landing place where a new user can ask Ari for help with setup, profiles, defaults, diagnostics, and other local-computer work they approve.

The home workspace is convenience state, not required system state. Users can delete it without changing `default_harness` or other global defaults. Project workspaces under `$HOME` continue to use normal deepest-folder resolution, so a project workspace wins over the broader home workspace when both match.

## Helper profiles

`helper` is a convention over ordinary profile storage. It is not a profile role, profile kind, or profile type.

- The default helper is the profile named `helper` in the requested workspace.
- Each workspace resolves only its own `helper` profile.
- Missing helpers return setup guidance; workspaces do not fall back to another workspace's helper.
- Home and project helpers share one helper prompt contract. Scope-specific behavior comes from runtime workspace context such as “workspace home at $HOME” or “workspace app at this root”.

Helpers teach, explain, diagnose, compare, draft, route, summarize, and request approval. They do not write project source or act as natural-language aliases for every CLI command. Specialist profiles are also ordinary profiles; behavior comes from prompts, config, and explicit permissions.

## Day-one flow

```bash
ari init --harness codex
ari workspace create my-app
ari workspace use my-app
ari status
ari api workspace.list --params '{}'
```

`ari init` asks only for the default harness in this slice. It does not install or authenticate Codex, Claude Code, OpenCode, or other external tools. Preferred model setup, provider catalogs, broad MCP marketplaces, proactive coaching, and automatic model-release detection are deferred.

## Ari tools and approvals

Starter helper tools are daemon-owned capabilities, not provider-specific magic:

- `ari.defaults.get`
- `ari.defaults.set`
- `ari.profile.draft`
- `ari.profile.save`
- `ari.self_check`
- `ari.run.explain_latest`

Every tool call carries scope metadata: source run, workspace id/kind, profile id/name, tool name, and whether the request is within default scope. Read-only tools can explain current Ari state. Write tools require a pre-issued, single-use approval marker with the exact scope, tool name, approver, timestamp, and approved request hash. Helpers cannot self-approve write tools.

Helpers can explain or initiate handoffs for out-of-scope changes. Defaults/profile writes require scoped approval. Helpers are denied project source writes by default.
