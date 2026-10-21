package source

import (
	"encoding/json"
	"fmt"
)

type Kind string

const (
	KindLocal   Kind = "local"
	KindGit     Kind = "git"
	KindArchive Kind = "archive"
)

type Transport struct {
	Kind             string `json:"kind"`
	URL              string `json:"url"`
	Revision         string `json:"revision,omitempty"`
	ResolvedRevision string `json:"resolved_revision,omitempty"`
	SHA256           string `json:"sha256,omitempty"`
}

type Package struct {
	ID          string
	Path        string
	Version     string
	Description string
}

type Catalog struct {
	ID       string
	Name     string
	Root     string
	Packages []Package
}

type Registered struct {
	Name       string     `json:"name"`
	Kind       Kind       `json:"kind"`
	Location   string     `json:"location"`
	SourceID   string     `json:"source_id"`
	SourceName string     `json:"source_name"`
	Transport  *Transport `json:"transport,omitempty"`
}

type RefreshKind string

const (
	RefreshLive      RefreshKind = "live"
	RefreshUnchanged RefreshKind = "unchanged"
	RefreshUpdated   RefreshKind = "updated"
	RefreshPinned    RefreshKind = "pinned"
)

type Refresh struct {
	Kind             RefreshKind
	Source           Registered
	PreviousRevision string
	NewRevision      string
}

type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

func Errorf(format string, args ...any) error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

func decodeRegistered(raw map[string]any) (Registered, error) {
	getString := func(key string) (string, error) {
		value, ok := raw[key].(string)
		if !ok {
			return "", Errorf("Invalid source registry entry.")
		}
		return value, nil
	}
	name, err := getString("name")
	if err != nil {
		return Registered{}, err
	}
	kindText, err := getString("kind")
	if err != nil {
		return Registered{}, err
	}
	location, err := getString("location")
	if err != nil {
		return Registered{}, err
	}
	sourceID, err := getString("source_id")
	if err != nil {
		return Registered{}, err
	}
	sourceName, err := getString("source_name")
	if err != nil {
		return Registered{}, err
	}
	kind := Kind(kindText)
	if kind != KindLocal && kind != KindGit && kind != KindArchive {
		return Registered{}, Errorf("Invalid source registry kind.")
	}
	var transport *Transport
	if rawTransport, ok := raw["transport"]; ok && rawTransport != nil {
		m, ok := rawTransport.(map[string]any)
		if !ok {
			return Registered{}, Errorf("Invalid source transport metadata.")
		}
		encoded, _ := json.Marshal(m)
		var t Transport
		if err := json.Unmarshal(encoded, &t); err != nil {
			return Registered{}, Errorf("Invalid source transport metadata.")
		}
		if t.Kind == "" || t.URL == "" {
			return Registered{}, Errorf("Invalid source transport metadata.")
		}
		if t.Kind == string(KindGit) && (t.Revision == "" || t.ResolvedRevision == "") {
			return Registered{}, Errorf("Git source metadata is incomplete.")
		}
		if t.Kind == string(KindArchive) && t.SHA256 == "" {
			return Registered{}, Errorf("Archive source metadata is incomplete.")
		}
		transport = &t
	}
	if kind == KindLocal && transport != nil {
		return Registered{}, Errorf("Invalid source registry transport.")
	}
	if kind != KindLocal && (transport == nil || transport.Kind != string(kind)) {
		return Registered{}, Errorf("Invalid source registry transport.")
	}
	return Registered{Name: name, Kind: kind, Location: location, SourceID: sourceID, SourceName: sourceName, Transport: transport}, nil
}
