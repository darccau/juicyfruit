package memory

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/darccau/juicyfruit/internal/fsx"
)

type Store struct {
	Directory string
	Now       func() time.Time
}

var headingPattern = regexp.MustCompile(`^## (\d{2}):(\d{2}):(\d{2}) ([a-z]+) \[([^\]]*)\]$`)
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var dateNamePattern = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})`)
var nonSlugPattern = regexp.MustCompile(`[^a-z0-9]+`)
var trimDashPattern = regexp.MustCompile(`^-+|-+$`)

func NewStore(directory string) *Store { return &Store{Directory: directory, Now: time.Now} }

func (s *Store) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func (s *Store) ConfigFile() string         { return filepath.Join(s.Directory, "config.json") }
func (s *Store) IndexFile() string          { return filepath.Join(s.Directory, "MEMORY.md") }
func (s *Store) JournalDirectory() string   { return filepath.Join(s.Directory, "journal") }
func (s *Store) NotesDirectory() string     { return filepath.Join(s.Directory, "notes") }
func (s *Store) ArchiveDirectory() string   { return filepath.Join(s.JournalDirectory(), ".archive") }
func (s *Store) GeneratedDirectory() string { return filepath.Join(s.Directory, "index") }
func (s *Store) EmbeddingIndexFile() string {
	return filepath.Join(s.GeneratedDirectory(), "embeddings.json")
}

func (s *Store) EnsureSkeleton() error {
	for _, dir := range []string{s.Directory, s.JournalDirectory(), s.NotesDirectory(), s.GeneratedDirectory()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if !fsx.IsFile(s.ConfigFile()) {
		if err := s.WriteConfig(DefaultConfig()); err != nil {
			return err
		}
	}
	return s.RegenerateIndex()
}

func (s *Store) ReadConfig() (Config, error) {
	if !fsx.IsFile(s.ConfigFile()) {
		return DefaultConfig(), nil
	}
	data, err := os.ReadFile(s.ConfigFile())
	if err != nil {
		return Config{}, Errorf("Cannot read memory configuration: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, Errorf("Cannot read memory configuration: %v", err)
	}
	return ConfigFromMap(raw)
}

func (s *Store) WriteConfig(config Config) error {
	config.Version = 1
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := fsx.WriteAtomic(s.ConfigFile(), append(data, '\n')); err != nil {
		return Errorf("Cannot update memory configuration: %v", err)
	}
	return nil
}

func (s *Store) AppendActivity(message string, tags []string, at *time.Time) (Entry, error) {
	return s.append(KindActivity, message, tags, at)
}

func (s *Store) AppendNote(message string, tags []string, at *time.Time) (Entry, error) {
	return s.append(KindNote, message, tags, at)
}

func (s *Store) append(kind EntryKind, message string, tags []string, at *time.Time) (Entry, error) {
	body := strings.TrimSpace(message)
	if body == "" {
		return Entry{}, Errorf("A memory entry cannot be empty.")
	}
	moment := s.now()
	if at != nil {
		moment = *at
	}
	file := s.JournalFileFor(moment)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return Entry{}, err
	}
	existing := "# Journal " + formatDate(moment) + "\n"
	if fsx.IsFile(file) {
		data, err := os.ReadFile(file)
		if err != nil {
			return Entry{}, err
		}
		existing = string(data)
	}
	heading := "## " + formatTime(moment, true) + " " + string(kind) + " [" + strings.Join(tags, ",") + "]"
	if err := writeAtomic(file, existing+"\n"+heading+"\n\n"+body+"\n"); err != nil {
		return Entry{}, err
	}
	if err := s.RegenerateIndex(); err != nil {
		return Entry{}, err
	}
	return Entry{At: moment, Kind: kind, Tags: append([]string{}, tags...), Message: body}, nil
}

func (s *Store) JournalFileFor(moment time.Time) string {
	return filepath.Join(s.JournalDirectory(), pad(moment.Year(), 4), formatDate(moment)+".md")
}

func (s *Store) WriteNote(title, body string, tags []string, at *time.Time) (Note, error) {
	moment := s.now()
	if at != nil {
		moment = *at
	}
	slugTitle := slugify(title)
	if slugTitle == "" {
		return Note{}, Errorf("Cannot derive a note slug from title: %s", title)
	}
	slug := formatDate(moment) + "-" + slugTitle
	path := filepath.Join(s.NotesDirectory(), slug+".md")
	if fsx.Exists(path) {
		return Note{}, Errorf("Note already exists: %s", slug)
	}
	tagLine := ""
	if len(tags) > 0 {
		tagLine = "tags: " + strings.Join(tags, ", ") + "\n"
	}
	contents := "# " + title + "\n\ndate: " + formatDate(moment) + "\n" + tagLine + "\n" + strings.TrimSpace(body) + "\n"
	if err := writeAtomic(path, contents); err != nil {
		return Note{}, err
	}
	if err := s.RegenerateIndex(); err != nil {
		return Note{}, err
	}
	return Note{Slug: slug, Title: title, Body: strings.TrimSpace(body), At: moment, Tags: append([]string{}, tags...)}, nil
}

func (s *Store) LoadAll() ([]Document, error) {
	documents := []Document{}
	roots := []struct {
		kind EntryKind
		dir  string
	}{{KindNote, s.NotesDirectory()}, {KindActivity, s.JournalDirectory()}}
	for _, root := range roots {
		if !fsx.IsDir(root.dir) {
			continue
		}
		files, err := markdownFiles(root.dir, true)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			relative, err := fsx.PosixRel(s.Directory, file)
			if err != nil {
				return nil, err
			}
			if IsExcludedMemoryPath(relative) {
				continue
			}
			data, err := os.ReadFile(file)
			if err != nil {
				return nil, err
			}
			text := string(data)
			name := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
			at := dateFromName(name)
			documents = append(documents, Document{Path: relative, Kind: root.kind, Title: titleOf(text, name), Text: text, At: at})
		}
	}
	sort.SliceStable(documents, func(i, j int) bool { return documents[i].Path < documents[j].Path })
	return documents, nil
}

func (s *Store) ListActivities(since *time.Time, limit *int) ([]Entry, error) {
	entries, err := s.readJournal()
	if err != nil {
		return nil, err
	}
	filtered := []Entry{}
	for _, entry := range entries {
		if since == nil || !entry.At.Before(*since) {
			filtered = append(filtered, entry)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].At.After(filtered[j].At) })
	if limit != nil {
		count := *limit
		if count < 0 {
			count = 0
		}
		if count < len(filtered) {
			filtered = filtered[:count]
		}
	}
	return filtered, nil
}

func (s *Store) ListNotes(since *time.Time, limit *int) ([]Note, error) {
	documents, err := s.LoadAll()
	if err != nil {
		return nil, err
	}
	notes := []Note{}
	for _, document := range documents {
		if document.Kind != KindNote {
			continue
		}
		at := time.Unix(0, 0)
		if document.At != nil {
			at = *document.At
		}
		if since != nil && at.Before(*since) {
			continue
		}
		notes = append(notes, Note{Slug: strings.TrimSuffix(filepath.Base(document.Path), filepath.Ext(document.Path)), Title: document.Title, Body: noteBody(document.Text), At: at, Tags: noteTags(document.Text)})
	}
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].At.After(notes[j].At) })
	if limit != nil {
		count := *limit
		if count < 0 {
			count = 0
		}
		if count < len(notes) {
			notes = notes[:count]
		}
	}
	return notes, nil
}

func (s *Store) Digest(since *time.Time, maxChars int) (string, error) {
	entries, err := s.ListActivities(since, nil)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", nil
	}
	byDay := map[string][]Entry{}
	days := []string{}
	for _, entry := range entries {
		day := formatDate(entry.At)
		if _, ok := byDay[day]; !ok {
			days = append(days, day)
		}
		byDay[day] = append(byDay[day], entry)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	var builder strings.Builder
	for _, day := range days {
		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString("## " + day + "\n")
		for _, entry := range byDay[day] {
			tags := ""
			if len(entry.Tags) > 0 {
				tags = " [" + strings.Join(entry.Tags, ",") + "]"
			}
			builder.WriteString("- " + formatTime(entry.At, false) + " " + string(entry.Kind) + ": " + singleLine(entry.Message) + tags + "\n")
		}
	}
	digest := strings.TrimRight(builder.String(), " \t\r\n")
	if maxChars <= 0 || len(digest) <= maxChars {
		return digest, nil
	}
	return digest[:maxChars] + "\n… (truncated)", nil
}

func (s *Store) Search(query string, limit int, keywordOnly bool, embeddings *OllamaClient, vectorWeight float64) ([]Hit, error) {
	documents, err := s.LoadAll()
	if err != nil {
		return nil, err
	}
	if keywordOnly || embeddings == nil || vectorWeight <= 0 {
		return KeywordSearch(documents, query, limit), nil
	}
	weight := clamp(vectorWeight, 0, 1)
	pool := limit
	if pool < 20 {
		pool = 20
	}
	index, err := IndexStore{File: s.EmbeddingIndexFile()}.Read()
	if err != nil {
		return KeywordSearch(documents, query, limit), nil
	}
	if index != nil {
		vectorHits, err := EmbeddingSearch(embeddings, index, documents, query, pool)
		if err == nil && len(vectorHits) > 0 {
			if weight >= 1 {
				return limitHits(vectorHits, limit), nil
			}
			keywordHits := KeywordSearch(documents, query, pool)
			return CombineHybridHits(keywordHits, vectorHits, weight, limit), nil
		}
	}
	return KeywordSearch(documents, query, limit), nil
}

func (s *Store) CompactJournal(olderThan time.Time, dryRun bool) (CompactReport, error) {
	if !fsx.IsDir(s.JournalDirectory()) {
		return CompactReport{}, nil
	}
	cutoff := time.Date(olderThan.Year(), olderThan.Month(), olderThan.Day(), 0, 0, 0, 0, olderThan.Location())
	files, err := markdownFiles(s.JournalDirectory(), true)
	if err != nil {
		return CompactReport{}, err
	}
	toArchive := []string{}
	entries := []Entry{}
	for _, file := range files {
		relative, _ := filepath.Rel(s.JournalDirectory(), file)
		hidden := false
		for _, segment := range strings.Split(filepath.ToSlash(relative), "/") {
			if strings.HasPrefix(segment, ".") {
				hidden = true
			}
		}
		if hidden {
			continue
		}
		day := dateFromName(strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)))
		if day == nil || !day.Before(cutoff) {
			continue
		}
		toArchive = append(toArchive, file)
		data, err := os.ReadFile(file)
		if err != nil {
			return CompactReport{}, err
		}
		entries = append(entries, parseJournal(*day, string(data))...)
	}
	if len(toArchive) == 0 {
		return CompactReport{}, nil
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].At.Before(entries[j].At) })
	moment := s.now()
	firstDay := formatDate(entries[0].At)
	lastDay := formatDate(entries[len(entries)-1].At)
	title := "Journal summary " + firstDay + " to " + lastDay
	baseSlug := formatDate(moment) + "-journal-summary-" + firstDay + "-to-" + lastDay
	noteFile := filepath.Join(s.NotesDirectory(), baseSlug+".md")
	if dryRun {
		relative, _ := fsx.PosixRel(s.Directory, noteFile)
		return CompactReport{ArchivedDays: len(toArchive), ArchivedEntries: len(entries), NotePath: relative}, nil
	}
	suffix := 0
	for fsx.Exists(noteFile) {
		suffix++
		noteFile = filepath.Join(s.NotesDirectory(), baseSlug+"-"+itoa(suffix)+".md")
	}
	contents := "# " + title + "\n\ndate: " + formatDate(moment) + "\ntags: compaction, journal-summary\n\n" + renderCompactSummary(entries) + "\n"
	if err := writeAtomic(noteFile, contents); err != nil {
		return CompactReport{}, err
	}
	for _, file := range toArchive {
		relative, _ := filepath.Rel(s.JournalDirectory(), file)
		archived := filepath.Join(s.ArchiveDirectory(), relative)
		if err := os.MkdirAll(filepath.Dir(archived), 0o755); err != nil {
			return CompactReport{}, err
		}
		if err := os.Rename(file, archived); err != nil {
			return CompactReport{}, err
		}
	}
	if err := s.RegenerateIndex(); err != nil {
		return CompactReport{}, err
	}
	relative, _ := fsx.PosixRel(s.Directory, noteFile)
	return CompactReport{ArchivedDays: len(toArchive), ArchivedEntries: len(entries), NotePath: relative}, nil
}

func (s *Store) UpdateIndex(client *OllamaClient, force bool) (IndexReport, error) {
	if err := os.MkdirAll(s.GeneratedDirectory(), 0o755); err != nil {
		return IndexReport{}, err
	}
	documents, err := s.LoadAll()
	if err != nil {
		return IndexReport{}, err
	}
	return BuildEmbeddingIndex(client, IndexStore{File: s.EmbeddingIndexFile()}, documents, force)
}

func (s *Store) RegenerateIndex() error {
	documents, err := s.LoadAll()
	if err != nil {
		return err
	}
	notes := []Document{}
	journal := []Document{}
	for _, document := range documents {
		if document.Kind == KindNote {
			notes = append(notes, document)
		} else {
			journal = append(journal, document)
		}
	}
	var builder strings.Builder
	builder.WriteString("# Juicyfruit memory index\n\nGenerated by `juicyfruit memory`. Every other file in this directory is append-only.\n\n## Notes\n\n")
	if len(notes) == 0 {
		builder.WriteString("- (none)\n")
	} else {
		for _, note := range notes {
			builder.WriteString("- `" + note.Path + "` — " + note.Title + "\n")
		}
	}
	builder.WriteString("\n## Journal\n\n")
	if len(journal) == 0 {
		builder.WriteString("- (none)\n")
	} else {
		for _, day := range journal {
			builder.WriteString("- `" + day.Path + "`\n")
		}
	}
	return writeAtomic(s.IndexFile(), builder.String())
}

func (s *Store) readJournal() ([]Entry, error) {
	if !fsx.IsDir(s.JournalDirectory()) {
		return []Entry{}, nil
	}
	files, err := markdownFiles(s.JournalDirectory(), true)
	if err != nil {
		return nil, err
	}
	entries := []Entry{}
	for _, file := range files {
		relative, _ := filepath.Rel(s.JournalDirectory(), file)
		hidden := false
		for _, segment := range strings.Split(filepath.ToSlash(relative), "/") {
			if strings.HasPrefix(segment, ".") {
				hidden = true
			}
		}
		if hidden {
			continue
		}
		day := dateFromName(strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)))
		if day == nil {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		entries = append(entries, parseJournal(*day, string(data))...)
	}
	return entries, nil
}

func parseJournal(day time.Time, text string) []Entry {
	entries := []Entry{}
	lines := strings.Split(text, "\n")
	for index := 0; index < len(lines); index++ {
		match := headingPattern.FindStringSubmatch(strings.TrimRight(lines[index], " \t\r\n"))
		if match == nil {
			continue
		}
		var kind EntryKind
		switch match[4] {
		case "note":
			kind = KindNote
		case "activity":
			kind = KindActivity
		default:
			continue
		}
		body := []string{}
		for cursor := index + 1; cursor < len(lines); cursor++ {
			if strings.HasPrefix(lines[cursor], "## ") {
				break
			}
			body = append(body, lines[cursor])
		}
		tags := []string{}
		for _, tag := range strings.Split(match[5], ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				tags = append(tags, tag)
			}
		}
		hour, _ := atoi(match[1])
		minute, _ := atoi(match[2])
		second, _ := atoi(match[3])
		moment := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, second, 0, day.Location())
		entries = append(entries, Entry{At: moment, Kind: kind, Tags: tags, Message: strings.TrimSpace(strings.Join(body, "\n"))})
	}
	return entries
}

func noteBody(text string) string {
	lines := strings.Split(text, "\n")
	index := 0
	if len(lines) > 0 && strings.HasPrefix(lines[0], "# ") {
		index = 1
	}
	for index < len(lines) && strings.TrimSpace(lines[index]) == "" {
		index++
	}
	for index < len(lines) && strings.Contains(lines[index], ":") {
		index++
	}
	for index < len(lines) && strings.TrimSpace(lines[index]) == "" {
		index++
	}
	return strings.TrimSpace(strings.Join(lines[index:], "\n"))
}

func noteTags(text string) []string {
	tags := []string{}
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "tags:") {
			continue
		}
		for _, tag := range strings.Split(line[len("tags:"):], ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				tags = append(tags, tag)
			}
		}
		return tags
	}
	return tags
}

func writeAtomic(file, contents string) error {
	if err := fsx.WriteStringAtomic(file, contents); err != nil {
		return Errorf("Cannot update %s: %v", file, err)
	}
	return nil
}

func slugify(title string) string {
	slug := strings.ToLower(title)
	slug = nonSlugPattern.ReplaceAllString(slug, "-")
	slug = trimDashPattern.ReplaceAllString(slug, "")
	if !slugPattern.MatchString(slug) {
		return ""
	}
	return slug
}

func titleOf(text, fallback string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}
	return fallback
}

func dateFromName(name string) *time.Time {
	match := dateNamePattern.FindStringSubmatch(name)
	if match == nil {
		return nil
	}
	year, _ := atoi(match[1])
	month, _ := atoi(match[2])
	day, _ := atoi(match[3])
	moment := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
	return &moment
}

func formatDate(moment time.Time) string {
	return pad(moment.Year(), 4) + "-" + pad(int(moment.Month()), 2) + "-" + pad(moment.Day(), 2)
}

func formatTime(moment time.Time, seconds bool) string {
	base := pad(moment.Hour(), 2) + ":" + pad(moment.Minute(), 2)
	if seconds {
		return base + ":" + pad(moment.Second(), 2)
	}
	return base
}

func pad(value, width int) string {
	text := itoa(value)
	for len(text) < width {
		text = "0" + text
	}
	return text
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	if negative {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}

func atoi(value string) (int, error) {
	number := 0
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, Errorf("invalid number")
		}
		number = number*10 + int(char-'0')
	}
	return number, nil
}

func singleLine(value string) string {
	words := whitespacePattern.Split(value, -1)
	out := []string{}
	for _, word := range words {
		if word != "" {
			out = append(out, word)
		}
	}
	return strings.Join(out, " ")
}

func renderCompactSummary(entries []Entry) string {
	byDay := map[string][]Entry{}
	days := []string{}
	for _, entry := range entries {
		day := formatDate(entry.At)
		if _, ok := byDay[day]; !ok {
			days = append(days, day)
		}
		byDay[day] = append(byDay[day], entry)
	}
	sort.Strings(days)
	var builder strings.Builder
	for _, day := range days {
		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString("## " + day + "\n")
		for _, entry := range byDay[day] {
			tags := ""
			if len(entry.Tags) > 0 {
				tags = " [" + strings.Join(entry.Tags, ",") + "]"
			}
			builder.WriteString("- " + formatTime(entry.At, false) + " " + string(entry.Kind) + ": " + singleLine(entry.Message) + tags + "\n")
		}
	}
	return strings.TrimRight(builder.String(), " \t\r\n")
}

func markdownFiles(root string, recursive bool) ([]string, error) {
	files := []string{}
	if !recursive {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				files = append(files, filepath.Join(root, entry.Name()))
			}
		}
		sort.Strings(files)
		return files, nil
	}
	all, err := fsx.WalkFiles(root)
	if err != nil {
		return nil, err
	}
	for _, file := range all {
		if strings.HasSuffix(file, ".md") {
			files = append(files, file)
		}
	}
	sort.Strings(files)
	return files, nil
}

func Max(a, b int) int { return int(math.Max(float64(a), float64(b))) }
