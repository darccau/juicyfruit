# Juicyfruit

[Português do Brasil](README.pt-BR.md) · [Español](README.es.md)

Juicyfruit is local-first infrastructure for durable, portable AI-assisted engineering.

Agents and chats are temporary. Project memory, task state, skills, rules, templates, and context should not be. Juicyfruit keeps that durable layer in developer-controlled files, then renders the right pieces into whichever agent target you explicitly choose.

## What Juicyfruit Provides

- Portable agent setup for Codex, Claude Code, Cursor, Antigravity/GCA, Devin, Gemini CLI, Via, and generic directory targets.
- Durable memory for project and user knowledge outside the current chat.
- Deterministic context packages for a specific task.
- Portable skills, rules, personas, sub-agents, templates, scripts, assets, and references.
- Durable task runtime with tasks, dependencies, owners, sessions, checkpoints, blockers, validations, and next actions.
- Read-only package sources, dependency resolution, lockfiles, transactional installation, status, diff, update, and safe uninstall.
- Local-first operation: state remains readable, versionable, recoverable, and inspectable on the developer machine.

## How It Works

```text
canonical Juicyfruit state
        |
        v
sources, packages, memory, tasks, context
        |
        v
target adapters
        |
        v
Codex / Claude Code / Cursor / Antigravity / Devin / Gemini CLI / Via / generic
```

Provider directories such as `.codex/`, `.claude/`, `.cursor/`, `.gemini/`, `.devin/`, `.via/`, `.agents/`, and `.juicyfruit/` are adapter outputs. They are not the canonical board, memory, or source of truth.

## Core Concepts

### Memory

Memory answers: "what do we know?"

It stores durable decisions, facts, conventions, lessons, and relevant history in user and project scopes:

- `~/.juicyfruit/memory/` for knowledge that applies across projects.
- `<repo>/.juicyfruit/memory/` for knowledge that only applies to one repository.

Memory is append-only by design. Journals grow over time, notes are written once, and derived indexes can be regenerated. Search works offline with keyword ranking. Optional local Ollama embeddings improve ranking when available and fall back to keywords when unavailable.

### Context

Context answers: "what should this task load right now?"

Projects can declare named context topics in `.juicyfruit/config.yaml`. A task can reference topics and files. `juicyfruit context build JCF-...` returns a deterministic `juicyfruit.context/v1` package with grouped sources and an approximate token estimate, so agents load relevant material instead of rediscovering the project every session.

### Skills

Skills are portable capability guides. Each skill lives in `skills/<name>/SKILL.md`, with optional references, scripts, and assets loaded only when needed. Skills keep agent behavior teachable without dumping every detail into every prompt.

### Rules

Rules are always-on constraints and standards. They live in `rules/` and are rendered by adapters into each selected target. Use rules for behavior that must apply broadly; use skills for task-specific procedures.

### Personas

Personas are lightweight voice and preference files. They live in `personas/` and can be seeded into targets without overwriting local user edits on future updates.

### Templates

Templates describe the desired shape of an output artifact: voice, structure, length, format, and constraints. Juicyfruit provides the schema and commands; teams create and package their own templates.

When creating a template, ask or provide its kind, what it is for, and output
format. The format is open: known targets are suggestions, and a project can
define a pure-text, custom-file, or renderer-specific target. New blank
templates are written to the repository under
`templates/<name>/TEMPLATE.md`, not to the user profile, so they can later be
packaged or exported.

### Packages And Sources

Packages group canonical content into versioned installable units. A package declares its supported targets, dependencies, conflicts, and content paths. Sources are read-only catalogs, either local, Git-backed, or archive-backed. Installing packages writes to target adapters, never to the source.

### Targets And Adapters

A target is an explicit agent environment such as `codex`, `claude-code`, `cursor`, `antigravity`, `devin`, `gemini-cli`, `via`, `generic`, or `dsh`. An adapter maps canonical Juicyfruit content into that target's directory layout. The DSH integration requires DSH `0.1.5-rc.3` or newer.

`juicyfruit setup --all` installs only targets that are both declared by the discovered official packages and already configured in the selected local scope. It does not install into every built-in adapter just because Juicyfruit knows about it.

### Task Runtime

Task Runtime answers: "what are we doing now?"

Projects keep durable work state in `.juicyfruit/` and machine-local state in `.juicyfruit/runtime/`. The core entities are:

- Run: a larger objective or orchestration unit.
- Task: a durable unit of work with acceptance criteria and dependencies.
- Session: one temporary worker instance using a supported adapter/provider.

`READY` is derived, not persisted. A task is ready when it is in `BACKLOG`, has no owner, is not blocked or terminal, and all dependencies are `DONE`.

Larger development flows end with a closure check: review the README set for
stale behavior or setup notes, then review changed items for memory relevance
before reporting the master task complete.

## Install

macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/darccau/juicyfruit/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/darccau/juicyfruit/main/scripts/install.ps1 | iex
```

The installer downloads the latest GitHub release for the current platform, validates the SHA-256 checksum, installs into `~/.juicyfruit/bin`, and updates the current shell PATH. Set `JUICYFRUIT_INSTALL_DIR` to choose another destination.

### Install as a DeepSeek Harness Plugin

The DeepSeek Harness integration operates on two complementary layers:

1. **GUI Plugin (Kanban & Conversation Tab)**: Register the Juicyfruit Cordis plugin into your DSH web profile directly pointing to the repository/package path or Git clone:
   ```sh
   # Install plugin from cloned repo or package path
   dsh plugin --profile web add /path/to/juicyfruit/packages/juicyfruit-plugin
   
   # Or directly start DSH web
   dsh --profile web
   ```
2. **Agent Skills & Adapters (`juicyfruit setup --dsh`)**: Renders canonical skills (including `juicyfruit-worker`), rules, and instructions into `.dsh/` so agents inside DSH can autonomously discover, claim, and execute tasks:
   ```sh
   # Render skills and rules into ~/.dsh (or project scope with --scope project)
   juicyfruit setup --dsh
   ```

Both methods can be used together for the full experience (UI tab in DSH Web + autonomous worker skills in DSH agents).

## Setup Targets

Install official packages into every configured target declared by those packages:

```sh
juicyfruit setup --all
```

Install only selected targets:

```sh
juicyfruit setup --codex
juicyfruit setup --cursor --gemini-cli
juicyfruit setup --devin --via
juicyfruit setup --claude-code
juicyfruit setup --antigravity
juicyfruit setup --generic
juicyfruit setup --dsh
```

Use `--scope project` to install into the current project instead of the user scope, and `--force` to overwrite locally modified managed files.

## Common Commands

```sh
juicyfruit init source ./my-source
juicyfruit source add canonical --local ./my-source
juicyfruit package list
juicyfruit package install android-core --target codex --scope user
juicyfruit package status --target codex
juicyfruit update --dry-run
juicyfruit upgrade --check
juicyfruit template new client-email --kind email --description "Use for client email. Not for internal chat." --format-target email
juicyfruit template validate client-email
```

```sh
juicyfruit memory setup --target codex
juicyfruit memory add "documented the task runtime"
juicyfruit memory add --kind note --title "Runtime decision" "tasks are canonical"
juicyfruit memory list --since 30d
juicyfruit memory search "task handoff"
juicyfruit memory digest --since 14d
juicyfruit memory capture
```

```sh
juicyfruit task create --title "Implement reconnect support"
juicyfruit task ready
juicyfruit session start --adapter codex
juicyfruit task claim JCF-... --adapter codex --session SES-...
juicyfruit task start JCF-...
juicyfruit task checkpoint JCF-... --completed "protocol" --current "tests"
juicyfruit task verify JCF-...
juicyfruit task done JCF-...
juicyfruit task resume JCF-...
juicyfruit context build JCF-...
```

Most runtime commands support `--json` for agents and tools.

## Repository Layout

```text
juicyfruit/
├── .github/             # CI, dependency updates, and repository templates
├── adapters/            # Agent-specific installation and rendering adapters
├── agents/              # Canonical sub-agents
├── cmd/juicyfruit/         # Cross-platform Go CLI entrypoint
├── docs/                # Architecture and migration documentation
├── packages/            # Installable bundles of canonical content
├── personas/            # Voice and durable preference seeds
├── profiles/            # Reproducible personal, work, and project selections
├── rules/               # Canonical always-on behavior and engineering rules
├── schemas/             # Versioned JSON contracts
└── skills/              # Canonical portable skills
```

## Architecture Notes

- [Agent Adapters](docs/architecture/agent-adapters.md)
- [Agents](docs/architecture/agents.md)
- [Context Engine](docs/architecture/context-engine.md)
- [Memory](docs/architecture/memory.md)
- [Personas](docs/architecture/personas.md)
- [Rules](docs/architecture/rules.md)
- [Skills](docs/architecture/skills.md)
- [Task Runtime](docs/architecture/task-runtime.md)
- [Templates](docs/architecture/templates.md)

## Development

The CLI is written in Go and targets the version declared in `go.mod`.

```sh
go build ./...
go vet ./...
go test ./...

# Run the CLI without installing it
go run ./cmd/juicyfruit --help

# Build a local executable
go build -o juicyfruit ./cmd/juicyfruit
```
