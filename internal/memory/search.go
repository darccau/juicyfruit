package memory

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	bm25K1 = 1.5
	bm25B  = 0.75
)

var termPattern = regexp.MustCompile(`[\p{L}\p{N}_]+`)
var whitespacePattern = regexp.MustCompile(`\s+`)

func KeywordSearch(documents []Document, query string, limit int) []Hit {
	terms := []string{}
	for _, term := range termPattern.FindAllString(strings.ToLower(query), -1) {
		if utf8.RuneCountInString(term) > 1 {
			terms = append(terms, term)
		}
	}
	if len(terms) == 0 {
		return []Hit{}
	}
	corpus := []Document{}
	for _, document := range documents {
		if !IsExcludedMemoryPath(document.Path) {
			corpus = append(corpus, document)
		}
	}
	if len(corpus) == 0 {
		return []Hit{}
	}
	haystacks := map[string]string{}
	lengths := map[string]int{}
	totalLength := 0
	for _, document := range corpus {
		source := stem(document.Path) + "\n" + document.Text
		haystack := strings.ToLower(source)
		haystacks[document.Path] = haystack
		length := len(termPattern.FindAllStringIndex(haystack, -1))
		lengths[document.Path] = length
		totalLength += length
	}
	averageLength := float64(totalLength) / float64(len(corpus))
	df := map[string]int{}
	for _, term := range terms {
		for _, document := range corpus {
			if strings.Contains(haystacks[document.Path], term) {
				df[term]++
			}
		}
	}
	n := len(corpus)
	idf := map[string]float64{}
	for _, term := range terms {
		if _, ok := idf[term]; ok {
			continue
		}
		idf[term] = math.Log(1 + (float64(n-df[term])+0.5)/(float64(df[term])+0.5))
	}
	hits := []Hit{}
	for _, document := range corpus {
		haystack := haystacks[document.Path]
		length := float64(lengths[document.Path])
		score := 0.0
		matched := false
		for _, term := range terms {
			frequency := float64(strings.Count(haystack, term))
			if frequency == 0 {
				continue
			}
			matched = true
			normalized := frequency * (bm25K1 + 1)
			lengthNorm := frequency + (bm25K1 * (1 - bm25B + (bm25B * length / averageLength)))
			score += idf[term] * (normalized / lengthNorm)
		}
		if !matched || score <= 0 {
			continue
		}
		position := -1
		for _, term := range terms {
			if index := strings.Index(haystack, term); index >= 0 && (position < 0 || index < position) {
				position = index
			}
		}
		if position < 0 {
			position = 0
		}
		hits = append(hits, Hit{Path: document.Path, Title: document.Title, Excerpt: excerptAround(stem(document.Path)+"\n"+document.Text, position), Score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Path < hits[j].Path
	})
	return limitHits(hits, limit)
}

func CombineHybridHits(keywordHits, vectorHits []Hit, vectorWeight float64, limit int) []Hit {
	weight := clamp(vectorWeight, 0, 1)
	if weight <= 0 {
		return limitHits(keywordHits, limit)
	}
	if weight >= 1 {
		return limitHits(vectorHits, limit)
	}
	keywordScores := normalizedScores(keywordHits)
	vectorScores := normalizedScores(vectorHits)
	byPath := map[string]Hit{}
	order := []string{}
	for _, hit := range keywordHits {
		if _, ok := byPath[hit.Path]; !ok {
			order = append(order, hit.Path)
		}
		byPath[hit.Path] = hit
	}
	for _, hit := range vectorHits {
		if _, ok := byPath[hit.Path]; !ok {
			order = append(order, hit.Path)
			byPath[hit.Path] = hit
		}
	}
	blended := []Hit{}
	for _, path := range order {
		combined := (1-weight)*keywordScores[path] + weight*vectorScores[path]
		if combined <= 0 {
			continue
		}
		blended = append(blended, byPath[path].WithScore(combined))
	}
	sort.SliceStable(blended, func(i, j int) bool {
		if blended[i].Score != blended[j].Score {
			return blended[i].Score > blended[j].Score
		}
		return blended[i].Path < blended[j].Path
	})
	return limitHits(blended, limit)
}

func normalizedScores(hits []Hit) map[string]float64 {
	out := map[string]float64{}
	if len(hits) == 0 {
		return out
	}
	maxScore := hits[0].Score
	for _, hit := range hits[1:] {
		if hit.Score > maxScore {
			maxScore = hit.Score
		}
	}
	if maxScore <= 0 {
		return out
	}
	for _, hit := range hits {
		out[hit.Path] = hit.Score / maxScore
	}
	return out
}

func limitHits(hits []Hit, limit int) []Hit {
	if limit < 1 {
		limit = 1
	}
	if limit > 20 {
		limit = 20
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return append([]Hit{}, hits...)
}

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func excerptAround(source string, position int) string {
	start := position - 80
	if start < 0 {
		start = 0
	}
	end := position + 240
	if end > len(source) {
		end = len(source)
	}
	if start > len(source) {
		start = len(source)
	}
	if end < start {
		end = start
	}
	parts := whitespacePattern.Split(source[start:end], -1)
	words := []string{}
	for _, part := range parts {
		if part != "" {
			words = append(words, part)
		}
	}
	return strings.Join(words, " ")
}

func stem(path string) string {
	parts := strings.Split(path, "/")
	name := parts[len(parts)-1]
	dot := strings.LastIndex(name, ".")
	if dot <= 0 {
		return name
	}
	return name[:dot]
}
