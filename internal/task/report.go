package task

import (
	"sort"
	"time"
)

type StatusInterval struct {
	Status Status
	Start  time.Time
	End    time.Time
}

type ReportEntry struct {
	Task           Task
	Age            time.Duration
	TimeInStatus   map[Status]time.Duration
	BlockedCount   int
	VerifyDuration *time.Duration
}

type Report struct {
	GeneratedAt           time.Time
	Entries               []ReportEntry
	CountByStatus         map[Status]int
	TotalTimeByStatus     map[Status]time.Duration
	BlockedTasksCount     int
	AverageVerifyDuration *time.Duration
	DoingTimeByAdapter    map[string]time.Duration
}

var statusTransitionEvents = map[string]Status{
	"created": StatusBacklog, "claimed": StatusClaimed, "started": StatusDoing,
	"blocked": StatusBlocked, "unblocked": StatusBacklog, "verifying": StatusVerifying,
	"done": StatusDone, "cancelled": StatusCancelled, "released": StatusBacklog,
}

func BuildReport(tasks []Task, eventsByTask map[string][]Event, now time.Time) Report {
	now = now.UTC()
	entries := []ReportEntry{}
	countByStatus := map[Status]int{}
	totalTimeByStatus := map[Status]time.Duration{}
	doingTimeByAdapter := map[string]time.Duration{}
	verifyDurations := []time.Duration{}
	blockedTasksCount := 0
	for _, task := range tasks {
		events := append([]Event{}, eventsByTask[task.ID]...)
		sort.SliceStable(events, func(i, j int) bool { return events[i].CreatedAt.Before(events[j].CreatedAt) })
		type transition struct {
			At     time.Time
			Status Status
		}
		transitions := []transition{}
		for _, event := range events {
			if status, ok := statusTransitionEvents[event.Type]; ok {
				transitions = append(transitions, transition{At: event.CreatedAt.UTC(), Status: status})
			}
		}
		timeInStatus := map[Status]time.Duration{}
		blockedCount := 0
		var verifyingAt, doneAt *time.Time
		for index, item := range transitions {
			status := item.Status
			start := item.At
			terminal := status == StatusDone || status == StatusCancelled
			var end time.Time
			if index+1 < len(transitions) {
				end = transitions[index+1].At
			} else if terminal {
				end = start
			} else {
				end = now
			}
			timeInStatus[status] += end.Sub(start)
			if status == StatusBlocked {
				blockedCount++
			}
			if status == StatusVerifying {
				value := start
				verifyingAt = &value
			}
			if status == StatusDone {
				value := start
				doneAt = &value
			}
		}
		var verifyDuration *time.Duration
		if verifyingAt != nil && doneAt != nil && !doneAt.Before(*verifyingAt) {
			value := doneAt.Sub(*verifyingAt)
			verifyDuration = &value
			verifyDurations = append(verifyDurations, value)
		}
		age := now.Sub(task.CreatedAt.UTC())
		if task.Status == StatusDone || task.Status == StatusCancelled {
			age = task.UpdatedAt.UTC().Sub(task.CreatedAt.UTC())
		}
		entries = append(entries, ReportEntry{Task: task, Age: age, TimeInStatus: timeInStatus, BlockedCount: blockedCount, VerifyDuration: verifyDuration})
		countByStatus[task.Status]++
		for status, duration := range timeInStatus {
			totalTimeByStatus[status] += duration
		}
		if blockedCount > 0 {
			blockedTasksCount++
		}
		if duration := timeInStatus[StatusDoing]; duration > 0 {
			adapter := "unassigned"
			if task.Owner != nil {
				adapter = task.Owner.Adapter
			}
			doingTimeByAdapter[adapter] += duration
		}
	}
	var averageVerifyDuration *time.Duration
	if len(verifyDurations) > 0 {
		total := time.Duration(0)
		for _, duration := range verifyDurations {
			total += duration
		}
		value := total / time.Duration(len(verifyDurations))
		averageVerifyDuration = &value
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].BlockedCount != entries[j].BlockedCount {
			return entries[i].BlockedCount > entries[j].BlockedCount
		}
		return entries[i].Task.ID < entries[j].Task.ID
	})
	return Report{GeneratedAt: now, Entries: entries, CountByStatus: countByStatus, TotalTimeByStatus: totalTimeByStatus, BlockedTasksCount: blockedTasksCount, AverageVerifyDuration: averageVerifyDuration, DoingTimeByAdapter: doingTimeByAdapter}
}

func (e ReportEntry) Map() map[string]any {
	var owner any
	if e.Task.Owner != nil {
		owner = map[string]any{"adapter": e.Task.Owner.Adapter, "agent": e.Task.Owner.Agent, "session": e.Task.Owner.Session}
	}
	timeIn := map[string]any{}
	for status, duration := range e.TimeInStatus {
		timeIn[string(status)] = int64(duration.Seconds())
	}
	var verify any
	if e.VerifyDuration != nil {
		verify = int64(e.VerifyDuration.Seconds())
	}
	return map[string]any{"id": e.Task.ID, "title": e.Task.Title, "status": string(e.Task.Status), "priority": e.Task.Priority, "owner": owner, "age_seconds": int64(e.Age.Seconds()), "time_in_status_seconds": timeIn, "blocked_count": e.BlockedCount, "verify_duration_seconds": verify}
}

func (r Report) Map() map[string]any {
	entries := []any{}
	for _, entry := range r.Entries {
		entries = append(entries, entry.Map())
	}
	statusCounts := map[string]any{}
	for status, count := range r.CountByStatus {
		statusCounts[string(status)] = count
	}
	timeByStatus := map[string]any{}
	for status, duration := range r.TotalTimeByStatus {
		timeByStatus[string(status)] = int64(duration.Seconds())
	}
	doing := map[string]any{}
	for adapter, duration := range r.DoingTimeByAdapter {
		doing[adapter] = int64(duration.Seconds())
	}
	var verify any
	if r.AverageVerifyDuration != nil {
		verify = int64(r.AverageVerifyDuration.Seconds())
	}
	return map[string]any{
		"generated_at": formatTimestamp(r.GeneratedAt), "total_tasks": len(r.Entries),
		"count_by_status": statusCounts, "total_time_by_status_seconds": timeByStatus,
		"blocked_tasks_count": r.BlockedTasksCount, "average_verify_duration_seconds": verify,
		"doing_time_by_adapter_seconds": doing, "tasks": entries,
	}
}
