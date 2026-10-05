package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func scrollModel() model {
	m := model{input: newTestInput(), entries: make([]chatEntry, 0, 40)}
	for i := 0; i < 40; i++ {
		m.entries = append(m.entries, chatEntry{Role: roleSystem, Content: "entry " + string(rune('a'+i%26))})
	}
	m.width, m.height = 40, 12
	m.ready = true
	m.layout()
	return m
}

func newTestInput() textinput.Model {
	input := textinput.New()
	input.Focus()
	return input
}

func TestViewportScrollKeysWhenInputEmpty(t *testing.T) {
	m := scrollModel()
	if m.viewport.YOffset == 0 {
		t.Fatal("setup did not start at the bottom")
	}
	start := m.viewport.YOffset
	for _, key := range []tea.KeyType{tea.KeyUp, tea.KeyPgUp, tea.KeyHome} {
		next, _ := m.handleKey(tea.KeyMsg{Type: key})
		m = next.(model)
	}
	if m.viewport.YOffset >= start || !m.viewport.AtTop() {
		t.Fatalf("up/pageup/home offset = %d, start %d", m.viewport.YOffset, start)
	}

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnd})
	m = next.(model)
	if !m.viewport.AtBottom() {
		t.Fatal("End did not return to the bottom")
	}
}

func TestViewportArrowDoesNotHijackTypedInput(t *testing.T) {
	m := scrollModel()
	m.input.SetValue("hello")
	start := m.viewport.YOffset
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if got := next.(model).viewport.YOffset; got != start {
		t.Fatalf("typed input moved viewport from %d to %d", start, got)
	}
}

func TestRefreshTranscriptPreservesManualScroll(t *testing.T) {
	m := scrollModel()
	m.viewport.PageUp()
	if m.viewport.AtBottom() {
		t.Fatal("setup did not scroll upward")
	}
	offset := m.viewport.YOffset
	m.entries = append(m.entries, chatEntry{Role: roleSystem, Content: strings.Repeat("new", 5)})
	m.refreshTranscript()
	if m.viewport.YOffset != offset {
		t.Fatalf("refresh moved manual offset from %d to %d", offset, m.viewport.YOffset)
	}
}

func TestMouseWheelScrollsViewport(t *testing.T) {
	m := scrollModel()
	start := m.viewport.YOffset
	next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	m = next.(model)
	if m.viewport.YOffset >= start {
		t.Fatalf("wheel up offset = %d, start %d", m.viewport.YOffset, start)
	}
	next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if next.(model).viewport.YOffset <= m.viewport.YOffset {
		t.Fatal("wheel down did not move viewport down")
	}
}
