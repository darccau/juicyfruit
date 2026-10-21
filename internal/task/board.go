package task

import (
	"sort"
	"time"
)

type BoardEntry struct {
	Task Task
	Age  time.Duration
}

type BoardColumn struct {
	Title   string
	Entries []BoardEntry
}

type Board struct {
	GeneratedAt    time.Time
	Columns        []BoardColumn
	WaitingCount   int
	DoneCount      int
	CancelledCount int
}

func BuildBoard(tasks []Task, now time.Time) Board {
	now = now.UTC()
	done := map[string]bool{}
	for _, task := range tasks {
		if task.Status == StatusDone {
			done[task.ID] = true
		}
	}
	ready, inProgress, verifying, blocked := []Task{}, []Task{}, []Task{}, []Task{}
	waiting, doneCount, cancelledCount := 0, 0, 0
	for _, task := range tasks {
		switch task.Status {
		case StatusBacklog:
			if task.IsClaimable(done) {
				ready = append(ready, task)
			} else {
				waiting++
			}
		case StatusClaimed, StatusDoing:
			inProgress = append(inProgress, task)
		case StatusVerifying:
			verifying = append(verifying, task)
		case StatusBlocked:
			blocked = append(blocked, task)
		case StatusDone:
			doneCount++
		case StatusCancelled:
			cancelledCount++
		}
	}
	sortBoardTasks(ready)
	sortBoardTasks(inProgress)
	sortBoardTasks(verifying)
	sortBoardTasks(blocked)
	entries := func(tasks []Task) []BoardEntry {
		out := []BoardEntry{}
		for _, task := range tasks {
			out = append(out, BoardEntry{Task: task, Age: now.Sub(task.CreatedAt.UTC())})
		}
		return out
	}
	return Board{
		GeneratedAt: now,
		Columns: []BoardColumn{
			{Title: "READY", Entries: entries(ready)},
			{Title: "IN_PROGRESS", Entries: entries(inProgress)},
			{Title: "VERIFYING", Entries: entries(verifying)},
			{Title: "BLOCKED", Entries: entries(blocked)},
		},
		WaitingCount: waiting, DoneCount: doneCount, CancelledCount: cancelledCount,
	}
}

func sortBoardTasks(tasks []Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		left, right := boardPriority(tasks[i].Priority), boardPriority(tasks[j].Priority)
		if left != right {
			return left > right
		}
		if !tasks[i].CreatedAt.Equal(tasks[j].CreatedAt) {
			return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
		}
		return tasks[i].ID < tasks[j].ID
	})
}

func boardPriority(value string) int {
	switch lower := trimLower(value); lower {
	case "critical", "urgent", "p0":
		return 4
	case "high", "p1":
		return 3
	case "low", "p3":
		return 1
	case "lowest", "p4":
		return 0
	default:
		return 2
	}
}

func trimLower(value string) string {
	out := []rune{}
	started := false
	for _, char := range value {
		if !started && (char == ' ' || char == '\t' || char == '\n' || char == '\r') {
			continue
		}
		started = true
		if char >= 'A' && char <= 'Z' {
			char += 'a' - 'A'
		}
		out = append(out, char)
	}
	for len(out) > 0 && (out[len(out)-1] == ' ' || out[len(out)-1] == '\t' || out[len(out)-1] == '\n' || out[len(out)-1] == '\r') {
		out = out[:len(out)-1]
	}
	return string(out)
}

func (e BoardEntry) Map() map[string]any {
	var owner any
	if e.Task.Owner != nil {
		owner = map[string]any{"adapter": e.Task.Owner.Adapter, "agent": e.Task.Owner.Agent, "session": e.Task.Owner.Session}
	}
	return map[string]any{"id": e.Task.ID, "title": e.Task.Title, "status": string(e.Task.Status), "priority": e.Task.Priority, "owner": owner, "blocker": e.Task.Blocker, "age_seconds": int64(e.Age.Seconds())}
}

func (c BoardColumn) Map() map[string]any {
	items := []any{}
	for _, entry := range c.Entries {
		items = append(items, entry.Map())
	}
	return map[string]any{"title": c.Title, "count": len(c.Entries), "tasks": items}
}

func (b Board) Map() map[string]any {
	columns := []any{}
	for _, column := range b.Columns {
		columns = append(columns, column.Map())
	}
	return map[string]any{"generated_at": formatTimestamp(b.GeneratedAt), "columns": columns, "waiting_count": b.WaitingCount, "done_count": b.DoneCount, "cancelled_count": b.CancelledCount}
}
