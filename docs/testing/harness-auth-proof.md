# Harness auth proof procedures

Former planning source was consolidated into `.ari/workspace-event-orchestration/PRD.md`. This procedure remains the standalone harness-auth proof reference.

## Safe fake-harness proof

Run:

```sh
nix develop -c just auth-proof-fake
```

This builds the local Ari CLI and fake harness executable, starts an isolated Ari daemon, and exercises Ari's CLI/daemon/adapter/process boundary against fake Claude, Codex, and OpenCode executables via `ARI_CLAUDE_EXECUTABLE`, `ARI_CODEX_EXECUTABLE`, and `ARI_OPENCODE_EXECUTABLE`. It uses temporary daemon socket/database/config roots, records fake-harness invocation evidence in a temporary JSONL file, and fails if the sentinel secret appears in recorded proof artifacts.

Covered behavior includes Ari `auth doctor`, `auth status`, Codex device-code login, Claude named-slot login/logout projection, OpenCode provider-method discovery, OpenCode session start through the Ari daemon/adapter/process boundary, fake auth-required and malformed modes, projection summary capture, and the sentinel leak trap.

## Opt-in live smoke

Run only with an explicit gate:

```sh
ARI_AUTH_LIVE_SMOKE=1 nix develop -c just auth-live-smoke
```

This command is intentionally outside `just verify` and CI. It runs harnesses with a minimal allowlisted environment plus temporary `HOME`, `XDG_CONFIG_HOME`, and `XDG_DATA_HOME`; Claude also gets `CLAUDE_CONFIG_DIR`, and Codex gets `CODEX_HOME`. It reports each harness as `exercised`, `skipped`, `unsupported`, `unsafe`, or `failed` with the attempted pathway label, and treats OAuth/device-code/redirect initiation as the success boundary.

Claude is reported `unsafe` on macOS because Keychain/shared-store isolation cannot be proven by this smoke, and on other platforms the smoke checks temp-profile auth status before attempting login. Codex prefers `codex login --device-auth`. OpenCode probes the `opencode serve` provider-auth endpoint; until Ari implements a bounded provider-authorize step, method discovery is reported as `unsupported` rather than successful auth initiation.

The smoke redacts common token/code query parameters, bearer tokens, standalone user/device codes, and an optional exact `ARI_AUTH_LIVE_SMOKE_TOKEN` value, but operators should still review output before attaching it to handoffs or reviews. Do not complete OAuth in this smoke. Do not attach raw tokens, auth files, or unredacted command output to reviews.
