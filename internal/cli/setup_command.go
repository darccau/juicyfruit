package cli

import (
	"os/exec"
	"sort"
	"strings"

	"github.com/darccau/juicyfruit/internal/pkg"
	"github.com/darccau/juicyfruit/internal/source"
	"github.com/darccau/juicyfruit/internal/version"
)

const officialRepository = "https://github.com/darccau/juicyfruit.git"

func setupCommand() *Command {
	return &Command{
		Name: "setup", Description: "Install official Juicyfruit packages for one or more agents.",
		Parser: NewParser(
			Spec{Name: "all", Type: BoolFlag, Help: "Install every target declared by the official packages."},
			Spec{Name: "codex", Type: BoolFlag}, Spec{Name: "claude", Alias: "claude-code", Type: BoolFlag},
			Spec{Name: "cursor", Type: BoolFlag}, Spec{Name: "antigravity", Type: BoolFlag},
			Spec{Name: "devin", Type: BoolFlag}, Spec{Name: "gemini", Alias: "gemini-cli", Type: BoolFlag},
			Spec{Name: "generic", Type: BoolFlag}, Spec{Name: "via", Type: BoolFlag}, Spec{Name: "dsh", Type: BoolFlag},
			Spec{Name: "profile", Type: StringFlag, Default: "web"},
			Spec{Name: "scope", Type: StringFlag, Default: "user", Allowed: []string{"user", "project"}},
			Spec{Name: "force", Type: BoolFlag},
		),
		Run: runSetup,
	}
}

func runSetup(ctx *Context, values *Values) error {
	if len(values.positionals) > 0 {
		return Usagef("Setup does not accept positional arguments.")
	}
	official, err := ensureOfficialSource(ctx)
	if err != nil {
		return err
	}
	candidates, err := ctx.App.Catalog.Discover()
	if err != nil {
		return err
	}
	officialCandidates := []pkg.Candidate{}
	for _, candidate := range candidates {
		if candidate.SourceName == official.Name {
			officialCandidates = append(officialCandidates, candidate)
		}
	}
	if len(officialCandidates) == 0 {
		return pkg.Errorf("The official Juicyfruit source does not contain any packages.")
	}
	scope := pkg.ScopeUser
	if values.String("scope") == "project" {
		scope = pkg.ScopeProject
	}
	targets, err := setupTargets(values, officialCandidates, scope, ctx)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		ctx.Log.Info("No configured official package targets found for %s scope.", scope)
		return nil
	}
	if values.Parsed("profile") && !containsString(targets, "dsh") {
		return Usagef("--profile can only be used with --dsh.")
	}
	resolver := conflictResolver(ctx, values.Bool("force"))
	for _, target := range targets {
		seen := map[string]bool{}
		ids := []string{}
		for _, candidate := range officialCandidates {
			if candidate.Manifest.Supports(target) && !seen[candidate.Manifest.ID] {
				seen[candidate.Manifest.ID] = true
				ids = append(ids, candidate.Manifest.ID)
			}
		}
		sort.Strings(ids)
		if len(ids) == 0 {
			ctx.Log.Info("No official packages support %s; skipping.", target)
			continue
		}
		resolution, err := ctx.App.Resolver.Resolve(ids, target, official.Name)
		if err != nil {
			return err
		}
		result, err := ctx.App.Installer.Install(resolution, ctx.App.Roots, scope, resolver)
		if err != nil {
			return err
		}
		ctx.Log.Success("Installed %d package(s) for %s (%s).", len(result.Lockfile.Packages), target, scope)
		reportSkipped(ctx, result.SkippedFiles)
		if target == "dsh" {
			for _, candidate := range officialCandidates {
				if candidate.Manifest.ID != "juicyfruit-plugin" {
					continue
				}
				profile := values.String("profile")
				ctx.Log.Info("Installing Juicyfruit DSH plugin into profile \"%s\"...", profile)
				command := exec.Command("dsh", "plugin", "--profile", profile, "add", candidate.PackageRoot)
				output, err := command.CombinedOutput()
				if err != nil {
					ctx.Log.Error("Failed to install DSH plugin: %s", strings.TrimSpace(string(output)))
				} else {
					ctx.Log.Success("Installed Juicyfruit DSH plugin into DSH profile \"%s\".", profile)
				}
				break
			}
		}
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func setupTargets(values *Values, candidates []pkg.Candidate, scope pkg.Scope, ctx *Context) ([]string, error) {
	flags := []struct{ flag, target string }{
		{"codex", "codex"}, {"claude", "claude-code"}, {"cursor", "cursor"}, {"antigravity", "antigravity"},
		{"devin", "devin"}, {"generic", "generic"}, {"gemini", "gemini-cli"}, {"via", "via"}, {"dsh", "dsh"},
	}
	selected := []string{}
	for _, item := range flags {
		if values.Bool(item.flag) {
			selected = append(selected, item.target)
		}
	}
	all := values.Bool("all")
	if all && len(selected) > 0 {
		return nil, Usagef("--all cannot be combined with individual agent flags.")
	}
	if !all && len(selected) == 0 {
		return nil, Usagef("Select --all or at least one agent flag.")
	}
	if !all {
		return selected, nil
	}
	declared := map[string]bool{}
	for _, candidate := range candidates {
		for _, target := range candidate.Manifest.Targets {
			declared[target] = true
		}
	}
	configured := map[string]bool{}
	for _, target := range pkg.ConfiguredIDs(ctx.App.Roots, scope) {
		configured[target] = true
	}
	out := []string{}
	for _, target := range pkg.SupportedTargets() {
		if declared[target] && configured[target] {
			out = append(out, target)
		}
	}
	return out, nil
}

func ensureOfficialSource(ctx *Context) (source.Registered, error) {
	sources, err := ctx.App.Registry.Load()
	if err != nil {
		return source.Registered{}, err
	}
	officialRevision := "v" + version.Version
	var official []source.Registered
	for _, item := range sources {
		if item.SourceID == "juicyfruit" {
			official = append(official, item)
		}
	}
	if len(official) > 0 {
		item := official[0]
		if item.Kind == source.KindGit && item.Transport != nil && item.Transport.URL == officialRepository && item.Transport.Revision != officialRevision {
			ctx.Log.Info("Refreshing official Juicyfruit source...")
			snapshot, err := ctx.App.Snapshots.FetchGit(officialRepository, officialRevision)
			if err != nil {
				return source.Registered{}, err
			}
			updated, _, err := ctx.App.Registry.ReplaceSnapshot(item.Name, snapshot)
			return updated, err
		}
		return item, nil
	}
	for _, item := range sources {
		if item.Name == "juicyfruit" {
			return source.Registered{}, source.Errorf("Source name \"juicyfruit\" is already used by a different source.")
		}
	}
	ctx.Log.Info("Adding official Juicyfruit source...")
	snapshot, err := ctx.App.Snapshots.FetchGit(officialRepository, officialRevision)
	if err != nil {
		return source.Registered{}, err
	}
	entry, _, err := ctx.App.Registry.AddSnapshot(snapshot, "juicyfruit")
	return entry, err
}
