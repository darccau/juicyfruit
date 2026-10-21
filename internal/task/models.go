package task

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Error struct{ Message string }

func (e *Error) Error() string                { return e.Message }
func Errorf(format string, args ...any) error { return &Error{Message: fmt.Sprintf(format, args...)} }

type Status string

const (
	StatusBacklog   Status = "BACKLOG"
	StatusClaimed   Status = "CLAIMED"
	StatusDoing     Status = "DOING"
	StatusVerifying Status = "VERIFYING"
	StatusBlocked   Status = "BLOCKED"
	StatusDone      Status = "DONE"
	StatusCancelled Status = "CANCELLED"
)

func ParseStatus(value string) (Status, error) {
	switch strings.ToUpper(value) {
	case "BACKLOG", "READY":
		return StatusBacklog, nil
	case "CLAIMED":
		return StatusClaimed, nil
	case "DOING":
		return StatusDoing, nil
	case "VERIFYING":
		return StatusVerifying, nil
	case "BLOCKED":
		return StatusBlocked, nil
	case "DONE":
		return StatusDone, nil
	case "CANCELLED":
		return StatusCancelled, nil
	default:
		return "", Errorf("Unsupported task status: %s", value)
	}
}

type Owner struct {
	Adapter string `json:"adapter"`
	Agent   string `json:"agent"`
	Session string `json:"session"`
}

type Checkpoint struct {
	Completed    []string          `json:"completed"`
	Current      *string           `json:"current"`
	Remaining    []string          `json:"remaining"`
	ChangedFiles []string          `json:"changed_files"`
	Validations  map[string]string `json:"validations"`
	NextAction   *string           `json:"next_action"`
}

func EmptyCheckpoint() Checkpoint {
	return Checkpoint{Completed: []string{}, Remaining: []string{}, ChangedFiles: []string{}, Validations: map[string]string{}}
}

func (c Checkpoint) Merge(update Checkpoint) Checkpoint {
	remaining := c.Remaining
	if len(update.Remaining) > 0 {
		remaining = update.Remaining
	}
	current := c.Current
	if update.Current != nil {
		current = update.Current
	}
	next := c.NextAction
	if update.NextAction != nil {
		next = update.NextAction
	}
	validations := map[string]string{}
	for key, value := range c.Validations {
		validations[key] = value
	}
	for key, value := range update.Validations {
		validations[key] = value
	}
	return Checkpoint{
		Completed: mergeList(c.Completed, update.Completed), Current: current,
		Remaining: remaining, ChangedFiles: mergeList(c.ChangedFiles, update.ChangedFiles),
		Validations: validations, NextAction: next,
	}
}

type ContextHints struct {
	Topics   []string `json:"topics"`
	Files    []string `json:"files"`
	Template *string  `json:"template,omitempty"`
}

func EmptyHints() ContextHints { return ContextHints{Topics: []string{}, Files: []string{}} }

type Task struct {
	Schema        string       `json:"schema"`
	ID            string       `json:"id"`
	Title         string       `json:"title"`
	Status        Status       `json:"status"`
	Priority      string       `json:"priority"`
	Run           *string      `json:"run"`
	Track         *string      `json:"track"`
	Worktree      *string      `json:"worktree"`
	Branch        *string      `json:"branch"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
	Owner         *Owner       `json:"owner"`
	PreviousOwner *Owner       `json:"previous_owner"`
	Dependencies  []string     `json:"dependencies"`
	Acceptance    []string     `json:"acceptance"`
	Context       ContextHints `json:"context"`
	Checkpoint    Checkpoint   `json:"checkpoint"`
	Blocker       *string      `json:"blocker"`
}

func (t Task) IsClaimable(done map[string]bool) bool {
	if t.Status != StatusBacklog || t.Owner != nil {
		return false
	}
	for _, dependency := range t.Dependencies {
		if !done[dependency] {
			return false
		}
	}
	return true
}

func (t Task) MarshalJSON() ([]byte, error) {
	checkpoint := normalizeCheckpoint(t.Checkpoint)
	context := t.Context
	context.Topics = nonNilStrings(context.Topics)
	context.Files = nonNilStrings(context.Files)
	return json.Marshal(struct {
		Schema        string       `json:"schema"`
		ID            string       `json:"id"`
		Title         string       `json:"title"`
		Status        Status       `json:"status"`
		Priority      string       `json:"priority"`
		Run           *string      `json:"run"`
		Track         *string      `json:"track"`
		Worktree      *string      `json:"worktree"`
		Branch        *string      `json:"branch"`
		CreatedAt     string       `json:"created_at"`
		UpdatedAt     string       `json:"updated_at"`
		Owner         *Owner       `json:"owner"`
		PreviousOwner *Owner       `json:"previous_owner"`
		Dependencies  []string     `json:"dependencies"`
		Acceptance    []string     `json:"acceptance"`
		Context       ContextHints `json:"context"`
		Checkpoint    Checkpoint   `json:"checkpoint"`
		Blocker       *string      `json:"blocker"`
	}{
		Schema: "juicyfruit.task/v1", ID: t.ID, Title: t.Title, Status: t.Status,
		Priority: t.Priority, Run: t.Run, Track: t.Track, Worktree: t.Worktree,
		Branch: t.Branch, CreatedAt: formatTimestamp(t.CreatedAt), UpdatedAt: formatTimestamp(t.UpdatedAt),
		Owner: t.Owner, PreviousOwner: t.PreviousOwner,
		Dependencies: nonNilStrings(t.Dependencies), Acceptance: nonNilStrings(t.Acceptance),
		Context: context, Checkpoint: checkpoint, Blocker: t.Blocker,
	})
}

func (t *Task) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw["schema"] != "juicyfruit.task/v1" {
		return Errorf("Unsupported task schema.")
	}
	getString := func(key string) (string, error) {
		value, ok := raw[key].(string)
		if !ok || value == "" {
			return "", Errorf("Invalid or missing string field: %s", key)
		}
		return value, nil
	}
	id, err := getString("id")
	if err != nil {
		return err
	}
	title, err := getString("title")
	if err != nil {
		return err
	}
	statusText, err := getString("status")
	if err != nil {
		return err
	}
	status, err := ParseStatus(statusText)
	if err != nil {
		return err
	}
	priority, err := getString("priority")
	if err != nil {
		return err
	}
	createdAt, err := parseTimeField(raw, "created_at", false)
	if err != nil {
		return err
	}
	updatedAt, err := parseTimeField(raw, "updated_at", false)
	if err != nil {
		return err
	}
	*t = Task{
		Schema: "juicyfruit.task/v1", ID: id, Title: title, Status: status, Priority: priority,
		Run: optionalString(raw, "run"), Track: optionalString(raw, "track"),
		Worktree: optionalString(raw, "worktree"), Branch: optionalString(raw, "branch"),
		CreatedAt: createdAt, UpdatedAt: updatedAt,
		Owner: parseOwner(raw["owner"]), PreviousOwner: parseOwner(raw["previous_owner"]),
		Dependencies: stringList(raw, "dependencies"), Acceptance: stringList(raw, "acceptance"),
		Context: parseHints(raw["context"]), Checkpoint: parseCheckpoint(raw["checkpoint"]),
		Blocker: optionalString(raw, "blocker"),
	}
	return nil
}

func (t Task) Copy() Task { return t }

type Event struct {
	Schema    string         `json:"schema"`
	ID        string         `json:"id"`
	Task      string         `json:"task"`
	Type      string         `json:"type"`
	CreatedAt time.Time      `json:"created_at"`
	Data      map[string]any `json:"data"`
}

type SessionStatus string

const (
	SessionActive SessionStatus = "ACTIVE"
	SessionClosed SessionStatus = "CLOSED"
)

type Session struct {
	Schema         string        `json:"schema"`
	ID             string        `json:"id"`
	Adapter        string        `json:"adapter"`
	Agent          string        `json:"agent"`
	DSHSessionID   *string       `json:"dsh_session_id"`
	StartedAt      time.Time     `json:"started_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	Status         SessionStatus `json:"status"`
	EndedAt        *time.Time    `json:"ended_at"`
	CloseReason    *string       `json:"close_reason"`
	TasksClaimed   []string      `json:"tasks_claimed"`
	TasksWorked    []string      `json:"tasks_worked"`
	LastCheckpoint *string       `json:"last_checkpoint"`
}

func (s Session) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Schema         string        `json:"schema"`
		ID             string        `json:"id"`
		Adapter        string        `json:"adapter"`
		Agent          string        `json:"agent"`
		DSHSessionID   *string       `json:"dsh_session_id"`
		StartedAt      string        `json:"started_at"`
		UpdatedAt      string        `json:"updated_at"`
		Status         SessionStatus `json:"status"`
		EndedAt        *string       `json:"ended_at"`
		CloseReason    *string       `json:"close_reason"`
		TasksClaimed   []string      `json:"tasks_claimed"`
		TasksWorked    []string      `json:"tasks_worked"`
		LastCheckpoint *string       `json:"last_checkpoint"`
	}{
		Schema: "juicyfruit.session/v1", ID: s.ID, Adapter: s.Adapter, Agent: s.Agent,
		DSHSessionID: s.DSHSessionID, StartedAt: formatTimestamp(s.StartedAt),
		UpdatedAt: formatTimestamp(s.UpdatedAt), Status: s.Status,
		EndedAt: formatOptionalTime(s.EndedAt), CloseReason: s.CloseReason,
		TasksClaimed: nonNilStrings(s.TasksClaimed), TasksWorked: nonNilStrings(s.TasksWorked),
		LastCheckpoint: s.LastCheckpoint,
	})
}

func (s *Session) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw["schema"] != "juicyfruit.session/v1" {
		return Errorf("Unsupported session schema.")
	}
	statusText, _ := raw["status"].(string)
	var status SessionStatus
	switch statusText {
	case "ACTIVE":
		status = SessionActive
	case "CLOSED":
		status = SessionClosed
	default:
		return Errorf("Unsupported session status: %s", statusText)
	}
	id, _ := raw["id"].(string)
	adapter, _ := raw["adapter"].(string)
	agent, _ := raw["agent"].(string)
	if id == "" || adapter == "" || agent == "" {
		return Errorf("Invalid session fields.")
	}
	startedAt, err := parseTimeField(raw, "started_at", false)
	if err != nil {
		return err
	}
	updatedAt, err := parseTimeField(raw, "updated_at", false)
	if err != nil {
		return err
	}
	var endedAt *time.Time
	if raw["ended_at"] != nil {
		value, err := parseTimeField(raw, "ended_at", true)
		if err != nil {
			return err
		}
		endedAt = &value
	}
	*s = Session{
		Schema: "juicyfruit.session/v1", ID: id, Adapter: adapter, Agent: agent,
		DSHSessionID: optionalString(raw, "dsh_session_id"), StartedAt: startedAt,
		UpdatedAt: updatedAt, Status: status, EndedAt: endedAt,
		CloseReason: optionalString(raw, "close_reason"), TasksClaimed: stringList(raw, "tasks_claimed"),
		TasksWorked: stringList(raw, "tasks_worked"), LastCheckpoint: optionalString(raw, "last_checkpoint"),
	}
	return nil
}

type Run struct {
	Schema    string    `json:"schema"`
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Summary   *string   `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Tasks     []string  `json:"tasks"`
}

func (r Run) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Schema    string   `json:"schema"`
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		Summary   *string  `json:"summary"`
		CreatedAt string   `json:"created_at"`
		UpdatedAt string   `json:"updated_at"`
		Tasks     []string `json:"tasks"`
	}{
		Schema: "juicyfruit.run/v1", ID: r.ID, Title: r.Title, Summary: r.Summary,
		CreatedAt: formatTimestamp(r.CreatedAt), UpdatedAt: formatTimestamp(r.UpdatedAt),
		Tasks: nonNilStrings(r.Tasks),
	})
}

func (r *Run) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw["schema"] != "juicyfruit.run/v1" {
		return Errorf("Unsupported run schema.")
	}
	id, _ := raw["id"].(string)
	title, _ := raw["title"].(string)
	if id == "" || title == "" {
		return Errorf("Invalid run fields.")
	}
	createdAt, err := parseTimeField(raw, "created_at", false)
	if err != nil {
		return err
	}
	updatedAt, err := parseTimeField(raw, "updated_at", false)
	if err != nil {
		return err
	}
	*r = Run{Schema: "juicyfruit.run/v1", ID: id, Title: title, Summary: optionalString(raw, "summary"), CreatedAt: createdAt, UpdatedAt: updatedAt, Tasks: stringList(raw, "tasks")}
	return nil
}

func parseOwner(value any) *Owner {
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	adapter, _ := m["adapter"].(string)
	agent, _ := m["agent"].(string)
	session, _ := m["session"].(string)
	return &Owner{Adapter: adapter, Agent: agent, Session: session}
}

func optionalString(raw map[string]any, key string) *string {
	value, ok := raw[key].(string)
	if !ok {
		return nil
	}
	return &value
}

func stringList(raw map[string]any, key string) []string {
	values, ok := raw[key].([]any)
	if !ok {
		return []string{}
	}
	out := []string{}
	for _, value := range values {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func parseHints(value any) ContextHints {
	m, ok := value.(map[string]any)
	if !ok {
		return EmptyHints()
	}
	return ContextHints{Topics: stringList(m, "topics"), Files: stringList(m, "files"), Template: optionalString(m, "template")}
}

func parseCheckpoint(value any) Checkpoint {
	m, ok := value.(map[string]any)
	if !ok {
		return EmptyCheckpoint()
	}
	validations := map[string]string{}
	if raw, ok := m["validations"].(map[string]any); ok {
		for key, value := range raw {
			if text, ok := value.(string); ok {
				validations[key] = text
			}
		}
	}
	return Checkpoint{
		Completed: stringList(m, "completed"), Current: optionalString(m, "current"),
		Remaining: stringList(m, "remaining"), ChangedFiles: stringList(m, "changed_files"),
		Validations: validations, NextAction: optionalString(m, "next_action"),
	}
}

func normalizeCheckpoint(c Checkpoint) Checkpoint {
	c.Completed = nonNilStrings(c.Completed)
	c.Remaining = nonNilStrings(c.Remaining)
	c.ChangedFiles = nonNilStrings(c.ChangedFiles)
	if c.Validations == nil {
		c.Validations = map[string]string{}
	}
	return c
}

func parseTimeField(raw map[string]any, key string, optional bool) (time.Time, error) {
	value, ok := raw[key].(string)
	if !ok || value == "" {
		if optional {
			return time.Time{}, nil
		}
		return time.Time{}, Errorf("Invalid or missing string field: %s", key)
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func mergeList(left, right []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range append(append([]string{}, left...), right...) {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func sortedUnique(values ...string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func formatTimestamp(value time.Time) string {
	value = value.UTC().Truncate(time.Microsecond)
	if value.Nanosecond()%1000 == 0 {
		return value.Format("2006-01-02T15:04:05.000Z")
	}
	return value.Format("2006-01-02T15:04:05.000000Z")
}

func formatOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	text := formatTimestamp(*value)
	return &text
}

func (e Event) MarshalJSON() ([]byte, error) {
	data := e.Data
	if data == nil {
		data = map[string]any{}
	}
	return json.Marshal(struct {
		Schema    string         `json:"schema"`
		ID        string         `json:"id"`
		Task      string         `json:"task"`
		Type      string         `json:"type"`
		CreatedAt string         `json:"created_at"`
		Data      map[string]any `json:"data"`
	}{
		Schema: "juicyfruit.task-event/v1", ID: e.ID, Task: e.Task, Type: e.Type,
		CreatedAt: formatTimestamp(e.CreatedAt), Data: data,
	})
}

func NewID(prefix string) string {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	value := time.Now().UnixMilli()
	timePart := make([]byte, 10)
	for index := 9; index >= 0; index-- {
		timePart[index] = alphabet[value&31]
		value >>= 5
	}
	entropy := make([]byte, 10)
	randomBytes := make([]byte, 10)
	_, _ = rand.Read(randomBytes)
	for index := range entropy {
		entropy[index] = alphabet[int(randomBytes[index])%32]
	}
	return prefix + "-" + string(timePart) + string(entropy)
}
