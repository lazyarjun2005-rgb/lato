package runtime

import (
	"time"

	"lato/internal/providers"
)

// EventType describes an observable step in an agent run.
type EventType int

const (
	EventText EventType = iota
	EventThinking
	EventToolStart
	EventToolFinish
	EventDone
	EventError
	EventMemory
	EventRetry
	EventTodos
)

// Backwards-compatible names for the initial streaming tool-call API.
const (
	EventToolCall   = EventToolStart
	EventToolResult = EventToolFinish
)

// ToolResult describes the outcome of one tool execution.
type ToolResult struct {
	ToolCallID string
	Name       string
	Arguments  map[string]any
	Content    string
	IsError    bool
	Success    bool
	Duration   time.Duration
	Err        error
}

// TodoItem is the ephemeral task state exposed by the todo_write tool.
type TodoItem struct {
	Title  string `json:"title"`
	Status string `json:"status"`
}

// Event is emitted by StreamChat as the shared runtime loop progresses.
// EventDone contains the final model response. EventError contains the error
// that stopped the run.
type Event struct {
	Type       EventType
	Text       string
	Thinking   string
	ToolCall   *providers.ToolCall
	ToolResult *ToolResult
	Response   *providers.Response
	Err        error

	// Count carries the number of relevant project-memory entries that
	// were injected for this request (EventMemory only).
	Count int

	// Retry fields describe a transient provider recovery attempt.
	Attempt    int
	MaxAttempt int
	Delay      time.Duration

	// Todos carries the current in-memory todo snapshot.
	Todos []TodoItem
}
