package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/darccau/juicyfruit/internal/fsx"
	"github.com/darccau/juicyfruit/internal/task"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

func jsonText(value any, indent bool) string {
	if indent {
		data, err := json.MarshalIndent(value, "", "  ")
		if err == nil {
			return string(data)
		}
	}
	data, _ := json.Marshal(value)
	return string(data)
}

func exactlyOne(values *Values, kind string) (string, error) {
	if len(values.positionals) != 1 {
		return "", Usagef("Expected exactly one %s.", kind)
	}
	return values.positionals[0], nil
}

func initCommand() *Command {
	group := &Command{Name: "init", Description: "Scaffold Juicyfruit repositories and artifacts."}
	group.Children = []*Command{{
		Name: "source", Description: "Create a new Juicyfruit source repository skeleton at a path.",
		Usage: "juicyfruit init source <path>",
		Parser: NewParser(
			Spec{Name: "id", Type: StringFlag, Help: "Source identifier."},
			Spec{Name: "name", Type: StringFlag, Help: "Human-readable source name."},
			Spec{Name: "force", Type: BoolFlag, Help: "Allow scaffolding into a non-empty directory."},
		),
		Run: runInitSource,
	}}
	return group
}

func runInitSource(ctx *Context, values *Values) error {
	if len(values.positionals) > 1 {
		return Usagef("Expected at most one target path.")
	}
	targetPath := "."
	if len(values.positionals) == 1 {
		targetPath = values.positionals[0]
	}
	absolute, _ := filepath.Abs(targetPath)
	target := filepath.Clean(absolute)
	id := strings.TrimSpace(values.String("id"))
	if id == "" {
		id = slugify(filepath.Base(target))
	}
	if !identifierPattern.MatchString(id) {
		return Usagef("Source id \"%s\" must match %s. Pass --id.", id, identifierPattern.String())
	}
	name := strings.TrimSpace(values.String("name"))
	if name == "" {
		name = titleCase(id)
	}
	force := values.Bool("force")
	if fsx.IsDir(target) {
		entries, _ := os.ReadDir(target)
		if len(entries) > 0 && !force {
			return Usagef("%s is not empty. Re-run with --force to scaffold anyway.", target)
		}
	}
	files := initSourceSkeleton(id, name)
	existing := []string{}
	for relative := range files {
		if fsx.Exists(filepath.Join(target, filepath.FromSlash(relative))) {
			existing = append(existing, relative)
		}
	}
	sortStrings(existing)
	if len(existing) > 0 {
		return Usagef("Refusing to overwrite existing files: %s.", strings.Join(existing, ", "))
	}
	for relative, contents := range files {
		path := filepath.Join(target, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			return err
		}
	}
	ctx.Log.Success("Scaffolded Juicyfruit source \"%s\" at %s.", id, target)
	ctx.Log.Info("Next steps:")
	ctx.Log.Info("  1. Replace skills/hello and packages/example-core with real content. skills/map-project is ready to ship as-is.")
	ctx.Log.Info("  2. Commit and push, or keep it local.")
	ctx.Log.Info("  3. Register it: juicyfruit source add %s --local %s", id, target)
	return nil
}

func sortStrings(values []string) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}

func slugify(value string) string {
	lower := strings.ToLower(value)
	var builder strings.Builder
	lastDash := false
	for _, char := range lower {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
			lastDash = false
		} else if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func titleCase(id string) string {
	parts := strings.Split(id, "-")
	for index, part := range parts {
		if part == "" {
			continue
		}
		parts[index] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func initSourceSkeleton(id, name string) map[string]string {
	return map[string]string{
		"juicyfruit-source.yaml": fmt.Sprintf(`schema_version: 1
id: %s
name: %s
description: %s — an Juicyfruit source repository.
kind: local
path: .
read_only: true
packages_path: packages
profiles_path: profiles
`, id, name, name),
		"packages/example-core/package.yaml": `schema_version: 1
id: example-core
name: Example Core
version: 0.1.0
description: Replace this with your first real package.
license: MIT
targets:
  - codex
  - claude-code
  - cursor
  - antigravity
  - devin
  - generic
  - gemini-cli
  - via
contents:
  skills:
    - skills/hello
    - skills/map-project
  rules: []
  agents: []
dependencies: []
conflicts: []
`,
		"skills/hello/SKILL.md": `---
name: hello
description: Sample skill. Replace it with a real one or delete it.
---

# Hello

Replace this file with a real skill. Each skill is one directory under ` + "`skills/`" + `
with a required ` + "`SKILL.md`" + `. See the Juicyfruit repository for the format.
`,
		"skills/map-project/SKILL.md": `---
name: map-project
description: Use on first contact with an unfamiliar repository to produce a structured docs/ map.
---

# Map Project

Map Project turns a cold repository into a small, structured set of documents.
The goal is economic: the first session pays the exploration cost once and
writes it down, so every later session reads docs/ instead of re-deriving the
architecture from source.

## Method

1. Survey the repo.
2. Carve areas by responsibility.
3. Document each area under docs/architecture/.
4. Write docs/architecture/overview.md.
5. Index everything in docs/map.md.
6. Verify the docs against the code.
7. Leave a dated memory note that docs/ is the canonical map.
`,
		"rules/.gitkeep":    "",
		"agents/.gitkeep":   "",
		"personas/.gitkeep": "",
		"profiles/.gitkeep": "",
		"AGENTS.md": fmt.Sprintf(`# %s Bootstrap

This repository uses Juicyfruit.

- On first contact, run the `+"`map-project`"+` skill to generate `+"`docs/`"+`, then read
  `+"`docs/map.md`"+` at the start of later sessions instead of re-exploring.
- Canonical work state lives in `+"`.juicyfruit/`"+`.
- Claim a task before changing implementation state.
- Load only context relevant to the task.
- Persist compact checkpoints as work progresses.
- Verify before marking work done.
- Provider directories are adapter outputs, not canonical state.
`, name),
		".gitignore": `.DS_Store
.juicyfruit/runtime/
.juicyfruit-state/
`,
		"README.md": fmt.Sprintf(`# %s

An Juicyfruit source repository. It publishes skills, rules, personas, agents, and
packages that the `+"`juicyfruit`"+` CLI installs into agent environments. Juicyfruit only
ever reads from here.
`, name),
		"CHANGELOG.md": `# Changelog

All notable changes to this source are documented in this file.

## [Unreleased]

### Added

- Initial source skeleton.
`,
	}
}

func hooksCommand() *Command {
	group := &Command{Name: "hooks", Description: "Manage Git hooks for code quality and workflows."}
	group.Children = []*Command{
		{Name: "install", Description: "Install Git hooks in the current repository.", Parser: NewParser(Spec{Name: "force", Alias: "f", Type: BoolFlag, Help: "Overwrite existing hooks if present."}), Run: runHooksInstall},
		{Name: "uninstall", Description: "Uninstall Git hooks from the current repository.", Run: runHooksUninstall},
	}
	return group
}

const preCommitHookTemplate = `#!/bin/sh
# Juicyfruit pre-commit hook
# Auto-generated by ` + "`juicyfruit hooks install`" + `

set -e

if command -v go >/dev/null 2>&1; then
  changed="$(gofmt -l .)"
  if [ -n "$changed" ]; then
    echo "gofmt needed on:" >&2
    echo "$changed" >&2
    exit 1
  fi
  go vet ./...
fi
`

func runHooksInstall(ctx *Context, values *Values) error {
	if !fsx.IsDir(".git") {
		return Usagef("Not a git repository. Cannot install hooks.")
	}
	hooksDir := filepath.Join(".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	file := filepath.Join(hooksDir, "pre-commit")
	if fsx.Exists(file) && !values.Bool("force") {
		ctx.Log.Error("Hook pre-commit already exists. Use --force to overwrite.")
		return &ExitError{Code: ExitConfig}
	}
	if err := os.WriteFile(file, []byte(preCommitHookTemplate), 0o755); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		_ = exec.Command("chmod", "+x", file).Run()
	}
	ctx.Log.Success("Installed Git hooks successfully.")
	return nil
}

func runHooksUninstall(ctx *Context, values *Values) error {
	file := filepath.Join(".git", "hooks", "pre-commit")
	if fsx.Exists(file) {
		if err := os.Remove(file); err != nil {
			return err
		}
		ctx.Log.Success("Uninstalled Git hooks successfully.")
	} else {
		ctx.Log.Info("No hooks to uninstall.")
	}
	return nil
}

func runCommand() *Command {
	group := &Command{Name: "run", Description: "Manage durable task runs."}
	group.Children = []*Command{
		{Name: "create", Description: "Create a run.", Parser: NewParser(Spec{Name: "title", Type: StringFlag, Mandatory: true}, Spec{Name: "summary", Type: StringFlag}, Spec{Name: "json", Type: BoolFlag}), Run: runCreateRun},
		{Name: "list", Description: "List runs.", Parser: NewParser(Spec{Name: "json", Type: BoolFlag}), Run: runListRuns},
		{Name: "show", Description: "Show a run.", Parser: NewParser(Spec{Name: "json", Type: BoolFlag}), Run: runShowRun},
	}
	return group
}

func runCreateRun(ctx *Context, values *Values) error {
	run, err := ctx.App.Tasks.CreateRun(values.String("title"), values.StringPtr("summary"))
	if err != nil {
		return err
	}
	if values.Bool("json") {
		ctx.Log.Info(jsonText(run, true))
	} else {
		ctx.Log.Success("Created %s: %s", run.ID, run.Title)
	}
	return nil
}

func runListRuns(ctx *Context, values *Values) error {
	runs, err := ctx.App.Tasks.ListRuns()
	if err != nil {
		return err
	}
	if values.Bool("json") {
		ctx.Log.Info(jsonText(runs, true))
	} else if len(runs) == 0 {
		ctx.Log.Info("No runs.")
	} else {
		for _, run := range runs {
			ctx.Log.Info("%s\t%s\t%d task(s)", run.ID, run.Title, len(run.Tasks))
		}
	}
	return nil
}

func runShowRun(ctx *Context, values *Values) error {
	id, err := exactlyOne(values, "run ID")
	if err != nil {
		return err
	}
	run, err := ctx.App.Tasks.ReadRun(id)
	if err != nil {
		return err
	}
	if values.Bool("json") {
		ctx.Log.Info(jsonText(run, true))
	} else {
		summary := "-"
		if run.Summary != nil {
			summary = *run.Summary
		}
		ctx.Log.Info("%s\ntitle: %s\nsummary: %s\ntasks: %s", run.ID, run.Title, summary, strings.Join(run.Tasks, ", "))
	}
	return nil
}

func sessionCommand() *Command {
	group := &Command{Name: "session", Description: "Manage ephemeral worker sessions."}
	group.Children = []*Command{
		{Name: "start", Description: "Start a worker session.", Parser: NewParser(Spec{Name: "adapter", Type: StringFlag, Mandatory: true}, Spec{Name: "agent", Type: StringFlag, Default: "executor"}, Spec{Name: "dsh-session", Type: StringFlag}, Spec{Name: "json", Type: BoolFlag}), Run: runStartSession},
		{Name: "list", Description: "List sessions.", Parser: NewParser(Spec{Name: "json", Type: BoolFlag}), Run: runListSessions},
		{Name: "show", Description: "Show a session.", Parser: NewParser(Spec{Name: "json", Type: BoolFlag}), Run: runShowSession},
		{Name: "close", Description: "Close a worker session.", Parser: NewParser(Spec{Name: "reason", Type: StringFlag, Default: "manual", Allowed: []string{"completed", "manual", "context-limit", "provider-limit", "crash-recovery", "handoff", "unknown"}}, Spec{Name: "capture-memory", Type: BoolFlag}, Spec{Name: "json", Type: BoolFlag}), Run: runCloseSession},
	}
	return group
}

func runStartSession(ctx *Context, values *Values) error {
	session, err := ctx.App.Tasks.StartSession(values.String("adapter"), values.String("agent"), values.StringPtr("dsh-session"))
	if err != nil {
		return err
	}
	if values.Bool("json") {
		ctx.Log.Info(jsonText(session, true))
	} else {
		ctx.Log.Success("Started %s.", session.ID)
	}
	return nil
}

func runListSessions(ctx *Context, values *Values) error {
	sessions, err := ctx.App.Tasks.ListSessions()
	if err != nil {
		return err
	}
	if values.Bool("json") {
		ctx.Log.Info(jsonText(sessions, true))
	} else if len(sessions) == 0 {
		ctx.Log.Info("No sessions.")
	} else {
		for _, session := range sessions {
			ctx.Log.Info("%s\t%s\t%s\t%s", session.ID, session.Status, session.Adapter, session.Agent)
		}
	}
	return nil
}

func runShowSession(ctx *Context, values *Values) error {
	id, err := exactlyOne(values, "session ID")
	if err != nil {
		return err
	}
	session, err := ctx.App.Tasks.ReadSession(id)
	if err != nil {
		return err
	}
	if values.Bool("json") {
		ctx.Log.Info(jsonText(session, true))
	} else {
		reason := "-"
		if session.CloseReason != nil {
			reason = *session.CloseReason
		}
		ctx.Log.Info("%s\nstatus: %s\nadapter: %s\nagent: %s\ntasks_claimed: %s\ntasks_worked: %s\nclose_reason: %s", session.ID, session.Status, session.Adapter, session.Agent, strings.Join(session.TasksClaimed, ", "), strings.Join(session.TasksWorked, ", "), reason)
	}
	return nil
}

func runCloseSession(ctx *Context, values *Values) error {
	id, err := exactlyOne(values, "session ID")
	if err != nil {
		return err
	}
	session, err := ctx.App.Tasks.CloseSession(id, values.String("reason"))
	if err != nil {
		return err
	}
	if values.Bool("capture-memory") || ctx.App.MemoryCaptureEnabled("project") {
		if err := ctx.App.CaptureSession(session); err != nil {
			return err
		}
	}
	if values.Bool("json") {
		ctx.Log.Info(jsonText(session, true))
	} else {
		reason := ""
		if session.CloseReason != nil {
			reason = *session.CloseReason
		}
		ctx.Log.Success("Closed %s: %s.", session.ID, reason)
	}
	return nil
}

func contextCommand() *Command {
	group := &Command{Name: "context", Description: "Build minimal task context packages."}
	group.Children = []*Command{{
		Name: "build", Description: "Build context for a task.",
		Parser: NewParser(Spec{Name: "target-tokens", Type: StringFlag, Default: "6000"}, Spec{Name: "hard-limit-tokens", Type: StringFlag, Default: "12000"}, Spec{Name: "json", Type: BoolFlag}),
		Run:    runBuildContext,
	}}
	return group
}

func runBuildContext(ctx *Context, values *Values) error {
	id, err := exactlyOne(values, "task ID")
	if err != nil {
		return err
	}
	target, err := values.Int("target-tokens")
	if err != nil {
		return err
	}
	hard, err := values.Int("hard-limit-tokens")
	if err != nil {
		return err
	}
	context, err := ctx.App.Tasks.BuildContext(id, target, hard)
	if err != nil {
		return err
	}
	if values.Bool("json") {
		ctx.Log.Info(jsonText(context, true))
	} else {
		ctx.Log.Info("task: %s\nestimated_tokens: %d\ntarget_tokens: %d\nhard_limit_tokens: %d\nfiles: %s\ntemplates: %s\nmissing: %s", context.Task, context.EstimatedTokens, context.TargetTokens, context.HardLimitTokens, strings.Join(context.Sources["files"], ", "), strings.Join(context.Sources["templates"], ", "), strings.Join(context.Missing, ", "))
	}
	return nil
}

func taskIDOrError(values *Values) (string, error) {
	if len(values.positionals) != 1 {
		return "", Usagef("Expected exactly one task ID.")
	}
	return values.positionals[0], nil
}

func emitTask(ctx *Context, values *Values, task task.Task, message string, args ...any) {
	if values.Bool("json") {
		ctx.Log.Info(jsonText(task, true))
	} else {
		ctx.Log.Success(message, args...)
	}
}
