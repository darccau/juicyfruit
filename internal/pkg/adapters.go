package pkg

import (
	"os"
	"path/filepath"

	"github.com/darccau/juicyfruit/internal/fsx"
)

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

type Roots struct {
	UserRoot    string
	ProjectRoot string
}

func DefaultRoots() (Roots, error) {
	user := fsx.UserRoot()
	if user == "" {
		return Roots{}, Errorf("A user home or JUICYFRUIT_USER_ROOT is required for package installation.")
	}
	return Roots{UserRoot: user, ProjectRoot: fsx.ProjectRoot()}, nil
}

type TargetAdapter struct {
	ID                   string
	UserDirectoryName    string
	ProjectDirectoryName string
	ConfigurationMarkers []string
}

func (a TargetAdapter) TargetRoot(roots Roots, scope Scope) string {
	base := roots.UserRoot
	directory := a.UserDirectoryName
	if scope == ScopeProject {
		base = roots.ProjectRoot
		directory = a.ProjectDirectoryName
	}
	return filepath.Join(base, filepath.FromSlash(directory))
}

func (a TargetAdapter) IsConfigured(roots Roots, scope Scope) bool {
	root := a.TargetRoot(roots, scope)
	if !fsx.IsDir(root) {
		return false
	}
	if len(a.ConfigurationMarkers) == 0 {
		return true
	}
	for _, marker := range a.ConfigurationMarkers {
		if fsx.IsDir(filepath.Join(root, filepath.FromSlash(marker))) {
			return true
		}
	}
	return false
}

func (a TargetAdapter) StateFile(roots Roots, scope Scope) string {
	base := roots.UserRoot
	if scope == ScopeProject {
		base = roots.ProjectRoot
	}
	return filepath.Join(base, ".juicyfruit-state", "targets", a.ID, string(scope), "installed.json")
}

func (a TargetAdapter) Lockfile(roots Roots, scope Scope) string {
	base := roots.UserRoot
	if scope == ScopeProject {
		base = roots.ProjectRoot
	}
	return filepath.Join(base, ".juicyfruit", "runtime", "locks", a.ID, string(scope)+".lock.json")
}

var adapters = []TargetAdapter{
	{ID: "codex", UserDirectoryName: ".codex", ProjectDirectoryName: ".agents"},
	{ID: "claude-code", UserDirectoryName: ".claude", ProjectDirectoryName: ".claude"},
	{ID: "cursor", UserDirectoryName: ".cursor", ProjectDirectoryName: ".cursor"},
	{ID: "antigravity", UserDirectoryName: ".gemini/config", ProjectDirectoryName: ".agents"},
	{ID: "devin", UserDirectoryName: ".devin", ProjectDirectoryName: ".devin"},
	{ID: "dsh", UserDirectoryName: ".dsh", ProjectDirectoryName: ".dsh"},
	{ID: "generic", UserDirectoryName: ".juicyfruit", ProjectDirectoryName: ".juicyfruit", ConfigurationMarkers: []string{"skills", "rules", "agents", "templates"}},
	{ID: "gemini-cli", UserDirectoryName: ".gemini", ProjectDirectoryName: ".gemini"},
	{ID: "via", UserDirectoryName: ".via", ProjectDirectoryName: ".via"},
}

func AllAdapters() []TargetAdapter { return append([]TargetAdapter{}, adapters...) }

func AdapterForID(id string) (TargetAdapter, error) {
	for _, adapter := range adapters {
		if adapter.ID == id {
			return adapter, nil
		}
	}
	return TargetAdapter{}, Errorf("Unsupported installation target: %s", id)
}

func SupportedTargets() []string {
	out := make([]string, 0, len(adapters))
	for _, adapter := range adapters {
		out = append(out, adapter.ID)
	}
	return out
}

func ConfiguredIDs(roots Roots, scope Scope) []string {
	out := []string{}
	for _, adapter := range adapters {
		if adapter.IsConfigured(roots, scope) {
			out = append(out, adapter.ID)
		}
	}
	return out
}

func ensureInside(root, candidate string) error {
	if !fsx.WithinOrEqual(root, candidate) {
		return Errorf("Installation path escapes its root: %s", candidate)
	}
	return nil
}

func ensureNoSymlink(root, candidate string) error {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return Errorf("Installation path escapes its root: %s", candidate)
	}
	current := root
	if rel == "." {
		return nil
	}
	for _, segment := range splitPath(rel) {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return Errorf("Installation path contains a symbolic link: %s", current)
		}
	}
	return nil
}

func splitPath(path string) []string {
	var parts []string
	for path != "" && path != "." {
		dir, file := filepath.Split(path)
		if file != "" {
			parts = append([]string{file}, parts...)
		}
		path = filepath.Clean(dir)
		if path == "." {
			break
		}
	}
	return parts
}
