package task

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/darccau/juicyfruit/internal/fsx"
)

func (s *Store) StartSession(adapter, agent string, dshSessionID *string) (Session, error) {
	now := nowUTC()
	session := Session{
		Schema: "juicyfruit.session/v1", ID: NewID("SES"), Adapter: adapter, Agent: agent,
		DSHSessionID: dshSessionID, StartedAt: now, UpdatedAt: now, Status: SessionActive,
		TasksClaimed: []string{}, TasksWorked: []string{},
	}
	if err := s.writeJSON(s.sessionFile(session.ID), session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Store) ListSessions() ([]Session, error) {
	entries, err := os.ReadDir(s.sessionsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []Session{}, nil
		}
		return nil, err
	}
	files := []string{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			files = append(files, filepath.Join(s.sessionsDir(), entry.Name()))
		}
	}
	sort.Strings(files)
	sessions := []Session{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var session Session
		if err := json.Unmarshal(data, &session); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (s *Store) ReadSession(id string) (Session, error) {
	file := s.sessionFile(id)
	if !fsx.IsFile(file) {
		return Session{}, Errorf("Session not found: %s", id)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Store) CloseSession(id, reason string) (Session, error) {
	var result Session
	err := s.WithLock("session-"+id, func() error {
		session, err := s.ReadSession(id)
		if err != nil {
			return err
		}
		if session.Status == SessionClosed {
			result = session
			return nil
		}
		now := nowUTC()
		tasks, err := s.ListTasks()
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if task.Owner != nil && task.Owner.Session == id && task.Status != StatusDone && task.Status != StatusCancelled {
				if _, err := s.ReleaseTask(task.ID); err != nil {
					return err
				}
			}
		}
		session.Status = SessionClosed
		session.UpdatedAt = now
		session.EndedAt = &now
		session.CloseReason = &reason
		if err := s.writeSession(session); err != nil {
			return err
		}
		result = session
		return nil
	})
	return result, err
}

func (s *Store) CreateRun(title string, summary *string) (Run, error) {
	now := nowUTC()
	run := Run{Schema: "juicyfruit.run/v1", ID: NewID("RUN"), Title: title, Summary: summary, CreatedAt: now, UpdatedAt: now, Tasks: []string{}}
	if err := s.writeRun(run); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (s *Store) writeRun(run Run) error {
	if err := s.writeJSON(s.runFile(run.ID), run); err != nil {
		return err
	}
	runDir := filepath.Join(s.runsDir(), run.ID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteAtomic(filepath.Join(runDir, "manifest.json"), append(data, '\n'))
}

func (s *Store) ListRuns() ([]Run, error) {
	entries, err := os.ReadDir(s.runsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []Run{}, nil
		}
		return nil, err
	}
	files := []string{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			files = append(files, filepath.Join(s.runsDir(), entry.Name()))
		}
	}
	sort.Strings(files)
	runs := []Run{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var run Run
		if err := json.Unmarshal(data, &run); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

func (s *Store) ReadRun(id string) (Run, error) {
	file := s.runFile(id)
	if !fsx.IsFile(file) {
		return Run{}, Errorf("Run not found: %s", id)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return Run{}, err
	}
	var run Run
	if err := json.Unmarshal(data, &run); err != nil {
		return Run{}, err
	}
	return run, nil
}

func (s *Store) writeTask(task Task, event string, data map[string]any) error {
	if err := s.writeJSON(s.taskFile(task.ID), task); err != nil {
		return err
	}
	return s.appendEvent(task.ID, event, data)
}

func (s *Store) writeSession(session Session) error {
	return s.writeJSON(s.sessionFile(session.ID), session)
}

func (s *Store) appendEvent(taskID, eventType string, data map[string]any) error {
	if err := os.MkdirAll(s.eventsDir(), 0o755); err != nil {
		return err
	}
	event := Event{Schema: "juicyfruit.task-event/v1", ID: NewID("EVT"), Task: taskID, Type: eventType, CreatedAt: nowUTC(), Data: data}
	return s.writeJSON(filepath.Join(s.eventsDir(), event.ID+"-"+taskID+".json"), event)
}

func (s *Store) writeJSON(file string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := fsx.WriteAtomic(file, append(data, '\n')); err != nil {
		return Errorf("Cannot update %s: %v", file, err)
	}
	return nil
}

func (s *Store) WithLock(name string, callback func() error) error {
	if err := os.MkdirAll(s.locksDir(), 0o755); err != nil {
		return err
	}
	lock := filepath.Join(s.locksDir(), name+".lock")
	if err := s.acquireLock(lock, name); err != nil {
		return err
	}
	defer os.Remove(lock)
	payload := map[string]any{"pid": os.Getpid(), "created_at": nowUTC().Format(time.RFC3339Nano)}
	data, _ := json.MarshalIndent(payload, "", "  ")
	if err := fsx.WriteAtomic(lock, append(data, '\n')); err != nil {
		return err
	}
	return callback()
}

func (s *Store) acquireLock(lock, name string) error {
	file, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err == nil {
		file.Close()
		return nil
	}
	if !s.tryRecoverStaleLock(lock) {
		return Errorf("Runtime resource is locked: %s", name)
	}
	file, err = os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return Errorf("Runtime resource is locked: %s", name)
	}
	file.Close()
	return nil
}

func (s *Store) tryRecoverStaleLock(lock string) bool {
	info, err := os.Stat(lock)
	if err != nil {
		return false
	}
	timeout := s.StaleLockTimeout
	if timeout == 0 {
		timeout = 15 * time.Minute
	}
	if nowUTC().Sub(info.ModTime()) < timeout {
		return false
	}
	return os.Remove(lock) == nil
}

func ensureTransition(from, to Status) error {
	allowed := map[Status]map[Status]bool{
		StatusBacklog:   {StatusClaimed: true, StatusBlocked: true, StatusCancelled: true},
		StatusClaimed:   {StatusDoing: true, StatusBacklog: true, StatusBlocked: true, StatusCancelled: true},
		StatusDoing:     {StatusVerifying: true, StatusBlocked: true, StatusBacklog: true, StatusCancelled: true},
		StatusVerifying: {StatusDone: true, StatusDoing: true, StatusBlocked: true, StatusBacklog: true, StatusCancelled: true},
		StatusBlocked:   {StatusBacklog: true, StatusCancelled: true},
		StatusDone:      {},
		StatusCancelled: {},
	}
	if !allowed[from][to] {
		return Errorf("Invalid task transition: %s -> %s", from, to)
	}
	return nil
}

func ensureKnownDependencies(tasks []Task, dependencies []string) error {
	known := map[string]bool{}
	for _, task := range tasks {
		known[task.ID] = true
	}
	for _, dependency := range dependencies {
		if !known[dependency] {
			return Errorf("Unknown dependency: %s", dependency)
		}
	}
	return nil
}

func ensureAcyclic(tasks []Task, editedTask string, dependencies []string) error {
	graph := map[string][]string{}
	for _, task := range tasks {
		graph[task.ID] = task.Dependencies
	}
	if editedTask != "" {
		graph[editedTask] = dependencies
	}
	var visit func(current string, visiting, visited map[string]bool) bool
	visit = func(current string, visiting, visited map[string]bool) bool {
		if visiting[current] {
			return true
		}
		if visited[current] {
			return false
		}
		visiting[current] = true
		for _, next := range graph[current] {
			if visit(next, visiting, visited) {
				return true
			}
		}
		delete(visiting, current)
		visited[current] = true
		return false
	}
	for node := range graph {
		if visit(node, map[string]bool{}, map[string]bool{}) {
			return Errorf("Task dependency cycle rejected.")
		}
	}
	return nil
}

func ensureSafePaths(paths []string) error {
	for _, path := range paths {
		if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") || containsSegment(path) {
			return Errorf("Unsafe runtime path: %s", path)
		}
	}
	return nil
}

func containsSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func checkpointMap(checkpoint Checkpoint) map[string]any {
	return map[string]any{
		"completed": checkpoint.Completed, "current": checkpoint.Current,
		"remaining": checkpoint.Remaining, "changed_files": checkpoint.ChangedFiles,
		"validations": checkpoint.Validations, "next_action": checkpoint.NextAction,
	}
}

func Debug(format string, args ...any) string { return fmt.Sprintf(format, args...) }
