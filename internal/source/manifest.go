package source

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darccau/juicyfruit/internal/fsx"
	"gopkg.in/yaml.v3"
)

func LoadCatalog(directoryPath string) (Catalog, error) {
	root, err := fsx.ResolveDir(directoryPath)
	if err != nil {
		return Catalog{}, Errorf("Source directory does not exist: %s", directoryPath)
	}
	manifestPath := filepath.Join(root, "juicyfruit-source.yaml")
	if !fsx.IsFile(manifestPath) {
		return Catalog{}, Errorf("Source is missing juicyfruit-source.yaml.")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return Catalog{}, err
	}
	parsed, err := ParseYAMLMap(data, "source manifest")
	if err != nil {
		return Catalog{}, err
	}
	if err := ValidateLocalSource(parsed); err != nil {
		return Catalog{}, err
	}
	id := parsed["id"].(string)
	name := parsed["name"].(string)
	packagesPath, err := RequireRelativePath(parsed["packages_path"], "packages_path", true)
	if err != nil {
		return Catalog{}, err
	}
	packagesDir := filepath.Join(root, fsx.FromUnix(packagesPath))
	if !fsx.IsDir(packagesDir) {
		return Catalog{}, Errorf("Packages directory does not exist: %s", packagesPath)
	}
	resolvedPackages, err := fsx.ResolveDir(packagesDir)
	if err != nil {
		return Catalog{}, err
	}
	if resolvedPackages != root && !fsx.Within(root, resolvedPackages) {
		return Catalog{}, Errorf("Packages directory escapes the source: %s", packagesPath)
	}
	entries, err := os.ReadDir(resolvedPackages)
	if err != nil {
		return Catalog{}, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var packages []Package
	seen := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		packageDir := filepath.Join(resolvedPackages, entry.Name())
		packageFile := filepath.Join(packageDir, "package.yaml")
		relative, _ := fsx.PosixRel(root, packageFile)
		if !fsx.IsFile(packageFile) {
			return Catalog{}, Errorf("Package directory is missing its manifest: %s", relative)
		}
		resolvedPackage, err := fsx.ResolveFile(packageFile)
		if err != nil || !fsx.Within(root, resolvedPackage) {
			return Catalog{}, Errorf("Package path escapes the source: %s", relative)
		}
		pkg, err := LoadPackageSummary(packageFile, relative)
		if err != nil {
			return Catalog{}, err
		}
		if seen[pkg.ID] {
			return Catalog{}, Errorf("Duplicate package id: %s", pkg.ID)
		}
		seen[pkg.ID] = true
		packages = append(packages, pkg)
	}
	return Catalog{ID: id, Name: name, Root: root, Packages: packages}, nil
}

func LoadPackageSummary(path, relativePath string) (Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Package{}, err
	}
	parsed, err := ParseYAMLMap(data, "package manifest "+relativePath)
	if err != nil {
		return Package{}, err
	}
	if err := ValidatePackage(parsed); err != nil {
		return Package{}, err
	}
	return Package{
		ID:          parsed["id"].(string),
		Path:        relativePath,
		Version:     parsed["version"].(string),
		Description: parsed["description"].(string),
	}, nil
}

func ParseYAMLMap(data []byte, context string) (map[string]any, error) {
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, Errorf("Cannot parse %s: %v", context, err)
	}
	normalized := normalizeYAML(raw)
	value, ok := normalized.(map[string]any)
	if !ok {
		return nil, Errorf("%s must be a YAML object.", context)
	}
	return value, nil
}

func Strings(value any) []string {
	values, ok := value.([]any)
	if !ok {
		if list, ok := value.([]string); ok {
			return list
		}
		return nil
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func StringMap(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func PackageFileInfo(sourceRoot, packageRoot string, manifest map[string]any) ([]string, map[string]string, error) {
	contents := StringMap(manifest["contents"])
	var declared []string
	for _, raw := range contents {
		declared = append(declared, Strings(raw)...)
	}
	files, err := FilesForContents(sourceRoot, declared)
	if err != nil {
		return nil, nil, err
	}
	digests := map[string]string{}
	for _, file := range files {
		resolved, err := fsx.ResolveFile(file)
		if err != nil || !fsx.Within(sourceRoot, resolved) {
			return nil, nil, Errorf("Package content escapes its root: %s", file)
		}
		relative, _ := fsx.PosixRel(sourceRoot, resolved)
		data, err := os.ReadFile(resolved)
		if err != nil {
			return nil, nil, err
		}
		digests[relative] = fsx.Digest(data)
	}
	return files, digests, nil
}

func FilesForContents(root string, declared []string) ([]string, error) {
	canonical, err := fsx.ResolveDir(root)
	if err != nil {
		return nil, Errorf("Package directory does not exist: %s", root)
	}
	files := []string{}
	for _, relative := range declared {
		candidate := filepath.Join(canonical, fsx.FromUnix(relative))
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			resolved, err := fsx.ResolveFile(candidate)
			if err != nil || !fsx.Within(canonical, resolved) {
				return nil, Errorf("Package content escapes its root: %s", relative)
			}
			files = append(files, resolved)
			continue
		}
		if err == nil && info.IsDir() {
			resolvedDir, err := fsx.ResolveDir(candidate)
			if err != nil || !fsx.Within(canonical, resolvedDir) {
				return nil, Errorf("Package content escapes its root: %s", relative)
			}
			entries, err := fsx.WalkFiles(resolvedDir)
			if err != nil {
				return nil, Errorf("Package content contains a symbolic link: %s", relative)
			}
			sort.Strings(entries)
			files = append(files, entries...)
			continue
		}
		return nil, Errorf("Declared package content does not exist: %s", relative)
	}
	seen := map[string]bool{}
	unique := files[:0]
	for _, file := range files {
		if !seen[file] {
			seen[file] = true
			unique = append(unique, file)
		}
	}
	sort.Strings(unique)
	return unique, nil
}

func DigestPackage(packageRoot, contentRoot string, manifest map[string]any) (string, error) {
	canonicalPackage, err := fsx.ResolveDir(packageRoot)
	if err != nil {
		return "", err
	}
	canonicalRoot, err := fsx.ResolveDir(contentRoot)
	if err != nil {
		return "", err
	}
	files, _, err := PackageFileInfo(canonicalRoot, canonicalPackage, manifest)
	if err != nil {
		return "", err
	}
	all := append([]string{filepath.Join(canonicalPackage, "package.yaml")}, files...)
	byRelative := map[string]string{}
	for _, file := range all {
		resolved, err := fsx.ResolveFile(file)
		if err != nil {
			return "", err
		}
		if resolved == filepath.Join(canonicalPackage, "package.yaml") && !fsx.Within(canonicalRoot, resolved) {
			byRelative["package.yaml"] = resolved
			continue
		}
		if !fsx.Within(canonicalRoot, resolved) && resolved != canonicalRoot {
			return "", Errorf("Package content escapes its root: %s", file)
		}
		relative, _ := fsx.PosixRel(canonicalRoot, resolved)
		byRelative[relative] = resolved
	}
	keys := make([]string, 0, len(byRelative))
	for key := range byRelative {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		data, err := os.ReadFile(byRelative[key])
		if err != nil {
			return "", err
		}
		builder.WriteString(key)
		builder.WriteByte(0)
		builder.Write(data)
		builder.WriteByte(0)
	}
	return fsx.Digest([]byte(builder.String())), nil
}

func ValidateArchiveEntryPath(path string) (string, error) {
	if path == "" || path == "." || path == "./" {
		return "", nil
	}
	if strings.Contains(path, "\\") || strings.ContainsRune(path, 0) || strings.HasPrefix(path, "/") {
		return "", Errorf("Archive entry has an unsafe path: %s", path)
	}
	if len(path) >= 2 && path[1] == ':' {
		return "", Errorf("Archive entry has an unsafe path: %s", path)
	}
	if hasSegment(path, "..") {
		return "", Errorf("Archive entry has an unsafe path: %s", path)
	}
	normalized := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if normalized == "." || strings.HasPrefix(normalized, "../") {
		return "", Errorf("Archive entry has an unsafe path: %s", path)
	}
	return normalized, nil
}

func Describe(value any) string {
	return fmt.Sprint(value)
}
