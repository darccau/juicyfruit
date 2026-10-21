package task

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/darccau/juicyfruit/internal/fsx"
	"gopkg.in/yaml.v3"
)

func (s *Store) ClaimTask(id, adapter, agent, sessionID string) (Task, error) {
	var result Task
	err := s.WithLock("task-"+id, func() error {
		task, err := s.ReadTask(id)
		if err != nil {
			return err
		}
		if task.Owner != nil {
			return Errorf("Task already claimed.\n\nOwner:\n%s / %s\n\nSession:\n%s", task.Owner.Adapter, task.Owner.Agent, task.Owner.Session)
		}
		all, err := s.ListTasks()
		if err != nil {
			return err
		}
		done := map[string]bool{}
		for _, item := range all {
			if item.Status == StatusDone {
				done[item.ID] = true
			}
		}
		for _, dependency := range task.Dependencies {
			if !done[dependency] {
				return Errorf("Task dependencies are not satisfied: %s", id)
			}
		}
		session, err := s.ReadSession(sessionID)
		if err != nil {
			return err
		}
		if session.Status != SessionActive {
			return Errorf("Session is not active: %s", sessionID)
		}
		if session.Adapter != adapter {
			return Errorf("Session %s belongs to %s, not %s.", sessionID, session.Adapter, adapter)
		}
		gitRoot := s.gitProjectRootOrNil()
		slug := slugify(task.Title)
		branchName := ""
		if task.Branch != nil {
			branchName = *task.Branch
		} else {
			branchName = "jcf/" + task.ID + "-" + slug
		}
		var worktreePath *string
		if task.Worktree != nil {
			value := *task.Worktree
			worktreePath = &value
		} else if gitRoot != "" {
			value := filepath.Join(s.worktreeBase(), task.ID+"-"+slug)
			worktreePath = &value
		}
		if gitRoot != "" && worktreePath != nil && !fsx.Exists(*worktreePath) {
			parent := filepath.Dir(*worktreePath)
			if !fsx.Exists(parent) {
				if err := os.MkdirAll(parent, 0o755); err != nil {
					return Errorf("Cannot create worktree directory %s: %v. Grant write access there or set worktree_base in %s.", parent, err, filepath.Join(s.root(), "config.yaml"))
				}
			}
			check := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branchName)
			check.Dir = gitRoot
			branchExists := check.Run() == nil
			args := []string{"worktree", "add"}
			if !branchExists {
				args = append(args, "-b", branchName)
			}
			args = append(args, *worktreePath)
			if branchExists {
				args = append(args, branchName)
			}
			cmd := exec.Command("git", args...)
			cmd.Dir = gitRoot
			var stderr strings.Builder
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				return Errorf("Failed to create git worktree: %s", strings.TrimSpace(stderr.String()))
			}
		}
		if err := ensureTransition(task.Status, StatusClaimed); err != nil {
			return err
		}
		now := nowUTC()
		owner := &Owner{Adapter: adapter, Agent: agent, Session: sessionID}
		task.Status = StatusClaimed
		task.UpdatedAt = now
		task.Owner = owner
		task.Worktree = worktreePath
		if worktreePath == nil {
			task.Branch = nil
		} else {
			task.Branch = &branchName
		}
		data := map[string]any{"session": sessionID}
		if worktreePath != nil {
			data["worktree"] = *worktreePath
			data["branch"] = branchName
		}
		if err := s.writeTask(task, "claimed", data); err != nil {
			return err
		}
		session.UpdatedAt = now
		session.TasksClaimed = sortedUnique(append(session.TasksClaimed, id)...)
		if err := s.writeSession(session); err != nil {
			return err
		}
		result = task
		return nil
	})
	return result, err
}

func (s *Store) CleanupTask(id string, force bool) (Task, error) {
	var result Task
	err := s.WithLock("task-"+id, func() error {
		task, err := s.ReadTask(id)
		if err != nil {
			return err
		}
		worktreePath := task.Worktree
		branchName := task.Branch
		if worktreePath != nil && fsx.IsDir(*worktreePath) {
			statusCmd := exec.Command("git", "status", "--porcelain")
			statusCmd.Dir = *worktreePath
			var stdout strings.Builder
			statusCmd.Stdout = &stdout
			if statusCmd.Run() == nil {
				if strings.TrimSpace(stdout.String()) != "" && !force {
					return Errorf("Worktree is dirty: %s. Use --force to clean up anyway.", *worktreePath)
				}
			}
			args := []string{"worktree", "remove"}
			if force {
				args = append(args, "--force")
			}
			args = append(args, *worktreePath)
			root := s.gitProjectRootOrNil()
			if root == "" {
				root = s.ProjectRoot
			}
			removeCmd := exec.Command("git", args...)
			removeCmd.Dir = root
			var stderr strings.Builder
			removeCmd.Stderr = &stderr
			if err := removeCmd.Run(); err != nil && !force {
				return Errorf("Failed to remove git worktree: %s", strings.TrimSpace(stderr.String()))
			}
			prune := exec.Command("git", "worktree", "prune")
			prune.Dir = root
			_ = prune.Run()
			if fsx.Exists(*worktreePath) {
				_ = os.RemoveAll(*worktreePath)
			}
		}
		gitRoot := s.gitProjectRootOrNil()
		if branchName != nil && gitRoot != "" {
			flag := "-d"
			if force {
				flag = "-D"
			}
			cmd := exec.Command("git", "branch", flag, *branchName)
			cmd.Dir = gitRoot
			if cmd.Run() != nil && force {
				retry := exec.Command("git", "branch", "-D", *branchName)
				retry.Dir = gitRoot
				_ = retry.Run()
			}
		}
		task.UpdatedAt = nowUTC()
		task.Worktree = nil
		task.Branch = nil
		data := map[string]any{}
		if worktreePath != nil {
			data["previous_worktree"] = *worktreePath
		}
		if branchName != nil {
			data["previous_branch"] = *branchName
		}
		if err := s.writeTask(task, "cleaned", data); err != nil {
			return err
		}
		result = task
		return nil
	})
	return result, err
}

func (s *Store) gitProjectRootOrNil() string {
	gitDir := filepath.Join(s.ProjectRoot, ".git")
	info, err := os.Stat(gitDir)
	if err == nil && info.IsDir() {
		return s.ProjectRoot
	}
	if err == nil && !info.IsDir() {
		data, err := os.ReadFile(gitDir)
		if err == nil {
			text := strings.TrimSpace(string(data))
			if strings.HasPrefix(text, "gitdir:") {
				gitPath := strings.TrimSpace(text[7:])
				if !filepath.IsAbs(gitPath) {
					gitPath = filepath.Join(s.ProjectRoot, gitPath)
				}
				gitPath = filepath.Clean(gitPath)
				commonGit := filepath.Dir(filepath.Dir(gitPath))
				mainRoot := filepath.Dir(commonGit)
				if fsx.IsDir(filepath.Join(mainRoot, ".git")) {
					return mainRoot
				}
			}
		}
	}
	return ""
}

func (s *Store) worktreeBase() string {
	configFile := filepath.Join(s.root(), "config.yaml")
	if fsx.IsFile(configFile) {
		if data, err := os.ReadFile(configFile); err == nil {
			var doc map[string]any
			if yaml.Unmarshal(data, &doc) == nil {
				var base string
				for _, key := range []string{"worktree_base", "worktrees"} {
					if value, ok := doc[key].(string); ok {
						base = value
						break
					}
				}
				if base == "" {
					if worktree, ok := doc["worktree"].(map[string]any); ok {
						if value, ok := worktree["base"].(string); ok {
							base = value
						}
					}
				}
				if strings.TrimSpace(base) != "" {
					base = strings.TrimSpace(base)
					if filepath.IsAbs(base) {
						return filepath.Clean(base)
					}
					return filepath.Clean(filepath.Join(filepath.Dir(s.ProjectRoot), base))
				}
			}
		}
	}
	return filepath.Join(filepath.Dir(s.ProjectRoot), "juicyfruit-worktrees")
}

func slugify(title string) string {
	slug := strings.ToLower(title)
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "task"
	}
	return slug
}

func (s *Store) AddDependencies(id string, dependencies []string) (Task, error) {
	var result Task
	err := s.WithLock("task-"+id, func() error {
		task, err := s.ReadTask(id)
		if err != nil {
			return err
		}
		if task.Status != StatusBacklog {
			return Errorf("Dependencies can only be changed while BACKLOG: %s", id)
		}
		tasks, err := s.ListTasks()
		if err != nil {
			return err
		}
		if err := ensureKnownDependencies(tasks, dependencies); err != nil {
			return err
		}
		for _, dependency := range dependencies {
			if dependency == id {
				return Errorf("Task cannot depend on itself.")
			}
		}
		next := sortedUnique(append(task.Dependencies, dependencies...)...)
		if err := ensureAcyclic(tasks, id, next); err != nil {
			return err
		}
		task.Dependencies = next
		task.UpdatedAt = nowUTC()
		if err := s.writeTask(task, "dependencies-updated", map[string]any{"dependencies": next}); err != nil {
			return err
		}
		result = task
		return nil
	})
	return result, err
}

func (s *Store) StartTask(id string) (Task, error) { return s.transition(id, StatusDoing, "started") }
func (s *Store) VerifyTask(id string) (Task, error) {
	return s.transition(id, StatusVerifying, "verifying")
}
func (s *Store) DoneTask(id string) (Task, error) { return s.transition(id, StatusDone, "done") }
func (s *Store) CancelTask(id string) (Task, error) {
	return s.transition(id, StatusCancelled, "cancelled")
}

func (s *Store) ReleaseTask(id string) (Task, error) {
	var result Task
	err := s.WithLock("task-"+id, func() error {
		task, err := s.ReadTask(id)
		if err != nil {
			return err
		}
		if task.Owner == nil {
			result = task
			return nil
		}
		previous := task.Owner
		task.Status = StatusBacklog
		task.UpdatedAt = nowUTC()
		task.Owner = nil
		task.PreviousOwner = previous
		task.Blocker = nil
		if err := s.writeTask(task, "released", map[string]any{"session": previous.Session}); err != nil {
			return err
		}
		result = task
		return nil
	})
	return result, err
}

func (s *Store) BlockTask(id, reason string) (Task, error) {
	var result Task
	err := s.WithLock("task-"+id, func() error {
		task, err := s.ReadTask(id)
		if err != nil {
			return err
		}
		if task.Status == StatusDone || task.Status == StatusCancelled {
			return Errorf("Cannot block %s.", task.Status)
		}
		task.Status = StatusBlocked
		task.UpdatedAt = nowUTC()
		task.Blocker = &reason
		if err := s.writeTask(task, "blocked", map[string]any{"reason": reason}); err != nil {
			return err
		}
		result = task
		return nil
	})
	return result, err
}

func (s *Store) UnblockTask(id string) (Task, error) {
	var result Task
	err := s.WithLock("task-"+id, func() error {
		task, err := s.ReadTask(id)
		if err != nil {
			return err
		}
		if task.Status != StatusBlocked {
			return Errorf("Task is not blocked: %s", id)
		}
		task.Status = StatusBacklog
		task.UpdatedAt = nowUTC()
		task.Blocker = nil
		if err := s.writeTask(task, "unblocked", map[string]any{}); err != nil {
			return err
		}
		result = task
		return nil
	})
	return result, err
}

func (s *Store) CheckpointTask(id string, checkpoint Checkpoint) (Task, error) {
	var result Task
	err := s.WithLock("task-"+id, func() error {
		task, err := s.ReadTask(id)
		if err != nil {
			return err
		}
		if task.Status == StatusDone || task.Status == StatusCancelled {
			return Errorf("Cannot checkpoint %s.", task.Status)
		}
		now := nowUTC()
		task.UpdatedAt = now
		task.Checkpoint = task.Checkpoint.Merge(checkpoint)
		if err := s.writeTask(task, "checkpointed", checkpointMap(checkpoint)); err != nil {
			return err
		}
		if task.Owner != nil && fsx.IsFile(s.sessionFile(task.Owner.Session)) {
			session, err := s.ReadSession(task.Owner.Session)
			if err != nil {
				return err
			}
			session.UpdatedAt = now
			session.TasksWorked = sortedUnique(append(session.TasksWorked, id)...)
			if checkpoint.NextAction != nil {
				session.LastCheckpoint = checkpoint.NextAction
			} else if checkpoint.Current != nil {
				session.LastCheckpoint = checkpoint.Current
			}
			if err := s.writeSession(session); err != nil {
				return err
			}
		}
		result = task
		return nil
	})
	return result, err
}

func (s *Store) transition(id string, status Status, event string) (Task, error) {
	var result Task
	err := s.WithLock("task-"+id, func() error {
		task, err := s.ReadTask(id)
		if err != nil {
			return err
		}
		if err := ensureTransition(task.Status, status); err != nil {
			return err
		}
		if (status == StatusDoing || status == StatusVerifying) && task.Owner == nil {
			return Errorf("Task must be claimed before %s: %s", event, id)
		}
		task.Status = status
		task.UpdatedAt = nowUTC()
		if err := s.writeTask(task, event, map[string]any{}); err != nil {
			return err
		}
		result = task
		return nil
	})
	return result, err
}
