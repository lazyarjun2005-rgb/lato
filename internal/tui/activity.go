package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"lato/internal/permissions"
	"lato/internal/providers"
	"lato/internal/runtime"
	"lato/internal/session"
)

type activityStatus int

const (
	activityRunning activityStatus = iota
	activityComplete
	activityFailed
	activityCancelled
)

type activityItem struct {
	Kind, Label, Detail, Err string
	Status                   activityStatus
	Started, Ended           time.Time
	Duration                 time.Duration
}

// Keep enough structured history to restore a useful session while retaining
// a hard bound. Rendering still shows only the most recent compact rows.
const maxLiveActivities = 50

func (a activityItem) icon() string {
	switch a.Status {
	case activityComplete:
		return "✓"
	case activityFailed:
		return "✗"
	case activityCancelled:
		return "!"
	default:
		return "→"
	}
}

func (a activityItem) line(width int) string {
	label := a.Label
	if a.Detail != "" && a.Status == activityRunning {
		label += " · " + a.Detail
	}
	if a.Status == activityFailed && a.Err != "" {
		label += " · " + shortResult(a.Err)
	}
	return truncateDisplay(a.icon()+" "+label, width)
}

func restoredActivityText(a activityItem) string {
	label := a.Label
	if a.Detail != "" {
		label += " · " + a.Detail
	}
	if a.Err != "" {
		label += " · " + shortResult(a.Err)
	}
	return a.icon() + " " + label
}

func activityFromSession(a session.Activity) activityItem {
	status := activityComplete
	if a.Status == "running" {
		status = activityRunning
	} else if a.Status == "failed" {
		status = activityFailed
	} else if a.Status == "cancelled" {
		status = activityCancelled
	}
	return activityItem{
		Kind: a.Kind, Label: a.Label, Detail: a.Detail, Err: a.Error,
		Status: status, Started: a.StartedAt, Ended: a.EndedAt,
		Duration: a.Duration,
	}
}

func (a activityItem) sessionValue() session.Activity {
	status := "completed"
	if a.Status == activityRunning {
		status = "running"
	} else if a.Status == activityFailed {
		status = "failed"
	} else if a.Status == activityCancelled {
		status = "cancelled"
	}
	duration := a.Duration
	if duration == 0 && !a.Started.IsZero() && !a.Ended.IsZero() {
		duration = a.Ended.Sub(a.Started)
	}
	return session.Activity{
		Kind: a.Kind, Label: a.Label, Detail: a.Detail, Status: status,
		Error: a.Err, StartedAt: a.Started, EndedAt: a.Ended,
		Duration: duration,
	}
}

func (m *model) activityStart(call *providers.ToolCall) {
	if call == nil {
		return
	}
	item := activityItem{Kind: "tool", Label: call.Name, Detail: usefulToolArgument(call.Arguments), Status: activityRunning, Started: time.Now()}
	m.activities = appendBoundedActivity(m.activities, item)
}

func (m *model) activityFinish(result *runtime.ToolResult) {
	if result == nil {
		return
	}
	for i := len(m.activities) - 1; i >= 0; i-- {
		item := &m.activities[i]
		if item.Label != result.Name || item.Status != activityRunning {
			continue
		}
		item.Ended = time.Now()
		item.Duration = result.Duration
		if result.Success {
			item.Status = activityComplete
		} else {
			item.Status = activityFailed
			if result.Err != nil {
				item.Err = result.Err.Error()
			} else {
				item.Err = shortResult(result.Content)
			}
		}
		return
	}
}

func (m *model) finishLiveActivity(success bool, err error) {
	for i := len(m.activities) - 1; i >= 0; i-- {
		item := &m.activities[i]
		if item.Status != activityRunning {
			continue
		}
		item.Status = activityComplete
		if !success {
			item.Status = activityFailed
			if err != nil {
				item.Err = err.Error()
			}
		}
		item.Ended = time.Now()
	}
}

func (m *model) finishLiveActivityCancelled() {
	for i := len(m.activities) - 1; i >= 0; i-- {
		item := &m.activities[i]
		if item.Status != activityRunning {
			continue
		}
		item.Status = activityCancelled
		item.Err = "operation interrupted"
		item.Ended = time.Now()
	}
	for i := range m.todos {
		if m.todos[i].Status == "active" {
			m.todos[i].Status = "failed"
		}
	}
}

func formatToolStart(call *providers.ToolCall) string {
	if call == nil {
		return "Running tool"
	}

	if detail := usefulToolArgument(call.Arguments); detail != "" {
		// Model-supplied arguments may contain credential-shaped text;
		// mask values before they reach the transcript.
		return fmt.Sprintf("Running %s %s", call.Name, permissions.RedactSecrets(detail))
	}
	return fmt.Sprintf("Running %s", call.Name)
}

func formatToolFinish(result *runtime.ToolResult) string {
	if result == nil {
		return "✕ Tool failed"
	}

	if !result.Success {
		message := fmt.Sprintf("✕ %s failed", result.Name)
		if result.Err != nil {
			return message + ": " + result.Err.Error()
		}
		if summary := shortResult(result.Content); summary != "" {
			return message + ": " + summary
		}
		return message
	}

	var message string
	switch result.Name {
	case "list_files":
		if count := nonEmptyLines(result.Content); count > 0 {
			message = fmt.Sprintf("✓ Found %d entries", count)
		}
	case "read_file":
		if path, ok := result.Arguments["path"].(string); ok && path != "" {
			message = fmt.Sprintf("✓ Read %s", path)
		}
	case "write_file":
		if path, ok := result.Arguments["path"].(string); ok && path != "" {
			message = fmt.Sprintf("✓ Wrote %s", path)
		}
	}
	if message == "" {
		message = fmt.Sprintf("✓ %s completed", result.Name)
	}
	return message + durationSuffix(result.Duration)
}

func usefulToolArgument(args map[string]any) string {
	for _, key := range []string{"path", "command", "cmd", "query", "url"} {
		value, ok := args[key].(string)
		if !ok || value == "" {
			continue
		}
		if key == "command" || key == "cmd" {
			return strconv.Quote(value)
		}
		return value
	}
	return ""
}

func nonEmptyLines(value string) int {
	count := 0
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func shortResult(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	const limit = 120
	if len(value) > limit {
		return value[:limit-1] + "…"
	}
	return value
}

func durationSuffix(duration time.Duration) string {
	// Tool calls that finish instantly do not need visual noise. Duration is
	// still available on the structured event for richer consumers.
	if duration < 100*time.Millisecond {
		return ""
	}
	return fmt.Sprintf(" (%s)", duration.Round(10*time.Millisecond))
}
