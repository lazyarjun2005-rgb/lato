package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"lato/internal/runtime"
)

func TestEscArmsThenCancelsWithoutQuitting(t *testing.T) {
	m := model{waiting: true}
	first, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	armed := first.(model)
	if !armed.escArmed || armed.quitting {
		t.Fatalf("first Esc = %+v, want armed and not quitting", armed)
	}

	cancelled := false
	armed.cancel = func() { cancelled = true }
	second, _ := armed.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	stopped := second.(model)
	if !cancelled || !stopped.canceling || stopped.quitting {
		t.Fatalf("second Esc = %+v, cancelled=%v", stopped, cancelled)
	}
}

func TestIdleEscDoesNotQuit(t *testing.T) {
	got, _ := (model{}).handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got.(model).quitting {
		t.Fatal("idle Esc unexpectedly quit Lato")
	}
}

func TestPickerEscWinsWhileAgentIsRunning(t *testing.T) {
	m := newPaletteTestModel()
	m.input.SetValue("/mo")
	m.syncPalette()
	m.waiting = true
	cancelled := false
	m.cancel = func() { cancelled = true }

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(model)
	if got.palette.engaged() || got.escArmed || cancelled || got.canceling {
		t.Fatalf("picker Esc did not win: engaged=%v armed=%v cancelled=%v canceling=%v", got.palette.engaged(), got.escArmed, cancelled, got.canceling)
	}
}

func TestCancellationMarksLiveStateIncomplete(t *testing.T) {
	m := model{
		activities: []activityItem{{Label: "run_command", Status: activityRunning}},
		todos:      []runtime.TodoItem{{Title: "run tests", Status: "active"}},
	}
	m.finishLiveActivityCancelled()
	if m.activities[0].Status != activityCancelled {
		t.Fatalf("activity status = %v, want cancelled", m.activities[0].Status)
	}
	if m.todos[0].Status != "failed" {
		t.Fatalf("todo status = %q, want failed", m.todos[0].Status)
	}
}
