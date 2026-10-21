package template

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/darccau/juicyfruit/internal/fsx"
	"gopkg.in/yaml.v3"
)

type Error struct{ Message string }

func (e *Error) Error() string                { return e.Message }
func Errorf(format string, args ...any) error { return &Error{Message: fmt.Sprintf(format, args...)} }

type Origin string

const (
	OriginCanonical Origin = "canonical"
	OriginProject   Origin = "project"
	OriginUser      Origin = "user"
)

type Template struct {
	Name        string
	Kind        string
	Description string
	Frontmatter map[string]any
	Body        string
	Path        string
	Origin      Origin
}

func (t Template) FormatTarget() string {
	if format, ok := t.Frontmatter["format"].(map[string]any); ok {
		if target, ok := format["target"].(string); ok {
			return target
		}
	}
	return ""
}

func (t Template) ThemePath() string {
	if format, ok := t.Frontmatter["format"].(map[string]any); ok {
		if theme, ok := format["theme"].(string); ok {
			return theme
		}
	}
	return ""
}

func (t Template) Summary() string { return t.Name + "  (" + t.Kind + ")  " + t.Description }

type Roots struct {
	ProjectRoot string
	UserRoot    string
}

func DefaultRoots() Roots {
	project := fsx.ProjectRoot()
	if override := os.Getenv("JUICYFRUIT_PROJECT_ROOT"); override != "" {
		project = override
	} else {
		project = repositoryRoot(project)
	}
	user := os.Getenv("JUICYFRUIT_USER_ROOT")
	if user == "" {
		user = fsx.Home()
	}
	if user == "" {
		user = project
	}
	return Roots{ProjectRoot: project, UserRoot: user}
}

var agentDirectoryNames = []string{".claude", ".agents", ".codex", ".cursor", ".gemini/config", ".gemini", ".devin", ".via", ".juicyfruit"}

func SearchDirs(roots Roots) []string {
	out := []string{filepath.Join(roots.ProjectRoot, "templates")}
	for _, name := range agentDirectoryNames {
		out = append(out, filepath.Join(roots.ProjectRoot, filepath.FromSlash(name), "templates"))
	}
	for _, name := range agentDirectoryNames {
		out = append(out, filepath.Join(roots.UserRoot, filepath.FromSlash(name), "templates"))
	}
	return out
}

func repositoryRoot(start string) string {
	origin, err := filepath.Abs(start)
	if err != nil {
		origin = start
	}
	current := origin
	for {
		if fsx.Exists(filepath.Join(current, ".git")) || fsx.Exists(filepath.Join(current, ".juicyfruit")) {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return origin
		}
		current = parent
	}
}

func Parse(markdown, path string, origin Origin) (Template, error) {
	frontmatter, err := readFrontmatter(markdown, path)
	if err != nil {
		return Template{}, err
	}
	if err := Validate(frontmatter, path); err != nil {
		return Template{}, err
	}
	return Template{
		Name: frontmatter["name"].(string), Kind: frontmatter["kind"].(string),
		Description: frontmatter["description"].(string), Frontmatter: frontmatter,
		Body: readBody(markdown), Path: path, Origin: origin,
	}, nil
}

func readFrontmatter(markdown, path string) (map[string]any, error) {
	normalized := strings.ReplaceAll(markdown, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return nil, Errorf("Invalid template %s: missing --- frontmatter block", path)
	}
	end := strings.Index(normalized[4:], "\n---")
	if end < 0 {
		return nil, Errorf("Invalid template %s: unterminated frontmatter block", path)
	}
	end += 4
	yamlText := normalized[4 : end+1]
	var parsed any
	if err := yaml.Unmarshal([]byte(yamlText), &parsed); err != nil {
		return nil, Errorf("Cannot parse template %s: %v", path, err)
	}
	m, ok := normalize(parsed).(map[string]any)
	if !ok {
		return nil, Errorf("Invalid template %s: frontmatter must be a YAML map", path)
	}
	return m, nil
}

func readBody(markdown string) string {
	normalized := strings.ReplaceAll(markdown, "\r\n", "\n")
	end := strings.Index(normalized[4:], "\n---")
	if end < 0 {
		return ""
	}
	end += 4
	afterFence := strings.Index(normalized[end+1:], "\n")
	if afterFence < 0 {
		return ""
	}
	return strings.TrimSpace(normalized[end+1+afterFence+1:])
}

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
var safePathPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

func Validate(frontmatter map[string]any, path string) error {
	fail := func(reason string) error { return Errorf("Invalid template %s: %s", path, reason) }
	known := map[string]bool{"schema_version": true, "name": true, "kind": true, "description": true, "voice": true, "structure": true, "length": true, "format": true, "constraints": true, "examples": true}
	unknown := []string{}
	for key := range frontmatter {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		return fail("unknown frontmatter keys: " + strings.Join(unknown, ", "))
	}
	if frontmatter["schema_version"] != 1 {
		return fail("schema_version must be 1")
	}
	for _, field := range []string{"name", "kind"} {
		value, ok := frontmatter[field].(string)
		if !ok || !idPattern.MatchString(value) || len(value) > 64 {
			return fail(field + " must be a lowercase-hyphen identifier")
		}
	}
	description, ok := frontmatter["description"].(string)
	if !ok || description == "" || len(description) > 1024 {
		return fail("description must be a non-empty string")
	}
	if voice, ok := frontmatter["voice"]; ok && voice != nil {
		m, ok := voice.(map[string]any)
		if !ok {
			return fail("voice must be a map")
		}
		extra := []string{}
		for key := range m {
			if key != "temperature" && key != "person" && key != "greeting" && key != "signoff" {
				extra = append(extra, key)
			}
		}
		if len(extra) > 0 {
			return fail("unknown voice keys: " + strings.Join(extra, ", "))
		}
		if temperature, ok := m["temperature"]; ok && temperature != nil {
			value, ok := temperature.(string)
			if !ok || (value != "formal" && value != "neutral" && value != "warm" && value != "casual") {
				return fail("voice.temperature must be formal, neutral, warm, or casual")
			}
		}
	}
	if format, ok := frontmatter["format"]; ok && format != nil {
		m, ok := format.(map[string]any)
		if !ok {
			return fail("format must be a map")
		}
		extra := []string{}
		for key := range m {
			if key != "target" && key != "theme" {
				extra = append(extra, key)
			}
		}
		if len(extra) > 0 {
			return fail("unknown format keys: " + strings.Join(extra, ", "))
		}
		if target, ok := m["target"]; ok && target != nil {
			value, ok := target.(string)
			valid := map[string]bool{"markdown": true, "plain": true, "email": true, "pptx": true, "marp": true, "gamma": true, "figma-slides": true, "docx": true, "html": true}
			if !ok || !valid[value] {
				return fail("format.target must be one of: markdown, plain, email, pptx, marp, gamma, figma-slides, docx, html")
			}
		}
		if theme, ok := m["theme"]; ok && theme != nil {
			value, ok := theme.(string)
			if !ok || !isSafeRelative(value) {
				return fail("format.theme must be a template-relative path")
			}
		}
	}
	if examples, ok := frontmatter["examples"]; ok && examples != nil {
		list, ok := examples.([]any)
		if !ok {
			return fail("examples must be a list")
		}
		for _, item := range list {
			value, ok := item.(string)
			if !ok || !isSafeRelative(value) {
				return fail("examples entries must be template-relative paths")
			}
		}
	}
	if constraints, ok := frontmatter["constraints"]; ok && constraints != nil {
		m, ok := constraints.(map[string]any)
		if !ok {
			return fail("constraints must be a map")
		}
		extra := []string{}
		for key := range m {
			if key != "always" && key != "never" {
				extra = append(extra, key)
			}
		}
		if len(extra) > 0 {
			return fail("unknown constraints keys: " + strings.Join(extra, ", "))
		}
		for _, key := range []string{"always", "never"} {
			if value, ok := m[key]; ok && value != nil {
				if _, ok := value.([]any); !ok {
					return fail("constraints." + key + " must be a list")
				}
			}
		}
	}
	if length, ok := frontmatter["length"]; ok && length != nil {
		m, ok := length.(map[string]any)
		if !ok {
			return fail("length must be a map")
		}
		extra := []string{}
		for key := range m {
			if key != "min_words" && key != "max_words" && key != "max_chars" && key != "max_slides" {
				extra = append(extra, key)
			}
		}
		if len(extra) > 0 {
			return fail("unknown length keys: " + strings.Join(extra, ", "))
		}
		for key, value := range m {
			if _, ok := value.(int); !ok {
				return fail("length." + key + " must be an integer")
			}
		}
	}
	return nil
}

func isSafeRelative(value string) bool {
	if value == "" || len(value) > 512 || strings.HasPrefix(value, "/") || strings.Contains(value, "//") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return false
		}
	}
	return safePathPattern.MatchString(value)
}

func normalize(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			out[key] = normalize(item)
		}
		return out
	case map[any]any:
		out := map[string]any{}
		for key, item := range v {
			out[fmt.Sprint(key)] = normalize(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for index, item := range v {
			out[index] = normalize(item)
		}
		return out
	default:
		return v
	}
}

type Match struct {
	Template Template
	Reason   string
}

type Store struct{ Roots Roots }

func (s Store) List() ([]Template, error) {
	byName := map[string]Template{}
	for _, dir := range SearchDirs(s.Roots) {
		if !fsx.IsDir(dir) {
			continue
		}
		origin := s.originFor(dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			file := filepath.Join(dir, entry.Name(), "TEMPLATE.md")
			if !fsx.IsFile(file) {
				continue
			}
			data, err := os.ReadFile(file)
			if err != nil {
				return nil, err
			}
			parsed, err := Parse(string(data), s.displayPath(file), origin)
			if err != nil {
				return nil, err
			}
			if _, ok := byName[parsed.Name]; !ok {
				byName[parsed.Name] = parsed
			}
		}
	}
	out := []Template{}
	for _, item := range byName {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s Store) Find(name string) (*Template, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, item := range all {
		if item.Name == name {
			copy := item
			return &copy, nil
		}
	}
	return nil, nil
}

func (s Store) Match(query string, fuzzy bool) (*Match, error) {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return nil, nil
	}
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	for _, item := range all {
		if item.Name == normalized {
			return &Match{Template: item, Reason: "name"}, nil
		}
	}
	byKind := []Template{}
	for _, item := range all {
		if item.Kind == normalized {
			byKind = append(byKind, item)
		}
	}
	sort.SliceStable(byKind, func(i, j int) bool { return byKind[i].Name < byKind[j].Name })
	if len(byKind) > 0 {
		return &Match{Template: byKind[0], Reason: "exact-kind"}, nil
	}
	if !fuzzy {
		return nil, nil
	}
	terms := []string{}
	for _, term := range regexp.MustCompile(`[a-z0-9]+`).FindAllString(normalized, -1) {
		if len(term) > 1 {
			terms = append(terms, term)
		}
	}
	if len(terms) == 0 {
		return nil, nil
	}
	type scored struct {
		Template Template
		Score    int
	}
	scores := []scored{}
	for _, item := range all {
		haystack := strings.ToLower(item.Name + " " + item.Kind + " " + item.Description)
		score := 0
		for _, term := range terms {
			score += strings.Count(haystack, term)
		}
		if score > 0 {
			scores = append(scores, scored{Template: item, Score: score})
		}
	}
	if len(scores) == 0 {
		return nil, nil
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].Score != scores[j].Score {
			return scores[i].Score > scores[j].Score
		}
		return scores[i].Template.Name < scores[j].Template.Name
	})
	return &Match{Template: scores[0].Template, Reason: "keyword"}, nil
}

func (s Store) displayPath(filePath string) string {
	project := s.Roots.ProjectRoot
	if filePath == project || fsx.Within(project, filePath) {
		relative, _ := fsx.PosixRel(project, filePath)
		return relative
	}
	user := s.Roots.UserRoot
	if fsx.Within(user, filePath) {
		relative, _ := fsx.PosixRel(user, filePath)
		return relative
	}
	return filePath
}

func (s Store) originFor(dir string) Origin {
	canonical := filepath.Join(s.Roots.ProjectRoot, "templates")
	if dir == canonical {
		return OriginCanonical
	}
	if fsx.Within(s.Roots.ProjectRoot, dir) {
		return OriginProject
	}
	return OriginUser
}
