package cli

import (
	"strings"

	"github.com/darccau/juicyfruit/internal/source"
)

func sourceCommand() *Command {
	group := &Command{Name: "source", Description: "Manage read-only Juicyfruit package sources."}
	group.Children = []*Command{
		{Name: "add", Description: "Validate and register a source snapshot.", Parser: NewParser(Spec{Name: "local", Type: StringFlag}, Spec{Name: "git", Type: StringFlag}, Spec{Name: "archive", Type: StringFlag}, Spec{Name: "revision", Type: StringFlag}, Spec{Name: "sha256", Type: StringFlag}), Run: runSourceAdd},
		{Name: "list", Description: "List registered sources.", Run: runSourceList},
		{Name: "show", Description: "Show a registered source.", Run: runSourceShow},
		{Name: "test", Description: "Revalidate a registered source.", Run: runSourceTest},
		{Name: "sync", Description: "Sync and revalidate one or all registered sources.", Run: runSourceSync},
		{Name: "search", Description: "Search for packages across registered sources.", Run: runSourceSearch},
		{Name: "remove", Description: "Remove a source registration.", Run: runSourceRemove},
	}
	return group
}

func sourceName(values *Values) (string, error) { return exactlyOne(values, "source name") }

func runSourceAdd(ctx *Context, values *Values) error {
	name, err := sourceName(values)
	if err != nil {
		return err
	}
	local := strings.TrimSpace(values.String("local"))
	git := strings.TrimSpace(values.String("git"))
	archive := strings.TrimSpace(values.String("archive"))
	revision := strings.TrimSpace(values.String("revision"))
	sha := strings.TrimSpace(values.String("sha256"))
	count := 0
	for _, item := range []string{local, git, archive} {
		if item != "" {
			count++
		}
	}
	if count != 1 {
		return Usagef("Use exactly one of --local, --git, or --archive.")
	}
	var entry source.Registered
	if local != "" {
		if revision != "" || sha != "" {
			return Usagef("--revision and --sha256 cannot be used with --local.")
		}
		entry, _, err = ctx.App.Registry.AddLocal(local, name)
	} else if git != "" {
		if revision == "" || sha != "" {
			return Usagef("--git requires --revision and cannot use --sha256.")
		}
		var snapshot source.Snapshot
		snapshot, err = ctx.App.Snapshots.FetchGit(git, revision)
		if err != nil {
			return err
		}
		entry, _, err = ctx.App.Registry.AddSnapshot(snapshot, name)
	} else {
		if sha == "" || revision != "" {
			return Usagef("--archive requires --sha256 and cannot use --revision.")
		}
		var snapshot source.Snapshot
		snapshot, err = ctx.App.Snapshots.FetchArchive(archive, sha)
		if err != nil {
			return err
		}
		entry, _, err = ctx.App.Registry.AddSnapshot(snapshot, name)
	}
	if err != nil {
		return err
	}
	ctx.Log.Success("Added source %s (%s) from %s.", entry.Name, entry.SourceID, entry.Location)
	return nil
}

func runSourceList(ctx *Context, values *Values) error {
	sources, err := ctx.App.Registry.Load()
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		ctx.Log.Info("No sources registered.")
		return nil
	}
	for _, item := range sources {
		ctx.Log.Info("%s\t%s\t%s", item.Name, item.Kind, item.Location)
	}
	return nil
}

func runSourceShow(ctx *Context, values *Values) error {
	name, err := sourceName(values)
	if err != nil {
		return err
	}
	item, err := ctx.App.Registry.Find(name)
	if err != nil {
		return err
	}
	ctx.Log.Info("name: %s\nkind: %s\nsource_id: %s\nsource_name: %s\nlocation: %s", item.Name, item.Kind, item.SourceID, item.SourceName, item.Location)
	return nil
}

func runSourceTest(ctx *Context, values *Values) error {
	name, err := sourceName(values)
	if err != nil {
		return err
	}
	catalog, err := testSource(ctx, name)
	if err != nil {
		return err
	}
	ctx.Log.Success("Source %s is valid (%d packages).", name, len(catalog.Packages))
	return nil
}

func runSourceSync(ctx *Context, values *Values) error {
	if len(values.positionals) > 1 {
		return Usagef("Expected at most one source name.")
	}
	sources := []source.Registered{}
	var err error
	if len(values.positionals) == 1 {
		item, findErr := ctx.App.Registry.Find(values.positionals[0])
		if findErr != nil {
			return findErr
		}
		sources = append(sources, item)
	} else {
		sources, err = ctx.App.Registry.Load()
		if err != nil {
			return err
		}
	}
	if len(sources) == 0 {
		ctx.Log.Info("No sources registered to sync.")
		return nil
	}
	failures := 0
	for _, item := range sources {
		catalog, testErr := testSource(ctx, item.Name)
		if testErr != nil {
			failures++
			ctx.Log.Error("Failed to sync %s: %v", item.Name, testErr)
			continue
		}
		ctx.Log.Success("Synced %s (%d packages).", item.Name, len(catalog.Packages))
	}
	if failures > 0 {
		return &ExitError{Code: ExitSoftware}
	}
	return nil
}

func testSource(ctx *Context, name string) (source.Catalog, error) {
	entry, err := ctx.App.Registry.Find(name)
	if err != nil {
		return source.Catalog{}, err
	}
	catalog, err := source.LoadCatalog(entry.Location)
	if err != nil {
		return source.Catalog{}, err
	}
	if catalog.ID != entry.SourceID {
		return source.Catalog{}, source.Errorf("Source id mismatch: expected %s, got %s", entry.SourceID, catalog.ID)
	}
	return catalog, nil
}

func runSourceSearch(ctx *Context, values *Values) error {
	if len(values.positionals) == 0 {
		return Usagef("Expected a search query.")
	}
	query := strings.ToLower(strings.Join(values.positionals, " "))
	candidates, err := ctx.App.Catalog.Discover()
	if err != nil {
		return err
	}
	found := false
	for _, candidate := range candidates {
		manifest := candidate.Manifest
		if strings.Contains(strings.ToLower(manifest.ID), query) || strings.Contains(strings.ToLower(manifest.Name), query) || strings.Contains(strings.ToLower(manifest.Description), query) {
			found = true
			ctx.Log.Info("%s\t%s\t(%s)\t%s - %s", manifest.ID, manifest.Version, candidate.SourceName, manifest.Name, manifest.Description)
		}
	}
	if !found {
		ctx.Log.Info("No packages found matching \"%s\".", query)
	}
	return nil
}

func runSourceRemove(ctx *Context, values *Values) error {
	name, err := sourceName(values)
	if err != nil {
		return err
	}
	item, err := ctx.App.Registry.Remove(name)
	if err != nil {
		return err
	}
	ctx.Log.Success("Removed source %s.", item.Name)
	return nil
}
