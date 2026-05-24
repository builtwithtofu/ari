# ADR 0007: Ari secrets store boundary

Status: proposed

Date: 2026-05-24

## Context

Ari preserves native harness authentication where it is reliable, but some future capabilities need Ari-owned secrets: isolated harness auth slots where the harness cannot isolate credentials itself, and secure injection into LLM-run commands or tools. Secrets must not become ordinary profile defaults, auth slot metadata, operation payloads, command history, logs, or harness config.

The harness auth foundation intentionally reports current capability limits instead of storing credentials. The next implementation step that persists or projects secret values needs a durable security boundary first.

## Decision

Ari secrets will be a general daemon-owned capability, not a harness-specific credential shim.

- The daemon owns secret storage, grants, read/projection decisions, audit events, and redaction policy.
- Secret values at rest may live only in the Ari secrets store backend.
- Auth slot metadata, profile defaults, logs, operation records, command history, and harness config must not contain secret values or credential-source fields. Harness config may receive a secret only as a short-lived per-run projection selected by an adapter.
- Storage backend policy is platform secure storage first, with encrypted file fallback for headless or minimal environments.
- The provisional Go storage libraries are `zalando/go-keyring` for OS keychain access and `filippo.io/age` for encrypted file fallback, behind small Ari interfaces.
- Projection/delivery is adapter-selected. Prefer helper, file descriptor, stdin, or projected file mechanisms over environment variables. Environment variables are a compatibility fallback and must be labelled as a downgrade risk.
- Projection must be scoped to a specific operation/run and must not mutate the daemon process environment or global harness config.
- Profile defaults may reference future secret identifiers only through validated references, never inline values. Reference syntax and grant checks are implementation-gated and not accepted by this ADR alone.
- Operation records may record secret reference IDs, projection kind, grant IDs, and redacted summaries, but never secret values or reversible payload snapshots.

Before any implementation stores a secret value, the implementation ticket must define and test:

1. the backend selection and fallback path for the target platform;
2. encrypted-file fallback key management;
3. grant model for workspace, session, harness, and tool access;
4. audit events for secret reads/projections;
5. redaction for logs, operation records, RPC responses, and errors;
6. platform-specific storage/projection interfaces and build tags;
7. adapter-selected delivery behavior and risk labels.

## Consequences

- Harness auth can stay native-first while still having a future Ari-owned path for cases native auth cannot support safely.
- Codex named-slot execution through `CODEX_HOME` needs a per-run projection/config-root seam before it can be marked supported.
- OpenCode named account execution remains blocked until upstream storage isolation exists or Ari secrets can project isolated credentials safely.
- Tests must continue proving that auth diagnostics, auth slots, profile defaults, operation records, and RPC responses do not contain token-like fields or raw secret values.
- This ADR does not implement the store, choose all platform-specific details, or approve external vault integrations.

## Alternatives considered

- **Native harness auth only:** keeps Ari simpler but cannot support isolated named execution or secure LLM-run tool secret injection where harnesses lack native facilities.
- **Store harness credentials in auth slot metadata:** easy to model but violates the no-secret invariant and would leak through ordinary config/API paths.
- **Environment-variable-first projection:** simple and portable, but broad process inheritance and logging make it an unsafe default. It remains a labelled compatibility fallback only.
- **External vault first:** useful later for teams, but too large for the first Ari-owned local security boundary and not required for pre-alpha local runtime work.
