package pkg

import (
	"path/filepath"
	"sort"

	"github.com/darccau/juicyfruit/internal/fsx"
	"github.com/darccau/juicyfruit/internal/source"
)

type Catalog struct{ Registry *source.Registry }

func NewCatalog(registry *source.Registry) *Catalog { return &Catalog{Registry: registry} }

func (c *Catalog) Discover() ([]Candidate, error) {
	registered, err := c.Registry.Load()
	if err != nil {
		return nil, err
	}
	var candidates []Candidate
	for _, entry := range registered {
		catalog, err := source.LoadCatalog(entry.Location)
		if err != nil {
			return nil, Errorf("%s", err)
		}
		if catalog.ID != entry.SourceID {
			return nil, Errorf("Source identity changed from %s to %s.", entry.SourceID, catalog.ID)
		}
		for _, summary := range catalog.Packages {
			packageFile := filepath.Join(catalog.Root, fsx.FromUnix(summary.Path))
			packageRoot := filepath.Dir(packageFile)
			manifest, err := LoadManifest(packageRoot)
			if err != nil {
				return nil, err
			}
			if manifest.ID != summary.ID || manifest.Version != summary.Version {
				return nil, Errorf("Package summary differs from manifest: %s", summary.Path)
			}
			digest, err := PackageDigest(packageRoot, catalog.Root, manifest)
			if err != nil {
				return nil, Errorf("%s", err)
			}
			candidates = append(candidates, Candidate{
				SourceName: entry.Name, SourceRoot: catalog.Root,
				PackageRoot: packageRoot, Manifest: manifest, Digest: digest,
			})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return compareCandidates(candidates[i], candidates[j]) < 0 })
	return candidates, nil
}
