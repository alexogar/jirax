# Commons Site Playground

This folder is a small public Jirax playground aimed at trying the CLI with Codex, Copilot, or manual terminal use.

Use the Jirax CLI as the source of truth for Jira answers in this folder. Do not query `.jirax/jirax.db` directly unless you are explicitly debugging cache internals.

It is preconfigured for Apache's public Jira project:

- Jira base URL: `https://issues.apache.org/jira`
- Project: `COMMONSSITE`

That makes it a safe public example with real Jira data and no company credentials.

## What This Folder Is For

Use this folder when you want to:

- test Jirax against a real public Jira
- try agent workflows without touching your company Jira
- inspect how project config discovery works
- experiment with `know`, `search`, `issue`, and JQL filtering

## Quick Start

From the repository root, build Jirax:

```bash
go build -o jirax ./cmd/jirax
```

Then move into this folder:

```bash
cd example/commons-site
../../jirax config show --json
../../jirax know --json
../../jirax sync --json
../../jirax search commons --json
../../jirax search --jql 'status = "Open"' --mode auto --json
../../jirax issue COMMONSSITE-166 --json
```

Because `.jirax.json` lives in this folder, Jirax should automatically discover and use it.

## Good First Commands

```bash
../../jirax config show --json
../../jirax know fields --json
../../jirax know statuses
../../jirax know types
../../jirax search "build" --json
../../jirax search --jql 'summary ~ "build"' --mode local --json
../../jirax search --jql 'status in ("Open", "Reopened")' --mode remote --json
../../jirax issue COMMONSSITE-166 --json
```

## JQL Playground

This folder is a good place to try both local and remote JQL behavior:

```bash
../../jirax search --jql 'project = COMMONSSITE AND status = Open' --mode local --json
../../jirax search --jql 'summary ~ "site" AND labels is not empty' --mode auto --json
../../jirax search --jql 'reporter = builds' --mode remote --json
```

Practical guidance:

- Use `--mode local` when you want deterministic cache-only filtering.
- Use `--mode auto` when you want local-first behavior with remote fallback.
- Use `--mode remote` when you know the query uses Jira-only features the local subset may not support.
- Prefer plain `../../jirax know statuses` when you need workflow labels quickly. In current builds, treat `know` subcommands as human-readable first and only pipe to JSON tools after verifying the output is valid JSON.
- When you need a ranked result such as "oldest open issues," fetch the matching issues with `../../jirax search ... --json` and sort the returned JSON client-side instead of relying on `ORDER BY` in JQL.
- Remember that this folder already scopes Jirax to `COMMONSSITE`, so your extra JQL is combined with that project scope automatically.

## Agent Prompts

See [PROMPTS.md](/Users/alekseiogarkov/Projects/code/jirax/example/commons-site/PROMPTS.md) for ready-made prompts you can paste into Codex or Copilot chat while your working directory is this folder.

## Notes

- This example is intended for read-oriented experimentation.
- Public Jira behavior can change over time, but Apache Jira is a stable public test target.
- If anonymous access becomes more restricted later, you can still override config locally in `.jirax/config.json`.
