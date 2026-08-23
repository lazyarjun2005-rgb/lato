package task

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tasks.json")
	s, err := LoadFrom(path, "proj1")
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestStartCreatesActiveTask(t *testing.T) {
	s, _ := testStore(t)
	tk, err := s.Start("Add authentication to this project.")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != StatusActive || tk.Goal == "" || tk.ID == "" {
		t.Fatalf("task = %+v", tk)
	}
}

// TestSingleActivePolicy pins ambiguity safety: starting a new complex
// task pauses the previous active one.
func TestSingleActivePolicy(t *testing.T) {
	s, _ := testStore(t)
	first, _ := s.Start("first goal")
	second, _ := s.Start("second goal")

	gotFirst, err := s.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotFirst.Status != StatusPaused {
		t.Errorf("previous active status = %q, want paused", gotFirst.Status)
	}
	if second.Status != StatusActive {
		t.Errorf("new task status = %q", second.Status)
	}
}

func TestPersistenceAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	s, _ := LoadFrom(path, "proj1")
	tk, _ := s.Start("goal one")
	tk.NoteAction("edit_file: internal/auth/login.go")
	tk.AddChangedFile("internal/auth/login.go")
	tk.SetVerification("go test ./... → failed")
	if err := s.Save(tk); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadFrom(path, "proj1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.Get(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastAction != "edit_file: internal/auth/login.go" ||
		len(got.FilesChanged) != 1 ||
		!strings.Contains(got.Verification, "failed") {
		t.Fatalf("reloaded = %+v", got)
	}
}

func TestProjectIsolation(t *testing.T) {
	dir := t.TempDir()
	sa, _ := LoadFrom(filepath.Join(dir, "a.json"), "alpha")
	sb, _ := LoadFrom(filepath.Join(dir, "b.json"), "beta")

	if _, err := sa.Start("alpha-only goal"); err != nil {
		t.Fatal(err)
	}
	if len(sb.All()) != 0 {
		t.Error("project B sees project A tasks")
	}
}

func TestResumableExcludesFinished(t *testing.T) {
	s, _ := testStore(t)
	active, _ := s.Start("active work")
	paused, _ := s.Start("paused work") // pauses active
	done, _ := s.Start("finished work")
	_ = s.SetStatus(done.ID, StatusCompleted)

	rs := s.Resumable()
	if len(rs) != 2 {
		t.Fatalf("resumable = %d, want active+paused only", len(rs))
	}
	ids := map[string]bool{rs[0].ID: true, rs[1].ID: true}
	if !ids[paused.ID] {
		t.Error("paused task missing from resumable set")
	}
	_ = active
	if _, err := s.Get(done.ID); err != nil {
		t.Error("completed task should remain in history")
	}
}

func TestHistoryBoundNeverDropsResumable(t *testing.T) {
	s, _ := testStore(t)
	keeper, _ := s.Start("long-running work")

	for i := 0; i < maxTasksPerProject+5; i++ {
		tk, _ := s.Start("churn " + itoa(i))
		_ = s.SetStatus(tk.ID, StatusCompleted)
	}

	if len(s.All()) > maxTasksPerProject {
		t.Errorf("history grew to %d, cap %d", len(s.All()), maxTasksPerProject)
	}
	if _, err := s.Get(keeper.ID); err != nil {
		t.Error("resumable task was pruned by history churn")
	}
}

func TestPrefixResolutionAndAmbiguity(t *testing.T) {
	s, _ := testStore(t)
	a, _ := s.Start("alpha task aaaa")
	b, _ := s.Start("beta task bbbb")

	if _, err := s.Get(a.ID[:4]); err != nil {
		t.Errorf("unique prefix failed: %v", err)
	}
	short := commonPrefix(a.ID, b.ID)
	if _, err := s.Get(short); err == nil {
		t.Error("ambiguous prefix accepted")
	}
	_ = b
}

func commonPrefix(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return a[:i]
}

func TestSecretRedactionInFields(t *testing.T) {
	s, path := testStore(t)
	tk, _ := s.Start("connect using API_KEY=super-secret-9999 value")

	reloaded, _ := LoadFrom(path, "proj1")
	got, _ := reloaded.Get(tk.ID)
	if strings.Contains(got.Goal, "super-secret-9999") {
		t.Errorf("secret persisted in goal: %q", got.Goal)
	}

	tk.NoteAction("run_command with token: zzz-secret-token-zz")
	_ = s.Save(tk)
	reloaded2, _ := LoadFrom(path, "proj1")
	got2, _ := reloaded2.Get(tk.ID)
	if strings.Contains(got2.LastAction, "zzz-secret-token-zz") {
		t.Errorf("secret persisted in last action: %q", got2.LastAction)
	}
}

func TestPlanParsingFromModelOutput(t *testing.T) {
	s, _ := testStore(t)
	tk, _ := s.Start("multi-step goal")

	text := "Here is my plan:\n\n1. Inspect authentication architecture.\n" +
		"2. Implement login handler.\n3) Add validation.\n4. Run tests and verify."

	if !tk.SetPlanFromText(text) {
		t.Fatal("plan not captured")
	}
	if len(tk.Steps) != 4 {
		t.Fatalf("steps = %d (%+v)", len(tk.Steps), tk.Steps)
	}
	if tk.Steps[0].Title != "Inspect authentication architecture." || tk.Steps[0].State != "pending" {
		t.Errorf("step 0 = %+v", tk.Steps[0])
	}

	// Prose without numbered steps must NOT become a plan.
	tk2, _ := s.Start("another goal")
	if tk2.SetPlanFromText("I will just start editing files right away.") {
		t.Error("prose accepted as plan")
	}
}

func TestPreviewVariants(t *testing.T) {
	s, _ := testStore(t)
	tk, _ := s.Start("Add authentication to this project.")
	tk.SetPlanFromText("1. Inspect auth code\n2. Implement login handler\n3. Run tests")
	tk.MarkStepComplete("Inspect")
	tk.MarkStepComplete("Implement")
	tk.NoteAction("edit_file: internal/auth/login.go")
	tk.NextAction = "Add validation and run tests"
	tk.Status = StatusPaused

	paused := tk.Preview()
	for _, want := range []string{
		"Task: Add authentication",
		"Progress: 2/3",
		"Last: edit_file",
		"Next: Add validation and run tests",
		"Verify: Pending",
		"Status: paused",
	} {
		if !strings.Contains(paused, want) {
			t.Errorf("paused preview missing %q:\n%s", want, paused)
		}
	}

	tk.MarkStepComplete("Run tests")
	tk.SetVerification("go test ./... → passed")
	tk.Status = StatusCompleted
	completed := tk.Preview()
	for _, want := range []string{
		"Task: Add authentication",
		"Progress: 3/3",
		"Next: None",
		"Verify: PASS (go test ./...)",
		"Status: completed",
	} {
		if !strings.Contains(completed, want) {
			t.Errorf("completed preview missing %q:\n%s", want, completed)
		}
	}
	if strings.Contains(completed, "\x1b[") {
		t.Error("preview contains ANSI escapes; must be plain text for /copy")
	}

	// Blocked tasks keep the same schema and surface the failure.
	tk2, _ := s.Start("Update database layer.")
	tk2.SetPlanFromText("1. Update models\n2. Migrate schema\n3. Run tests")
	tk2.MarkStepComplete("Update models")
	tk2.MarkStepComplete("Migrate schema")
	tk2.NoteAction("run_command: go test ./...")
	tk2.SetVerification("go test ./... → failed")
	tk2.AddChangedFile("db.go")
	tk2.NextAction = "Fix failing migration"
	tk2.Status = StatusBlocked
	blocked := tk2.Preview()
	for _, want := range []string{
		"Progress: 2/3",
		"Last: run_command",
		"Next: Fix failing migration",
		"Verify: FAILED (go test ./...)",
		"Files changed: db.go",
		"Status: blocked",
	} {
		if !strings.Contains(blocked, want) {
			t.Errorf("blocked preview missing %q:\n%s", want, blocked)
		}
	}
}

// TestPreviewInterruptedAndFileNames pins the M15 presentation rules:
// a task still marked active in a rendered preview stopped without a
// clean pause and is shown as interrupted, and changed files are listed
// by name rather than count.
func TestPreviewInterruptedAndFileNames(t *testing.T) {
	s, _ := testStore(t)
	tk, _ := s.Start("Add authentication.")
	tk.SetPlanFromText("1. Inspect\n2. Implement\n3. Test\n4. Verify")
	tk.MarkStepComplete("Inspect")
	tk.MarkStepComplete("Implement")
	tk.AddChangedFile("auth.go")
	tk.NoteAction("edit_file: auth.go")
	tk.NextAction = "Run tests"
	// Status remains active: the process died before a clean stop.
	got := tk.Preview()
	for _, want := range []string{
		"Progress: 2/4",
		"Last: edit_file: auth.go",
		"Verify: Pending",
		"Files changed: auth.go",
		"Status: interrupted",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("interrupted preview missing %q:\n%s", want, got)
		}
	}
}

// TestPreviewLongFileListIsCapped keeps a many-file task from producing
// an endless Files changed line.
func TestPreviewLongFileListIsCapped(t *testing.T) {
	s, _ := testStore(t)
	tk, _ := s.Start("Rename across the tree.")
	for i := 0; i < maxFilesChanged; i++ {
		tk.AddChangedFile(fmt.Sprintf("pkg/dir%02d/file_with_a_reasonably_long_name.go", i))
	}
	got := tk.Preview()
	line := ""
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "Files changed:") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no Files changed line in:\n%s", got)
	}
	if len(line) > 220 || !strings.Contains(line, "more)") {
		t.Errorf("file list not capped as expected: %s", line)
	}
}

// TestVerificationOutcome pins classification of recorded verification
// notes; free text stays unclassified rather than guessed.
func TestVerificationOutcome(t *testing.T) {
	cases := map[string]string{
		"go test ./... → passed":       "pass",
		"go build ./... -> passed":     "pass",
		"go test ./... → failed":       "fail",
		"gofmt -l . → passed":          "pass",
		"edited three files":           "",
		"":                             "",
		"go vet ./... → passed mostly": "",
	}
	var tk Task
	for v, want := range cases {
		tk.Verification = v
		if got := tk.VerificationOutcome(); got != want {
			t.Errorf("VerificationOutcome(%q) = %q, want %q", v, got, want)
		}
	}
}

// TestStatusLabel pins the human-facing status names used in previews.
func TestStatusLabel(t *testing.T) {
	cases := map[Status]string{
		StatusActive:    "in progress",
		StatusPaused:    "paused",
		StatusCompleted: "completed",
		StatusBlocked:   "blocked",
		StatusAbandoned: "abandoned",
	}
	for status, want := range cases {
		if got := status.Label(); got != want {
			t.Errorf("Label(%q) = %q, want %q", status, got, want)
		}
	}
}

// TestProgressFromText pins model-reported step completion: only
// explicitly marked steps change, unknown numbers are ignored, and
// prose never completes anything.
func TestProgressFromText(t *testing.T) {
	var tk Task
	tk.SetPlanFromText("1. Inspect auth code\n2. Implement login handler\n3. Run tests")

	if tk.ProgressFromText("I feel good about step one.") {
		t.Error("prose changed plan state")
	}
	if !tk.ProgressFromText("[x] 1. Inspect auth code") {
		t.Fatal("marked step not recorded")
	}
	if done, total := tk.Progress(); done != 1 || total != 3 {
		t.Errorf("progress = %d/%d, want 1/3", done, total)
	}

	// Repeating a marker is idempotent; out-of-range numbers are ignored.
	if tk.ProgressFromText("[x] 1. Inspect auth code\n[x] 9. Does not exist") {
		t.Error("idempotent/out-of-range markers reported a change")
	}
	next, ok := tk.NextPending()
	if !ok || next.Title != "Implement login handler" {
		t.Errorf("next pending = %+v (ok=%v)", next, ok)
	}
}

func itoa(n int) string {
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
