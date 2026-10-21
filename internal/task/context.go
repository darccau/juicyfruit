package task

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darccau/juicyfruit/internal/fsx"
	"github.com/darccau/juicyfruit/internal/template"
	"gopkg.in/yaml.v3"
)

type ContextPackage struct {
	Task            string              `json:"task"`
	TargetTokens    int                 `json:"target_tokens"`
	HardLimitTokens int                 `json:"hard_limit_tokens"`
	EstimatedTokens int                 `json:"estimated_tokens"`
	Sources         map[string][]string `json:"sources"`
	Missing         []string            `json:"missing"`
}

func (c ContextPackage) MarshalJSON() ([]byte, error) {
	sources := map[string][]string{}
	for key, values := range c.Sources {
		sources[key] = nonNilStrings(values)
	}
	return json.Marshal(map[string]any{
		"schema": "juicyfruit.context/v1",
		"task":   c.Task,
		"budget": map[string]any{
			"target_tokens": c.TargetTokens, "hard_limit_tokens": c.HardLimitTokens,
			"estimated_tokens": c.EstimatedTokens, "estimator": "ceil(chars / 4)",
		},
		"sources": sources,
		"missing": nonNilStrings(c.Missing),
	})
}

func (s *Store) BuildContext(taskID string, targetTokens, hardLimitTokens int) (ContextPackage, error) {
	task, err := s.ReadTask(taskID)
	if err != nil {
		return ContextPackage{}, err
	}
	explicit := map[string]bool{}
	for _, file := range task.Context.Files {
		explicit[file] = true
	}
	topicFiles := map[string]bool{}
	missing := []string{}
	indexPath := filepath.Join(s.root(), "context", "index.yaml")
	if fsx.IsFile(indexPath) {
		data, err := os.ReadFile(indexPath)
		if err != nil {
			return ContextPackage{}, err
		}
		var document map[string]any
		if err := yaml.Unmarshal(data, &document); err != nil {
			return ContextPackage{}, err
		}
		contexts, _ := document["contexts"].(map[string]any)
		for _, topic := range task.Context.Topics {
			entry, ok := contexts[topic].(map[string]any)
			if !ok {
				missing = append(missing, "context:"+topic)
				continue
			}
			switch files := entry["files"].(type) {
			case []any:
				for _, file := range files {
					topicFiles[toString(file)] = true
				}
			case []string:
				for _, file := range files {
					topicFiles[file] = true
				}
			}
		}
	} else if len(task.Context.Topics) > 0 {
		missing = append(missing, "context:index.yaml")
	}
	allRequested := []string{}
	for path := range explicit {
		allRequested = append(allRequested, path)
	}
	for path := range topicFiles {
		allRequested = append(allRequested, path)
	}
	sort.Strings(allRequested)
	if err := ensureSafePaths(allRequested); err != nil {
		return ContextPackage{}, err
	}
	files := map[string]bool{}
	taskJSON, _ := json.MarshalIndent(task, "", "  ")
	estimated := estimateTokens(string(taskJSON))
	for _, pattern := range allRequested {
		resolved, err := s.resolveContextFiles(pattern)
		if err != nil {
			return ContextPackage{}, err
		}
		if len(resolved) == 0 {
			missing = append(missing, "file:"+pattern)
			continue
		}
		for _, resolvedPath := range resolved {
			files[resolvedPath] = true
			file := filepath.Join(s.ProjectRoot, fsx.FromUnix(resolvedPath))
			if fsx.IsFile(file) {
				data, err := os.ReadFile(file)
				if err != nil {
					return ContextPackage{}, err
				}
				estimated += estimateTokens(string(data))
			}
		}
	}
	personas, err := s.resolvePersonaFiles()
	if err != nil {
		return ContextPackage{}, err
	}
	for _, personaPath := range personas {
		file := filepath.Join(s.ProjectRoot, fsx.FromUnix(personaPath))
		if fsx.IsFile(file) {
			data, err := os.ReadFile(file)
			if err != nil {
				return ContextPackage{}, err
			}
			estimated += estimateTokens(string(data))
		}
	}
	sortedFiles := []string{}
	for file := range files {
		sortedFiles = append(sortedFiles, file)
	}
	sort.Strings(sortedFiles)
	templates := []string{}
	if task.Context.Template != nil && *task.Context.Template != "" {
		store := template.Store{Roots: template.Roots{ProjectRoot: s.ProjectRoot, UserRoot: s.ProjectRoot}}
		match, err := store.Match(*task.Context.Template, false)
		if err != nil {
			return ContextPackage{}, err
		}
		if match == nil {
			missing = append(missing, "template:"+*task.Context.Template)
		} else {
			templates = append(templates, match.Template.Path)
			file := filepath.Join(s.ProjectRoot, fsx.FromUnix(match.Template.Path))
			if fsx.IsFile(file) {
				data, err := os.ReadFile(file)
				if err != nil {
					return ContextPackage{}, err
				}
				estimated += estimateTokens(string(data))
			}
		}
	}
	return ContextPackage{
		Task: task.ID, TargetTokens: targetTokens, HardLimitTokens: hardLimitTokens,
		EstimatedTokens: estimated,
		Sources: map[string][]string{
			"rules": {}, "skills": {}, "personas": personas,
			"templates": sortedUnique(templates...), "memory": {},
			"files": sortedFiles, "decisions": {},
		},
		Missing: sortedUnique(missing...),
	}, nil
}

func (s *Store) resolveContextFiles(pattern string) ([]string, error) {
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		dir := filepath.Join(s.ProjectRoot, fsx.FromUnix(prefix))
		if !fsx.IsDir(dir) {
			return []string{}, nil
		}
		files, err := fsx.WalkFiles(dir)
		if err != nil {
			return nil, err
		}
		out := []string{}
		for _, file := range files {
			relative, _ := fsx.PosixRel(s.ProjectRoot, file)
			out = append(out, relative)
		}
		sort.Strings(out)
		return out, nil
	}
	if strings.Contains(pattern, "*") {
		return []string{pattern}, nil
	}
	file := filepath.Join(s.ProjectRoot, fsx.FromUnix(pattern))
	if fsx.IsFile(file) {
		return []string{pattern}, nil
	}
	return []string{}, nil
}

func (s *Store) resolvePersonaFiles() ([]string, error) {
	dir := filepath.Join(s.root(), "personas")
	if !fsx.IsDir(dir) {
		return []string{}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		relative, _ := fsx.PosixRel(s.ProjectRoot, filepath.Join(dir, entry.Name()))
		out = append(out, relative)
	}
	sort.Strings(out)
	return out, nil
}

func estimateTokens(value string) int { return (len(value) + 3) / 4 }

func toString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	data, _ := json.Marshal(value)
	return string(data)
}
