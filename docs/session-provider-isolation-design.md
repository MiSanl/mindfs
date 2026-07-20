# Session-Level Provider Isolation Design

## Status

Design proposal. No implementation has been applied.

## Goal

Make Claude and Codex API providers session-scoped in MindFS. A session must
continue using its bound provider, endpoint, credentials, and compatible model
after MindFS, Claude Code, or the Codex app-server restarts. Existing sessions
must never silently fall back to a current global provider configuration.

This design applies to Claude and Codex first. Agent-level provider selections
remain useful only as defaults for new sessions and for UI catalog display.

## Non-Goals

- Persisting API keys, base URLs, environment maps, or command-line arguments
  in session logs or session metadata.
- Treating a provider migration as an invisible in-place switch of an existing
  Claude or Codex native conversation.
- Blocking provider rename, edit, or deletion because historical sessions use
  the provider.

## Existing Storage Boundaries

MindFS has two relevant session storage forms:

| Data | Existing location | Use in this design |
| --- | --- | --- |
| Conversation exchanges | `<workspace>/.mindfs/sessions/<session>.jsonl` | Do not store provider settings or secrets here. |
| Session metadata | Existing `session-list.db` for the workspace, or MindFS's existing linked fallback database | Store non-secret provider bindings here. |
| Provider definitions, including keys | MindFS `api-providers.json` | Remains the source of current endpoint and credentials. |

The existing session database fallback mechanism remains authoritative. If a
workspace links to a fallback `session-list.db`, bindings use that database
instead of creating another configuration file.

## Session Provider Binding

Extend the existing `session_agent_bindings` row, keyed by `(session_key,
agent)`, with:

```text
provider_id        Stable ID from api-providers.json. Empty only for an
                   explicit native or legacy-unbound binding.
provider_revision  Non-secret version derived from provider configuration.
provider_protocol  Resolved protocol used for this agent.
provider_state     available | changed | deleted | incompatible | legacy_unbound
agent_session_id   Existing external Claude session ID or Codex thread ID.
agent_ctx_seq      Existing MindFS context sequence.
```

The binding must not contain an API key, a base URL, an environment map, a
Codex command argument, or a full provider display object.

`provider_id` is the identity. A provider rename must not change it.
`provider_revision` changes when endpoint, credential, protocol, or model
catalog changes. It ensures a stale Codex client cannot be reused after an
edit.

## Provider Management Semantics

Provider management is never blocked by historical sessions. Validation happens
when an existing session needs a runtime, not when a provider is edited or
deleted.

| Provider operation | Binding effect | Behavior when the session next sends, opens, or resumes |
| --- | --- | --- |
| Rename | No identity change | Resolve same ID and show the new name. |
| Change model catalog | Revision changes | Validate the selected model against the new catalog. |
| Rotate API key | Revision changes | New runtime uses the new key; no global fallback. |
| Change base URL | Revision changes | Mark changed and require compatibility validation before resume. |
| Change protocol | Revision changes | Mark incompatible if the bound agent no longer supports it. |
| Delete | Mark deleted at runtime resolution | Historical view remains available; sending requires restoring or migrating. |
| Create another provider with the same name | No automatic relationship | It has a different ID and requires explicit migration. |

The delete confirmation should state the number of affected sessions but must
not prevent deletion. Affected sessions remain readable. Their next runtime
operation returns a typed `session_provider_unavailable` error and exposes a
migration action.

## New Session Binding

1. The front end includes `provider_id` in the first message payload.
2. The server validates provider existence, protocol compatibility, and model
   compatibility before opening the external agent.
3. Under the existing session send lock, the server persists the provider
   binding before creating the external runtime.
4. The first provider binding is immutable for normal sends. Later messages may
   send the same `provider_id` as an assertion, but a different ID returns
   `session_provider_mismatch`.
5. Queued messages retain the provider ID selected at enqueue time. They must
   not acquire whichever provider is globally selected when they run later.

`preferences.LastConfigSelection` remains a default for a new session. It is
not a source of provider credentials for an existing session.

## Model Changes

Model changes are supported within a session only when the provider binding is
unchanged.

1. Resolve the session binding and current provider catalog.
2. Validate the requested model against that bound catalog or the explicit
   native catalog.
3. Call the runtime `SetModel` operation.
4. Persist the selected model only after `SetModel` succeeds.
5. On failure, keep the old model, binding, and runtime state. Do not write a
   partial session model update.

When no in-memory runtime exists, open or resume it with the bound provider and
the requested compatible model. A model ID must not authorize changing the
provider implicitly.

## Provider Migration

Changing a provider for an existing Claude or Codex native thread is not a safe
in-place operation. It changes credentials, endpoint, protocol behavior, and
potentially the provider's native conversation namespace.

The UI must offer an explicit `Migrate to provider` action. It creates a child
session and requires a target provider and target model.

The child session:

1. Copies the visible MindFS history prefix from the source session.
2. Binds the target provider before creating a runtime.
3. Starts a new external Claude session or Codex thread.
4. Records that the child is a provider migration in non-secret source metadata.

The source session binding and external native session are never modified.

## Claude Runtime

Claude execution is session-scoped.

- Build provider-specific environment only in memory.
- Pass it through the Claude SDK `WithEnv` option.
- Use `ANTHROPIC_BASE_URL` and `ANTHROPIC_AUTH_TOKEN` for the selected provider.
- Do not write `~/.claude/settings.json` for session execution.
- Resume with the stored external Claude session ID and the same resolved binding.

The existing Claude runtime already creates an SDK client per logical MindFS
session. It must additionally retain and validate provider identity/revision.

## Codex Runtime

Codex execution is session/provider scoped.

- Pass provider-specific environment through `CodexOptions.Env`.
- Use Codex app-server `-c` overrides for provider table, model provider,
  base URL, and wire API configuration.
- Do not write `~/.codex/config.toml` for session execution.
- Do not put API keys in `-c` arguments because command lines are observable.
  Pass secrets only through the spawned process environment.
- Change the Codex client cache key from `agentName` to at least:

```text
agent_name + provider_id + provider_revision
```

Two sessions with different providers therefore cannot reuse the same Codex
app-server client. A provider revision change starts a new client instead of
reusing a client initialized with stale credentials or endpoint settings.

If Codex requires a provider token only from a config file rather than its
inherited environment, create an isolated, private runtime `CODEX_HOME` only
after verifying that direct environment injection is insufficient. Such a
directory must have restricted permissions and must not be the shared global
`~/.codex` directory.

## Runtime Recovery and Failure Safety

When MindFS, Claude Code, or the Codex app-server stops, reopening an existing
session follows this sequence:

```text
load session-agent binding
  -> resolve provider_id from api-providers.json
  -> check state, revision, protocol, and model compatibility
  -> construct in-memory environment and Codex overrides if needed
  -> resume the stored external Claude session ID or Codex thread ID
  -> accept the next message only after resume succeeds
```

The following are hard failures, not fallback cases:

- Provider was deleted.
- Provider protocol is no longer compatible with the agent.
- Selected model is absent from the applicable catalog.
- Stored external session/thread resume fails.
- A request asserts a provider different from the existing session binding.

The current behavior that retries a failed external resume by opening a fresh
runtime session must be removed for bound sessions. It risks silently losing
native context continuity. A failure returns a typed error and offers explicit
migration instead.

An existing session must never read `preferences.LastConfigSelection`,
`~/.claude/settings.json`, or `~/.codex/config.toml` as a credential fallback.

## Auditing and Secret Hygiene

Add non-secret runtime logs at open and send boundaries:

```text
[agent/provider] open session=... agent=... provider_id=... revision=... base_url_origin=... model=...
[agent/provider] send session=... agent=... provider_id=... revision=... model=...
```

`base_url_origin` may contain only scheme, host, and optional port. Do not log
full URL paths, query strings, tokens, environment values, authorization
headers, or provider structs that include credentials.

The implementation must verify that API keys never appear in SQLite session
bindings, exchange JSONL, WebSocket payloads, public provider DTOs, or logs.

## Fork History Model

MindFS forks are independent sessions, not a shared append-only parent log.

For a normal fork, MindFS:

1. Finds the fork sequence in the parent session.
2. Creates a child session with `ParentSessionKey` and fork source metadata.
3. Copies the parent exchange prefix through the selected sequence into the
   child's own JSONL.
4. Creates a native Claude/Codex fork from the parent external session/thread.
5. Persists the child external session/thread ID.

The parent JSONL never receives the child's later exchanges. The child JSONL
contains the copied prefix plus its own future exchanges. The session database
stores the tree relationship through `parent_session_key`.

For this design:

- A normal fork copies the parent's provider binding and uses a native fork.
- A provider migration fork binds the explicitly selected target provider and
  creates a new external thread/session after copying the history prefix.
- Parent deletion currently cascades through child sessions; this existing
  behavior must be considered when presenting migration forks.

## UI Requirements

- Provider picker is a default for new sessions unless a session-specific
  migration flow is open.
- Existing session details display provider name, provider state, revision
  state, and model without exposing the endpoint secret or key.
- Deleted or incompatible providers show history normally and display a clear
  migration action before send.
- Provider edits show the number of affected sessions as information, not a
  blocking condition.
- Provider migration requires explicit target provider and target model.
- Model switch errors retain the UI's prior selected model.

## Implementation Areas

All implementation changes remain under the MindFS source tree.

```text
server/internal/session/manager.go
server/internal/session/types.go
server/internal/agent/types/types.go
server/internal/agent/pool.go
server/internal/agent/codex/session.go
server/internal/agent/claude/session.go
server/internal/api/agent_api_provider.go
server/internal/api/usecase/session.go
server/internal/api/ws.go
web/src/services/session.ts
web/src/components/* provider/session selection code
corresponding Go and front-end test files
```

The implementation must not write `.mindfs/sessions` directly, global Claude or
Codex configuration files, global environment variables, or installed MindFS
runtime files.

## Test Matrix

### Persistence and Provider Lifecycle

- Existing database migration creates legacy-unbound bindings without selecting
  the current global provider.
- Provider rename preserves session recoverability through provider ID.
- Provider edit changes revision and prevents stale runtime reuse.
- Provider deletion succeeds even when sessions refer to it.
- Deleted provider blocks send/resume with a typed error and does not fall back.
- Same-name replacement provider does not match an old binding.

### Claude and Codex Isolation

- Two Claude sessions with different providers receive different environments.
- Two Codex sessions with different providers create different clients.
- Same Codex provider and revision follows the defined sharing policy.
- Changed Codex provider revision creates a fresh client.
- No Claude/Codex session execution writes global provider configuration.
- API keys are absent from logs, SQLite bindings, JSONL, and WS payloads.

### Model, Resume, and Recovery

- Bound-provider model switch succeeds before persistence.
- Failed model switch leaves the prior model unchanged.
- MindFS restart rebuilds the original provider environment and resumes the
  original external session/thread.
- Claude/Codex runtime restart rebuilds the original provider environment.
- A changed global provider default cannot change an existing session.
- Deleted/incompatible provider and failed native resume do not create a fresh
  external conversation.
- Queued messages retain their enqueue-time provider binding.

### Forks and Migration

- Normal fork copies provider binding and native fork context.
- Provider migration fork binds the target provider and creates a fresh native
  conversation.
- Parent JSONL remains independent from child exchanges.
- Child JSONL contains copied prefix plus child continuation.

## Validation Gate

Before delivery:

```powershell
go test ./server/internal/session/ ./server/internal/agent/... ./server/internal/api/... -count=1
cd web
npm run typecheck
```

After implementation and tests, run an independent code review focused on
provider fallback, resume safety, provider cache keys, migration behavior,
concurrency, and secret exposure. Fix all confirmed issues and rerun the
relevant test suite.
