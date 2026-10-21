package source

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/darccau/juicyfruit/internal/fsx"
)

type Registry struct{ Path string }

type Check struct {
	Source  Registered
	Catalog Catalog
	Err     error
}

var registryNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func NewRegistry(path string) *Registry { return &Registry{Path: path} }

func DefaultRegistryPath() string { return filepath.Join(fsx.ConfigHome(), "sources.json") }

func (r *Registry) Load() ([]Registered, error) {
	if !fsx.Exists(r.Path) {
		return []Registered{}, nil
	}
	var raw map[string]any
	if err := fsx.ReadJSON(r.Path, &raw); err != nil {
		return nil, Errorf("Cannot read source registry: %v", err)
	}
	if raw["version"] != float64(1) && raw["version"] != 1 {
		return nil, Errorf("Unsupported source registry version.")
	}
	list, ok := raw["sources"].([]any)
	if !ok {
		return nil, Errorf("Invalid source registry.")
	}
	sources := make([]Registered, 0, len(list))
	names := map[string]bool{}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, Errorf("Invalid source registry entry.")
		}
		entry, err := decodeRegistered(m)
		if err != nil {
			return nil, err
		}
		if names[entry.Name] {
			return nil, Errorf("Duplicate source name in registry: %s", entry.Name)
		}
		names[entry.Name] = true
		sources = append(sources, entry)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	return sources, nil
}

func (r *Registry) Save(sources []Registered) error {
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	payload := map[string]any{"version": 1, "sources": sources}
	return fsx.WriteJSON(r.Path, payload)
}

func ValidateSourceName(name string) error {
	if !registryNamePattern.MatchString(name) {
		return Errorf("Invalid source name: %s", name)
	}
	return nil
}

func UniqueSourceName(requested, path string, existing []Registered) string {
	if requested == "" {
		requested = filepath.Base(filepath.Clean(path))
	}
	name := strings.ToLower(requested)
	if ValidateSourceName(name) != nil {
		name = "source"
	}
	names := map[string]bool{}
	for _, entry := range existing {
		names[entry.Name] = true
	}
	if !names[name] {
		return name
	}
	for i := 2; ; i++ {
		candidate := name + "-" + itoa(i)
		if !names[candidate] {
			return candidate
		}
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

func (r *Registry) Find(nameOrID string) (Registered, error) {
	sources, err := r.Load()
	if err != nil {
		return Registered{}, err
	}
	for _, entry := range sources {
		if entry.Name == nameOrID || entry.SourceID == nameOrID {
			return entry, nil
		}
	}
	return Registered{}, Errorf("Unknown source: %s", nameOrID)
}

func (r *Registry) AddLocal(directory, requestedName string) (Registered, Catalog, error) {
	root, err := fsx.ResolveDir(directory)
	if err != nil {
		return Registered{}, Catalog{}, Errorf("Source directory does not exist: %s", directory)
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		return Registered{}, Catalog{}, err
	}
	sources, err := r.Load()
	if err != nil {
		return Registered{}, Catalog{}, err
	}
	for _, entry := range sources {
		if entry.SourceID == catalog.ID {
			return Registered{}, Catalog{}, Errorf("Source id is already registered: %s", catalog.ID)
		}
	}
	name := UniqueSourceName(requestedName, root, sources)
	entry := Registered{Name: name, Kind: KindLocal, Location: root, SourceID: catalog.ID, SourceName: catalog.Name}
	sources = append(sources, entry)
	if err := r.Save(sources); err != nil {
		return Registered{}, Catalog{}, err
	}
	return entry, catalog, nil
}

func (r *Registry) AddSnapshot(snapshot Snapshot, requestedName string) (Registered, Catalog, error) {
	catalog, err := LoadCatalog(snapshot.Root)
	if err != nil {
		return Registered{}, Catalog{}, err
	}
	sources, err := r.Load()
	if err != nil {
		return Registered{}, Catalog{}, err
	}
	for _, entry := range sources {
		if entry.SourceID == catalog.ID {
			return Registered{}, Catalog{}, Errorf("Source id is already registered: %s", catalog.ID)
		}
	}
	name := UniqueSourceName(requestedName, snapshot.Root, sources)
	transport := snapshot.Transport
	entry := Registered{Name: name, Kind: Kind(transport.Kind), Location: snapshot.Root, SourceID: catalog.ID, SourceName: catalog.Name, Transport: &transport}
	sources = append(sources, entry)
	if err := r.Save(sources); err != nil {
		return Registered{}, Catalog{}, err
	}
	return entry, catalog, nil
}

func (r *Registry) Replace(nameOrID, directory string) (Registered, Catalog, error) {
	root, err := fsx.ResolveDir(directory)
	if err != nil {
		return Registered{}, Catalog{}, Errorf("Source directory does not exist: %s", directory)
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		return Registered{}, Catalog{}, err
	}
	sources, err := r.Load()
	if err != nil {
		return Registered{}, Catalog{}, err
	}
	index := -1
	for i, entry := range sources {
		if entry.Name == nameOrID || entry.SourceID == nameOrID {
			index = i
			break
		}
	}
	if index < 0 {
		return Registered{}, Catalog{}, Errorf("Unknown source: %s", nameOrID)
	}
	if catalog.ID != sources[index].SourceID {
		return Registered{}, Catalog{}, Errorf("Replacement source id does not match: %s", catalog.ID)
	}
	entry := sources[index]
	entry.Kind = KindLocal
	entry.Location = root
	entry.SourceName = catalog.Name
	entry.Transport = nil
	sources[index] = entry
	if err := r.Save(sources); err != nil {
		return Registered{}, Catalog{}, err
	}
	return entry, catalog, nil
}

func (r *Registry) ReplaceSnapshot(nameOrID string, snapshot Snapshot) (Registered, Catalog, error) {
	catalog, err := LoadCatalog(snapshot.Root)
	if err != nil {
		return Registered{}, Catalog{}, err
	}
	sources, err := r.Load()
	if err != nil {
		return Registered{}, Catalog{}, err
	}
	index := -1
	for i, entry := range sources {
		if entry.Name == nameOrID || entry.SourceID == nameOrID {
			index = i
			break
		}
	}
	if index < 0 {
		return Registered{}, Catalog{}, Errorf("Unknown source: %s", nameOrID)
	}
	if catalog.ID != sources[index].SourceID {
		return Registered{}, Catalog{}, Errorf("Replacement source id does not match: %s", catalog.ID)
	}
	entry := sources[index]
	transport := snapshot.Transport
	entry.Kind = Kind(transport.Kind)
	entry.Location = snapshot.Root
	entry.SourceName = catalog.Name
	entry.Transport = &transport
	sources[index] = entry
	if err := r.Save(sources); err != nil {
		return Registered{}, Catalog{}, err
	}
	return entry, catalog, nil
}

func (r *Registry) Remove(nameOrID string) (Registered, error) {
	sources, err := r.Load()
	if err != nil {
		return Registered{}, err
	}
	index := -1
	for i, entry := range sources {
		if entry.Name == nameOrID || entry.SourceID == nameOrID {
			index = i
			break
		}
	}
	if index < 0 {
		return Registered{}, Errorf("Unknown source: %s", nameOrID)
	}
	removed := sources[index]
	sources = append(sources[:index], sources[index+1:]...)
	if err := r.Save(sources); err != nil {
		return Registered{}, err
	}
	return removed, nil
}

func (r *Registry) Test() ([]Check, error) {
	sources, err := r.Load()
	if err != nil {
		return nil, err
	}
	checks := make([]Check, 0, len(sources))
	for _, entry := range sources {
		check := Check{Source: entry}
		catalog, err := LoadCatalog(entry.Location)
		if err != nil {
			check.Err = err
		} else if catalog.ID != entry.SourceID {
			check.Err = Errorf("Source id mismatch: expected %s, got %s", entry.SourceID, catalog.ID)
		} else {
			check.Catalog = catalog
		}
		checks = append(checks, check)
	}
	return checks, nil
}

func (r *Registry) Refresh(nameOrID string) (Refresh, error) {
	entry, err := r.Find(nameOrID)
	if err != nil {
		return Refresh{}, err
	}
	if entry.Kind == KindLocal {
		catalog, err := LoadCatalog(entry.Location)
		if err != nil {
			return Refresh{}, err
		}
		if catalog.ID != entry.SourceID {
			return Refresh{}, Errorf("Source id mismatch: expected %s, got %s", entry.SourceID, catalog.ID)
		}
		if catalog.Name != entry.SourceName {
			entry.SourceName = catalog.Name
			if err := r.update(entry); err != nil {
				return Refresh{}, err
			}
		}
		return Refresh{Kind: RefreshLive, Source: entry}, nil
	}
	if entry.Transport == nil {
		return Refresh{}, Errorf("Source transport metadata is missing.")
	}
	if entry.Kind == KindGit && commitPattern.MatchString(entry.Transport.Revision) {
		return Refresh{Kind: RefreshPinned, Source: entry, PreviousRevision: entry.Transport.ResolvedRevision}, nil
	}
	cache := NewSnapshotCache(fsx.CacheHome())
	var snapshot Snapshot
	if entry.Kind == KindGit {
		snapshot, err = cache.FetchGit(entry.Transport.URL, entry.Transport.Revision)
	} else {
		snapshot, err = cache.FetchArchive(entry.Transport.URL, entry.Transport.SHA256)
	}
	if err != nil {
		return Refresh{}, err
	}
	previous := entry.Transport.ResolvedRevision
	kind := RefreshUpdated
	if snapshot.CacheKey == filepath.Base(entry.Location) {
		kind = RefreshUnchanged
	}
	updated, _, err := r.ReplaceSnapshot(entry.Name, snapshot)
	if err != nil {
		return Refresh{}, err
	}
	if kind != RefreshUpdated {
		return Refresh{Kind: RefreshUnchanged, Source: updated, PreviousRevision: previous, NewRevision: previous}, nil
	}
	return Refresh{Kind: kind, Source: updated, PreviousRevision: previous, NewRevision: snapshot.Transport.ResolvedRevision}, nil
}

func (r *Registry) update(entry Registered) error {
	sources, err := r.Load()
	if err != nil {
		return err
	}
	for i := range sources {
		if sources[i].Name == entry.Name {
			sources[i] = entry
		}
	}
	return r.Save(sources)
}

func ReadRegistryRaw(path string) (map[string]any, error) {
	if !fsx.Exists(path) {
		return map[string]any{"version": 1, "sources": []any{}}, nil
	}
	var raw map[string]any
	if err := fsx.ReadJSON(path, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func Timestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func ensureDir(path string) error { return os.MkdirAll(path, 0o755) }
