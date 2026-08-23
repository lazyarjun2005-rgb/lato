package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/command"
	"lato/internal/effort"
	"lato/internal/index"
	"lato/internal/session"
	"lato/internal/workspace"
)

// fakeContext is a minimal in-memory command.Context, used to test
// builtin commands without a real TUI model or runtime.
type fakeContext struct {
	lines    []string
	cleared  bool
	quit     bool
	model    string
	provider string

	setModelErr    error
	setProviderErr error

	pickedSessions []session.Session

	openedProviderPicker bool
	openedModelPicker    bool
	openedConnectFlow    bool
	openedImportFlow     bool
	openedAddModelFlow   bool
	refreshedModels      bool
	refreshErr           error

	latestResponse string
	transcriptText string
	clipboardText  string
	clipboardErr   error
	memorySummary  string
	memoryErr      error
	taskList       string
	resumedTask    string
	resumeErr      error
	abandonErr     error
	skillsSummary  string

	workspace workspace.Info
	index     *index.Index

	permissionsSummary string
	resetCount         int
	resetCalls         int
	effort             string
	sessionOnly        bool
}

func (f *fakeContext) Println(format string, args ...any) {
	f.lines = append(f.lines, fmt.Sprintf(format, args...))
}
func (f *fakeContext) Clear()           { f.cleared = true }
func (f *fakeContext) Quit()            { f.quit = true }
func (f *fakeContext) Model() string    { return f.model }
func (f *fakeContext) Provider() string { return f.provider }

// Effort state for M16 command tests.
func (f *fakeContext) CurrentEffort() string {
	if f.effort == "" {
		return "medium"
	}
	return f.effort
}

func (f *fakeContext) SetEffort(level string, persist bool) error {
	lvl, err := effort.Parse(level)
	if err != nil {
		return err
	}
	f.effort = lvl.String()
	if !persist {
		f.sessionOnly = true
	}
	return nil
}

func (f *fakeContext) SetModel(name string) error {
	if f.setModelErr != nil {
		return f.setModelErr
	}
	f.model = name
	return nil
}

func (f *fakeContext) SetProvider(name string) error {
	if f.setProviderErr != nil {
		return f.setProviderErr
	}
	f.provider = name
	return nil
}

func (f *fakeContext) OpenSessionPicker(sessions []session.Session) {
	f.pickedSessions = sessions
}

func (f *fakeContext) OpenProviderPicker() { f.openedProviderPicker = true }
func (f *fakeContext) OpenModelPicker()    { f.openedModelPicker = true }
func (f *fakeContext) OpenConnectFlow()    { f.openedConnectFlow = true }
func (f *fakeContext) OpenImportFlow()     { f.openedImportFlow = true }
func (f *fakeContext) OpenAddModelFlow()   { f.openedAddModelFlow = true }
func (f *fakeContext) RefreshModels() error {
	f.refreshedModels = true
	return f.refreshErr
}
func (f *fakeContext) LatestResponse() string {
	return f.latestResponse
}
func (f *fakeContext) TranscriptText() string { return f.transcriptText }
func (f *fakeContext) WriteToClipboard(text string) error {
	if f.clipboardErr != nil {
		return f.clipboardErr
	}
	f.clipboardText = text
	return nil
}
func (f *fakeContext) MemorySummary() string          { return f.memorySummary }
func (f *fakeContext) RememberFact(text string) error { return f.memoryErr }
func (f *fakeContext) ForgetMemory(id string) error   { return f.memoryErr }
func (f *fakeContext) ClearMemory() error             { return f.memoryErr }
func (f *fakeContext) TaskList() string               { return f.taskList }
func (f *fakeContext) ResumeTask(idOrEmpty string) error {
	f.resumedTask = idOrEmpty
	return f.resumeErr
}
func (f *fakeContext) AbandonTask(id string) error { return f.abandonErr }

func (f *fakeContext) SkillsSummary() string { return f.skillsSummary }

func (f *fakeContext) Workspace() workspace.Info { return f.workspace }

func (f *fakeContext) Index() *index.Index { return f.index }

func (f *fakeContext) PermissionsSummary() string { return f.permissionsSummary }

func (f *fakeContext) ResetPermissions() int {
	f.resetCalls++
	return f.resetCount
}

func (f *fakeContext) resetCalled() bool { return f.resetCalls > 0 }

func TestExit(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewExit().Execute(ctx, nil); err != nil {
		t.Fatalf("Exit.Execute returned an error: %v", err)
	}
	if !ctx.quit {
		t.Fatal("Exit.Execute did not call ctx.Quit()")
	}
}

func TestClear(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewClear().Execute(ctx, nil); err != nil {
		t.Fatalf("Clear.Execute returned an error: %v", err)
	}
	if !ctx.cleared {
		t.Fatal("Clear.Execute did not call ctx.Clear()")
	}
}

func TestModel_NoArgsOpensPicker(t *testing.T) {
	ctx := &fakeContext{model: "qwen3:8b"}
	if err := NewModel().Execute(ctx, nil); err != nil {
		t.Fatalf("Model.Execute returned an error: %v", err)
	}
	if !ctx.openedModelPicker {
		t.Fatal("Model.Execute with no args did not open the model picker")
	}
	if len(ctx.lines) != 0 {
		t.Fatalf("ctx.lines = %v, want no lines when opening the picker", ctx.lines)
	}
}

func TestModel_WithArgSwitches(t *testing.T) {
	ctx := &fakeContext{model: "qwen3:8b"}
	if err := NewModel().Execute(ctx, []string{"llama3"}); err != nil {
		t.Fatalf("Model.Execute returned an error: %v", err)
	}
	if ctx.model != "llama3" {
		t.Fatalf("ctx.model = %q, want %q", ctx.model, "llama3")
	}
}

func TestModel_PropagatesSetModelError(t *testing.T) {
	ctx := &fakeContext{setModelErr: fmt.Errorf("nope")}
	if err := NewModel().Execute(ctx, []string{"llama3"}); err == nil {
		t.Fatal("Model.Execute did not return the error from ctx.SetModel")
	}
}

func TestModel_RejectsTooManyArgs(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewModel().Execute(ctx, []string{"a", "b"}); err == nil {
		t.Fatal("Model.Execute accepted more than one argument")
	}
}

func TestProvider_NoArgsOpensPicker(t *testing.T) {
	ctx := &fakeContext{provider: "ollama"}
	if err := NewProvider().Execute(ctx, nil); err != nil {
		t.Fatalf("Provider.Execute returned an error: %v", err)
	}
	if !ctx.openedProviderPicker {
		t.Fatal("Provider.Execute with no args did not open the provider picker")
	}
	if len(ctx.lines) != 0 {
		t.Fatalf("ctx.lines = %v, want no lines when opening the picker", ctx.lines)
	}
}

func TestProvider_WithArgSwitches(t *testing.T) {
	ctx := &fakeContext{provider: "ollama"}
	if err := NewProvider().Execute(ctx, []string{"lmstudio"}); err != nil {
		t.Fatalf("Provider.Execute returned an error: %v", err)
	}
	if ctx.provider != "lmstudio" {
		t.Fatalf("ctx.provider = %q, want %q", ctx.provider, "lmstudio")
	}
}

func TestWorkspace_PrintsSummary(t *testing.T) {
	ctx := &fakeContext{
		workspace: workspace.Info{
			Repository:     "lato",
			Language:       "Go",
			Module:         "lato",
			Branch:         "main",
			BuildSystem:    "Go modules",
			PackageManager: "Go modules",
		},
	}
	if err := NewWorkspace().Execute(ctx, nil); err != nil {
		t.Fatalf("Workspace.Execute returned an error: %v", err)
	}
	if len(ctx.lines) != 1 {
		t.Fatalf("Workspace.Execute produced %d lines, want 1", len(ctx.lines))
	}
	for _, want := range []string{"Repository", "lato", "Language", "Go", "Build System"} {
		if !strings.Contains(ctx.lines[0], want) {
			t.Errorf("workspace output missing %q:\n%s", want, ctx.lines[0])
		}
	}
}

// TestIndex_PrintsSummary builds a real temp repo, indexes it, and checks
// /index output reports the file, symbol, and ignored-path facts.
func TestIndex_PrintsSummary(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "go.mod", "module example.com/demo\n\ngo 1.26\n")
	writeTestFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	writeTestFile(t, dir, "internal/app/app.go", "package app\n\ntype S struct{}\n\nfunc (S) Go() {}\n")
	writeTestFile(t, dir, "node_modules/x/index.js", "// ignored\n")

	idx := index.NewBuilder(dir).Build()

	ctx := &fakeContext{index: idx}
	if err := NewIndex().Execute(ctx, nil); err != nil {
		t.Fatalf("Index.Execute returned an error: %v", err)
	}
	if len(ctx.lines) != 1 {
		t.Fatalf("Index.Execute produced %d lines, want 1", len(ctx.lines))
	}
	out := ctx.lines[0]
	for _, want := range []string{"Repository", "Files", "Directories", "Languages", "Source files", "Symbols", "Ignored paths", "built"} {
		if !strings.Contains(out, want) {
			t.Errorf("index output missing %q:\n%s", want, out)
		}
	}
}

// writeTestFile writes content into a temp dir, creating parents.
func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHelp_ListsAllRegisteredCommands(t *testing.T) {
	reg := command.NewRegistry()
	reg.Register(NewExit())
	reg.Register(NewClear())
	reg.Register(NewModel())
	reg.Register(NewProvider())
	help := NewHelp(reg)
	reg.Register(help)

	ctx := &fakeContext{}
	if err := help.Execute(ctx, nil); err != nil {
		t.Fatalf("Help.Execute returned an error: %v", err)
	}
	if len(ctx.lines) != 1 {
		t.Fatalf("Help.Execute produced %d lines, want 1", len(ctx.lines))
	}

	out := ctx.lines[0]
	for _, want := range []string{"/exit", "/clear", "/model", "/provider", "/help", "/quit", "/?"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q:\n%s", want, out)
		}
	}
}
