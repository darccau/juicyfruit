package pkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/darccau/juicyfruit/internal/fsx"
)

type StateStore struct{ File string }

func (s StateStore) Read(target string) (InstalledState, error) {
	if !fsx.IsFile(s.File) {
		return InstalledState{Version: 1, Target: target, Files: []ManagedFile{}}, nil
	}
	var raw map[string]any
	if err := fsx.ReadJSON(s.File, &raw); err != nil {
		return InstalledState{}, Errorf("Cannot read installed state: %v", err)
	}
	if raw["version"] != float64(1) || raw["target"] != target {
		return InstalledState{}, Errorf("Unsupported installed state format.")
	}
	list, ok := raw["files"].([]any)
	if !ok {
		return InstalledState{}, Errorf("Unsupported installed state format.")
	}
	files := []ManagedFile{}
	paths := map[string]bool{}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return InstalledState{}, Errorf("Invalid installed state entry.")
		}
		path, _ := m["path"].(string)
		digest, _ := m["digest"].(string)
		packageID, _ := m["package_id"].(string)
		mode := ModeManaged
		if value, ok := m["mode"].(string); ok {
			mode = FileMode(value)
		}
		if !fsx.SafeInstallPath(path) || !isDigest(digest) || packageID == "" || (mode != ModeManaged && mode != ModeSeed) || paths[path] {
			return InstalledState{}, Errorf("Invalid installed state entry.")
		}
		paths[path] = true
		files = append(files, ManagedFile{Path: path, Digest: digest, PackageID: packageID, Mode: mode})
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return InstalledState{Version: 1, Target: target, Files: files}, nil
}

func (s StateStore) Write(state InstalledState) error {
	state.Version = 1
	sort.SliceStable(state.Files, func(i, j int) bool { return state.Files[i].Path < state.Files[j].Path })
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := fsx.WriteAtomic(s.File, append(data, '\n')); err != nil {
		return Errorf("Cannot update installed state: %v", err)
	}
	return nil
}

type LockStore struct{ File string }

func (s LockStore) Read() (Lockfile, error) {
	var raw map[string]any
	if err := fsx.ReadJSON(s.File, &raw); err != nil {
		return Lockfile{}, Errorf("Cannot read package lockfile: %v", err)
	}
	if raw["version"] != float64(1) {
		return Lockfile{}, Errorf("Unsupported package lockfile format.")
	}
	target, ok := raw["target"].(string)
	if !ok {
		return Lockfile{}, Errorf("Unsupported package lockfile format.")
	}
	list, ok := raw["packages"].([]any)
	if !ok {
		return Lockfile{}, Errorf("Unsupported package lockfile format.")
	}
	packages := []LockedPackage{}
	identities := map[string]bool{}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return Lockfile{}, Errorf("Invalid package lockfile entry.")
		}
		id, _ := m["id"].(string)
		version, _ := m["version"].(string)
		sourceName, _ := m["source"].(string)
		digest, _ := m["digest"].(string)
		identity := id + "@" + version + "@" + sourceName
		if id == "" || version == "" || sourceName == "" || !isDigest(digest) || identities[identity] {
			return Lockfile{}, Errorf("Invalid package lockfile entry.")
		}
		identities[identity] = true
		packages = append(packages, LockedPackage{ID: id, Version: version, Source: sourceName, Digest: digest})
	}
	sort.SliceStable(packages, func(i, j int) bool { return packages[i].ID < packages[j].ID })
	return Lockfile{Version: 1, Target: target, Packages: packages}, nil
}

func (s LockStore) Write(lockfile Lockfile) error {
	lockfile.Version = 1
	data, err := json.MarshalIndent(lockfile, "", "  ")
	if err != nil {
		return err
	}
	if err := fsx.WriteAtomic(s.File, append(data, '\n')); err != nil {
		return Errorf("Cannot update package lockfile: %v", err)
	}
	return nil
}

func isDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

func restoreFile(path string, bytes []byte, existed bool) error {
	if !existed {
		_ = os.Remove(path)
		return nil
	}
	return fsx.WriteAtomic(path, bytes)
}

func writeBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
