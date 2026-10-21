package memory

import (
	"fmt"
	"strings"
	"time"
)

type Error struct{ Message string }

func (e *Error) Error() string                { return e.Message }
func Errorf(format string, args ...any) error { return &Error{Message: fmt.Sprintf(format, args...)} }

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

type EntryKind string

const (
	KindNote     EntryKind = "note"
	KindActivity EntryKind = "activity"
)

type EmbeddingsConfig struct {
	Enabled    bool   `json:"enabled"`
	Provider   string `json:"provider"`
	BaseURL    string `json:"baseUrl"`
	Model      string `json:"model"`
	Dimensions *int   `json:"dimensions,omitempty"`
}

func DefaultEmbeddingsConfig() EmbeddingsConfig {
	return EmbeddingsConfig{Enabled: false, Provider: "ollama", BaseURL: "http://127.0.0.1:11434", Model: "nomic-embed-text"}
}

type CaptureConfig struct {
	SessionEndHook bool     `json:"sessionEndHook"`
	GitDiffStat    bool     `json:"gitDiffStat"`
	Targets        []string `json:"targets"`
}

func DefaultCaptureConfig() CaptureConfig {
	return CaptureConfig{SessionEndHook: false, GitDiffStat: true, Targets: []string{}}
}

type Config struct {
	Version      int              `json:"version"`
	Embeddings   EmbeddingsConfig `json:"embeddings"`
	Capture      CaptureConfig    `json:"capture"`
	DefaultScope Scope            `json:"defaultScope"`
}

func DefaultConfig() Config {
	return Config{Version: 1, Embeddings: DefaultEmbeddingsConfig(), Capture: DefaultCaptureConfig(), DefaultScope: ScopeUser}
}

var CaptureTargets = map[string]bool{
	"codex": true, "claude-code": true, "cursor": true, "antigravity": true,
	"devin": true, "generic": true, "gemini-cli": true, "via": true,
}

func ConfigFromMap(raw map[string]any) (Config, error) {
	cfg := Config{Version: 1}
	for key := range raw {
		if key != "version" && key != "embeddings" && key != "capture" && key != "defaultScope" {
			return Config{}, Errorf("Unknown memory configuration key: %s", key)
		}
	}
	if raw["version"] != float64(1) {
		return Config{}, Errorf("Unsupported memory configuration format.")
	}
	embeddings, ok := raw["embeddings"].(map[string]any)
	if !ok {
		return Config{}, Errorf("Invalid memory configuration body.")
	}
	for key := range embeddings {
		if key != "enabled" && key != "provider" && key != "baseUrl" && key != "model" && key != "dimensions" {
			return Config{}, Errorf("Unknown embeddings key: %s", key)
		}
	}
	enabled, ok := embeddings["enabled"].(bool)
	if !ok {
		return Config{}, Errorf("Invalid embeddings.enabled value.")
	}
	cfg.Embeddings.Enabled = enabled
	provider := "ollama"
	if value, ok := embeddings["provider"].(string); ok {
		provider = value
	} else if embeddings["provider"] != nil {
		return Config{}, Errorf("Invalid embeddings.provider value.")
	}
	if provider != "ollama" {
		return Config{}, Errorf("Invalid embeddings.provider value.")
	}
	cfg.Embeddings.Provider = provider
	baseURL := "http://127.0.0.1:11434"
	if value, ok := embeddings["baseUrl"].(string); ok {
		baseURL = value
	} else if embeddings["baseUrl"] != nil {
		return Config{}, Errorf("Invalid embeddings.baseUrl value.")
	}
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return Config{}, Errorf("Invalid embeddings.baseUrl value.")
	}
	cfg.Embeddings.BaseURL = baseURL
	model := "nomic-embed-text"
	if value, ok := embeddings["model"].(string); ok {
		model = value
	} else if embeddings["model"] != nil {
		return Config{}, Errorf("Invalid embeddings.model value.")
	}
	if model == "" {
		return Config{}, Errorf("Invalid embeddings.model value.")
	}
	cfg.Embeddings.Model = model
	if value, ok := embeddings["dimensions"]; ok && value != nil {
		number, ok := value.(float64)
		if !ok || number < 1 || number != float64(int(number)) {
			return Config{}, Errorf("Invalid embeddings.dimensions value.")
		}
		dimensions := int(number)
		cfg.Embeddings.Dimensions = &dimensions
	}
	capture, ok := raw["capture"].(map[string]any)
	if !ok {
		return Config{}, Errorf("Invalid memory configuration body.")
	}
	for key := range capture {
		if key != "sessionEndHook" && key != "gitDiffStat" && key != "targets" {
			return Config{}, Errorf("Unknown capture key: %s", key)
		}
	}
	hook, ok := capture["sessionEndHook"].(bool)
	if !ok {
		return Config{}, Errorf("Invalid capture.sessionEndHook value.")
	}
	cfg.Capture.SessionEndHook = hook
	cfg.Capture.GitDiffStat = true
	if value, ok := capture["gitDiffStat"]; ok {
		boolean, ok := value.(bool)
		if !ok {
			return Config{}, Errorf("Invalid capture.gitDiffStat value.")
		}
		cfg.Capture.GitDiffStat = boolean
	}
	cfg.Capture.Targets = []string{}
	if value, ok := capture["targets"]; ok {
		list, ok := value.([]any)
		if !ok {
			return Config{}, Errorf("Invalid capture.targets value.")
		}
		seen := map[string]bool{}
		for _, item := range list {
			target, ok := item.(string)
			if !ok || !CaptureTargets[target] || seen[target] {
				return Config{}, Errorf("Invalid capture.targets value.")
			}
			seen[target] = true
			cfg.Capture.Targets = append(cfg.Capture.Targets, target)
		}
	}
	switch raw["defaultScope"] {
	case "user":
		cfg.DefaultScope = ScopeUser
	case "project":
		cfg.DefaultScope = ScopeProject
	default:
		return Config{}, Errorf("Invalid defaultScope value.")
	}
	return cfg, nil
}

type Entry struct {
	At         time.Time
	Kind       EntryKind
	Tags       []string
	Message    string
	ScopeLabel string
}

type Note struct {
	Slug  string
	Title string
	Body  string
	At    time.Time
	Tags  []string
}

type Document struct {
	Path  string
	Kind  EntryKind
	Title string
	Text  string
	At    *time.Time
}

type Hit struct {
	Path       string  `json:"path"`
	Title      string  `json:"title"`
	Excerpt    string  `json:"excerpt"`
	Score      float64 `json:"score"`
	ScopeLabel string  `json:"-"`
}

func (h Hit) WithScopeLabel(label string) Hit { h.ScopeLabel = label; return h }
func (h Hit) WithScore(score float64) Hit     { h.Score = score; return h }

type CompactReport struct {
	ArchivedDays    int
	ArchivedEntries int
	NotePath        string
}

func IsExcludedMemoryPath(path string) bool {
	segments := strings.Split(path, "/")
	if len(segments) > 0 && segments[0] == "index" {
		return true
	}
	for _, segment := range segments {
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}
