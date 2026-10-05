package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"lato/internal/tools"
)

type todoWrite struct{}

type todoInput struct {
	Title  string `json:"title"`
	Status string `json:"status"`
}

func NewTodoWrite() tools.Tool { return todoWrite{} }

func (todoWrite) Name() string { return "todo_write" }

func (todoWrite) Description() string {
	return "Create or update the current task list. Use statuses pending, active, completed, or failed. Keep exactly one task active when work is in progress."
}

func (todoWrite) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"todos": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":  map[string]any{"type": "string"},
						"status": map[string]any{"type": "string", "enum": []string{"pending", "active", "completed", "failed"}},
					},
					"required": []string{"title", "status"},
				},
			},
		},
		"required": []string{"todos"},
	}
}

func (todoWrite) Execute(_ context.Context, args map[string]any) (tools.Result, error) {
	raw, ok := args["todos"].([]any)
	if !ok {
		return tools.Result{}, fmt.Errorf("argument \"todos\" must be an array")
	}
	if len(raw) > 20 {
		return tools.Result{}, fmt.Errorf("todos cannot contain more than 20 items")
	}

	todos := make([]todoInput, 0, len(raw))
	active := 0
	for i, value := range raw {
		item, ok := value.(map[string]any)
		if !ok {
			return tools.Result{}, fmt.Errorf("todo %d must be an object", i+1)
		}
		title, _ := item["title"].(string)
		status, _ := item["status"].(string)
		title = strings.TrimSpace(title)
		status = strings.TrimSpace(strings.ToLower(status))
		if title == "" {
			return tools.Result{}, fmt.Errorf("todo %d has an empty title", i+1)
		}
		switch status {
		case "pending", "active", "completed", "failed":
		case "":
			return tools.Result{}, fmt.Errorf("todo %d has no status", i+1)
		default:
			return tools.Result{}, fmt.Errorf("todo %d has invalid status %q", i+1, status)
		}
		if status == "active" {
			active++
		}
		todos = append(todos, todoInput{Title: title, Status: status})
	}
	if active > 1 {
		return tools.Result{}, fmt.Errorf("todo list can have only one active item")
	}

	payload, err := json.Marshal(struct {
		Todos []todoInput `json:"todos"`
	}{Todos: todos})
	if err != nil {
		return tools.Result{}, fmt.Errorf("encode todos: %w", err)
	}
	return tools.Result{Content: string(payload)}, nil
}
