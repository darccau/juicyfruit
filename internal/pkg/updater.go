package pkg

import (
	"sort"
	"strings"

	"github.com/darccau/juicyfruit/internal/fsx"
	"github.com/darccau/juicyfruit/internal/source"
)

type UpdateStatus string

const (
	UpdateUpToDate        UpdateStatus = "up_to_date"
	UpdateUpdated         UpdateStatus = "updated"
	UpdateUnavailable     UpdateStatus = "unavailable"
	UpdateSkippedModified UpdateStatus = "skipped_modified"
)

type SourceUpdate struct {
	Name             string
	Kind             source.RefreshKind
	PreviousRevision string
	NewRevision      string
	Detail           string
}

type PackageUpdate struct {
	Target      string
	Scope       Scope
	PackageID   string
	Status      UpdateStatus
	FromVersion string
	ToVersion   string
	Detail      string
}

type UpdateReport struct {
	Sources  []SourceUpdate
	Packages []PackageUpdate
}

type Updater struct {
	Registry *source.Registry
	Catalog  *Catalog
	Resolver *Resolver
	Roots    Roots
}

type installation struct {
	Adapter  TargetAdapter
	Scope    Scope
	Lockfile Lockfile
}

func (u *Updater) Run(targets, scopes, packageIDs []string, dryRun, refreshSources bool) (UpdateReport, error) {
	scopeSet := []Scope{ScopeUser, ScopeProject}
	if len(scopes) > 0 {
		scopeSet = nil
		for _, value := range scopes {
			scope, err := NormalizeScope(value)
			if err != nil {
				return UpdateReport{}, err
			}
			scopeSet = append(scopeSet, scope)
		}
	}
	targetSet := toSet(targets)
	installations := []installation{}
	for _, adapter := range AllAdapters() {
		if len(targetSet) > 0 && !targetSet[adapter.ID] {
			continue
		}
		for _, scope := range scopeSet {
			file := adapter.Lockfile(u.Roots, scope)
			if !fsx.Exists(file) {
				continue
			}
			lockfile, err := (LockStore{File: file}).Read()
			if err != nil {
				return UpdateReport{}, err
			}
			if len(lockfile.Packages) == 0 {
				continue
			}
			installations = append(installations, installation{Adapter: adapter, Scope: scope, Lockfile: lockfile})
		}
	}
	referenced := []string{}
	seen := map[string]bool{}
	for _, inst := range installations {
		for _, pkg := range inst.Lockfile.Packages {
			if !seen[pkg.Source] {
				seen[pkg.Source] = true
				referenced = append(referenced, pkg.Source)
			}
		}
	}
	sort.Strings(referenced)
	sourceUpdates := []SourceUpdate{}
	for _, name := range referenced {
		row, err := u.refreshSource(name, refreshSources, dryRun)
		if err != nil {
			return UpdateReport{}, err
		}
		sourceUpdates = append(sourceUpdates, row)
	}
	packageUpdates := []PackageUpdate{}
	if len(installations) > 0 {
		candidates, err := u.Catalog.Discover()
		if err != nil {
			return UpdateReport{}, err
		}
		available := map[string]bool{}
		for _, candidate := range candidates {
			available[candidate.Manifest.ID] = true
		}
		for _, inst := range installations {
			rows, err := u.updateInstallation(inst, available, packageIDs, dryRun)
			if err != nil {
				return UpdateReport{}, err
			}
			packageUpdates = append(packageUpdates, rows...)
		}
	}
	return UpdateReport{Sources: sourceUpdates, Packages: packageUpdates}, nil
}

func (u *Updater) refreshSource(name string, refreshSources, dryRun bool) (SourceUpdate, error) {
	if !refreshSources {
		return SourceUpdate{Name: name, Kind: source.RefreshUnchanged, Detail: "source refresh skipped (--no-refresh-sources)"}, nil
	}
	if dryRun {
		return SourceUpdate{Name: name, Kind: source.RefreshUnchanged, Detail: "run without --dry-run to pull newer revisions"}, nil
	}
	refresh, err := u.Registry.Refresh(name)
	if err != nil {
		return SourceUpdate{}, err
	}
	return SourceUpdate{Name: name, Kind: refresh.Kind, PreviousRevision: refresh.PreviousRevision, NewRevision: refresh.NewRevision}, nil
}

func (u *Updater) updateInstallation(inst installation, available map[string]bool, packageIDs []string, dryRun bool) ([]PackageUpdate, error) {
	locked := map[string]LockedPackage{}
	for _, pkg := range inst.Lockfile.Packages {
		locked[pkg.ID] = pkg
	}
	requested := []string{}
	requestedSet := toSet(packageIDs)
	for id := range locked {
		if len(requestedSet) == 0 || requestedSet[id] {
			requested = append(requested, id)
		}
	}
	sort.Strings(requested)
	if len(requested) == 0 {
		return []PackageUpdate{}, nil
	}
	rows := []PackageUpdate{}
	resolvable := []string{}
	for _, id := range requested {
		if available[id] {
			resolvable = append(resolvable, id)
		} else {
			rows = append(rows, PackageUpdate{Target: inst.Adapter.ID, Scope: inst.Scope, PackageID: id, Status: UpdateUnavailable, FromVersion: locked[id].Version, Detail: "removed from source " + locked[id].Source})
		}
	}
	if len(resolvable) == 0 {
		return rows, nil
	}
	resolution, err := u.Resolver.Resolve(resolvable, inst.Adapter.ID, "")
	if err != nil {
		for _, id := range resolvable {
			rows = append(rows, PackageUpdate{Target: inst.Adapter.ID, Scope: inst.Scope, PackageID: id, Status: UpdateUnavailable, FromVersion: locked[id].Version, Detail: err.Error()})
		}
		return rows, nil
	}
	resolvedLock := map[string]LockedPackage{}
	for _, pkg := range NewLockfile(resolution).Packages {
		resolvedLock[pkg.ID] = pkg
	}
	statuses, err := (Installer{}).Status(inst.Adapter.ID, u.Roots, inst.Scope)
	if err != nil {
		return nil, err
	}
	blocked := map[string]bool{}
	for _, status := range statuses {
		if status.Condition == ConditionModified {
			blocked[status.File.PackageID] = true
		}
	}
	changedIDs := []string{}
	for _, id := range resolvable {
		before := locked[id]
		after, ok := resolvedLock[id]
		if !ok {
			continue
		}
		changed := after.Version != before.Version || after.Digest != before.Digest
		if !changed {
			rows = append(rows, PackageUpdate{Target: inst.Adapter.ID, Scope: inst.Scope, PackageID: id, Status: UpdateUpToDate, FromVersion: before.Version, ToVersion: before.Version})
		} else if blocked[id] {
			rows = append(rows, PackageUpdate{Target: inst.Adapter.ID, Scope: inst.Scope, PackageID: id, Status: UpdateSkippedModified, FromVersion: before.Version, ToVersion: after.Version, Detail: "locally modified managed file"})
		} else {
			changedIDs = append(changedIDs, id)
			rows = append(rows, PackageUpdate{Target: inst.Adapter.ID, Scope: inst.Scope, PackageID: id, Status: UpdateUpdated, FromVersion: before.Version, ToVersion: after.Version})
		}
	}
	if dryRun || len(changedIDs) == 0 {
		return rows, nil
	}
	applied, err := u.Resolver.Resolve(changedIDs, inst.Adapter.ID, "")
	if err != nil {
		return nil, err
	}
	_, err = (Installer{}).Install(applied, u.Roots, inst.Scope, nil)
	if err != nil {
		if !strings.Contains(err.Error(), "modified managed file") {
			return nil, err
		}
		for index, row := range rows {
			if row.Status != UpdateUpdated {
				continue
			}
			rows[index] = PackageUpdate{Target: row.Target, Scope: row.Scope, PackageID: row.PackageID, Status: UpdateSkippedModified, FromVersion: row.FromVersion, ToVersion: row.ToVersion, Detail: err.Error()}
		}
	}
	return rows, nil
}
