package source

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	idPattern       = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	semverPattern   = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	relativePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
)

var supportedTargets = map[string]bool{
	"codex": true, "claude-code": true, "cursor": true, "antigravity": true,
	"devin": true, "dsh": true, "generic": true, "gemini-cli": true, "via": true,
}

var contentKinds = map[string]bool{
	"skills": true, "rules": true, "agents": true, "personas": true,
	"templates": true, "scripts": true, "assets": true, "references": true,
	"plugin": true,
}

func ValidateLocalSource(value map[string]any) error {
	if err := rejectUnknownKeys(value, []string{"schema_version", "id", "name", "description", "kind", "path", "read_only", "packages_path", "profiles_path"}, "source"); err != nil {
		return err
	}
	if err := requireSchemaVersion(value, "source"); err != nil {
		return err
	}
	if _, err := RequireID(value["id"], "source id"); err != nil {
		return err
	}
	if err := requireText(value["name"], "source name", 256); err != nil {
		return err
	}
	if _, ok := value["description"]; ok {
		if err := requireText(value["description"], "description", 256); err != nil {
			return err
		}
	}
	if value["kind"] != "local" {
		return Errorf("Only local source manifests are supported.")
	}
	if value["read_only"] != true {
		return Errorf("Source manifests must be read-only.")
	}
	if _, err := RequireRelativePath(value["path"], "path", true); err != nil {
		return err
	}
	if _, err := RequireRelativePath(value["packages_path"], "packages_path", true); err != nil {
		return err
	}
	if _, ok := value["profiles_path"]; ok {
		if _, err := RequireRelativePath(value["profiles_path"], "profiles_path", true); err != nil {
			return err
		}
	}
	return nil
}

func ValidatePackage(value map[string]any) error {
	if err := rejectUnknownKeys(value, []string{"schema_version", "id", "name", "version", "description", "license", "targets", "contents", "dependencies", "conflicts"}, "package"); err != nil {
		return err
	}
	if err := requireSchemaVersion(value, "package"); err != nil {
		return err
	}
	id, err := RequireID(value["id"], "package id")
	if err != nil {
		return err
	}
	if err := requireText(value["name"], "package name", 256); err != nil {
		return err
	}
	if err := requireText(value["description"], "package description", 256); err != nil {
		return err
	}
	if _, ok := value["license"]; ok {
		if err := requireText(value["license"], "license", 256); err != nil {
			return err
		}
	}
	if _, err := RequireVersion(value["version"], id); err != nil {
		return err
	}
	if err := validateTargets(value["targets"], id); err != nil {
		return err
	}
	if err := validateContents(value["contents"], id); err != nil {
		return err
	}
	if _, ok := value["dependencies"]; ok {
		if err := validateDependencies(value["dependencies"]); err != nil {
			return err
		}
	}
	if _, ok := value["conflicts"]; ok {
		if err := validateConflicts(value["conflicts"]); err != nil {
			return err
		}
	}
	return nil
}

func RequireID(value any, field string) (string, error) {
	text, err := requireTextValue(value, field, 64)
	if err != nil {
		return "", err
	}
	if !idPattern.MatchString(text) {
		return "", Errorf("Invalid %s: %s", field, text)
	}
	return text, nil
}

func RequireVersion(value any, packageID string) (string, error) {
	text, err := requireTextValue(value, "package version", 256)
	if err != nil {
		return "", err
	}
	if !semverPattern.MatchString(text) {
		return "", Errorf("Invalid semantic version for %s: %s", packageID, text)
	}
	return text, nil
}

func RequireRelativePath(value any, field string, allowDot bool) (string, error) {
	path, err := requireTextValue(value, field, 512)
	if err != nil {
		return "", err
	}
	if (allowDot && path == ".") || (relativePattern.MatchString(path) && !strings.Contains(path, "//") && !hasSegment(path, "..")) {
		return path, nil
	}
	return "", Errorf("%s must stay inside the source: %s", field, path)
}

func validateTargets(value any, packageID string) error {
	values, ok := value.([]any)
	if !ok {
		return Errorf("Package targets must be a list.")
	}
	if len(values) == 0 {
		return Errorf("Package targets cannot be empty: %s", packageID)
	}
	seen := map[string]bool{}
	for _, item := range values {
		target, ok := item.(string)
		if !ok || !supportedTargets[target] {
			return Errorf("Invalid package target for %s: %v", packageID, item)
		}
		if seen[target] {
			return Errorf("Duplicate package target for %s: %s", packageID, target)
		}
		seen[target] = true
	}
	return nil
}

func validateContents(value any, packageID string) error {
	contents, ok := value.(map[string]any)
	if !ok {
		return Errorf("Package contents must be an object: %s", packageID)
	}
	for key := range contents {
		if !contentKinds[key] {
			return Errorf("Unknown package contents properties: %s", key)
		}
	}
	for key, raw := range contents {
		values, ok := raw.([]any)
		if !ok {
			return Errorf("Package content %s must be a list.", key)
		}
		seen := map[string]bool{}
		for _, item := range values {
			path, err := RequireRelativePath(item, key, false)
			if err != nil {
				return err
			}
			if seen[path] {
				return Errorf("Duplicate package content path in %s: %s", key, path)
			}
			seen[path] = true
		}
	}
	return nil
}

func validateDependencies(value any) error {
	values, ok := value.([]any)
	if !ok {
		return Errorf("Package dependencies must be a list.")
	}
	seen := map[string]bool{}
	for _, item := range values {
		m, ok := item.(map[string]any)
		if !ok {
			return Errorf("Each package dependency must be an object.")
		}
		if err := rejectUnknownKeys(m, []string{"id", "version"}, "package dependency"); err != nil {
			return err
		}
		id, err := RequireID(m["id"], "dependency id")
		if err != nil {
			return err
		}
		version, err := RequireVersion(m["version"], id)
		if err != nil {
			return err
		}
		key := id + "@" + version
		if seen[key] {
			return Errorf("Duplicate package dependency: %s", key)
		}
		seen[key] = true
	}
	return nil
}

func validateConflicts(value any) error {
	values, ok := value.([]any)
	if !ok {
		return Errorf("Package conflicts must be a list.")
	}
	seen := map[string]bool{}
	for _, item := range values {
		id, err := RequireID(item, "conflict id")
		if err != nil {
			return err
		}
		if seen[id] {
			return Errorf("Duplicate package conflict: %s", id)
		}
		seen[id] = true
	}
	return nil
}

func requireSchemaVersion(value map[string]any, context string) error {
	if value["schema_version"] != 1 {
		return Errorf("Unsupported %s schema version.", context)
	}
	return nil
}

func requireText(value any, field string, max int) error {
	_, err := requireTextValue(value, field, max)
	return err
}

func requireTextValue(value any, field string, max int) (string, error) {
	text, ok := value.(string)
	if !ok || text == "" || len(text) > max {
		return "", Errorf("Missing or invalid %s.", field)
	}
	return text, nil
}

func rejectUnknownKeys(value map[string]any, allowed []string, context string) error {
	allowedSet := map[string]bool{}
	for _, key := range allowed {
		allowedSet[key] = true
	}
	var unknown []string
	for key := range value {
		if !allowedSet[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		return Errorf("Unknown %s properties: %s", context, strings.Join(unknown, ", "))
	}
	return nil
}

func hasSegment(value, segment string) bool {
	for _, part := range strings.Split(value, "/") {
		if part == segment {
			return true
		}
	}
	return false
}

func normalizeYAML(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = normalizeYAML(item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[fmt.Sprint(key)] = normalizeYAML(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalizeYAML(item)
		}
		return out
	default:
		return v
	}
}
