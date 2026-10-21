package pkg

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darccau/juicyfruit/internal/fsx"
	"github.com/darccau/juicyfruit/internal/source"
)

type ConflictDecision string

const (
	ConflictOverwrite ConflictDecision = "overwrite"
	ConflictSkip      ConflictDecision = "skip"
)

type ConflictResolver func(path string) ConflictDecision

type Installer struct{}

type plannedFile struct {
	Path      string
	Bytes     []byte
	Digest    string
	PackageID string
	Mode      FileMode
}

func (Installer) Install(resolution Resolution, roots Roots, scope Scope, onModified ConflictResolver) (InstallationResult, error) {
	adapter, err := AdapterForID(resolution.Target)
	if err != nil {
		return InstallationResult{}, err
	}
	safeRoots, err := safeRoots(roots, scope, true)
	if err != nil {
		return InstallationResult{}, err
	}
	scopeRoot := rootsForScope(safeRoots, scope)
	targetRoot := adapter.TargetRoot(safeRoots, scope)
	stateFile := adapter.StateFile(safeRoots, scope)
	lockFile := adapter.Lockfile(safeRoots, scope)
	for _, candidate := range []string{targetRoot, stateFile, lockFile} {
		if err := ensureInside(scopeRoot, candidate); err != nil {
			return InstallationResult{}, err
		}
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return InstallationResult{}, Errorf("Cannot access installation root: %v", err)
	}
	if err := ensureInside(scopeRoot, targetRoot); err != nil {
		return InstallationResult{}, err
	}
	stateStore := StateStore{File: stateFile}
	lockStore := LockStore{File: lockFile}
	current, err := stateStore.Read(adapter.ID)
	if err != nil {
		return InstallationResult{}, err
	}
	plan, err := buildPlan(resolution)
	if err != nil {
		return InstallationResult{}, err
	}
	replacing := map[string]bool{}
	for _, candidate := range resolution.Packages {
		replacing[candidate.Manifest.ID] = true
	}
	removals, skipped, preserved, err := validatePlan(plan, current, targetRoot, replacing, onModified)
	if err != nil {
		return InstallationResult{}, err
	}
	untouched := map[string]bool{}
	for _, path := range skipped {
		untouched[path] = true
	}
	for _, path := range preserved {
		untouched[path] = true
	}
	effective := make([]plannedFile, 0, len(plan))
	for _, file := range plan {
		if !untouched[file.Path] {
			effective = append(effective, file)
		}
	}
	stage := filepath.Join(targetRoot, ".juicyfruit-stage-"+fsx.Nonce())
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return InstallationResult{}, err
	}
	defer func() {
		if fsx.Exists(stage) {
			_ = os.RemoveAll(stage)
		}
	}()
	if err := stageFiles(effective, stage); err != nil {
		return InstallationResult{}, err
	}
	nextState := nextState(current, effective, adapter.ID, replacing, toSet(skipped))
	lockfile := NewLockfile(resolution)
	lockBytes, lockExisted := readOptional(lockFile)
	if err := lockStore.Write(lockfile); err != nil {
		return InstallationResult{}, err
	}
	if err := commit(effective, removals, stage, targetRoot, stateStore, nextState); err != nil {
		_ = restoreFile(lockFile, lockBytes, lockExisted)
		return InstallationResult{}, err
	}
	installed := make([]string, 0, len(effective))
	for _, file := range effective {
		installed = append(installed, filepath.Join(targetRoot, fsx.FromUnix(file.Path)))
	}
	sort.Strings(skipped)
	sort.Strings(preserved)
	return InstallationResult{Lockfile: lockfile, InstalledFiles: installed, SkippedFiles: skipped, PreservedFiles: preserved}, nil
}

func (Installer) Status(target string, roots Roots, scope Scope) ([]FileStatus, error) {
	adapter, err := AdapterForID(target)
	if err != nil {
		return nil, err
	}
	safeRoots, err := safeRoots(roots, scope, false)
	if err != nil {
		return nil, err
	}
	scopeRoot := rootsForScope(safeRoots, scope)
	root := adapter.TargetRoot(safeRoots, scope)
	stateFile := adapter.StateFile(safeRoots, scope)
	for _, candidate := range []string{root, stateFile} {
		if err := ensureInside(scopeRoot, candidate); err != nil {
			return nil, err
		}
	}
	state, err := (StateStore{File: stateFile}).Read(adapter.ID)
	if err != nil {
		return nil, err
	}
	results := []FileStatus{}
	for _, record := range state.Files {
		destination := filepath.Join(root, fsx.FromUnix(record.Path))
		if err := ensureInside(root, destination); err != nil {
			return nil, err
		}
		condition := ConditionMissing
		if fsx.IsFile(destination) {
			digest, err := fsx.DigestFile(destination)
			if err != nil {
				return nil, err
			}
			if digest == record.Digest {
				condition = ConditionUnchanged
			} else {
				condition = ConditionModified
			}
		}
		results = append(results, FileStatus{File: record, Condition: condition})
	}
	return results, nil
}

func (Installer) Uninstall(target string, roots Roots, scope Scope, packageIDs []string) ([]FileStatus, error) {
	adapter, err := AdapterForID(target)
	if err != nil {
		return nil, err
	}
	safeRoots, err := safeRoots(roots, scope, false)
	if err != nil {
		return nil, err
	}
	scopeRoot := rootsForScope(safeRoots, scope)
	root := adapter.TargetRoot(safeRoots, scope)
	stateFile := adapter.StateFile(safeRoots, scope)
	for _, candidate := range []string{root, stateFile} {
		if err := ensureInside(scopeRoot, candidate); err != nil {
			return nil, err
		}
	}
	store := StateStore{File: stateFile}
	statuses, err := (Installer{}).Status(target, roots, scope)
	if err != nil {
		return nil, err
	}
	var requested map[string]bool
	if packageIDs != nil {
		requested = toSet(packageIDs)
	}
	remaining := []ManagedFile{}
	for _, entry := range statuses {
		selected := requested == nil || requested[entry.File.PackageID]
		if !selected || entry.File.Mode == ModeSeed || entry.Condition == ConditionModified {
			remaining = append(remaining, entry.File)
			continue
		}
		if entry.Condition == ConditionUnchanged {
			path := filepath.Join(root, fsx.FromUnix(entry.File.Path))
			if err := ensureInside(root, path); err != nil {
				return nil, err
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	if err := store.Write(InstalledState{Version: 1, Target: adapter.ID, Files: remaining}); err != nil {
		return nil, err
	}
	return statuses, nil
}

func buildPlan(resolution Resolution) ([]plannedFile, error) {
	plan := []plannedFile{}
	for _, candidate := range resolution.Packages {
		sourceRoot, err := fsx.ResolveDir(candidate.SourceRoot)
		if err != nil {
			return nil, err
		}
		kinds := make([]string, 0, len(candidate.Manifest.Contents))
		for kind := range candidate.Manifest.Contents {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		for _, kind := range kinds {
			files, err := source.FilesForContents(sourceRoot, candidate.Manifest.Contents[kind])
			if err != nil {
				return nil, Errorf("%s", err)
			}
			for _, file := range files {
				relative, err := fsx.PosixRel(sourceRoot, file)
				if err != nil {
					return nil, err
				}
				bytes, err := os.ReadFile(file)
				if err != nil {
					return nil, err
				}
				mode := ModeManaged
				if kind == "personas" {
					mode = ModeSeed
				}
				plan = append(plan, plannedFile{Path: relative, Bytes: bytes, Digest: fsx.Digest(bytes), PackageID: candidate.Manifest.ID, Mode: mode})
			}
		}
	}
	sort.SliceStable(plan, func(i, j int) bool { return plan[i].Path < plan[j].Path })
	return plan, nil
}

func validatePlan(plan []plannedFile, state InstalledState, targetRoot string, replacing map[string]bool, onModified ConflictResolver) ([]ManagedFile, []string, []string, error) {
	planned := map[string]bool{}
	skipped := []string{}
	preserved := []string{}
	managed := map[string]ManagedFile{}
	for _, file := range state.Files {
		managed[file.Path] = file
	}
	for _, file := range plan {
		destination := filepath.Join(targetRoot, fsx.FromUnix(file.Path))
		if err := ensureInside(targetRoot, destination); err != nil {
			return nil, nil, nil, err
		}
		if planned[file.Path] {
			return nil, nil, nil, Errorf("Installation collision at %s.", file.Path)
		}
		planned[file.Path] = true
		if !fsx.SafeInstallPath(file.Path) {
			return nil, nil, nil, Errorf("Unsafe installation destination: %s", file.Path)
		}
		if fsx.Exists(destination) {
			if file.Mode == ModeSeed {
				preserved = append(preserved, file.Path)
				continue
			}
			record, ok := managed[file.Path]
			if !ok {
				return nil, nil, nil, Errorf("Refusing to overwrite unmanaged file: %s", file.Path)
			}
			if record.PackageID != file.PackageID {
				return nil, nil, nil, Errorf("Installation collision at %s.", file.Path)
			}
			digest, err := fsx.DigestFile(destination)
			if err != nil {
				return nil, nil, nil, err
			}
			if digest != record.Digest {
				if onModified == nil {
					return nil, nil, nil, Errorf("Refusing to overwrite modified managed file: %s", file.Path)
				}
				if onModified(file.Path) == ConflictSkip {
					skipped = append(skipped, file.Path)
				}
			}
		} else if fsx.IsDir(destination) {
			return nil, nil, nil, Errorf("Installation collision at %s.", file.Path)
		}
	}
	removals := []ManagedFile{}
	for _, file := range state.Files {
		if file.Mode == ModeSeed {
			continue
		}
		if !replacing[file.PackageID] || planned[file.Path] {
			continue
		}
		destination := filepath.Join(targetRoot, fsx.FromUnix(file.Path))
		if fsx.IsFile(destination) {
			digest, err := fsx.DigestFile(destination)
			if err != nil {
				return nil, nil, nil, err
			}
			if digest != file.Digest {
				return nil, nil, nil, Errorf("Refusing to remove modified managed file: %s", file.Path)
			}
		}
		removals = append(removals, file)
	}
	return removals, skipped, preserved, nil
}

func nextState(current InstalledState, plan []plannedFile, target string, replacing map[string]bool, skipped map[string]bool) InstalledState {
	planned := map[string]bool{}
	for _, file := range plan {
		planned[file.Path] = true
	}
	files := []ManagedFile{}
	for _, file := range current.Files {
		if !replacing[file.PackageID] || (file.Mode == ModeSeed && !planned[file.Path]) || skipped[file.Path] {
			files = append(files, file)
		}
	}
	for _, file := range plan {
		files = append(files, ManagedFile{Path: file.Path, Digest: file.Digest, PackageID: file.PackageID, Mode: file.Mode})
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return InstalledState{Version: 1, Target: target, Files: files}
}

func stageFiles(plan []plannedFile, stage string) error {
	for _, file := range plan {
		path := filepath.Join(stage, fsx.FromUnix(file.Path))
		if err := writeBytes(path, file.Bytes); err != nil {
			return err
		}
		digest, err := fsx.DigestFile(path)
		if err != nil {
			return err
		}
		if digest != file.Digest {
			return Errorf("Staging digest mismatch: %s", file.Path)
		}
	}
	return nil
}

func commit(plan []plannedFile, removals []ManagedFile, stage, targetRoot string, store StateStore, next InstalledState) error {
	backups := map[string]string{}
	written := []string{}
	allPaths := []string{}
	for _, file := range plan {
		allPaths = append(allPaths, file.Path)
	}
	for _, file := range removals {
		allPaths = append(allPaths, file.Path)
	}
	for _, path := range allPaths {
		destination := filepath.Join(targetRoot, fsx.FromUnix(path))
		if err := ensureInside(targetRoot, destination); err != nil {
			return err
		}
		if fsx.Exists(destination) {
			backup := filepath.Join(stage, "backups", fsx.FromUnix(path))
			if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
				return err
			}
			if err := os.Rename(destination, backup); err != nil {
				return err
			}
			backups[path] = backup
		}
	}
	rollback := func() {
		for i := len(written) - 1; i >= 0; i-- {
			_ = os.Remove(written[i])
		}
		for path, backup := range backups {
			destination := filepath.Join(targetRoot, fsx.FromUnix(path))
			if !fsx.Exists(destination) {
				_ = os.MkdirAll(filepath.Dir(destination), 0o755)
				_ = os.Rename(backup, destination)
			}
		}
	}
	for _, file := range plan {
		sourcePath := filepath.Join(stage, fsx.FromUnix(file.Path))
		destination := filepath.Join(targetRoot, fsx.FromUnix(file.Path))
		if err := ensureInside(targetRoot, destination); err != nil {
			rollback()
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			rollback()
			return err
		}
		if err := os.Rename(sourcePath, destination); err != nil {
			rollback()
			return err
		}
		written = append(written, destination)
	}
	if err := store.Write(next); err != nil {
		rollback()
		return err
	}
	return nil
}

func safeRoots(roots Roots, scope Scope, create bool) (Roots, error) {
	requested := rootsForScope(roots, scope)
	if create {
		if err := os.MkdirAll(requested, 0o755); err != nil {
			return Roots{}, Errorf("Cannot access installation root: %v", err)
		}
	}
	safe := requested
	if fsx.Exists(requested) {
		resolved, err := fsx.ResolveDir(requested)
		if err != nil {
			return Roots{}, Errorf("Cannot access installation root: %v", err)
		}
		safe = resolved
	} else {
		absolute, err := filepath.Abs(requested)
		if err != nil {
			return Roots{}, Errorf("Cannot access installation root: %v", err)
		}
		safe = filepath.Clean(absolute)
	}
	if scope == ScopeUser {
		return Roots{UserRoot: safe, ProjectRoot: roots.ProjectRoot}, nil
	}
	return Roots{UserRoot: roots.UserRoot, ProjectRoot: safe}, nil
}

func rootsForScope(roots Roots, scope Scope) string {
	if scope == ScopeUser {
		return roots.UserRoot
	}
	return roots.ProjectRoot
}

func readOptional(path string) ([]byte, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

func toSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[value] = true
	}
	return out
}

func NormalizeScope(value string) (Scope, error) {
	switch strings.ToLower(value) {
	case "user":
		return ScopeUser, nil
	case "project":
		return ScopeProject, nil
	default:
		return "", Errorf("Unsupported installation scope: %s", value)
	}
}
