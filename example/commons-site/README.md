# Commons Site Playground

This folder is a small public Jirax playground aimed at trying the CLI with Codex, Copilot, or manual terminal use.

It is preconfigured for Apache's public Jira project:

- Jira base URL: `https://issues.apache.org/jira`
- Project: `COMMONSSITE`

That makes it a safe public example with real Jira data and no company credentials.

## What This Folder Is For

Use this folder when you want to:

- test Jirax against a real public Jira
- try agent workflows without touching your company Jira
- inspect how project config discovery works
- experiment with `know`, `search`, and `issue` commands

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
../../jirax issue COMMONSSITE-166 --json
```

Because `.jirax.json` lives in this folder, Jirax should automatically discover and use it.

## Good First Commands

```bash
../../jirax config show --json
../../jirax know fields --json
../../jirax know statuses --json
../../jirax know types --json
../../jirax search "build" --json
../../jirax issue COMMONSSITE-166 --json
```

## Agent Prompts

See [PROMPTS.md](/Users/alekseiogarkov/Projects/code/jirax/example/commons-site/PROMPTS.md) for ready-made prompts you can paste into Codex or Copilot chat while your working directory is this folder.

## Notes

- This example is intended for read-oriented experimentation.
- Public Jira behavior can change over time, but Apache Jira is a stable public test target.
- If anonymous access becomes more restricted later, you can still override config locally in `.jirax/config.json`.
