package cli

import (
	"fmt"

	"github.com/darccau/juicyfruit/internal/pkg"
	"github.com/darccau/juicyfruit/internal/source"
	"github.com/darccau/juicyfruit/internal/upgrade"
	"github.com/darccau/juicyfruit/internal/version"
)

func updateCommand() *Command {
	return &Command{
		Name: "update", Description: "Update installed Juicyfruit skills and packages from their sources.",
		Parser: NewParser(
			Spec{Name: "target", Type: MultiFlag, Allowed: pkg.SupportedTargets()},
			Spec{Name: "scope", Type: MultiFlag, Allowed: []string{"user", "project"}},
			Spec{Name: "package", Type: MultiFlag},
			Spec{Name: "dry-run", Type: BoolFlag},
			Spec{Name: "refresh-sources", Type: BoolFlag, Default: "true", Negatable: true},
		),
		Run: runUpdate,
	}
}

func runUpdate(ctx *Context, values *Values) error {
	scopes := []pkg.Scope{}
	for _, value := range values.List("scope") {
		if value == "project" {
			scopes = append(scopes, pkg.ScopeProject)
		} else {
			scopes = append(scopes, pkg.ScopeUser)
		}
	}
	updater := &pkg.Updater{Registry: ctx.App.Registry, Catalog: ctx.App.Catalog, Resolver: ctx.App.Resolver, Roots: ctx.App.Roots}
	report, err := updater.Run(values.List("target"), scopeStrings(scopes), values.List("package"), values.Bool("dry-run"), values.Bool("refresh-sources"))
	if err != nil {
		return err
	}
	dryRun := values.Bool("dry-run")
	if len(report.Sources) == 0 && len(report.Packages) == 0 {
		ctx.Log.Info("No installed Juicyfruit packages found.")
		return nil
	}
	for _, item := range report.Sources {
		ctx.Log.Info(sourceUpdateLine(item))
	}
	updated := 0
	for _, item := range report.Packages {
		ctx.Log.Info(packageUpdateLine(item, dryRun))
		if item.Status == pkg.UpdateUpdated {
			updated++
		}
	}
	if dryRun {
		if updated > 0 {
			ctx.Log.Info("Dry run: %d package(s) would be updated. No changes written.", updated)
		} else {
			ctx.Log.Info("Dry run: everything is already up to date.")
		}
		return nil
	}
	if updated > 0 {
		refreshed := 0
		for _, item := range report.Sources {
			if item.Kind == source.RefreshUpdated {
				refreshed++
			}
		}
		ctx.Log.Success("Updated %d package(s); %d source(s) refreshed.", updated, refreshed)
	} else {
		ctx.Log.Info("Everything is already up to date.")
	}
	return nil
}

func scopeStrings(scopes []pkg.Scope) []string {
	out := []string{}
	for _, scope := range scopes {
		out = append(out, string(scope))
	}
	return out
}

func sourceUpdateLine(item pkg.SourceUpdate) string {
	switch item.Kind {
	case source.RefreshUpdated:
		return fmt.Sprintf("refreshed %s: %s..%s", item.Name, shortRevision(item.PreviousRevision), shortRevision(item.NewRevision))
	case source.RefreshLive:
		return fmt.Sprintf("%s: local source (live)", item.Name)
	case source.RefreshPinned:
		return fmt.Sprintf("%s: pinned (archive checksum)", item.Name)
	default:
		detail := item.Detail
		if detail == "" {
			detail = "source unchanged"
		}
		return fmt.Sprintf("%s: %s", item.Name, detail)
	}
}

func packageUpdateLine(item pkg.PackageUpdate, dryRun bool) string {
	prefix := fmt.Sprintf("%s/%s %s: ", item.Target, item.Scope, item.PackageID)
	switch item.Status {
	case pkg.UpdateUpToDate:
		return prefix + "up to date"
	case pkg.UpdateUpdated:
		verb := "updated"
		if dryRun {
			verb = "would update"
		}
		if item.FromVersion == item.ToVersion {
			refreshed := "refreshed"
			if dryRun {
				refreshed = "would refresh"
			}
			return prefix + refreshed + " content (" + item.ToVersion + ")"
		}
		return fmt.Sprintf("%s%s %s -> %s", prefix, verb, item.FromVersion, item.ToVersion)
	case pkg.UpdateUnavailable:
		return prefix + "unavailable (" + item.Detail + ")"
	default:
		return prefix + "skipped (" + item.Detail + ")"
	}
}

func shortRevision(value string) string {
	if value == "" {
		return "(unknown)"
	}
	if len(value) <= 7 {
		return value
	}
	return value[:7]
}

func upgradeCommand() *Command {
	return &Command{
		Name: "upgrade", Description: "Update the Juicyfruit CLI to the latest release.",
		Parser: NewParser(Spec{Name: "check", Type: BoolFlag}, Spec{Name: "force", Type: BoolFlag}),
		Run:    runUpgrade,
	}
}

func runUpgrade(ctx *Context, values *Values) error {
	outcome, err := upgrade.Run(upgrade.NewClient(), version.Version, values.Bool("check"), values.Bool("force"))
	if err != nil {
		return err
	}
	if outcome.Applied {
		ctx.Log.Success("Upgraded Juicyfruit %s -> %s.", outcome.PreviousVersion, outcome.LatestVersion)
		if outcome.Note != "" {
			ctx.Log.Warn(outcome.Note)
		}
		return nil
	}
	if values.Bool("check") && outcome.UpdateAvailable {
		ctx.Log.Info("Update available: %s -> %s. Run: juicyfruit upgrade", outcome.PreviousVersion, outcome.LatestVersion)
		return nil
	}
	ctx.Log.Info("Juicyfruit is already up to date (%s).", outcome.PreviousVersion)
	return nil
}
