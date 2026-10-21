package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/darccau/juicyfruit/internal/fsx"
	"github.com/darccau/juicyfruit/internal/memory"
	"github.com/darccau/juicyfruit/internal/pkg"
	"github.com/darccau/juicyfruit/internal/task"
)

const memoryPackageID = "memory-core"

var knownEmbeddingModels = []string{
	"nomic-embed-text", "mxbai-embed-large", "bge-m3", "snowflake-arctic-embed2",
	"snowflake-arctic-embed", "embeddinggemma", "all-minilm", "paraphrase-multilingual",
}

func memoryCommand() *Command {
	group := &Command{Name: "memory", Description: "Record and recall project and user memory."}
	group.Children = []*Command{
		{Name: "setup", Description: "Prepare a memory store and install memory-core.", Parser: NewParser(
			Spec{Name: "scope", Type: StringFlag, Default: "user", Allowed: []string{"user", "project"}},
			Spec{Name: "all", Type: BoolFlag}, Spec{Name: "hook", Type: BoolFlag, Default: "true", Negatable: true},
			Spec{Name: "source", Type: StringFlag}, Spec{Name: "target", Type: MultiFlag, Allowed: supportedCaptureTargets()},
		), Run: runMemorySetup},
		{Name: "add", Description: "Record one memory entry.", Parser: NewParser(
			Spec{Name: "scope", Type: StringFlag, Allowed: []string{"user", "project"}},
			Spec{Name: "kind", Type: StringFlag, Default: "activity", Allowed: []string{"note", "activity"}},
			Spec{Name: "title", Type: StringFlag}, Spec{Name: "tag", Alias: "t", Type: MultiFlag},
		), Run: runMemoryAdd},
		{Name: "search", Description: "Rank memory documents against a query.", Parser: NewParser(
			Spec{Name: "scope", Type: StringFlag, Default: "all", Allowed: []string{"user", "project", "all"}},
			Spec{Name: "limit", Type: StringFlag, Default: "8"}, Spec{Name: "weight", Type: StringFlag, Default: "1"},
			Spec{Name: "keyword", Type: BoolFlag},
		), Run: runMemorySearch},
		{Name: "list", Description: "List recent memory entries, newest first.", Parser: NewParser(
			Spec{Name: "scope", Type: StringFlag, Default: "all", Allowed: []string{"user", "project", "all"}},
			Spec{Name: "since", Type: StringFlag, Default: "7d"},
			Spec{Name: "kind", Type: StringFlag, Default: "all", Allowed: []string{"all", "activity", "note"}},
			Spec{Name: "limit", Type: StringFlag},
		), Run: runMemoryList},
		{Name: "digest", Description: "Summarize recent activity for a task briefing.", Parser: NewParser(
			Spec{Name: "scope", Type: StringFlag, Default: "all", Allowed: []string{"user", "project", "all"}},
			Spec{Name: "since", Type: StringFlag, Default: "14d"}, Spec{Name: "max-chars", Type: StringFlag, Default: "2000"},
		), Run: runMemoryDigest},
		{Name: "index", Description: "Rebuild the embedding index for a memory store.", Parser: NewParser(
			Spec{Name: "scope", Type: StringFlag, Default: "all", Allowed: []string{"user", "project", "all"}},
			Spec{Name: "force", Type: BoolFlag},
		), Run: runMemoryIndex},
		{Name: "capture", Description: "Record the end of a working session.", Parser: NewParser(
			Spec{Name: "scope", Type: StringFlag, Allowed: []string{"user", "project"}},
		), Run: runMemoryCapture},
		{Name: "compact", Description: "Fold old journal entries into a durable summary note.", Parser: NewParser(
			Spec{Name: "scope", Type: StringFlag, Default: "all", Allowed: []string{"user", "project", "all"}},
			Spec{Name: "older-than", Type: StringFlag, Default: "90d"}, Spec{Name: "dry-run", Type: BoolFlag},
		), Run: runMemoryCompact},
	}
	return group
}

func supportedCaptureTargets() []string {
	out := []string{}
	for key := range memory.CaptureTargets {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func memoryScope(ctx *Context, scope memory.Scope) memory.Store {
	directory := ctx.App.MemoryRoots.UserDirectory
	if scope == memory.ScopeProject {
		directory = ctx.App.MemoryRoots.ProjectDirectory
	}
	return *memory.NewStore(directory)
}

func memoryScopeOption(ctx *Context, values *Values, fallback memory.Scope) memory.Scope {
	switch values.String("scope") {
	case "user":
		return memory.ScopeUser
	case "project":
		return memory.ScopeProject
	}
	return fallback
}

func configuredMemoryScope(ctx *Context) memory.Scope {
	store := memoryScope(ctx, memory.ScopeUser)
	config, err := store.ReadConfig()
	if err != nil {
		return memory.ScopeUser
	}
	return config.DefaultScope
}

func selectedMemoryScopes(values *Values) []memory.Scope {
	switch values.String("scope") {
	case "user":
		return []memory.Scope{memory.ScopeUser}
	case "project":
		return []memory.Scope{memory.ScopeProject}
	default:
		return []memory.Scope{memory.ScopeUser, memory.ScopeProject}
	}
}

func memoryMessage(values *Values) (string, error) {
	message := strings.TrimSpace(strings.Join(values.positionals, " "))
	if message == "" {
		return "", Usagef("Expected a memory message.")
	}
	return message, nil
}

var relativeWindowPattern = func(value string) (int, byte, bool) {
	if len(value) < 2 {
		return 0, 0, false
	}
	unit := value[len(value)-1]
	if unit != 'd' && unit != 'w' && unit != 'm' {
		return 0, 0, false
	}
	amount := 0
	for _, char := range value[:len(value)-1] {
		if char < '0' || char > '9' {
			return 0, 0, false
		}
		amount = amount*10 + int(char-'0')
	}
	return amount, unit, true
}

func relativeWindow(values *Values, name string, now time.Time) (*time.Time, error) {
	value := strings.TrimSpace(values.String(name))
	if value == "" {
		return nil, nil
	}
	amount, unit, ok := relativeWindowPattern(value)
	if !ok {
		return nil, Usagef("Invalid --%s value: %s. Use 7d, 2w, or 3m.", name, value)
	}
	days := amount
	if unit == 'w' {
		days = amount * 7
	} else if unit == 'm' {
		days = amount * 30
	}
	result := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -days)
	return &result, nil
}

func runMemorySetup(ctx *Context, values *Values) error {
	unattended := values.Bool("all")
	scope := memoryScopeOption(ctx, values, memory.ScopeUser)
	store := memoryScope(ctx, scope)
	configuredTargets := configuredMemoryTargets(ctx, scope)
	if err := store.EnsureSkeleton(); err != nil {
		return err
	}
	hook := values.Bool("hook")
	if !values.Parsed("hook") && !unattended {
		hook = ctx.Log.Confirm("Enable end-of-session capture hook?", true)
	}
	embeddings := resolveEmbeddings(ctx, unattended)
	requested := values.List("target")
	targets := requested
	if len(targets) == 0 {
		if unattended {
			targets = configuredTargets
		} else if len(configuredTargets) == 0 {
			ctx.Log.Info("No configured agent targets found; memory will be prepared without installing memory-core.")
			targets = []string{}
		} else if containsString(configuredTargets, "codex") {
			targets = []string{"codex"}
		} else {
			targets = []string{configuredTargets[0]}
		}
	}
	defaultScope := scope
	config := memory.Config{Version: 1, Embeddings: embeddings, Capture: memory.CaptureConfig{SessionEndHook: hook, GitDiffStat: true, Targets: append([]string{}, targets...)}, DefaultScope: defaultScope}
	if err := store.WriteConfig(config); err != nil {
		return err
	}
	if err := installMemoryPackage(ctx, values, targets, scope); err != nil {
		return err
	}
	if hook && containsString(targets, "claude-code") {
		root := ctx.App.Roots.UserRoot
		if scope == memory.ScopeProject {
			root = ctx.App.Roots.ProjectRoot
		}
		settings := filepath.Join(root, ".claude", "settings.json")
		written, err := memory.HookWriter{}.EnsureStopHook(settings, "juicyfruit memory capture --scope "+string(scope))
		if err != nil {
			return err
		}
		if written {
			ctx.Log.Info("Registered the capture hook in %s.", settings)
		} else {
			ctx.Log.Info("Capture hook already present in %s.", settings)
		}
	}
	recall := "keyword only"
	if embeddings.Enabled {
		recall = embeddings.Model
	}
	ctx.Log.Success("Memory ready at %s (embeddings: %s).", store.Directory, recall)
	return nil
}

func configuredMemoryTargets(ctx *Context, scope memory.Scope) []string {
	installScope := pkg.ScopeUser
	if scope == memory.ScopeProject {
		installScope = pkg.ScopeProject
	}
	configured := map[string]bool{}
	for _, target := range pkg.ConfiguredIDs(ctx.App.Roots, installScope) {
		configured[target] = true
	}
	out := []string{}
	for _, target := range supportedCaptureTargets() {
		if configured[target] {
			out = append(out, target)
		}
	}
	return out
}

func resolveEmbeddings(ctx *Context, unattended bool) memory.EmbeddingsConfig {
	disabled := memory.DefaultEmbeddingsConfig()
	disabled.Enabled = false
	probe := memory.NewOllamaClient(disabled.BaseURL, disabled.Model)
	if !probe.Probe() {
		ctx.Log.Info("Ollama not reachable at %s; memory will use keyword search.", disabled.BaseURL)
		return disabled
	}
	installed, err := probe.ListModels()
	if err != nil {
		return disabled
	}
	model := ""
	for _, candidate := range knownEmbeddingModels {
		if modelInstalled(installed, candidate) {
			model = candidate
			break
		}
	}
	if model == "" {
		fallback := knownEmbeddingModels[0]
		if unattended {
			ctx.Log.Info("No known embedding model is installed (%s); memory will use keyword search. Run `ollama pull %s` and re-run setup to enable it.", strings.Join(knownEmbeddingModels, ", "), fallback)
			return disabled
		}
		if !ctx.Log.Confirm(fmt.Sprintf("Download embedding model \"%s\" (~274 MB) now?", fallback), false) {
			return disabled
		}
		if err := probe.Pull(fallback, func(message string) { ctx.Log.Detail("%s", message) }); err != nil {
			ctx.Log.Info("Could not download %s: %v", fallback, err)
			return disabled
		}
		model = fallback
	} else {
		ctx.Log.Info("Using installed embedding model \"%s\".", model)
	}
	client := memory.NewOllamaClient(disabled.BaseURL, model)
	vectors, err := client.Embed([]string{"juicyfruit"})
	if err != nil {
		ctx.Log.Info("Embedding provider rejected a probe request; memory will use keyword search.")
		return disabled
	}
	enabled := disabled
	enabled.Enabled = true
	enabled.Model = model
	enabled.Dimensions = nil
	if len(vectors) > 0 && len(vectors[0]) > 0 {
		dimensions := len(vectors[0])
		enabled.Dimensions = &dimensions
	}
	return enabled
}

func modelInstalled(installed []string, requested string) bool {
	withTag := func(value string) string {
		if strings.Contains(value, ":") {
			return value
		}
		return value + ":latest"
	}
	target := withTag(requested)
	for _, item := range installed {
		if withTag(item) == target {
			return true
		}
	}
	return false
}

func installMemoryPackage(ctx *Context, values *Values, targets []string, scope memory.Scope) error {
	if len(targets) == 0 {
		return nil
	}
	requestedSource := strings.TrimSpace(values.String("source"))
	if requestedSource != "" {
		candidates, err := ctx.App.Catalog.Discover()
		if err != nil {
			return err
		}
		available := false
		for _, candidate := range candidates {
			if candidate.Manifest.ID == memoryPackageID && candidate.SourceName == requestedSource {
				available = true
			}
		}
		if !available {
			return memory.Errorf("Source %s does not provide %s. Register it: juicyfruit source add %s --local <path>", requestedSource, memoryPackageID, requestedSource)
		}
	}
	installScope := pkg.ScopeUser
	if scope == memory.ScopeProject {
		installScope = pkg.ScopeProject
	}
	for _, target := range targets {
		resolution, err := ctx.App.Resolver.Resolve([]string{memoryPackageID}, target, requestedSource)
		if err != nil {
			return memory.Errorf("%s Register a source that provides %s: juicyfruit source add <name> --local <path>", err, memoryPackageID)
		}
		if _, err := ctx.App.Installer.Install(resolution, ctx.App.Roots, installScope, conflictResolver(ctx, false)); err != nil {
			return err
		}
		ctx.Log.Info("Installed %s for %s (%s).", memoryPackageID, target, scope)
	}
	return nil
}

func runMemoryAdd(ctx *Context, values *Values) error {
	message, err := memoryMessage(values)
	if err != nil {
		return err
	}
	scope := memoryScopeOption(ctx, values, configuredMemoryScope(ctx))
	store := memoryScope(ctx, scope)
	if err := store.EnsureSkeleton(); err != nil {
		return err
	}
	if values.String("kind") == "note" {
		title := strings.TrimSpace(values.String("title"))
		if title == "" {
			return Usagef("--title is required for --kind note.")
		}
		note, err := store.WriteNote(title, message, values.List("tag"), nil)
		if err != nil {
			return err
		}
		ctx.Log.Success("Wrote %s.", filepath.Join(store.NotesDirectory(), note.Slug+".md"))
		return nil
	}
	entry, err := store.AppendActivity(message, values.List("tag"), nil)
	if err != nil {
		return err
	}
	ctx.Log.Success("Appended to %s.", store.JournalFileFor(entry.At))
	return nil
}

func runMemorySearch(ctx *Context, values *Values) error {
	query, err := memoryMessage(values)
	if err != nil {
		return err
	}
	limit, err := values.Int("limit")
	if err != nil {
		return err
	}
	keywordOnly := values.Bool("keyword")
	weight, err := values.Float("weight", 1)
	if err != nil {
		return err
	}
	hits := []memory.Hit{}
	for _, scope := range selectedMemoryScopes(values) {
		store := memoryScope(ctx, scope)
		if !dirExists(store.Directory) {
			continue
		}
		config, err := store.ReadConfig()
		if err != nil {
			return err
		}
		var client *memory.OllamaClient
		if !keywordOnly && config.Embeddings.Enabled {
			client = memory.NewOllamaClient(config.Embeddings.BaseURL, config.Embeddings.Model)
		}
		scoped, err := store.Search(query, limit, keywordOnly, client, weight)
		if err != nil {
			return err
		}
		for _, hit := range scoped {
			hits = append(hits, hit.WithScopeLabel(string(scope)))
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Path < hits[j].Path
	})
	if len(hits) == 0 {
		ctx.Log.Info("No matches.")
		return nil
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 20 {
		limit = 20
	}
	for index, hit := range hits {
		if index >= limit {
			break
		}
		ctx.Log.Info("%.3f\t%s\t%s\t%s\t%s", hit.Score, hit.ScopeLabel, hit.Path, hit.Title, hit.Excerpt)
	}
	return nil
}

func dirExists(path string) bool { return fsx.IsDir(path) }

type listedMemory struct {
	At    time.Time
	Kind  memory.EntryKind
	Scope string
	Tags  []string
	Title string
	Text  string
}

func runMemoryList(ctx *Context, values *Values) error {
	since, err := relativeWindow(values, "since", time.Now())
	if err != nil {
		return err
	}
	limit, err := values.Int("limit")
	if err != nil {
		return err
	}
	kind := values.String("kind")
	entries := []listedMemory{}
	for _, scope := range selectedMemoryScopes(values) {
		store := memoryScope(ctx, scope)
		if !dirExists(store.Directory) {
			continue
		}
		if kind == "all" || kind == "activity" {
			activities, err := store.ListActivities(since, nil)
			if err != nil {
				return err
			}
			for _, entry := range activities {
				entries = append(entries, listedMemory{At: entry.At, Kind: entry.Kind, Scope: string(scope), Tags: entry.Tags, Text: entry.Message})
			}
		}
		if kind == "all" || kind == "note" {
			notes, err := store.ListNotes(since, nil)
			if err != nil {
				return err
			}
			for _, note := range notes {
				entries = append(entries, listedMemory{At: note.At, Kind: memory.KindNote, Scope: string(scope), Tags: note.Tags, Title: note.Title, Text: note.Body})
			}
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].At.After(entries[j].At) })
	if len(entries) == 0 {
		ctx.Log.Info("No entries.")
		return nil
	}
	for index, entry := range entries {
		if limit > 0 && index >= limit {
			break
		}
		title := ""
		if entry.Title != "" {
			title = entry.Title + ": "
		}
		ctx.Log.Info("%s\t%s\t%s\t[%s]\t%s%s", entry.At.Format("2006-01-02 15:04"), entry.Scope, entry.Kind, strings.Join(entry.Tags, ","), title, singleLine(entry.Text))
	}
	return nil
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func runMemoryDigest(ctx *Context, values *Values) error {
	since, err := relativeWindow(values, "since", time.Now())
	if err != nil {
		return err
	}
	maxChars, err := values.Int("max-chars")
	if err != nil {
		return err
	}
	scopes := selectedMemoryScopes(values)
	rendered := false
	for _, scope := range scopes {
		store := memoryScope(ctx, scope)
		if !dirExists(store.Directory) {
			continue
		}
		digest, err := store.Digest(since, maxChars)
		if err != nil {
			return err
		}
		if digest == "" {
			continue
		}
		rendered = true
		if len(scopes) > 1 {
			ctx.Log.Info("# %s\n%s", scope, digest)
		} else {
			ctx.Log.Info("%s", digest)
		}
	}
	if !rendered {
		ctx.Log.Info("No entries.")
	}
	return nil
}

func runMemoryIndex(ctx *Context, values *Values) error {
	force := values.Bool("force")
	for _, scope := range selectedMemoryScopes(values) {
		store := memoryScope(ctx, scope)
		if !dirExists(store.Directory) {
			continue
		}
		config, err := store.ReadConfig()
		if err != nil {
			return err
		}
		if !config.Embeddings.Enabled {
			return memory.Errorf("Embeddings are disabled. Run: juicyfruit memory setup")
		}
		client := memory.NewOllamaClient(config.Embeddings.BaseURL, config.Embeddings.Model)
		if !client.Probe() {
			return memory.Errorf("Ollama not reachable at %s.", config.Embeddings.BaseURL)
		}
		report, err := store.UpdateIndex(client, force)
		if err != nil {
			return err
		}
		ctx.Log.Success("%s: embedded %d, reused %d, pruned %d.", scope, report.Embedded, report.Reused, report.Pruned)
	}
	return nil
}

func runMemoryCapture(ctx *Context, values *Values) error {
	scope := memoryScopeOption(ctx, values, configuredMemoryScope(ctx))
	store := memoryScope(ctx, scope)
	if err := store.EnsureSkeleton(); err != nil {
		return err
	}
	config, err := store.ReadConfig()
	if err != nil {
		return err
	}
	if _, err := store.AppendActivity("session ended", []string{"session"}, nil); err != nil {
		return err
	}
	if config.Capture.GitDiffStat {
		if diff := gitDiffStat(ctx.App.Roots.ProjectRoot); diff != "" {
			if _, err := store.AppendActivity("session diff\n\n"+diff, []string{"session", "diff"}, nil); err != nil {
				return err
			}
		}
	}
	if _, err := store.AppendActivity("TODO: summarize what was done this session", []string{"todo"}, nil); err != nil {
		return err
	}
	ctx.Log.Success("Captured session memory in %s.", store.Directory)
	return nil
}

func gitDiffStat(root string) string {
	command := exec.Command("git", "diff", "--stat")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return ""
	}
	lines := []string{}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
		if len(lines) >= 20 {
			break
		}
	}
	return strings.Join(lines, "\n")
}

func runMemoryCompact(ctx *Context, values *Values) error {
	olderThan, err := relativeWindow(values, "older-than", time.Now())
	if err != nil {
		return err
	}
	if olderThan == nil {
		return Usagef("--older-than cannot be empty.")
	}
	dryRun := values.Bool("dry-run")
	compacted := false
	for _, scope := range selectedMemoryScopes(values) {
		store := memoryScope(ctx, scope)
		if !dirExists(store.Directory) {
			continue
		}
		report, err := store.CompactJournal(*olderThan, dryRun)
		if err != nil {
			return err
		}
		if report.ArchivedDays == 0 {
			ctx.Log.Info("%s: nothing older than --older-than.", scope)
			continue
		}
		compacted = true
		verb := "archived"
		if dryRun {
			verb = "would archive"
		}
		ctx.Log.Success("%s: %s %d journal day(s) (%d entries) into %s.", scope, verb, report.ArchivedDays, report.ArchivedEntries, report.NotePath)
	}
	if !compacted && !dryRun {
		ctx.Log.Info("Nothing to compact.")
	}
	return nil
}

func (a *App) MemoryCaptureEnabled(scope string) bool {
	directory := a.MemoryRoots.ProjectDirectory
	if scope == "user" {
		directory = a.MemoryRoots.UserDirectory
	}
	store := memory.NewStore(directory)
	if !fsx.IsFile(store.ConfigFile()) {
		return false
	}
	config, err := store.ReadConfig()
	return err == nil && config.Capture.SessionEndHook
}

func (a *App) CaptureSession(session task.Session) error {
	directory := a.MemoryRoots.ProjectDirectory
	store := memory.NewStore(directory)
	if err := store.EnsureSkeleton(); err != nil {
		return err
	}
	compact := func(values []string) string {
		if len(values) == 0 {
			return "-"
		}
		return strings.Join(values, ", ")
	}
	reason := ""
	if session.CloseReason != nil {
		reason = *session.CloseReason
	}
	message := fmt.Sprintf("session %s ended (%s); claimed: %s; worked: %s; task checkpoints persisted in .juicyfruit/tasks/", session.ID, reason, compact(session.TasksClaimed), compact(session.TasksWorked))
	_, err := store.AppendActivity(message, []string{"session", "task-runtime"}, nil)
	return err
}
