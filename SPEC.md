# Jirax Specification

## 1. Product Definition

Jirax is a local-first Jira CLI designed primarily for AI assistants and power users.

Its core model is:

- reads happen against a local SQLite cache
- writes happen through explicit Jira API mutations
- every command is scoped by a context
- sync is automatic, incremental, and conservative

Jirax is intended to feel:

- fast enough for iterative assistant workflows
- explicit enough to be safe for writes
- flexible enough for real enterprise Jira instances
- portable across implementation languages

This specification is the source-of-truth product contract. It should be implementable in:

- Go
- TypeScript/Node.js
- another systems/runtime language if needed later

The product behavior matters more than the implementation language.

## 2. Core Principles

### 2.1 Local-first

The CLI should prefer local cached state for queries whenever possible.

### 2.2 Sync-on-command

Commands should refresh relevant Jira state automatically, but in a scoped and incremental way.

### 2.3 Agent-first

The interface should expose machine-friendly JSON and discovery-oriented commands so assistants do not need to guess Jira schema or workflow state.

### 2.4 Safe writes

Writes should be validated, refresh before mutation, and support dry-run flows where possible.

### 2.5 Enterprise reality

The product must handle:

- custom fields
- multi-project monorepos
- Jira Cloud and Data Center
- internal/private Jira hosts
- custom internal CA certificates
- rate limits and transient overload

## 3. Goals

### 3.1 Primary goals

- Fast local issue lookup and search
- Reliable scoped sync
- Assistant-friendly knowledge discovery
- Safe issue creation and mutation
- Cross-platform single-binary operation
- Portable design that can be reimplemented in Node.js if needed

### 3.2 Non-goals for V1

- Full Jira mirror of all entities
- Full offline write queueing
- Full workflow/field normalization across every Jira variant
- GUI or TUI
- Plugin marketplace
- Background daemon requirement

## 4. Target Users

### 4.1 Primary

- AI coding assistants
- AI ops/support assistants
- engineers working from terminals

### 4.2 Secondary

- power users scripting Jira workflows
- monorepo teams spanning multiple Jira projects

## 5. High-Level Architecture

Jirax has four logical layers:

1. Config and context resolution
2. Jira API client
3. Local storage and indexing
4. CLI command surface

### 5.1 Read path

1. Resolve config and context
2. Run scoped sync if needed
3. Query local SQLite
4. Return text or JSON

### 5.2 Write path

1. Resolve config and context
2. Refresh targeted issue or metadata
3. Validate mutation
4. Optionally dry-run
5. Send Jira mutation
6. Log operation
7. Resync affected issue(s)

## 6. Context Model

Every command runs inside a context.

The context defines:

- what issues are in scope
- what fields are fetched and indexed
- what server configuration is active

### 6.1 Context scope types

Exactly one of these scope types must be active:

- `project`
- `projects`
- `jql`

### 6.2 Single-project context

Example:

```json
{
  "context": {
    "project": "DEMO"
  }
}
```

Resolved JQL:

```text
project = DEMO
```

### 6.3 Multi-project context

Example:

```json
{
  "context": {
    "projects": ["DEMO", "PLAT", "OPS"]
  }
}
```

Resolved JQL:

```text
project in (DEMO, PLAT, OPS)
```

This is especially important for monorepos.

### 6.4 Raw JQL context

Example:

```json
{
  "context": {
    "jql": "project in (DEMO, OPS) AND assignee = currentUser()"
  }
}
```

### 6.5 Context fields

`context.fields` is a dictionary, not just a list.

It maps assistant-friendly names to Jira field ids or canonical field names.

Example:

```json
{
  "context": {
    "fields": {
      "sprint": "customfield_10010",
      "epic": "customfield_10011",
      "summary": "summary"
    }
  }
}
```

Behavior:

- keys are local aliases / semantic names
- values are Jira field ids or Jira field names
- values are used for fetch/indexing
- keys improve readability for assistants and config authors

Backwards compatibility may still allow array-style field config, but the canonical spec shape is a dictionary.

### 6.6 Context naming

Contexts may have a `name`, but name is metadata, not scope.

## 7. Configuration Model

Jirax configuration is layered.

### 7.1 Config sources

Config precedence should be:

1. CLI flags
2. Environment variables
3. Local writable overlay: `.jirax/config.json`
4. Project config: `.jirax.json` or `.jirax.conf`
5. Built-in defaults

If implementation chooses a slightly different merge order internally, the user-visible behavior must still effectively match this precedence.

### 7.2 Project config discovery

Jirax should search upward from the current working directory for the nearest:

- `.jirax.json`
- `.jirax.conf`
- `.jirax/config.json`

The nearest matching project root becomes the active root.

### 7.3 Writable overlay

Jirax should maintain a writable local overlay at:

```text
.jirax/config.json
```

Purpose:

- preserve local overrides
- avoid editing checked-in project defaults when using `ctx set`

### 7.4 Supported config keys

#### Server

- `base_url`
- `user`
- `token`
- `ca_cert_file`
- `insecure_skip_verify`

#### Context

- `name`
- `project`
- `projects`
- `jql`
- `fields`

#### Storage

- `database_path`

#### Aliases

- `aliases`

### 7.5 Environment variables

Jirax should support:

- `JIRAX_BASE_URL`
- `JIRAX_USER`
- `JIRAX_TOKEN`
- `JIRAX_CA_CERT_FILE`
- `JIRAX_INSECURE_SKIP_VERIFY`
- `JIRAX_DB_PATH`

### 7.6 `.jirax.conf`

`.jirax.conf` is a simple key-value format for easy repo-level configuration.

Supported keys:

- `base_url`
- `user`
- `token`
- `ca_cert_file`
- `insecure_skip_verify`
- `database_path`
- `name`
- `project`
- `projects`
- `jql`
- `fields`
- `alias.<name>`

Example:

```ini
base_url=https://jira.internal.company
user=agent@company.com
ca_cert_file=./certs/company-root-ca.pem
projects=DEMO,PLAT,OPS
fields=sprint:customfield_10010,epic:customfield_10011
alias.sprint=customfield_10010
```

## 8. Server and Transport Requirements

### 8.1 Supported Jira deployments

- Jira Cloud
- Jira Data Center
- Jira Server-like internal deployments where API compatibility is sufficient

### 8.2 Authentication

Minimum requirement:

- basic auth style credentials or API token usage supported by current Jira endpoint behavior

Future auth schemes may be added later.

### 8.3 Custom certificates

Jirax must support internal/private Jira installations protected by custom or internal CA chains.

Support requirements:

- trust custom PEM CA bundle via config/env/flag
- optionally support `insecure_skip_verify` as an escape hatch
- insecure mode must be clearly documented as last resort only

### 8.4 Rate limits and transient failure handling

Jirax must not fail immediately on common transient Jira/API gateway failures.

At minimum it should retry:

- `429`
- `502`
- `503`
- `504`
- selected transient network timeouts or connection resets

Retry behavior should:

- honor `Retry-After` when present
- use bounded exponential backoff otherwise
- stop after a finite retry budget
- surface useful error messages if retries are exhausted

## 9. Sync Model

### 9.1 Sync triggers

Sync should happen:

- automatically on read commands where cached freshness matters
- before writes
- during explicit `jirax sync`

### 9.2 Sync types

#### Bootstrap sync

Used for:

- first use
- brand new context
- empty DB for current context

#### Incremental sync

Used for normal command execution.

Should filter by updated timestamp when possible.

#### Full sync

Explicit user request to ignore incremental cursor.

### 9.3 Targeted sync

Before write operations or direct issue inspection, Jirax should support targeted refresh of one issue or one relevant metadata set.

### 9.4 Sync safety

Sync should be idempotent and restart-safe.

### 9.5 Performance expectations

The implementation should aim for:

- local issue lookups in well under 1 second
- local full-text search in well under 1 second
- sync cost bounded by current scope, not global Jira size

## 10. Local Storage Model

SQLite is the local storage engine.

This must be embedded/bundled, not depend on a system `sqlite3` binary.

That requirement exists because:

- Windows support matters
- zero-dependency binaries are preferred
- CI and user machines should not need extra SQLite installation

### 10.1 Required tables

- `issues`
- `issue_raw`
- `comments`
- `changelog`
- `field_catalog`
- `field_aliases`
- `operation_log`
- `sync_state`
- FTS table for issue search

### 10.2 Table intent

#### `issues`

Normalized issue snapshot for primary query workloads.

#### `issue_raw`

Raw Jira JSON payload for debugging, compatibility, and future migrations.

#### `comments`

Normalized comments.

#### `changelog`

Normalized status/history changes or raw-ish change entries.

#### `field_catalog`

Discovered Jira fields and metadata.

#### `field_aliases`

Resolved alias-to-field mapping.

#### `operation_log`

Log of attempted mutations.

#### `sync_state`

Last successful sync timestamp per context.

### 10.3 Search

Use SQLite FTS5 or an equivalent embedded full-text capability.

Search should cover at least:

- issue key
- summary
- description

## 11. Field System

Jira instances vary heavily in field shape and custom fields.

Jirax must support:

- automatic field discovery
- custom field ids
- human-friendly aliases
- preserving raw field payloads

### 11.1 Field discovery

Jirax should fetch Jira field metadata and cache:

- field id
- field display name
- field type
- whether field is custom

### 11.2 Field aliases

Aliases may come from:

- project config
- local overlay
- auto-generated field name slugs

### 11.3 Extraction

The normalized issue model should extract common fields directly but retain custom fields in a structured map.

## 12. Command Surface

### 12.1 Required commands

- `jirax config show`
- `jirax ctx set`
- `jirax know`
- `jirax sync`
- `jirax issue`
- `jirax search`
- `jirax create`
- `jirax edit`
- `jirax comment`
- `jirax transition`

### 12.2 `jirax config show`

Purpose:

- expose the resolved config and config discovery details

Requirements:

- support `--json`
- include effective config
- include project config path
- include overlay config path
- include discovered root dir

### 12.3 `jirax ctx set`

Purpose:

- set or update the local overlay context

Inputs:

- `--project KEY`
- `--projects A,B,C`
- `--jql QUERY`
- optional server settings
- optional field mapping settings

Rules:

- exactly one of `--project`, `--projects`, or `--jql`

### 12.4 `jirax know`

Purpose:

- assistant-oriented discovery of current Jira shape and scope

Subcommands:

- `overview`
- `fields`
- `statuses`
- `types`
- `transitions ISSUE-123`

Requirements:

- support `--json`
- sync before reading when needed

### 12.5 `jirax sync`

Purpose:

- force sync

Options:

- `--full`
- `--json`

### 12.6 `jirax issue`

Purpose:

- fetch one issue from cache after targeted refresh

Options:

- `--json`

### 12.7 `jirax search`

Purpose:

- full-text and recent issue query over local cache

Options:

- `--limit`
- `--json`

### 12.8 `jirax create`

Purpose:

- create issue through Jira API

Requirements:

- validate using Jira create metadata
- support `--dry-run`
- support additional fields as JSON
- if context contains multiple projects, caller should pass `--project`

### 12.9 `jirax edit`

Purpose:

- update issue fields

Requirements:

- targeted refresh before write
- validate against edit metadata
- support `--dry-run`

### 12.10 `jirax comment`

Purpose:

- add a comment to an issue

Requirements:

- targeted refresh before write
- support `--dry-run`

### 12.11 `jirax transition`

Purpose:

- move issue through workflow

Requirements:

- inspect valid transitions first
- targeted refresh before write
- support `--dry-run`

## 13. Output Model

Every important command should support JSON output.

### 13.1 Text output

Text output should be readable in a terminal and optimized for humans.

### 13.2 JSON output

JSON output should be stable enough for assistant workflows and scripts.

### 13.3 Assistant-friendly behavior

The JSON shape should prioritize:

- explicit keys
- useful metadata
- resolved scope visibility
- discoverability of fields and transitions

## 14. Mutation Safety

Writes are never blind.

Required safeguards:

- refresh before write
- validate required/editable fields
- dry-run support where possible
- explicit operation logging
- resync after mutation

## 15. Knowledge/Discovery Workflow

Assistants should generally follow this order:

1. `jirax config show --json`
2. `jirax know --json`
3. `jirax know fields --json`
4. `jirax know statuses --json`
5. `jirax know transitions ISSUE-123 --json` when planning a transition
6. `jirax issue ISSUE-123 --json` for targeted inspection
7. write commands with `--dry-run` first

This workflow is a first-class product requirement, not just documentation advice.

## 16. Cross-Platform Requirements

Jirax must work on:

- macOS
- Linux
- Windows

Implications:

- no dependency on system `sqlite3`
- path handling must be platform-safe
- test cleanup must explicitly close DB handles
- CI should validate Windows builds and tests

## 17. Implementation Portability

This product may need a future Node.js/TypeScript implementation.

The spec therefore avoids depending on Go-only concepts.

### 17.1 Behavior that must remain stable across implementations

- layered config discovery
- context rules
- knowledge command semantics
- sync model
- rate-limit retry behavior
- bundled embedded SQLite
- JSON output contracts at a high level
- mutation safety rules

### 17.2 Suggested Node.js mapping

If reimplemented in TypeScript/Node.js:

- CLI framework: `commander`, `yargs`, or similar
- HTTP: `fetch`/`undici`/`got`
- SQLite: bundled embedded driver such as `better-sqlite3` or equivalent packaged approach
- config merge and discovery: same precedence model
- retry/backoff: shared product behavior, not transport-specific accident

## 18. Test Requirements

At minimum, the test suite should cover:

- config discovery and merge precedence
- field-map parsing
- single-project vs multi-project vs JQL scope generation
- issue normalization
- local store round-trip
- full-text search
- knowledge queries
- retry behavior on rate limits
- Windows-safe DB cleanup behavior

## 19. V1 Acceptance Criteria

Jirax V1 is acceptable when all of the following are true:

- A user can configure repo-level scope with `.jirax.json` or `.jirax.conf`
- A user can scope to one project, multiple projects, or JQL
- Read commands return useful data from local cache
- `know` commands expose enough information for assistants to avoid guessing
- Writes validate and support dry-run where appropriate
- Sync survives common transient Jira rate-limit/server failures
- Internal CA-backed Jira can be configured without OS-level certificate hacks
- The CLI works on Windows/macOS/Linux
- The product could be reimplemented in Node.js without changing core behavior

## 20. Summary

Jirax is not just “a Jira CLI.”

It is a:

- local-first Jira cache
- safe Jira mutation layer
- schema discovery tool
- assistant-oriented terminal interface

Any implementation, whether Go or TypeScript, should preserve that identity.
