# Harness auth proof procedures

This procedure is the standalone harness-auth proof reference.

## Safe fake-harness proof

Run:

```sh
nix develop -c just auth-proof-fake
```

This builds the local Ari CLI and fake harness executable, starts an isolated Ari daemon, and exercises Ari's CLI/daemon/adapter/process boundary against fake Claude, Codex, OpenCode, pi, and Grok executables via `ARI_CLAUDE_EXECUTABLE`, `ARI_CODEX_EXECUTABLE`, `ARI_OPENCODE_EXECUTABLE`, `ARI_PI_EXECUTABLE`, and `ARI_GROK_EXECUTABLE`. It uses temporary daemon socket/database/config roots, records fake-harness invocation evidence in a temporary JSONL file, and fails if the sentinel secret appears in recorded proof artifacts.

Covered behavior includes Ari `auth doctor`, `auth status` across all five harnesses, Codex and Grok device-code login, Claude named-slot login/logout projection, OpenCode provider-method discovery, session starts for OpenCode, pi, and Grok through the Ari daemon/adapter/process boundary, fake auth-required and malformed modes, projection summary capture (config roots verbatim; `OPENCODE_AUTH_CONTENT`, `ANTHROPIC_API_KEY`, and `XAI_API_KEY` hash-only), and the sentinel leak trap.

## Fake harness modes

The fake harness binary (`cmd/fake-harness`) selects a persona with `ARI_FAKE_HARNESS` (claude, codex, opencode, pi, grok) and behavior with `ARI_FAKE_HARNESS_MODE`, which accepts a comma-separated list combining one behavior mode with modifiers:

- Behavior modes: `authenticated` (default), `auth-required`, `oauth-start`, `logout-success`, `malformed`/`unknown-output`, `delivery-claude-pty`, `delivery-codex-app-server`, `exit-rate-limit`, `partial-failure`, `auth-expired-midrun`, `hang` (blocks until SIGTERM).
- Modifiers: `stream-incremental` (one OS write per NDJSON line, exercising partial-stream reads) and `exit-code:<n>` (override the exit code after normal output).

Setting `ARI_FAKE_HARNESS_STATE_DIR` makes fake sessions stateful: each run appends a turn to `<state>/<harness>/sessions/<id>.jsonl` and output is tagged `(turn N)`, so tests prove resume flags reattach (opencode `--session`, pi `--session`/`--session-id`/`-c`, grok `-r`/`-c`, claude restart-only rejection, codex thread reuse). In authenticated mode, `codex app-server`, `pi --mode rpc`, and `grok agent stdio` run interactive line-protocol engines that answer requests as they arrive and enforce the sentinel trap per input line.

## Opt-in live smoke

Run only with an explicit gate:

```sh
ARI_AUTH_LIVE_SMOKE=1 nix develop -c just auth-live-smoke
```

This command is intentionally outside `just verify` and CI. It runs harnesses with a minimal allowlisted environment plus temporary `HOME`, `XDG_CONFIG_HOME`, and `XDG_DATA_HOME`; Claude also gets `CLAUDE_CONFIG_DIR`, and Codex gets `CODEX_HOME`. It reports each harness as `exercised`, `skipped`, `unsupported`, `unsafe`, or `failed` with the attempted pathway label, and treats OAuth/device-code/redirect initiation as the success boundary. Grok gets a temporary `GROK_HOME` and prefers `grok login --device-auth`; pi is reported `unsupported` because its auth is provider env keys with no login flow to initiate.

Claude is reported `unsafe` on macOS because Keychain/shared-store isolation cannot be proven by this smoke, and on other platforms the smoke checks temp-profile auth status before attempting login. Codex prefers `codex login --device-auth`. OpenCode probes the `opencode serve` provider-auth endpoint; until Ari implements a bounded provider-authorize step, method discovery is reported as `unsupported` rather than successful auth initiation.

The smoke redacts common token/code query parameters, bearer tokens, standalone user/device codes, and an optional exact `ARI_AUTH_LIVE_SMOKE_TOKEN` value, but operators should still review output before attaching it to handoffs or reviews. Do not complete OAuth in this smoke. Do not attach raw tokens, auth files, or unredacted command output to reviews.
