# Jirax

> Local-first Jira CLI for humans and agents.

Jirax keeps Jira reads fast, scoped, and scriptable by syncing a slice of your Jira data into a local SQLite database. The CLI uses that local cache for issue lookups and search, while writes stay explicit and guarded with refresh-before-write and dry-run validation.

## Status

Jirax is ready to live in a GitHub repository with:

- a clean `cmd/` + `internal/` layout
- unit and store-backed tests
- embedded SQLite with no external binary dependency
- GitHub Actions CI on pushes and pull requests
- tag-driven release automation for macOS, Linux, and Windows binaries

## Why It Exists

Most Jira CLIs make every read feel like a network round-trip. Jirax takes a different stance:

- Sync a project or JQL-defined scope into local SQLite.
- Support monorepos that map to multiple Jira projects in one working tree.
- Query cached issues with sub-second local reads.
- Preserve raw Jira payloads while also storing a normalized schema.
- Support JSON output everywhere so scripts and agents can consume it cleanly.
- Treat writes as a separate, safer path from reads.

## Project Layout

```text
.
├── example/commons-site/ # public Jira playground for assistants
├── cmd/jirax/           # CLI entrypoint
├── internal/jirax/      # app, config, Jira client, store, tests
├── SPEC.md              # product spec
├── Makefile             # build/test helpers
└── README.md
```

## Requirements

- Go `1.26+`
- Jira credentials for live sync/write operations

Check the local tools with:

```bash
go version
```

## Build

```bash
make build
```

Or directly:

```bash
go build -o jirax ./cmd/jirax
```

## GitHub Actions

The repository includes two workflows:

- `.github/workflows/ci.yml`
  Runs tests and a fresh build on `ubuntu-latest`, `macos-latest`, and `windows-latest` for pushes to `main` and pull requests.
- `.github/workflows/release.yml`
  Builds release archives and publishes a GitHub Release when you push a tag like `v0.1.0`.

Create a release with:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The workflow will attach release archives for:

- `darwin-amd64`
- `darwin-arm64`
- `linux-amd64`
- `linux-arm64`
- `windows-amd64`
- `windows-arm64`

## Agent Workflow

Jirax now includes an agent-oriented skill in [SKILL.md](/Users/alekseiogarkov/Projects/code/jirax/SKILL.md) plus a `know` command family for quick orientation.

Use these first when an assistant needs to understand the current Jira world:

```bash
./jirax know --json
./jirax know fields --json
./jirax know statuses --json
./jirax know types --json
./jirax know transitions DEMO-123 --json
```

These commands are meant to answer questions like:

- What scope is synced right now?
- Which fields exist, and which are custom?
- Which statuses and issue types are actually present?
- Which transitions are valid for this issue?
- Which Jira projects are actually in scope for this repo?

## Project Config Files

Jirax can now be configured directly from a folder or repository with either:

- `.jirax.json`
- `.jirax.conf`

Jirax searches upward from the current working directory and uses the nearest project config it finds. It also keeps a writable overlay at `.jirax/config.json` in that same project root, so checked-in defaults and local overrides can coexist.

Example `.jirax.json`:

```json
{
  "server": {
    "base_url": "https://your-company.atlassian.net",
    "user": "agent@company.com",
    "ca_cert_file": "./certs/company-root-ca.pem"
  },
  "context": {
    "name": "repo",
    "projects": ["DEMO", "PLAT", "OPS"],
    "fields": {
      "sprint": "customfield_10010"
    }
  },
  "aliases": {
    "sprint": "customfield_10010"
  }
}
```

Example `.jirax.conf`:

```ini
base_url=https://your-company.atlassian.net
user=agent@company.com
ca_cert_file=./certs/company-root-ca.pem
projects=DEMO,PLAT,OPS
fields=customfield_10010,customfield_10020
alias.sprint=customfield_10010
```

You can also provide named field mappings in `.jirax.conf` with `name:field_id` pairs:

```ini
fields=sprint:customfield_10010,epic:customfield_10011
```

Supported `.jirax.conf` keys:

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

Inspect the fully resolved config with:

```bash
./jirax config show --json
```

## Start

Show help:

```bash
./jirax help
```

Set a local context:

```bash
./jirax ctx set --project DEMO
./jirax ctx set --projects DEMO,PLAT,OPS
```

Set a real Jira-backed context:

```bash
export JIRAX_BASE_URL="https://your-company.atlassian.net"
export JIRAX_USER="you@company.com"
export JIRAX_TOKEN="jira_api_token"

./jirax ctx set --projects DEMO,PLAT,OPS
./jirax sync
./jirax search authentication
./jirax issue DEMO-123
```

For internal Jira servers with custom certificates:

```bash
./jirax ctx set \
  --projects DEMO,PLAT,OPS \
  --base-url "https://jira.internal.company" \
  --user "you@company.com" \
  --token "jira_api_token" \
  --ca-cert-file "./certs/company-root-ca.pem"
```

If your environment is especially locked down and you need a temporary escape hatch, Jirax also supports:

```bash
./jirax ctx set --project DEMO --insecure-skip-verify
```

Use that only as a last resort for internal environments.

You can also pass credentials inline:

```bash
./jirax ctx set \
  --projects DEMO,PLAT,OPS \
  --base-url "https://your-company.atlassian.net" \
  --user "you@company.com" \
  --token "jira_api_token"
```

## Public Playground

There is a ready-to-use public example in [example/commons-site](/Users/alekseiogarkov/Projects/code/jirax/example/commons-site/README.md).

That folder is configured for Apache's public `COMMONSSITE` Jira project and is meant for testing Jirax with Codex or Copilot without using private company infrastructure.

## Test

Run the full test suite:

```bash
make test
```

Or directly:

```bash
go test ./...
```

The current test suite covers:

- context field handling and scope generation
- create/edit validation helpers
- transition matching
- issue normalization
- SQLite bootstrap, sync-state persistence, issue round-trip, FTS search, and local JQL filtering

## Command Guide

### Scope and Sync

```bash
./jirax ctx set --project DEMO
./jirax ctx set --projects DEMO,PLAT,OPS
./jirax ctx set --jql 'project = DEMO AND assignee = currentUser()'
./jirax know
./jirax sync
./jirax sync --full
```

### Reads

```bash
./jirax config show --json
./jirax issue DEMO-123
./jirax issue DEMO-123 --json
./jirax search "billing timeout"
./jirax search --limit 50 --json sync
./jirax search --jql 'status = "In Progress" AND labels in (cli, urgent)' --mode local --json
./jirax search --jql 'assignee = currentUser() AND updated >= -7d' --mode remote --json
./jirax search --jql 'project = DEMO AND sprint = "Sprint 4" ORDER BY updated DESC' --mode auto --json
```

`jirax search` now supports two styles:

- Text search: fast FTS search over the local cache.
- JQL search: with `--jql`, Jirax combines the current context scope with your extra filter and runs it either locally, remotely, or with local-first fallback.

JQL modes:

- `--mode local`: run against the synced cache only.
- `--mode remote`: send the resolved JQL to Jira directly.
- `--mode auto`: try the local cache first and fall back to Jira if the query uses unsupported local JQL features.

The local JQL engine intentionally supports a useful subset rather than pretending to implement all of Jira JQL. Supported local operators include:

- boolean logic with `AND`, `OR`, `NOT`, and parentheses
- `=`, `!=`, `~`, `!~`, `IN`, `NOT IN`
- `>`, `>=`, `<`, `<=` for sortable text, numbers, and `created` or `updated`
- `IS EMPTY`, `IS NOT EMPTY`
- `ORDER BY ... ASC|DESC`

Supported local fields include `project`, `key`, `summary`, `description`, `status`, `issuetype`, `priority`, `assignee`, `reporter`, `labels`, `created`, `updated`, and cached custom fields such as `customfield_10010` or field aliases discovered during sync.

### Writes

Dry-run first:

```bash
./jirax create --project DEMO --summary "Ship CLI docs" --dry-run
./jirax edit DEMO-123 --fields-json '{"summary":"New title"}' --dry-run
./jirax comment DEMO-123 --body "Investigating now" --dry-run
./jirax transition DEMO-123 --to "Done" --dry-run
```

Then execute:

```bash
./jirax create --project DEMO --summary "Ship CLI docs"
./jirax edit DEMO-123 --fields-json '{"summary":"New title"}'
./jirax comment DEMO-123 --body "Investigating now"
./jirax transition DEMO-123 --to "Done"
```

In multi-project contexts, `create` should usually pass `--project` explicitly so the target project is unambiguous.

## Local State

By default Jirax stores local state in the project directory:

- `.jirax/config.json`
- `.jirax/jirax.db`

You can override the database location with:

```bash
export JIRAX_DB_PATH=/absolute/path/to/jirax.db
```

## Notes

- Reads are local-first, but the CLI still syncs on commands to keep data fresh.
- Writes call Jira directly and log the attempted mutation locally.
- SQLite is bundled through a pure-Go driver, so the CLI works without a separately installed `sqlite3` binary.
- Jira API calls automatically retry on rate limits and transient `502`/`503`/`504` failures with backoff.
