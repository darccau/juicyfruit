package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/darccau/juicyfruit/internal/fsx"
	"github.com/darccau/juicyfruit/internal/template"
)

const formatTargetExamples = "markdown, plain, email, pptx, marp, gamma, figma-slides, docx, html"

func templateCommand() *Command {
	group := &Command{Name: "template", Description: "Create, inspect, and resolve output templates for authored artifacts."}
	group.Children = []*Command{
		{Name: "list", Description: "List every installed and canonical template.", Parser: NewParser(Spec{Name: "json", Type: BoolFlag}), Run: runTemplateList},
		{Name: "show", Description: "Print one template contract in full.", Usage: "juicyfruit template show <name>", Parser: NewParser(Spec{Name: "json", Type: BoolFlag}), Run: runTemplateShow},
		{Name: "new", Description: "Scaffold a new template under templates/<name>/.", Usage: "juicyfruit template new <name> --kind <kind> --description <text> --format-target <target>", Parser: NewParser(Spec{Name: "kind", Type: StringFlag}, Spec{Name: "description", Type: StringFlag}, Spec{Name: "format-target", Type: StringFlag}, Spec{Name: "force", Type: BoolFlag}), Run: runTemplateNew},
		{Name: "validate", Description: "Validate one template, or every template when no name is given.", Usage: "juicyfruit template validate [<name>]", Run: runTemplateValidate},
		{Name: "match", Description: "Resolve the best template for an artifact kind or request.", Usage: "juicyfruit template match <kind-or-request>", Parser: NewParser(Spec{Name: "json", Type: BoolFlag}), Run: runTemplateMatch},
	}
	return group
}

func templateStore(ctx *Context) template.Store { return template.Store{Roots: ctx.App.Templates} }

func runTemplateList(ctx *Context, values *Values) error {
	templates, err := templateStore(ctx).List()
	if err != nil {
		return err
	}
	if values.Bool("json") {
		out := []map[string]any{}
		for _, item := range templates {
			out = append(out, map[string]any{"name": item.Name, "kind": item.Kind, "description": item.Description, "origin": string(item.Origin), "path": item.Path})
		}
		ctx.Log.Info(jsonText(out, true))
		return nil
	}
	if len(templates) == 0 {
		ctx.Log.Info("No templates found. Author one with: juicyfruit template new <name> --kind <kind> --description <text> --format-target <target>")
		return nil
	}
	for _, item := range templates {
		ctx.Log.Info("%-24s %-12s %s", item.Name, item.Kind, item.Description)
	}
	return nil
}

func runTemplateShow(ctx *Context, values *Values) error {
	name, err := exactlyOne(values, "template name")
	if err != nil {
		return err
	}
	item, err := templateStore(ctx).Find(name)
	if err != nil {
		return err
	}
	if item == nil {
		return Usagef("No template named \"%s\".", name)
	}
	if values.Bool("json") {
		ctx.Log.Info(jsonText(map[string]any{"name": item.Name, "kind": item.Kind, "description": item.Description, "origin": string(item.Origin), "path": item.Path, "frontmatter": item.Frontmatter, "body": item.Body}, true))
	} else {
		ctx.Log.Info("name: %s\nkind: %s\norigin: %s\npath: %s\ndescription: %s\n\n%s", item.Name, item.Kind, item.Origin, item.Path, item.Description, item.Body)
	}
	return nil
}

func runTemplateNew(ctx *Context, values *Values) error {
	name, err := exactlyOne(values, "template name")
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if !identifierPattern.MatchString(name) {
		return Usagef("Template name \"%s\" must match %s.", name, identifierPattern.String())
	}
	kind := strings.TrimSpace(values.String("kind"))
	if kind == "" {
		return Usagef("Pass --kind (email, slides, doc, memo, …).")
	}
	if !identifierPattern.MatchString(kind) {
		return Usagef("Template kind \"%s\" must match %s.", kind, identifierPattern.String())
	}
	description := strings.TrimSpace(values.String("description"))
	if description == "" {
		return Usagef("Pass --description to explain what this template is for.")
	}
	target := strings.TrimSpace(values.String("format-target"))
	if target == "" {
		return Usagef("Pass --format-target (%s, or a project-defined target).", formatTargetExamples)
	}
	if !identifierPattern.MatchString(target) {
		return Usagef("Template format target \"%s\" must match %s.", target, identifierPattern.String())
	}
	dir := filepath.Join(ctx.App.Templates.ProjectRoot, "templates", name)
	file := filepath.Join(dir, "TEMPLATE.md")
	force := values.Bool("force")
	if fsx.Exists(file) && !force {
		return Usagef("%s already exists. Re-run with --force to overwrite.", file)
	}
	if fsx.IsDir(dir) && !force {
		entries, _ := os.ReadDir(dir)
		if len(entries) > 0 {
			return Usagef("%s is not empty. Re-run with --force to scaffold anyway.", dir)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(file, []byte(templateSkeleton(name, kind, description, target)), 0o644); err != nil {
		return err
	}
	ctx.Log.Success("Scaffolded template \"%s\" at %s.", name, file)
	ctx.Log.Info("Next steps:")
	ctx.Log.Info("  1. Fill in voice, structure, and constraints in TEMPLATE.md.")
	ctx.Log.Info("  2. Validate it: juicyfruit template validate %s", name)
	ctx.Log.Info("  3. Ship it in a package via contents.templates, or keep it local.")
	return nil
}

func templateSkeleton(name, kind, description, target string) string {
	return fmt.Sprintf(`---
schema_version: 1
name: %s
kind: %s
description: %s
voice:
  temperature: neutral
  person: first-person-plural
structure:
  - opening: state the purpose in one sentence
  - body: one idea per paragraph
  - close: the explicit next step
length:
  max_words: 300
format:
  target: %s
constraints:
  always: []
  never: []
examples: []
---

Blank template contract for "%s".

Describe, in prose, what this artifact is, how it should come out, the tone to
hold, phrasing to prefer and avoid, and the reasoning behind the structure
above. An agent reads this before it writes.
`, name, kind, description, target, kind)
}

func runTemplateValidate(ctx *Context, values *Values) error {
	if len(values.positionals) > 1 {
		return Usagef("Expected at most one template name.")
	}
	store := templateStore(ctx)
	if len(values.positionals) == 1 {
		name := values.positionals[0]
		item, err := store.Find(name)
		if err != nil {
			return err
		}
		if item == nil {
			ctx.Log.Error("No template named \"%s\".", name)
			return &ExitError{Code: ExitConfig}
		}
		ctx.Log.Success("%s: valid (%s)", item.Name, item.Path)
		return nil
	}
	templates, err := store.List()
	if err != nil {
		return err
	}
	if len(templates) == 0 {
		ctx.Log.Info("No templates to validate.")
		return nil
	}
	for _, item := range templates {
		ctx.Log.Success("%s: valid (%s)", item.Name, item.Path)
	}
	return nil
}

func runTemplateMatch(ctx *Context, values *Values) error {
	if len(values.positionals) == 0 {
		return Usagef("Expected an artifact kind or request.")
	}
	query := strings.Join(values.positionals, " ")
	match, err := templateStore(ctx).Match(query, false)
	if err != nil {
		return err
	}
	asJSON := values.Bool("json")
	if match == nil {
		if asJSON {
			ctx.Log.Info(jsonText(map[string]any{"match": nil, "query": query}, true))
		} else {
			ctx.Log.Info("No template matches \"%s\".", query)
		}
		return nil
	}
	item := match.Template
	if asJSON {
		ctx.Log.Info(jsonText(map[string]any{"query": query, "reason": match.Reason, "name": item.Name, "kind": item.Kind, "description": item.Description, "path": item.Path, "format_target": item.FormatTarget(), "theme": item.ThemePath()}, true))
	} else {
		ctx.Log.Info("%s (%s)\npath: %s\n%s", item.Name, match.Reason, item.Path, item.Description)
	}
	return nil
}
