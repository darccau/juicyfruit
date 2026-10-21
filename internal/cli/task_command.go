package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/darccau/juicyfruit/internal/task"
)

func taskCommand() *Command {
	group := &Command{Name: "task", Description: "Manage durable task runtime state."}
	jsonFlag := Spec{Name: "json", Type: BoolFlag, Help: "Emit JSON."}
	group.Children = []*Command{
		{Name: "create", Description: "Create a durable task.", Parser: NewParser(
			Spec{Name: "title", Type: StringFlag, Mandatory: true},
			Spec{Name: "priority", Type: StringFlag, Default: "normal"},
			Spec{Name: "run", Type: StringFlag}, Spec{Name: "track", Type: StringFlag},
			Spec{Name: "depends-on", Type: MultiFlag}, Spec{Name: "acceptance", Type: MultiFlag},
			Spec{Name: "topic", Type: MultiFlag}, Spec{Name: "file", Type: MultiFlag}, jsonFlag,
		), Run: runTaskCreate},
		{Name: "list", Description: "List durable tasks.", Parser: NewParser(Spec{Name: "status", Type: StringFlag}, Spec{Name: "owner", Type: StringFlag}, jsonFlag), Run: runTaskList},
		{Name: "ready", Description: "List tasks eligible to claim.", Parser: NewParser(Spec{Name: "track", Type: StringFlag}, jsonFlag), Run: runTaskReady},
		{Name: "show", Description: "Show a task.", Parser: NewParser(jsonFlag), Run: runTaskShow},
		{Name: "depend", Description: "Add dependencies to a backlog task.", Parser: NewParser(Spec{Name: "on", Type: MultiFlag}, jsonFlag), Run: runTaskDepend},
		{Name: "claim", Description: "Atomically claim a task.", Parser: NewParser(Spec{Name: "adapter", Type: StringFlag, Mandatory: true}, Spec{Name: "agent", Type: StringFlag, Default: "executor"}, Spec{Name: "session", Type: StringFlag, Mandatory: true}, jsonFlag), Run: runTaskClaim},
		{Name: "cleanup", Description: "Clean up task worktree and branch.", Parser: NewParser(Spec{Name: "force", Type: BoolFlag}, jsonFlag), Run: runTaskCleanup},
		{Name: "start", Description: "Start active work on a claimed task.", Parser: NewParser(jsonFlag), Run: taskTransition("start")},
		{Name: "checkpoint", Description: "Persist a compact task checkpoint.", Parser: NewParser(Spec{Name: "completed", Type: MultiFlag}, Spec{Name: "current", Type: StringFlag}, Spec{Name: "remaining", Type: MultiFlag}, Spec{Name: "file", Type: MultiFlag}, Spec{Name: "validation", Type: MultiFlag}, Spec{Name: "next-action", Type: StringFlag}, jsonFlag), Run: runTaskCheckpoint},
		{Name: "block", Description: "Block a task.", Parser: NewParser(Spec{Name: "reason", Type: StringFlag, Mandatory: true}, jsonFlag), Run: runTaskBlock},
		{Name: "unblock", Description: "Return a blocked task to backlog.", Parser: NewParser(jsonFlag), Run: taskTransition("unblock")},
		{Name: "verify", Description: "Move a task into verification.", Parser: NewParser(jsonFlag), Run: taskTransition("verify")},
		{Name: "done", Description: "Mark a verified task done.", Parser: NewParser(jsonFlag), Run: taskTransition("done")},
		{Name: "release", Description: "Release ownership for handoff.", Parser: NewParser(jsonFlag), Run: taskTransition("release")},
		{Name: "cancel", Description: "Cancel a task.", Parser: NewParser(jsonFlag), Run: taskTransition("cancel")},
		{Name: "resume", Description: "Build a compact handoff packet for a task.", Parser: NewParser(jsonFlag), Run: runTaskResume},
		{Name: "report", Description: "Summarize time-in-status, blocking rate, and verification speed across every durable task.", Parser: NewParser(jsonFlag), Run: runTaskReport},
		{Name: "board", Description: "Render READY, IN_PROGRESS, VERIFYING, and BLOCKED tasks as a terminal board.", Parser: NewParser(jsonFlag, Spec{Name: "watch", Type: BoolFlag}, Spec{Name: "interval", Type: StringFlag, Default: "2"}, Spec{Name: "max-iterations", Type: StringFlag, Hidden: true}), Run: runTaskBoard},
	}
	return group
}

func runTaskCreate(ctx *Context, values *Values) error {
	title, err := requireOption(values, "title")
	if err != nil {
		return err
	}
	t, err := ctx.App.Tasks.CreateTask(title, values.String("priority"), values.StringPtr("run"), values.StringPtr("track"), values.List("depends-on"), values.List("acceptance"), task.ContextHints{Topics: values.List("topic"), Files: values.List("file")})
	if err != nil {
		return err
	}
	if values.Bool("json") {
		ctx.Log.Info(jsonText(t, true))
	} else {
		ctx.Log.Success("Created %s: %s", t.ID, t.Title)
	}
	return nil
}

func requireOption(values *Values, name string) (string, error) {
	value := strings.TrimSpace(values.String(name))
	if value == "" {
		return "", Usagef("Missing required option --%s.", name)
	}
	return value, nil
}

func runTaskList(ctx *Context, values *Values) error {
	tasks, err := ctx.App.Tasks.ListTasks()
	if err != nil {
		return err
	}
	if status := values.String("status"); status != "" {
		if strings.EqualFold(status, "READY") {
			tasks, err = ctx.App.Tasks.ReadyTasks("")
			if err != nil {
				return err
			}
		} else {
			parsed, err := task.ParseStatus(status)
			if err != nil {
				return err
			}
			filtered := []task.Task{}
			for _, item := range tasks {
				if item.Status == parsed {
					filtered = append(filtered, item)
				}
			}
			tasks = filtered
		}
	}
	if owner := values.String("owner"); owner != "" {
		filtered := []task.Task{}
		for _, item := range tasks {
			if item.Owner != nil && item.Owner.Adapter == owner {
				filtered = append(filtered, item)
			}
		}
		tasks = filtered
	}
	if values.Bool("json") {
		ctx.Log.Info(jsonText(tasks, true))
	} else if len(tasks) == 0 {
		ctx.Log.Info("No tasks.")
	} else {
		for _, item := range tasks {
			owner := "-"
			if item.Owner != nil {
				owner = item.Owner.Adapter
			}
			ctx.Log.Info("%s\t%s\t%s\t%s", item.ID, item.Status, owner, item.Title)
		}
	}
	return nil
}

func runTaskReady(ctx *Context, values *Values) error {
	tasks, err := ctx.App.Tasks.ReadyTasks(values.String("track"))
	if err != nil {
		return err
	}
	if values.Bool("json") {
		ctx.Log.Info(jsonText(tasks, true))
	} else if len(tasks) == 0 {
		ctx.Log.Info("No ready tasks.")
	} else {
		for _, item := range tasks {
			ctx.Log.Info("%s\tREADY\t%s", item.ID, item.Title)
		}
	}
	return nil
}

func runTaskShow(ctx *Context, values *Values) error {
	id, err := taskIDOrError(values)
	if err != nil {
		return err
	}
	item, err := ctx.App.Tasks.ReadTask(id)
	if err != nil {
		return err
	}
	if values.Bool("json") {
		ctx.Log.Info(jsonText(item, true))
	} else {
		ctx.Log.Info(resumeText(item))
	}
	return nil
}

func runTaskDepend(ctx *Context, values *Values) error {
	id, err := taskIDOrError(values)
	if err != nil {
		return err
	}
	if len(values.List("on")) == 0 {
		return Usagef("Expected at least one --on dependency.")
	}
	item, err := ctx.App.Tasks.AddDependencies(id, values.List("on"))
	if err != nil {
		return err
	}
	emitTask(ctx, values, item, "Updated dependencies for %s.", item.ID)
	return nil
}

func runTaskClaim(ctx *Context, values *Values) error {
	id, err := taskIDOrError(values)
	if err != nil {
		return err
	}
	item, err := ctx.App.Tasks.ClaimTask(id, values.String("adapter"), values.String("agent"), values.String("session"))
	if err != nil {
		return err
	}
	emitTask(ctx, values, item, "Claimed %s.", item.ID)
	return nil
}

func runTaskCleanup(ctx *Context, values *Values) error {
	id, err := taskIDOrError(values)
	if err != nil {
		return err
	}
	item, err := ctx.App.Tasks.CleanupTask(id, values.Bool("force"))
	if err != nil {
		return err
	}
	emitTask(ctx, values, item, "Cleaned up %s.", item.ID)
	return nil
}

func taskTransition(name string) func(*Context, *Values) error {
	return func(ctx *Context, values *Values) error {
		id, err := taskIDOrError(values)
		if err != nil {
			return err
		}
		var item task.Task
		switch name {
		case "start":
			item, err = ctx.App.Tasks.StartTask(id)
		case "unblock":
			item, err = ctx.App.Tasks.UnblockTask(id)
		case "verify":
			item, err = ctx.App.Tasks.VerifyTask(id)
		case "done":
			item, err = ctx.App.Tasks.DoneTask(id)
		case "release":
			item, err = ctx.App.Tasks.ReleaseTask(id)
		case "cancel":
			item, err = ctx.App.Tasks.CancelTask(id)
		}
		if err != nil {
			return err
		}
		emitTask(ctx, values, item, "%s: %s", item.ID, item.Status)
		return nil
	}
}

func runTaskBlock(ctx *Context, values *Values) error {
	id, err := taskIDOrError(values)
	if err != nil {
		return err
	}
	reason, err := requireOption(values, "reason")
	if err != nil {
		return err
	}
	item, err := ctx.App.Tasks.BlockTask(id, reason)
	if err != nil {
		return err
	}
	emitTask(ctx, values, item, "Blocked %s.", item.ID)
	return nil
}

func runTaskCheckpoint(ctx *Context, values *Values) error {
	id, err := taskIDOrError(values)
	if err != nil {
		return err
	}
	validations := map[string]string{}
	for _, value := range values.List("validation") {
		if cut := strings.IndexByte(value, '='); cut >= 0 {
			validations[value[:cut]] = value[cut+1:]
		}
	}
	item, err := ctx.App.Tasks.CheckpointTask(id, task.Checkpoint{
		Completed: values.List("completed"), Current: values.StringPtr("current"),
		Remaining: values.List("remaining"), ChangedFiles: values.List("file"),
		Validations: validations, NextAction: values.StringPtr("next-action"),
	})
	if err != nil {
		return err
	}
	emitTask(ctx, values, item, "Checkpointed %s.", item.ID)
	return nil
}

func runTaskResume(ctx *Context, values *Values) error {
	id, err := taskIDOrError(values)
	if err != nil {
		return err
	}
	item, err := ctx.App.Tasks.ReadTask(id)
	if err != nil {
		return err
	}
	if values.Bool("json") {
		ctx.Log.Info(jsonText(resumeMap(item), true))
	} else {
		ctx.Log.Info(resumeText(item))
	}
	return nil
}

func resumeMap(item task.Task) map[string]any {
	return map[string]any{
		"task": item.ID, "title": item.Title, "status": string(item.Status), "run": item.Run,
		"acceptance": item.Acceptance, "owner": item.Owner, "previous_owner": item.PreviousOwner,
		"completed": item.Checkpoint.Completed, "current": item.Checkpoint.Current,
		"remaining":      item.Checkpoint.Remaining,
		"relevant_files": map[string]any{"context": item.Context.Files, "changed": item.Checkpoint.ChangedFiles},
		"validations":    item.Checkpoint.Validations, "blocker": item.Blocker,
		"next_action": item.Checkpoint.NextAction,
	}
}

func resumeText(item task.Task) string {
	join := func(values []string) string {
		if len(values) == 0 {
			return "-"
		}
		return strings.Join(values, ", ")
	}
	ptr := func(value *string) string {
		if value == nil {
			return "-"
		}
		return *value
	}
	owner := func(value *task.Owner) string {
		if value == nil {
			return "-"
		}
		return value.Adapter + " / " + value.Agent + " / " + value.Session
	}
	validations, _ := json.Marshal(item.Checkpoint.Validations)
	return fmt.Sprintf("Task: %s\nTitle: %s\nStatus: %s\nRun: %s\nOwner: %s\nPrevious owner: %s\nAcceptance: %s\nCompleted: %s\nCurrent: %s\nRemaining: %s\nContext files: %s\nChanged files: %s\nValidations: %s\nBlocker: %s\nNext action: %s",
		item.ID, item.Title, item.Status, ptr(item.Run), owner(item.Owner), owner(item.PreviousOwner),
		join(item.Acceptance), join(item.Checkpoint.Completed), ptr(item.Checkpoint.Current), join(item.Checkpoint.Remaining),
		join(item.Context.Files), join(item.Checkpoint.ChangedFiles), string(validations), ptr(item.Blocker), ptr(item.Checkpoint.NextAction))
}

func runTaskReport(ctx *Context, values *Values) error {
	tasks, err := ctx.App.Tasks.ListTasks()
	if err != nil {
		return err
	}
	events := map[string][]task.Event{}
	for _, item := range tasks {
		list, err := ctx.App.Tasks.ListTaskEvents(item.ID)
		if err != nil {
			return err
		}
		events[item.ID] = list
	}
	report := task.BuildReport(tasks, events, time.Now())
	if values.Bool("json") {
		ctx.Log.Info(jsonText(report.Map(), true))
	} else {
		ctx.Log.Info(reportText(report))
	}
	return nil
}

func reportText(report task.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task Runtime Report — %s\n", cliTimestamp(report.GeneratedAt))
	fmt.Fprintf(&b, "Total tasks: %d\n", len(report.Entries))
	fmt.Fprintf(&b, "Blocked at least once: %d\n", report.BlockedTasksCount)
	fmt.Fprintf(&b, "Average VERIFYING -> DONE: %s\n\n", formatDurationPtr(report.AverageVerifyDuration))
	b.WriteString("By status:\n")
	for _, status := range []task.Status{task.StatusBacklog, task.StatusClaimed, task.StatusDoing, task.StatusVerifying, task.StatusBlocked, task.StatusDone, task.StatusCancelled} {
		count := report.CountByStatus[status]
		if count == 0 {
			continue
		}
		fmt.Fprintf(&b, "  %s: %d task(s), %s total time\n", status, count, formatDuration(report.TotalTimeByStatus[status]))
	}
	if len(report.DoingTimeByAdapter) > 0 {
		b.WriteString("\nDOING time by adapter:\n")
		keys := []string{}
		for key := range report.DoingTimeByAdapter {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(&b, "  %s: %s\n", key, formatDuration(report.DoingTimeByAdapter[key]))
		}
	}
	if len(report.Entries) > 0 {
		b.WriteString("\nTasks:\n")
		for _, entry := range report.Entries {
			fmt.Fprintf(&b, "  %s [%s] %s — age %s, blocked %dx\n", entry.Task.ID, entry.Task.Status, entry.Task.Title, formatDuration(entry.Age), entry.BlockedCount)
		}
	}
	return strings.TrimRight(b.String(), " \t\r\n")
}

func cliTimestamp(value time.Time) string {
	value = value.UTC().Truncate(time.Microsecond)
	if value.Nanosecond()%1000 == 0 {
		return value.Format("2006-01-02T15:04:05.000Z")
	}
	return value.Format("2006-01-02T15:04:05.000000Z")
}

func formatDuration(value time.Duration) string {
	hours := int(value.Hours())
	minutes := int(value.Minutes()) % 60
	seconds := int(value.Seconds()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func formatDurationPtr(value *time.Duration) string {
	if value == nil {
		return "-"
	}
	return formatDuration(*value)
}

func runTaskBoard(ctx *Context, values *Values) error {
	watch := values.Bool("watch")
	if !watch {
		board, err := currentBoard(ctx)
		if err != nil {
			return err
		}
		renderBoard(ctx, board, values.Bool("json"))
		return nil
	}
	interval, err := values.Int("interval")
	if err != nil || interval <= 0 {
		return Usagef("--interval must be a positive integer.")
	}
	var maxIterations *int
	if values.Parsed("max-iterations") {
		parsed, err := values.Int("max-iterations")
		if err != nil {
			return Usagef("--max-iterations must be an integer.")
		}
		maxIterations = &parsed
	}
	for iteration := 0; maxIterations == nil || iteration < *maxIterations; iteration++ {
		board, err := currentBoard(ctx)
		if err != nil {
			return err
		}
		if !values.Bool("json") && iteration > 0 {
			ctx.Log.Info("\x1B[2J\x1B[H")
		}
		renderBoard(ctx, board, values.Bool("json"))
		if maxIterations != nil && iteration+1 >= *maxIterations {
			break
		}
		time.Sleep(time.Duration(interval) * time.Second)
	}
	return nil
}

func currentBoard(ctx *Context) (task.Board, error) {
	tasks, err := ctx.App.Tasks.ListTasks()
	if err != nil {
		return task.Board{}, err
	}
	return task.BuildBoard(tasks, time.Now()), nil
}

func renderBoard(ctx *Context, board task.Board, asJSON bool) {
	if asJSON {
		ctx.Log.Info(jsonText(board.Map(), true))
	} else {
		ctx.Log.Info(boardText(board))
	}
}

func boardText(board task.Board) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task Board — %s\n", cliTimestamp(board.GeneratedAt))
	for _, column := range board.Columns {
		fmt.Fprintf(&b, "\n%s (%d)\n", column.Title, len(column.Entries))
		if len(column.Entries) == 0 {
			b.WriteString("  (empty)\n")
			continue
		}
		for _, entry := range column.Entries {
			extra := ""
			if entry.Task.Owner != nil {
				extra += fmt.Sprintf(" · %s/%s", entry.Task.Owner.Adapter, entry.Task.Owner.Agent)
			}
			if entry.Task.Blocker != nil {
				extra += " · blocked: " + *entry.Task.Blocker
			}
			fmt.Fprintf(&b, "  %s [%s] %s%s · age %s\n", entry.Task.ID, entry.Task.Priority, entry.Task.Title, extra, formatDuration(entry.Age))
		}
	}
	fmt.Fprintf(&b, "\nWaiting on dependencies: %d · Done: %d · Cancelled: %d", board.WaitingCount, board.DoneCount, board.CancelledCount)
	return b.String()
}
