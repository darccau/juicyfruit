package memory

import (
	"encoding/json"
	"os"

	"github.com/darccau/juicyfruit/internal/fsx"
)

var HookEvents = []string{"Stop", "SessionEnd"}

type HookWriter struct{}

func (HookWriter) EnsureStopHook(settingsFile, command string) (bool, error) {
	document := map[string]any{}
	if fsx.IsFile(settingsFile) {
		data, err := os.ReadFile(settingsFile)
		if err != nil {
			return false, err
		}
		if err := json.Unmarshal(data, &document); err != nil {
			return false, Errorf("Refusing to edit unparseable settings.json: %s", settingsFile)
		}
	}
	if document == nil {
		return false, Errorf("Refusing to edit unparseable settings.json: %s", settingsFile)
	}
	hooks := map[string]any{}
	if raw, ok := document["hooks"]; ok {
		existing, ok := raw.(map[string]any)
		if !ok {
			return false, Errorf("Refusing to edit unparseable settings.json: %s", settingsFile)
		}
		for key, value := range existing {
			hooks[key] = value
		}
	}
	changed := false
	for _, event := range HookEvents {
		current := []any{}
		if raw, ok := hooks[event]; ok {
			list, ok := raw.([]any)
			if !ok {
				return false, Errorf("Refusing to edit unparseable settings.json: %s", settingsFile)
			}
			current = list
		}
		contains := false
		for _, matcher := range current {
			m, ok := matcher.(map[string]any)
			if !ok {
				continue
			}
			list, ok := m["hooks"].([]any)
			if !ok {
				continue
			}
			for _, hook := range list {
				hm, ok := hook.(map[string]any)
				if ok && hm["command"] == command {
					contains = true
				}
			}
		}
		if contains {
			hooks[event] = current
			continue
		}
		hooks[event] = append(current, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}})
		changed = true
	}
	if !changed {
		return false, nil
	}
	if fsx.IsFile(settingsFile) {
		data, err := os.ReadFile(settingsFile)
		if err != nil {
			return false, err
		}
		if err := os.WriteFile(settingsFile+".juicyfruit-bak", data, 0o644); err != nil {
			return false, err
		}
	}
	document["hooks"] = hooks
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return false, err
	}
	if err := writeAtomic(settingsFile, string(data)+"\n"); err != nil {
		return false, err
	}
	return true, nil
}
