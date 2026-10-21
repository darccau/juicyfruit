package pkg

import (
	"os"
	"path/filepath"

	"github.com/darccau/juicyfruit/internal/fsx"
	"github.com/darccau/juicyfruit/internal/source"
)

func LoadManifest(packageDirectory string) (Manifest, error) {
	root, err := fsx.ResolveDir(packageDirectory)
	if err != nil {
		return Manifest{}, Errorf("Package directory does not exist: %s", packageDirectory)
	}
	path := filepath.Join(root, "package.yaml")
	if !fsx.IsFile(path) {
		return Manifest{}, Errorf("Package is missing package.yaml: %s", packageDirectory)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	parsed, err := source.ParseYAMLMap(data, "package manifest")
	if err != nil {
		return Manifest{}, Errorf("%s", err)
	}
	if err := source.ValidatePackage(parsed); err != nil {
		return Manifest{}, Errorf("%s", err)
	}
	manifest := Manifest{
		SchemaVersion: 1,
		ID:            parsed["id"].(string),
		Name:          parsed["name"].(string),
		Version:       parsed["version"].(string),
		Description:   parsed["description"].(string),
		Targets:       source.Strings(parsed["targets"]),
		Contents:      map[string][]string{},
	}
	if license, ok := parsed["license"].(string); ok {
		manifest.License = license
	}
	for key, raw := range source.StringMap(parsed["contents"]) {
		manifest.Contents[key] = source.Strings(raw)
	}
	if deps, ok := parsed["dependencies"].([]any); ok {
		for _, raw := range deps {
			m := source.StringMap(raw)
			manifest.Dependencies = append(manifest.Dependencies, Dependency{ID: m["id"].(string), Version: m["version"].(string)})
		}
	}
	manifest.Conflicts = source.Strings(parsed["conflicts"])
	return manifest, nil
}

func PackageDigest(packageRoot, contentRoot string, manifest Manifest) (string, error) {
	return source.DigestPackage(packageRoot, contentRoot, manifest.Map())
}
