package memory

import (
	"encoding/json"
	"math"
	"os"
	"sort"

	"github.com/darccau/juicyfruit/internal/fsx"
)

type Vector struct {
	ID     string    `json:"id"`
	Path   string    `json:"path"`
	Kind   string    `json:"kind"`
	SHA256 string    `json:"sha256"`
	Values []float64 `json:"values"`
}

type Index struct {
	Version    int      `json:"version"`
	Model      string   `json:"model"`
	Dimensions int      `json:"dimensions"`
	Vectors    []Vector `json:"vectors"`
}

type IndexStore struct{ File string }

func (s IndexStore) Read() (*Index, error) {
	if !fsx.IsFile(s.File) {
		return nil, nil
	}
	var raw map[string]any
	if err := fsx.ReadJSON(s.File, &raw); err != nil {
		return nil, Errorf("Cannot read embedding index: %v", err)
	}
	if raw["version"] != float64(1) {
		return nil, Errorf("Unsupported embedding index format.")
	}
	model, _ := raw["model"].(string)
	dimensions, ok := raw["dimensions"].(float64)
	vectorsRaw, ok2 := raw["vectors"].([]any)
	if !ok || !ok2 || model == "" {
		return nil, Errorf("Unsupported embedding index format.")
	}
	index := &Index{Version: 1, Model: model, Dimensions: int(dimensions), Vectors: []Vector{}}
	for _, item := range vectorsRaw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, Errorf("Invalid embedding index entry.")
		}
		id, _ := m["id"].(string)
		path, _ := m["path"].(string)
		kind, _ := m["kind"].(string)
		digest, _ := m["sha256"].(string)
		valuesRaw, ok := m["values"].([]any)
		if id == "" || path == "" || kind == "" || digest == "" || !ok {
			return nil, Errorf("Invalid embedding index entry.")
		}
		values := make([]float64, 0, len(valuesRaw))
		for _, value := range valuesRaw {
			number, ok := value.(float64)
			if !ok {
				return nil, Errorf("Invalid embedding index vector.")
			}
			values = append(values, number)
		}
		index.Vectors = append(index.Vectors, Vector{ID: id, Path: path, Kind: kind, SHA256: digest, Values: values})
	}
	return index, nil
}

func (s IndexStore) Write(index Index) error {
	index.Version = 1
	if index.Vectors == nil {
		index.Vectors = []Vector{}
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	if err := fsx.WriteAtomic(s.File, append(data, '\n')); err != nil {
		return Errorf("Cannot update embedding index: %v", err)
	}
	return nil
}

type IndexReport struct {
	Embedded int
	Reused   int
	Pruned   int
}

func CosineSimilarity(left, right []float64) float64 {
	if len(left) != len(right) || len(left) == 0 {
		return 0
	}
	dot, leftNorm, rightNorm := 0.0, 0.0, 0.0
	for index := range left {
		dot += left[index] * right[index]
		leftNorm += left[index] * left[index]
		rightNorm += right[index] * right[index]
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

func BuildEmbeddingIndex(client *OllamaClient, store IndexStore, documents []Document, force bool) (IndexReport, error) {
	searchable := []Document{}
	for _, document := range documents {
		if !IsExcludedMemoryPath(document.Path) {
			searchable = append(searchable, document)
		}
	}
	sort.SliceStable(searchable, func(i, j int) bool { return searchable[i].Path < searchable[j].Path })
	current, err := store.Read()
	if err != nil {
		return IndexReport{}, err
	}
	reusable := map[string]Vector{}
	if current != nil && current.Model == client.Model && !force {
		for _, vector := range current.Vectors {
			reusable[vector.Path] = vector
		}
	}
	vectors := []Vector{}
	pending := []Document{}
	digests := map[string]string{}
	reused := 0
	for _, document := range searchable {
		digest := fsx.Digest([]byte(document.Text))
		digests[document.Path] = digest
		if existing, ok := reusable[document.Path]; ok && existing.SHA256 == digest {
			vectors = append(vectors, existing)
			reused++
			continue
		}
		pending = append(pending, document)
	}
	if len(pending) > 0 {
		inputs := make([]string, 0, len(pending))
		for _, document := range pending {
			inputs = append(inputs, document.Text)
		}
		embeddings, err := client.Embed(inputs)
		if err != nil {
			return IndexReport{}, err
		}
		if len(embeddings) != len(pending) {
			return IndexReport{}, Errorf("Embedding provider returned an unexpected number of vectors.")
		}
		for index, document := range pending {
			vectors = append(vectors, Vector{ID: document.Path, Path: document.Path, Kind: string(document.Kind), SHA256: digests[document.Path], Values: embeddings[index]})
		}
	}
	sort.SliceStable(vectors, func(i, j int) bool { return vectors[i].Path < vectors[j].Path })
	paths := map[string]bool{}
	for _, document := range searchable {
		paths[document.Path] = true
	}
	pruned := 0
	if current != nil {
		for _, vector := range current.Vectors {
			if !paths[vector.Path] {
				pruned++
			}
		}
	}
	dimensions := 0
	if len(vectors) > 0 {
		dimensions = len(vectors[0].Values)
	}
	if err := store.Write(Index{Model: client.Model, Dimensions: dimensions, Vectors: vectors}); err != nil {
		return IndexReport{}, err
	}
	return IndexReport{Embedded: len(pending), Reused: reused, Pruned: pruned}, nil
}

func EmbeddingSearch(client *OllamaClient, index *Index, documents []Document, query string, limit int) ([]Hit, error) {
	byPath := map[string]Document{}
	for _, document := range documents {
		byPath[document.Path] = document
	}
	if len(index.Vectors) == 0 || len(byPath) == 0 {
		return []Hit{}, nil
	}
	embeddings, err := client.Embed([]string{query})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, Errorf("Embedding provider returned no vectors.")
	}
	queryVector := embeddings[0]
	hits := []Hit{}
	for _, vector := range index.Vectors {
		document, ok := byPath[vector.Path]
		if !ok {
			continue
		}
		score := CosineSimilarity(queryVector, vector.Values)
		if score <= 0 {
			continue
		}
		hits = append(hits, Hit{Path: document.Path, Title: document.Title, Excerpt: vectorExcerpt(document.Text), Score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Path < hits[j].Path
	})
	return limitHits(hits, limit), nil
}

func vectorExcerpt(text string) string {
	collapsed := whitespacePattern.Split(text, -1)
	words := []string{}
	for _, word := range collapsed {
		if word != "" {
			words = append(words, word)
		}
	}
	joined := ""
	for index, word := range words {
		if index > 0 {
			joined += " "
		}
		joined += word
	}
	if len(joined) <= 240 {
		return joined
	}
	return joined[:240]
}

func removeFile(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
