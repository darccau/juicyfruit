# Memory subsystem introduction

## Context

The repository had persistent state for sources, packages, and installations,
but nothing that let an agent record why a decision was made or recall it in a
later session. An earlier local prototype carried keyword search over a note
vault, but that capability never shipped to the CLI. This change adds a
first-class `memory` domain to `juicyfruit_cli` and a `memory-core` package that
teaches agents to use it.

Nothing was moved or renamed. Every change is additive except the removal of the
`rules/.gitkeep` placeholder, which the two new rule files replace.

## Evidence captured during execution

- Before the change, `dart test` reported 77 passing tests and 1 skipped.
- After the change, `dart test` reports 173 passing tests and 1 skipped; the 96
  new tests cover paths, configuration, the store, keyword search, the Ollama
  client, the vector index, the hook writer, and the command group.
- `dart format --output=none --set-exit-if-changed .` reported 0 of 55 files
  changed.
- `dart analyze --fatal-infos --fatal-warnings` reported no issues.
- `dart run build_runner build` wrote 1 output and left the working tree clean.
- `dart compile exe bin/juicyfruit.dart` produced a working binary whose
  `memory --help` lists all seven subcommands.
- A scratch-home smoke run created `config.json`, `MEMORY.md`, the dated journal
  file, and the installed skill and rules under the Claude Code target root.
  With no embedding model present, setup reported keyword-only recall and wrote
  `"enabled": false`.

## Surface

| Command | Effect |
| --- | --- |
| `juicyfruit memory setup` | Creates the store, resolves recall, installs `memory-core`, and registers the capture hook |
| `juicyfruit memory add <message>` | Appends a journal entry, or writes a durable note with `--kind note --title` |
| `juicyfruit memory search <query>` | Ranks documents by embeddings when available, always falling back to keywords |
| `juicyfruit memory list --since 7d` | Prints journal entries newest first, merged across scopes |
| `juicyfruit memory digest --since 14d` | Renders a day-grouped briefing bounded by `--max-chars` |
| `juicyfruit memory index` | Embeds new and changed documents and prunes deleted ones |
| `juicyfruit memory capture` | Records session end, an optional `git diff --stat`, and a summary TODO |

New persisted contracts: `schemas/memory-config.schema.json` for `config.json`
and an internal `{"version": 1}` envelope for `index/embeddings.json`.

## Verification contract

Changes to memory run `.github/workflows/juicyfruit_cli.yaml` on macOS, Linux, and
Windows. The workflow formats, analyzes, and tests the CLI, then executes a
memory end-to-end block against temporary `JUICYFRUIT_MEMORY_HOME`,
`JUICYFRUIT_CONFIG_HOME`, and `JUICYFRUIT_USER_ROOT` directories: `memory setup --all`
against the canonical source, an activity entry, a note, a search, a list, and a
digest. The workflow also watches `rules/**`, so a change to the shipped rules
re-runs the package resolution that installs them.
