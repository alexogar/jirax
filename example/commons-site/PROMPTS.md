# Playground Prompts

These prompts are written for assistants operating from `example/commons-site/`.

## Orientation

```text
Use only the Jirax CLI in this folder to inspect the current Jira scope, list the available fields, statuses, and issue types, and summarize what this project looks like. Prefer JSON output where the command actually returns valid JSON, but do not fall back to sqlite or direct cache inspection.
```

## Search

```text
Use Jirax to search this public Jira project for issues related to build failures or broken builds. Show the top relevant matches and explain why they seem relevant.
```

## Local JQL Search

```text
Use Jirax to search this public Jira project with structured JQL instead of plain text. Start with `jirax know --json`, inspect statuses with `jirax know statuses`, then run a few `jirax search --jql ... --mode local --json` queries to find recently updated open issues or issues whose summary mentions "build". Summarize the most relevant matches and mention which JQL worked best locally.
```

## Auto Fallback JQL

```text
Use Jirax to explore this project with `jirax search --jql ... --mode auto --json`. Try one simple query that should work locally and one more Jira-specific query that may require remote fallback. If you need ranked output such as oldest or newest issues, fetch the matching set first and sort the JSON client-side instead of relying on `ORDER BY` in JQL. Tell me which mode was actually used for each search and summarize the results.
```

## Issue Inspection

```text
Use Jirax to inspect COMMONSSITE-166 and summarize the issue, current status, type, priority, and any important context from the description or changelog.
```

## Workflow Discovery

```text
Use Jirax to discover what statuses exist in this project and explain the likely workflow shape of the project from the available data. Prefer `jirax know statuses` for the status list and only treat subcommand output as JSON after verifying it parses.
```

## Agent Safety Pattern

```text
Before making assumptions, run `jirax config show --json` and `jirax know --json`, then continue using `jirax know fields --json`, `jirax know statuses`, `jirax search --jql ... --mode auto --json`, and `jirax issue ... --json` as needed. Use the CLI as the source of truth for Jira answers, not direct sqlite queries. Explain your findings briefly after inspecting the data.
```
