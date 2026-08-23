// Package task provides persistent session/task continuity for Lato
// (M12). A task records what a complex multi-step request was trying to
// achieve, how far it got, and where to pick up — surviving exits,
// crashes, and restarts. It is deliberately NOT conversation history and
// NOT project knowledge (that is internal/memory): only bounded,
// structured progress state is kept.
//
// Storage is local and outside any repository, under the operating
// system's user configuration directory, keyed by the same project
// identity as M11 memory (internal/memory.ProjectID), so tasks from one
// workspace never leak into another.
package task

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"lato/internal/memory"
)

// Status is the small explicit state set for tasks.
type Status string

const (
	StatusActive    Status = "active"    // currently executing in this process
	StatusPaused    Status = "paused"    // resumable; not currently running
	StatusCompleted Status = "completed" // reached verification/completion
	StatusBlocked   Status = "blocked"   // needs user input or external change
	StatusAbandoned Status = "abandoned" // retired by the user
)

// Bounds keep task state concise and history finite.
const (
	maxTasksPerProject = 20
	maxSteps           = 10
	maxFilesChanged    = 12
	fieldCap           = 200 // characters for single-line fields
	titleCap           = 80
)

// Step is one entry of a task's plan snapshot.
type Step struct {
	Title string `json:"title"`
	State string `json:"state"` // pending | completed | failed
}

// Task is one unit of persistent work.
type Task struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	Goal         string    `json:"goal"`
	Status       Status    `json:"status"`
	Steps        []Step    `json:"steps,omitempty"`
	LastAction   string    `json:"last_action,omitempty"`
	NextAction   string    `json:"next_action,omitempty"`
	Verification string    `json:"verification,omitempty"`
	FilesChanged []string  `json:"files_changed,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Store holds a project's task records.
type Store struct {
	path      string
	ProjectID string `json:"project_id"`
	Tasks     []Task `json:"tasks"`
}

// dir returns the task directory under Lato's user configuration.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	p := filepath.Join(base, "lato", "tasks")
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", fmt.Errorf("create task directory: %w", err)
	}
	return p, nil
}

// Load opens the task store for a project (identified by the M11
// project ID), creating an empty one when absent.
func Load(projectID string) (*Store, error) {
	d, err := Dir()
	if err != nil {
		return nil, err
	}
	return LoadFrom(filepath.Join(d, projectID+".json"), projectID)
}

// LoadFrom opens a store at an explicit path (tests use this).
func LoadFrom(path, projectID string) (*Store, error) {
	s := &Store{path: path, ProjectID: projectID}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read task store: %w", err)
	}
	if err := json.Unmarshal(raw, s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

func (s *Store) save() error {
	if s.ProjectID == "" {
		return errors.New("task store has no project identity; cannot persist")
	}
	if s.path == "" {
		d, err := Dir()
		if err != nil {
			return err
		}
		s.path = filepath.Join(d, "tasks.json")
	}
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tasks: %w", err)
	}
	return os.WriteFile(s.path, out, 0o600)
}

// Start begins a new active task, pausing any previously active one so
// at most one task is ever ambiguous-free. The goal is sanitized before
// storage.
func (s *Store) Start(goal string) (*Task, error) {
	goal = sanitizeLine(goal, fieldCap)
	if strings.TrimSpace(goal) == "" {
		return nil, errors.New("task goal cannot be empty")
	}
	for i := range s.Tasks {
		if s.Tasks[i].Status == StatusActive {
			s.Tasks[i].Status = StatusPaused
			s.Tasks[i].UpdatedAt = time.Now().UTC()
		}
	}
	now := time.Now().UTC()
	t := &Task{
		ID:        newID(),
		Goal:      goal,
		Status:    StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.Tasks = append(s.Tasks, *t)
	s.prune()
	if err := s.save(); err != nil {
		return nil, err
	}
	return t, nil
}

// Get resolves one task by exact ID or unambiguous prefix.
func (s *Store) Get(idOrPrefix string) (Task, error) {
	var matches []int
	for i := range s.Tasks {
		id := s.Tasks[i].ID
		if id == idOrPrefix || strings.HasPrefix(id, idOrPrefix) {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return Task{}, fmt.Errorf("no task with id %q", idOrPrefix)
	case 1:
		return s.Tasks[matches[0]], nil
	default:
		return Task{}, fmt.Errorf("id %q matches %d tasks; use more characters", idOrPrefix, len(matches))
	}
}

// Save upserts a task (by ID), refreshes its timestamp, sanitizes
// mutable fields, and prunes history. Active and paused tasks are never
// pruned.
func (s *Store) Save(t *Task) error {
	t.Goal = sanitizeLine(t.Goal, fieldCap)
	t.LastAction = sanitizeLine(t.LastAction, fieldCap)
	t.NextAction = sanitizeLine(t.NextAction, fieldCap)
	t.Verification = sanitizeLine(t.Verification, fieldCap)
	if len(t.FilesChanged) > maxFilesChanged {
		t.FilesChanged = t.FilesChanged[:maxFilesChanged]
	}
	t.UpdatedAt = time.Now().UTC()

	for i := range s.Tasks {
		if s.Tasks[i].ID == t.ID {
			s.Tasks[i] = *t
			s.prune()
			return s.save()
		}
	}
	s.Tasks = append(s.Tasks, *t)
	s.prune()
	return s.save()
}

// prune keeps history bounded without ever dropping resumable work.
func (s *Store) prune() {
	if len(s.Tasks) <= maxTasksPerProject {
		return
	}
	sort.SliceStable(s.Tasks, func(i, j int) bool { return s.Tasks[i].UpdatedAt.After(s.Tasks[j].UpdatedAt) })
	kept := make([]Task, 0, maxTasksPerProject)
	resumableCount := 0
	for _, tk := range s.Tasks {
		if tk.Status == StatusActive || tk.Status == StatusPaused {
			kept = append(kept, tk)
			resumableCount++
		}
	}
	slot := maxTasksPerProject - resumableCount
	for _, tk := range s.Tasks {
		if slot <= 0 {
			break
		}
		if tk.Status != StatusActive && tk.Status != StatusPaused {
			kept = append(kept, tk)
			slot--
		}
	}
	s.Tasks = kept
}

// Resumable lists tasks that can be continued (newest first).
func (s *Store) Resumable() []Task {
	var out []Task
	for _, t := range s.All() {
		if t.Status == StatusActive || t.Status == StatusPaused {
			out = append(out, t)
		}
	}
	return out
}

// All returns every task, newest first.
func (s *Store) All() []Task {
	out := make([]Task, len(s.Tasks))
	copy(out, s.Tasks)
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// SetStatus transitions one task's status.
func (s *Store) SetStatus(idOrPrefix string, status Status) error {
	i := -1
	var err error
	if i, err = s.indexOf(idOrPrefix); err != nil {
		return err
	}
	s.Tasks[i].Status = status
	s.Tasks[i].UpdatedAt = time.Now().UTC()
	return s.save()
}

func (s *Store) indexOf(idOrPrefix string) (int, error) {
	for i := range s.Tasks {
		id := s.Tasks[i].ID
		if id == idOrPrefix || strings.HasPrefix(id, idOrPrefix) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("no task with id %q", idOrPrefix)
}

// --- task mutation helpers (sanitize on write) ---------------------------

// SetPlanFromText extracts a numbered plan ("1. …") from model output
// into structured steps. It reports whether a real plan (≥2 steps) was
// captured; single lines are ignored as noise.
func (t *Task) SetPlanFromText(text string) bool {
	re := regexp.MustCompile(`(?m)^\s*\d+[.)]\s+(.+)$`)
	var steps []Step
	for _, m := range re.FindAllStringSubmatch(text, maxSteps) {
		title := sanitizeLine(m[1], titleCap)
		if title == "" {
			continue
		}
		steps = append(steps, Step{Title: title, State: "pending"})
	}
	if len(steps) < 2 {
		return false
	}
	if len(steps) > maxSteps {
		steps = steps[:maxSteps]
	}
	t.Steps = steps
	return true
}

// ProgressFromText applies model-reported step completion to the saved
// plan: lines like "[x] 2. Implement login handler" mark step 2 done.
// Numbers refer to the plan this task captured at its start; unknown
// numbers are ignored, and nothing is ever inferred from prose. Reports
// whether any step changed.
func (t *Task) ProgressFromText(text string) bool {
	re := regexp.MustCompile(`(?mi)^\s*\[x\]\s*(\d+)[.)]?`)
	changed := false
	for _, m := range re.FindAllStringSubmatch(text, maxSteps) {
		n := 0
		for _, r := range m[1] {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		if n < 1 || n > len(t.Steps) {
			continue
		}
		if t.Steps[n-1].State != "completed" {
			t.Steps[n-1].State = "completed"
			changed = true
		}
	}
	return changed
}

// NoteAction records the last meaningful action.
func (t *Task) NoteAction(action string) {
	t.LastAction = sanitizeLine(action, fieldCap)
}

// AddChangedFile remembers a file this task modified (bounded).
func (t *Task) AddChangedFile(path string) {
	path = sanitizeLine(path, fieldCap)
	if path == "" {
		return
	}
	for _, f := range t.FilesChanged {
		if f == path {
			return
		}
	}
	if len(t.FilesChanged) < maxFilesChanged {
		t.FilesChanged = append(t.FilesChanged, path)
	}
}

// SetVerification records verification status (e.g. "go test ./... passed").
func (t *Task) SetVerification(v string) {
	t.Verification = sanitizeLine(v, fieldCap)
}

// Progress returns completed-step count and total.
func (t *Task) Progress() (done, total int) {
	total = len(t.Steps)
	for _, s := range t.Steps {
		if s.State == "completed" {
			done++
		}
	}
	return done, total
}

// Preview renders the compact, structured status block shown after task
// boundaries and available after restart. Every status uses the same
// field set — Task, Progress, Last, Next, Verify, Files changed,
// Status — so the shape is predictable at a glance. It contains only
// recorded state fields; nothing is invented and no reasoning or tool
// output is included.
func (t *Task) Preview() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task: %s\n", t.Title())
	if done, total := t.Progress(); total > 0 {
		fmt.Fprintf(&b, "Progress: %d/%d\n", done, total)
	}
	if t.LastAction != "" {
		fmt.Fprintf(&b, "Last: %s\n", t.LastAction)
	}
	fmt.Fprintf(&b, "Next: %s\n", t.nextDisplay())
	fmt.Fprintf(&b, "Verify: %s\n", t.verifyDisplay())
	if files := t.changedFilesDisplay(); files != "" {
		fmt.Fprintf(&b, "Files changed: %s\n", files)
	}
	fmt.Fprintf(&b, "Status: %s", t.previewStatusLabel())
	return b.String()
}

// nextDisplay renders the Next line: the explicitly recorded next
// action when one exists (it is the most specific statement of intent),
// otherwise the first pending plan step. Completed tasks have no next
// step.
func (t *Task) nextDisplay() string {
	if t.Status == StatusCompleted {
		return "None"
	}
	if t.NextAction != "" {
		return t.NextAction
	}
	if next, ok := t.NextPending(); ok {
		return next.Title
	}
	return "-"
}

// verifyDisplay renders the Verify line from real recorded outcomes:
// PASS/FAILED for classified results, the raw note otherwise, Pending
// while work is ongoing, and "not run" for completions without one.
func (t *Task) verifyDisplay() string {
	switch t.VerificationOutcome() {
	case "pass":
		return "PASS (" + verificationCommand(t.Verification) + ")"
	case "fail":
		return "FAILED (" + verificationCommand(t.Verification) + ")"
	}
	if strings.TrimSpace(t.Verification) != "" {
		return t.Verification
	}
	if t.Status == StatusCompleted {
		return "not run"
	}
	return "Pending"
}

// verificationCommand extracts the command portion of a "<cmd> → passed"
// note.
func verificationCommand(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.LastIndex(v, "→"); i >= 0 {
		return strings.TrimSpace(v[:i])
	}
	if i := strings.LastIndex(v, "->"); i >= 0 {
		return strings.TrimSpace(v[:i])
	}
	return v
}

// changedFilesDisplay lists modified file names comma-separated, capped
// so a long list cannot dominate the preview.
func (t *Task) changedFilesDisplay() string {
	switch len(t.FilesChanged) {
	case 0:
		return ""
	case 1:
		return t.FilesChanged[0]
	}
	const maxLen = 160
	shown := 0
	var parts []string
	for i, f := range t.FilesChanged {
		candidate := strings.Join(append(append([]string{}, parts...), f), ", ")
		if candidate == "" || len(candidate) > maxLen && i > 0 {
			break
		}
		parts = append(parts, f)
		shown++
	}
	out := strings.Join(parts, ", ")
	if remaining := len(t.FilesChanged) - shown; remaining > 0 {
		out += fmt.Sprintf(" … (+%d more)", remaining)
	}
	return out
}

// previewStatusLabel maps the stored status to its end-of-run display
// form. A task still marked active in a rendered preview stopped without
// a clean pause (process exit, crash), which users read as interrupted;
// genuinely running tasks are never previewed.
func (t *Task) previewStatusLabel() string {
	if t.Status == StatusActive {
		return "interrupted"
	}
	return t.Status.Label()
}

// VerificationOutcome classifies the most recent recorded verification:
// "pass", "fail", or "" when nothing conclusive was recorded. It reads
// only the structured "<command> → passed|failed" notes written by the
// task tracker — no state is invented here.
func (t *Task) VerificationOutcome() string {
	v := strings.TrimSpace(t.Verification)
	for _, sep := range []string{"→", "->"} {
		switch {
		case strings.HasSuffix(v, sep+" passed"):
			return "pass"
		case strings.HasSuffix(v, sep+" failed"):
			return "fail"
		}
	}
	return ""
}

// Label renders the human-facing name of a status.
func (s Status) Label() string {
	switch s {
	case StatusActive:
		return "in progress"
	case StatusPaused:
		return "paused"
	case StatusCompleted:
		return "completed"
	case StatusBlocked:
		return "blocked"
	case StatusAbandoned:
		return "abandoned"
	default:
		return string(s)
	}
}

// NextPending returns the first step not yet completed.
func (t *Task) NextPending() (Step, bool) {
	for _, s := range t.Steps {
		if s.State != "completed" {
			return s, true
		}
	}
	return Step{}, false
}

// MarkStepComplete marks the first matching pending/in-progress step done.
func (t *Task) MarkStepComplete(titleFragment string) {
	for i := range t.Steps {
		if t.Steps[i].State != "completed" && strings.Contains(strings.ToLower(t.Steps[i].Title), strings.ToLower(titleFragment)) {
			t.Steps[i].State = "completed"
			return
		}
	}
}

// Title returns a short display title derived from the goal.
func (t *Task) Title() string {
	first := t.Goal
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = first[:i]
	}
	if len(first) > titleCap {
		first = first[:titleCap-1] + "…"
	}
	return first
}

// ResumeBrief renders the saved state embedded into a resume prompt.
func (t *Task) ResumeBrief() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Goal: %s\n", t.Goal)
	if len(t.Steps) > 0 {
		b.WriteString("Plan:\n")
		for _, s := range t.Steps {
			fmt.Fprintf(&b, "- [%s] %s\n", s.State, s.Title)
		}
	}
	if t.LastAction != "" {
		fmt.Fprintf(&b, "Last action: %s\n", t.LastAction)
	}
	if t.Verification != "" {
		fmt.Fprintf(&b, "Verification: %s\n", t.Verification)
	}
	if len(t.FilesChanged) > 0 {
		fmt.Fprintf(&b, "Files already modified: %s\n", strings.Join(t.FilesChanged, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// sanitizeLine trims, caps length, and redacts credential-shaped text so
// secrets never reach disk.
func sanitizeLine(s string, cap int) string {
	s = strings.TrimSpace(s)
	if memory.ContainsSecret(s) {
		s = memory.RedactIfSecret(s)
	}
	if len(s) > cap {
		s = s[:cap-1] + "…"
	}
	return s
}

func newID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
