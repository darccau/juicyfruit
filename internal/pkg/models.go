package pkg

import (
	"fmt"
	"sort"
	"strings"
)

type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

func Errorf(format string, args ...any) error { return &Error{Message: fmt.Sprintf(format, args...)} }

type Dependency struct {
	ID      string `json:"id" yaml:"id"`
	Version string `json:"version" yaml:"version"`
}

type Manifest struct {
	SchemaVersion int                 `json:"schema_version" yaml:"schema_version"`
	ID            string              `json:"id" yaml:"id"`
	Name          string              `json:"name" yaml:"name"`
	Version       string              `json:"version" yaml:"version"`
	Description   string              `json:"description" yaml:"description"`
	License       string              `json:"license,omitempty" yaml:"license,omitempty"`
	Targets       []string            `json:"targets" yaml:"targets"`
	Contents      map[string][]string `json:"contents" yaml:"contents"`
	Dependencies  []Dependency        `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Conflicts     []string            `json:"conflicts,omitempty" yaml:"conflicts,omitempty"`
}

func (m Manifest) Map() map[string]any {
	contents := map[string]any{}
	for key, values := range m.Contents {
		contents[key] = values
	}
	out := map[string]any{
		"schema_version": m.SchemaVersion,
		"id":             m.ID,
		"name":           m.Name,
		"version":        m.Version,
		"description":    m.Description,
		"targets":        m.Targets,
		"contents":       contents,
	}
	if m.License != "" {
		out["license"] = m.License
	}
	if len(m.Dependencies) > 0 {
		deps := make([]any, 0, len(m.Dependencies))
		for _, dep := range m.Dependencies {
			deps = append(deps, map[string]any{"id": dep.ID, "version": dep.Version})
		}
		out["dependencies"] = deps
	}
	if len(m.Conflicts) > 0 {
		out["conflicts"] = m.Conflicts
	}
	return out
}

func (m Manifest) Supports(target string) bool {
	for _, item := range m.Targets {
		if item == target {
			return true
		}
	}
	return false
}

type Candidate struct {
	SourceName  string
	SourceRoot  string
	PackageRoot string
	Manifest    Manifest
	Digest      string
}

func (c Candidate) Identity() string { return c.Manifest.ID + "@" + c.Manifest.Version }

type Resolution struct {
	Target   string
	Packages []Candidate
}

type LockedPackage struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Source  string `json:"source"`
	Digest  string `json:"digest"`
}

type Lockfile struct {
	Version  int             `json:"version"`
	Target   string          `json:"target"`
	Packages []LockedPackage `json:"packages"`
}

func NewLockfile(res Resolution) Lockfile {
	packages := make([]LockedPackage, 0, len(res.Packages))
	for _, candidate := range res.Packages {
		packages = append(packages, LockedPackage{
			ID: candidate.Manifest.ID, Version: candidate.Manifest.Version,
			Source: candidate.SourceName, Digest: candidate.Digest,
		})
	}
	sort.SliceStable(packages, func(i, j int) bool {
		if packages[i].ID == packages[j].ID {
			return packages[i].Source < packages[j].Source
		}
		return packages[i].ID < packages[j].ID
	})
	return Lockfile{Version: 1, Target: res.Target, Packages: packages}
}

type FileMode string

const (
	ModeManaged FileMode = "managed"
	ModeSeed    FileMode = "seed"
)

type ManagedFile struct {
	Path      string   `json:"path"`
	Digest    string   `json:"digest"`
	PackageID string   `json:"package_id"`
	Mode      FileMode `json:"mode,omitempty"`
}

type InstalledState struct {
	Version int           `json:"version"`
	Target  string        `json:"target"`
	Files   []ManagedFile `json:"files"`
}

type Condition string

const (
	ConditionUnchanged Condition = "unchanged"
	ConditionModified  Condition = "modified"
	ConditionMissing   Condition = "missing"
)

type FileStatus struct {
	File      ManagedFile
	Condition Condition
}

type InstallationResult struct {
	Lockfile       Lockfile
	InstalledFiles []string
	SkippedFiles   []string
	PreservedFiles []string
}

func compareCandidates(left, right Candidate) int {
	if left.Manifest.ID != right.Manifest.ID {
		return strings.Compare(left.Manifest.ID, right.Manifest.ID)
	}
	if left.Manifest.Version != right.Manifest.Version {
		return strings.Compare(left.Manifest.Version, right.Manifest.Version)
	}
	return strings.Compare(left.SourceName, right.SourceName)
}
