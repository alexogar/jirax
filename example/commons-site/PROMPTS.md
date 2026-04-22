# Playground Prompts

These prompts are written for assistants operating from `example/commons-site/`.

## Orientation

```text
Use Jirax in this folder to inspect the current Jira scope, list the available fields, statuses, and issue types, and summarize what this project looks like. Prefer JSON output first, then give me a short human summary.
```

## Search

```text
Use Jirax to search this public Jira project for issues related to build failures or broken builds. Show the top relevant matches and explain why they seem relevant.
```

## Issue Inspection

```text
Use Jirax to inspect COMMONSSITE-166 and summarize the issue, current status, type, priority, and any important context from the description or changelog.
```

## Workflow Discovery

```text
Use Jirax to discover what statuses exist in this project and explain the likely workflow shape of the project from the available data.
```

## Agent Safety Pattern

```text
Before making assumptions, run `jirax config show --json` and `jirax know --json`, then continue using `jirax know fields --json` and `jirax issue ... --json` as needed. Explain your findings briefly after inspecting the data.
```
