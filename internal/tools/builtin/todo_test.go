package builtin

import (
	"context"
	"encoding/json"
	"testing"
)

func TestTodoWriteValidatesSingleActiveItem(t *testing.T) {
	tool := NewTodoWrite()
	args := map[string]any{"todos": []any{
		map[string]any{"title": "Inspect", "status": "completed"},
		map[string]any{"title": "Implement", "status": "active"},
		map[string]any{"title": "Verify", "status": "pending"},
	}}
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var payload struct {
		Todos []todoInput `json:"todos"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if len(payload.Todos) != 3 || payload.Todos[1].Status != "active" {
		t.Fatalf("unexpected todos: %+v", payload.Todos)
	}
}

func TestTodoWriteRejectsMultipleActiveItems(t *testing.T) {
	_, err := NewTodoWrite().Execute(context.Background(), map[string]any{"todos": []any{
		map[string]any{"title": "One", "status": "active"},
		map[string]any{"title": "Two", "status": "active"},
	}})
	if err == nil {
		t.Fatal("expected multiple-active validation error")
	}
}
