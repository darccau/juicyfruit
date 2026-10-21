package memory

import (
	"os"
	"path/filepath"

	"github.com/darccau/juicyfruit/internal/fsx"
)

type Roots struct {
	UserDirectory    string
	ProjectDirectory string
}

func DefaultRoots() (Roots, error) {
	user, err := DefaultUserMemoryDirectory()
	if err != nil {
		return Roots{}, err
	}
	return Roots{UserDirectory: user, ProjectDirectory: ProjectMemoryDirectory(fsx.ProjectRoot())}, nil
}

func DefaultUserMemoryDirectory() (string, error) {
	if value := os.Getenv("JUICYFRUIT_MEMORY_HOME"); value != "" {
		return value, nil
	}
	if value := os.Getenv("JUICYFRUIT_HOME"); value != "" {
		return filepath.Join(value, "memory"), nil
	}
	home := fsx.Home()
	if home == "" {
		return "", Errorf("HOME is required to store Juicyfruit memory.")
	}
	return filepath.Join(home, ".juicyfruit", "memory"), nil
}

func ProjectMemoryDirectory(start string) string {
	root := os.Getenv("JUICYFRUIT_PROJECT_ROOT")
	if root == "" {
		root = repositoryRoot(start)
	}
	return filepath.Join(root, ".juicyfruit", "memory")
}

func repositoryRoot(start string) string {
	origin, err := filepath.Abs(start)
	if err != nil {
		origin = start
	}
	current := origin
	for {
		if fsx.Exists(filepath.Join(current, ".git")) {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return origin
		}
		current = parent
	}
}

func ConfigFile(directory string) string { return filepath.Join(directory, "config.json") }
