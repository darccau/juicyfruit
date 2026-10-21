package pkg

import (
	"sort"
	"strings"
)

type Resolver struct{ Catalog *Catalog }

func NewResolver(catalog *Catalog) *Resolver { return &Resolver{Catalog: catalog} }

func (r *Resolver) Resolve(packageIDs []string, target, sourceName string) (Resolution, error) {
	candidates, err := r.Catalog.Discover()
	if err != nil {
		return Resolution{}, err
	}
	byIdentity := map[string][]Candidate{}
	for _, candidate := range candidates {
		if sourceName != "" && candidate.SourceName != sourceName {
			continue
		}
		key := candidate.Identity()
		byIdentity[key] = append(byIdentity[key], candidate)
	}
	for key := range byIdentity {
		sort.SliceStable(byIdentity[key], func(i, j int) bool {
			return byIdentity[key][i].SourceName < byIdentity[key][j].SourceName
		})
	}
	selected := map[string]Candidate{}
	resolving := []string{}
	resolved := []Candidate{}
	var visit func(id, version string) error
	visit = func(id, version string) error {
		var matching []Candidate
		for key, values := range byIdentity {
			if strings.HasPrefix(key, id+"@") && (version == "" || key == id+"@"+version) {
				matching = append(matching, values...)
			}
		}
		sort.SliceStable(matching, func(i, j int) bool {
			if matching[i].Manifest.Version != matching[j].Manifest.Version {
				return matching[i].Manifest.Version < matching[j].Manifest.Version
			}
			return matching[i].SourceName < matching[j].SourceName
		})
		if len(matching) == 0 {
			requested := id
			if version != "" {
				requested = id + "@" + version
			}
			return Errorf("Package is not available: %s", requested)
		}
		candidate := matching[len(matching)-1]
		if !candidate.Manifest.Supports(target) {
			return Errorf("Package %s does not support target %s.", candidate.Manifest.ID, target)
		}
		if existing, ok := selected[id]; ok {
			if existing.Manifest.Version != candidate.Manifest.Version {
				return Errorf("Version conflict for %s: %s and %s.", id, existing.Manifest.Version, candidate.Manifest.Version)
			}
			return nil
		}
		for index, value := range resolving {
			if value == id {
				cycle := append(append([]string{}, resolving[index:]...), id)
				return Errorf("Dependency cycle: %s", strings.Join(cycle, " -> "))
			}
		}
		resolving = append(resolving, id)
		deps := append([]Dependency{}, candidate.Manifest.Dependencies...)
		sort.SliceStable(deps, func(i, j int) bool {
			if deps[i].ID != deps[j].ID {
				return deps[i].ID < deps[j].ID
			}
			return deps[i].Version < deps[j].Version
		})
		for _, dep := range deps {
			if err := visit(dep.ID, dep.Version); err != nil {
				return err
			}
		}
		for _, existing := range selected {
			if contains(candidate.Manifest.Conflicts, existing.Manifest.ID) || contains(existing.Manifest.Conflicts, candidate.Manifest.ID) {
				return Errorf("Package conflict: %s and %s.", candidate.Manifest.ID, existing.Manifest.ID)
			}
		}
		selected[id] = candidate
		resolving = resolving[:len(resolving)-1]
		resolved = append(resolved, candidate)
		return nil
	}
	requested := uniqueStrings(packageIDs)
	sort.Strings(requested)
	if len(requested) == 0 {
		return Resolution{}, Errorf("At least one package must be requested.")
	}
	for _, id := range requested {
		if id == "" {
			return Resolution{}, Errorf("Package id cannot be empty.")
		}
		if err := visit(id, ""); err != nil {
			return Resolution{}, err
		}
	}
	return Resolution{Target: target, Packages: resolved}, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
