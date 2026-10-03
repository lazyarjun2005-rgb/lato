package tools

import (
	"context"
	"errors"
	"testing"
)

// echoTool returns whatever "text" argument it was given, and demonstrates
// the ArgumentError path when it's missing.
type echoTool struct{}

func (echoTool) Name() string                { return "echo" }
func (echoTool) Description() string         { return "echoes its text argument" }
func (echoTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (echoTool) Execute(_ context.Context, args map[string]any) (Result, error) {
	text, err := StringArg(args, "text")
	if err != nil {
		return Result{}, err
	}
	return Result{Content: text}, nil
}

// failingTool always fails at the domain level (IsError), never as a Go error.
type failingTool struct{}

func (failingTool) Name() string                { return "fail" }
func (failingTool) Description() string         { return "always reports a domain failure" }
func (failingTool) InputSchema() map[string]any { return map[string]any{"type": "object"} }
func (failingTool) Execute(context.Context, map[string]any) (Result, error) {
	return Result{IsError: true, Content: "something went wrong"}, nil
}

func newTestManager(t *testing.T, tools ...Tool) *Manager {
	t.Helper()
	reg := NewRegistry()
	m := NewManager(reg)
	for _, tool := range tools {
		if err := m.Register(tool); err != nil {
			t.Fatalf("Register(%q) unexpected error: %v", tool.Name(), err)
		}
	}
	return m
}

func TestManager_ExecuteUnknownTool(t *testing.T) {
	m := newTestManager(t)

	_, err := m.Execute(context.Background(), "nope", nil)
	if err == nil {
		t.Fatal("Execute() with unknown tool = nil error, want an error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Execute() error = %v, want it to wrap ErrNotFound", err)
	}
}

func TestManager_ExecuteSuccess(t *testing.T) {
	m := newTestManager(t, echoTool{})

	result, err := m.Execute(context.Background(), "echo", map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("Execute() result.IsError = true, want false")
	}
	if result.Content != "hi" {
		t.Fatalf("Execute() result.Content = %q, want %q", result.Content, "hi")
	}
}

func TestManager_ExecuteNilArgsTreatedAsEmpty(t *testing.T) {
	m := newTestManager(t, echoTool{})

	_, err := m.Execute(context.Background(), "echo", nil)
	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("Execute() error = %v, want it to wrap *ArgumentError", err)
	}
}

func TestManager_ExecuteWrapsToolErrorWithToolName(t *testing.T) {
	m := newTestManager(t, echoTool{})

	_, err := m.Execute(context.Background(), "echo", map[string]any{})

	var execErr *ExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("Execute() error = %v, want it to wrap *ExecutionError", err)
	}
	if execErr.Tool != "echo" {
		t.Fatalf("ExecutionError.Tool = %q, want %q", execErr.Tool, "echo")
	}

	var argErr *ArgumentError
	if !errors.As(err, &argErr) {
		t.Fatalf("Execute() error = %v, want errors.As to reach the underlying *ArgumentError", err)
	}
}

func TestManager_ExecuteDomainFailureIsNotAGoError(t *testing.T) {
	m := newTestManager(t, failingTool{})

	result, err := m.Execute(context.Background(), "fail", nil)
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v, want nil (domain failures use Result.IsError)", err)
	}
	if !result.IsError {
		t.Fatalf("Execute() result.IsError = false, want true")
	}
	if result.Content != "something went wrong" {
		t.Fatalf("Execute() result.Content = %q, want %q", result.Content, "something went wrong")
	}
}

func TestManager_DefinitionsAndList(t *testing.T) {
	m := newTestManager(t, echoTool{}, failingTool{})

	if len(m.List()) != 2 {
		t.Fatalf("List() returned %d tools, want 2", len(m.List()))
	}
	if len(m.Definitions()) != 2 {
		t.Fatalf("Definitions() returned %d entries, want 2", len(m.Definitions()))
	}
}

// strictEchoTool has a full schema; invalid arguments must be rejected
// before Execute is reached.
type strictEchoTool struct {
	calls int
}

func (t *strictEchoTool) Name() string        { return "strict_echo" }
func (t *strictEchoTool) Description() string { return "strict echo" }
func (t *strictEchoTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
		"required": []string{"text"},
	}
}
func (t *strictEchoTool) Execute(_ context.Context, args map[string]any) (Result, error) {
	t.calls++
	return Result{Content: "ok"}, nil
}

func TestManager_ExecuteValidatesArgsBeforeSideEffect(t *testing.T) {
	tool := &strictEchoTool{}
	m := newTestManager(t, tool)

	if _, err := m.Execute(context.Background(), "strict_echo", map[string]any{}); err == nil {
		t.Fatal("missing required should error")
	}
	if _, err := m.Execute(context.Background(), "strict_echo", map[string]any{"text": 7}); err == nil {
		t.Fatal("wrong type should error")
	}
	if _, err := m.Execute(context.Background(), "strict_echo", map[string]any{"text": ""}); err == nil {
		t.Fatal("empty required string should error")
	}
	if tool.calls != 0 {
		t.Fatalf("Execute side effect count = %d, want 0", tool.calls)
	}
	if _, err := m.Execute(context.Background(), "strict_echo", map[string]any{"text": "hi"}); err != nil {
		t.Fatalf("valid call: %v", err)
	}
	if tool.calls != 1 {
		t.Fatalf("Execute side effect count = %d, want 1", tool.calls)
	}
}
