---
name: jirax-agent
description: Use this skill when working with the Jirax CLI to inspect Jira context, discover fields and statuses, choose safe mutations, and answer questions from the local synced cache before calling Jira directly.
---

# Jirax Agent

Use Jirax as a local-first Jira interface. Prefer read commands that inspect the local cache and current context before making assumptions or issuing writes.

Jirax may inherit project defaults from `.jirax.json` or `.jirax.conf` in the current folder or one of its parents. Treat those files as part of the working context when results look pre-scoped or repo-specific.
Contexts may scope a single Jira project, multiple Jira projects, or raw JQL. In monorepos, assume multi-project scope is possible until `./jirax config show --json` or `./jirax know --json` says otherwise.

## Workflow

1. Start by checking the current scope:
   `./jirax know --json`
   `./jirax config show --json`
2. Discover schema and workflow vocabulary before planning edits:
   `./jirax know fields --json`
   `./jirax know statuses --json`
   `./jirax know types --json`
3. Inspect specific issues with:
   `./jirax issue ISSUE-123 --json`
4. Search the synced cache with:
   `./jirax search "query" --json`
   `./jirax search --jql 'status = "In Progress"' --mode auto --json`
5. Before transitioning an issue, inspect allowed transitions:
   `./jirax know transitions ISSUE-123 --json`
6. For writes, prefer dry runs first:
   `./jirax create ... --dry-run --json`
   `./jirax edit ISSUE-123 ... --dry-run --json`
   `./jirax comment ISSUE-123 ... --dry-run --json`
   `./jirax transition ISSUE-123 ... --dry-run --json`

## Guidance

- Treat `jirax know` as the default orientation step for agents.
- Use JSON output whenever the result will feed another step.
- Use cached field and status information to avoid inventing field ids, issue types, or workflow states.
- Prefer `search --jql ... --mode auto --json` when the task needs structured filtering; it uses the cache first and falls back to Jira for unsupported local JQL.
- Use `know transitions` before choosing a transition target because workflows vary per issue.
- In multi-project contexts, pass `--project` explicitly on `create` unless the config clearly resolves to one project.
- If the local context looks wrong, reset scope with `./jirax ctx set ...` and sync again.
- Remember that `ctx set` writes a local overlay in `.jirax/config.json`, while `.jirax.json` and `.jirax.conf` are good project defaults to check into a repo.

## Core Commands

```bash
./jirax know --json
./jirax know fields --json
./jirax know statuses --json
./jirax know types --json
./jirax know transitions ISSUE-123 --json
./jirax issue ISSUE-123 --json
./jirax search "text" --json
./jirax search --jql 'status = "In Progress" AND labels in (cli, urgent)' --mode auto --json
```
