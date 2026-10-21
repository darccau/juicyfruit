package task

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/darccau/juicyfruit/internal/fsx"
)

type Store struct {
	ProjectRoot      string
	StaleLockTimeout time.Duration
}

func NewStore(projectRoot string) *Store {
	return &Store{ProjectRoot: projectRoot, StaleLockTimeout: 15 * time.Minute}
}

func DefaultProjectRoot() string {
	start := fsx.ProjectRoot()
	current, err := filepath.Abs(start)
	if err != nil {
		current = start
	}
	origin := current
	var nearest string
	for {
		if nearest == "" && fsx.Exists(filepath.Join(current, ".juicyfruit")) {
			nearest = current
		}
		gitPath := filepath.Join(current, ".git")
		if fsx.IsDir(gitPath) {
			return current
		}
		if fsx.IsFile(gitPath) {
			data, err := os.ReadFile(gitPath)
			if err == nil {
				text := strings.TrimSpace(string(data))
				if strings.HasPrefix(text, "gitdir:") {
					gitDir := text[7:]
					gitDir = strings.TrimSpace(gitDir)
					if !filepath.IsAbs(gitDir) {
						gitDir = filepath.Join(current, gitDir)
					}
					gitDir = filepath.Clean(gitDir)
					commonGit := filepath.Dir(filepath.Dir(gitDir))
					mainRoot := filepath.Dir(commonGit)
					if fsx.IsDir(filepath.Join(mainRoot, ".git")) {
						return mainRoot
					}
				}
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			if nearest != "" {
				return nearest
			}
			return origin
		}
		current = parent
	}
}

func (s *Store) root() string                 { return filepath.Join(s.ProjectRoot, ".juicyfruit") }
func (s *Store) tasksDir() string             { return filepath.Join(s.root(), "tasks") }
func (s *Store) runsDir() string              { return filepath.Join(s.root(), "runs") }
func (s *Store) eventsDir() string            { return filepath.Join(s.root(), "task-events") }
func (s *Store) runtimeDir() string           { return filepath.Join(s.root(), "runtime") }
func (s *Store) sessionsDir() string          { return filepath.Join(s.runtimeDir(), "sessions") }
func (s *Store) locksDir() string             { return filepath.Join(s.runtimeDir(), "locks") }
func (s *Store) taskFile(id string) string    { return filepath.Join(s.tasksDir(), id+".json") }
func (s *Store) sessionFile(id string) string { return filepath.Join(s.sessionsDir(), id+".json") }
func (s *Store) runFile(id string) string     { return filepath.Join(s.runsDir(), id+".json") }

func nowUTC() time.Time { return time.Now().UTC() }

func (s *Store) CreateTask(title, priority string, run, track *string, dependencies, acceptance []string, context ContextHints) (Task, error) {
	var result Task
	err := s.WithLock("tasks", func() error {
		now := nowUTC()
		tasks, err := s.ListTasks()
		if err != nil {
			return err
		}
		if err := ensureKnownDependencies(tasks, dependencies); err != nil {
			return err
		}
		if err := ensureSafePaths(context.Files); err != nil {
			return err
		}
		if err := ensureAcyclic(tasks, "", dependencies); err != nil {
			return err
		}
		var parent *Run
		if run != nil {
			value, err := s.ReadRun(*run)
			if err != nil {
				return err
			}
			parent = &value
		}
		task := Task{
			Schema: "juicyfruit.task/v1", ID: NewID("JCF"), Title: title, Status: StatusBacklog,
			Priority: priority, Run: run, Track: track, CreatedAt: now, UpdatedAt: now,
			Dependencies: sortedUnique(dependencies...), Acceptance: acceptance,
			Context: context, Checkpoint: EmptyCheckpoint(),
		}
		if task.Context.Topics == nil {
			task.Context.Topics = []string{}
		}
		if task.Context.Files == nil {
			task.Context.Files = []string{}
		}
		if err := s.writeJSON(s.taskFile(task.ID), task); err != nil {
			return err
		}
		if err := s.appendEvent(task.ID, "created", map[string]any{"title": title}); err != nil {
			return err
		}
		if parent != nil {
			parent.UpdatedAt = now
			parent.Tasks = sortedUnique(append(parent.Tasks, task.ID)...)
			if err := s.writeRun(*parent); err != nil {
				return err
			}
		}
		result = task
		return nil
	})
	return result, err
}

func (s *Store) ListTasks() ([]Task, error) {
	entries, err := os.ReadDir(s.tasksDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []Task{}, nil
		}
		return nil, err
	}
	files := []string{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			files = append(files, filepath.Join(s.tasksDir(), entry.Name()))
		}
	}
	sort.Strings(files)
	tasks := []Task{}
	for _, file := range files {
		task, err := readTaskFile(file)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (s *Store) ReadTask(id string) (Task, error) {
	file := s.taskFile(id)
	if !fsx.IsFile(file) {
		return Task{}, Errorf("Task not found: %s", id)
	}
	task, err := readTaskFile(file)
	if err != nil {
		return Task{}, Errorf("Cannot read task %s: %v", id, err)
	}
	return task, nil
}

func readTaskFile(file string) (Task, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Task{}, err
	}
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return Task{}, err
	}
	return task, nil
}

func (s *Store) ListTaskEvents(taskID string) ([]Event, error) {
	entries, err := os.ReadDir(s.eventsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []Event{}, nil
		}
		return nil, err
	}
	files := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasSuffix(name, "-"+taskID+".json") && strings.HasPrefix(name, "EVT-") {
			files = append(files, filepath.Join(s.eventsDir(), name))
		}
	}
	sort.Strings(files)
	events := []Event{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var event Event
		if err := json.Unmarshal(data, &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *Store) ReadyTasks(track string) ([]Task, error) {
	tasks, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	done := map[string]bool{}
	for _, task := range tasks {
		if task.Status == StatusDone {
			done[task.ID] = true
		}
	}
	ready := []Task{}
	for _, task := range tasks {
		if !task.IsClaimable(done) {
			continue
		}
		if track != "" && task.Track != nil && *task.Track != track {
			continue
		}
		ready = append(ready, task)
	}
	sort.SliceStable(ready, func(i, j int) bool { return compareReady(ready[i], ready[j]) < 0 })
	return ready, nil
}

func compareReady(left, right Task) int {
	if rankLeft, rankRight := priorityRank(left.Priority), priorityRank(right.Priority); rankLeft != rankRight {
		return rankRight - rankLeft
	}
	if !left.CreatedAt.Equal(right.CreatedAt) {
		if left.CreatedAt.Before(right.CreatedAt) {
			return -1
		}
		return 1
	}
	return strings.Compare(left.ID, right.ID)
}

func priorityRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical", "urgent", "p0":
		return 4
	case "high", "p1":
		return 3
	case "normal", "medium", "p2":
		return 2
	case "low", "p3":
		return 1
	default:
		return 0
	}
}
