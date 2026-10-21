package cli

import (
	"fmt"
	"strings"

	"github.com/darccau/juicyfruit/internal/pkg"
)

func packageCommand() *Command {
	group := &Command{Name: "package", Description: "Discover and install versioned Juicyfruit packages."}
	targetSpecs := []Spec{
		{Name: "target", Type: StringFlag, Mandatory: true, Allowed: pkg.SupportedTargets(), Help: "Agent environment to manage."},
		{Name: "scope", Type: StringFlag, Default: "user", Allowed: []string{"user", "project"}, Help: "Install for the user or current project."},
	}
	installSpecs := append(append([]Spec{}, targetSpecs...), Spec{Name: "force", Type: BoolFlag, Help: "Overwrite locally modified managed files without asking."})
	stateSpecs := append([]Spec{}, targetSpecs...)
	group.Children = []*Command{
		{Name: "list", Description: "List packages from registered sources.", Run: runPackageList},
		{Name: "search", Description: "Search package IDs, names, and descriptions.", Run: runPackageSearch},
		{Name: "show", Description: "Show package metadata from every matching source.", Run: runPackageShow},
		{Name: "install", Description: "Resolve and transactionally install packages.", Parser: NewParser(installSpecs...), Run: runPackageInstall},
		{Name: "status", Description: "Show the state of Juicyfruit-managed files.", Parser: NewParser(stateSpecs...), Run: packageState(false)},
		{Name: "diff", Description: "Show missing or locally modified managed files.", Parser: NewParser(stateSpecs...), Run: packageState(true)},
		{Name: "uninstall", Description: "Remove unchanged Juicyfruit-managed package files.", Parser: NewParser(stateSpecs...), Run: runPackageUninstall},
	}
	return group
}

func runPackageList(ctx *Context, values *Values) error {
	candidates, err := ctx.App.Catalog.Discover()
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		ctx.Log.Info("No packages available.")
		return nil
	}
	for _, candidate := range candidates {
		ctx.Log.Info("%s\t%s\t%s", candidate.Manifest.ID, candidate.Manifest.Version, candidate.SourceName)
	}
	return nil
}

func runPackageSearch(ctx *Context, values *Values) error {
	if len(values.positionals) != 1 {
		return Usagef("Expected one search query.")
	}
	query := strings.ToLower(values.positionals[0])
	candidates, err := ctx.App.Catalog.Discover()
	if err != nil {
		return err
	}
	found := false
	for _, candidate := range candidates {
		manifest := candidate.Manifest
		if strings.Contains(strings.ToLower(manifest.ID), query) || strings.Contains(strings.ToLower(manifest.Name), query) || strings.Contains(strings.ToLower(manifest.Description), query) {
			found = true
			ctx.Log.Info("%s\t%s\t%s", manifest.ID, manifest.Version, candidate.SourceName)
		}
	}
	if !found {
		ctx.Log.Info("No packages matched \"%s\".", query)
	}
	return nil
}

func runPackageShow(ctx *Context, values *Values) error {
	if len(values.positionals) != 1 {
		return Usagef("Expected one package ID.")
	}
	id := values.positionals[0]
	candidates, err := ctx.App.Catalog.Discover()
	if err != nil {
		return err
	}
	found := false
	for _, candidate := range candidates {
		if candidate.Manifest.ID != id {
			continue
		}
		found = true
		manifest := candidate.Manifest
		ctx.Log.Info("id: %s\nname: %s\nversion: %s\nsource: %s\ndigest: %s\ntargets: %s\ndescription: %s", manifest.ID, manifest.Name, manifest.Version, candidate.SourceName, candidate.Digest, strings.Join(manifest.Targets, ", "), manifest.Description)
	}
	if !found {
		return pkg.Errorf("Package is not available: %s", id)
	}
	return nil
}

func packageTarget(values *Values) (string, error) {
	target := strings.TrimSpace(values.String("target"))
	if target == "" {
		return "", Usagef("Missing required option --target.")
	}
	return target, nil
}

func packageScope(values *Values) pkg.Scope {
	if values.String("scope") == "project" {
		return pkg.ScopeProject
	}
	return pkg.ScopeUser
}

func conflictResolver(ctx *Context, force bool) pkg.ConflictResolver {
	return func(path string) pkg.ConflictDecision {
		if force {
			return pkg.ConflictOverwrite
		}
		if ctx.Log.Confirm(fmt.Sprintf("Overwrite locally modified %s?", path), false) {
			return pkg.ConflictOverwrite
		}
		return pkg.ConflictSkip
	}
}

func reportSkipped(ctx *Context, skipped []string) {
	if len(skipped) == 0 {
		return
	}
	ctx.Log.Warn("Kept %d locally modified file(s); re-run with --force to overwrite:", len(skipped))
	for _, path := range skipped {
		ctx.Log.Warn("  %s", path)
	}
}

func runPackageInstall(ctx *Context, values *Values) error {
	if len(values.positionals) == 0 {
		return Usagef("Expected at least one package ID.")
	}
	target, err := packageTarget(values)
	if err != nil {
		return err
	}
	resolution, err := ctx.App.Resolver.Resolve(values.positionals, target, "")
	if err != nil {
		return err
	}
	result, err := ctx.App.Installer.Install(resolution, ctx.App.Roots, packageScope(values), conflictResolver(ctx, values.Bool("force")))
	if err != nil {
		return err
	}
	ctx.Log.Success("Installed %d package(s) for %s (%s).", len(result.Lockfile.Packages), target, packageScope(values))
	reportSkipped(ctx, result.SkippedFiles)
	return nil
}

func packageState(changedOnly bool) func(*Context, *Values) error {
	return func(ctx *Context, values *Values) error {
		target, err := packageTarget(values)
		if err != nil {
			return err
		}
		statuses, err := ctx.App.Installer.Status(target, ctx.App.Roots, packageScope(values))
		if err != nil {
			return err
		}
		visible := []pkg.FileStatus{}
		for _, item := range statuses {
			if changedOnly && item.Condition == pkg.ConditionUnchanged {
				continue
			}
			visible = append(visible, item)
		}
		if len(visible) == 0 {
			if changedOnly {
				ctx.Log.Info("No managed file changes.")
			} else {
				ctx.Log.Info("No managed files.")
			}
			return nil
		}
		for _, item := range visible {
			ctx.Log.Info("%s\t%s", item.Condition, item.File.Path)
		}
		return nil
	}
}

func runPackageUninstall(ctx *Context, values *Values) error {
	target, err := packageTarget(values)
	if err != nil {
		return err
	}
	ids := values.positionals
	var selected []string
	if len(ids) > 0 {
		selected = ids
	}
	statuses, err := ctx.App.Installer.Uninstall(target, ctx.App.Roots, packageScope(values), selected)
	if err != nil {
		return err
	}
	modified := 0
	for _, item := range statuses {
		if item.Condition == pkg.ConditionModified {
			modified++
		}
	}
	ctx.Log.Success("Uninstall completed for %s; %d modified file(s) preserved.", target, modified)
	return nil
}
