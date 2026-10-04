package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"lato/internal/theme"
)

func TestThemePickerSearchIsCaseInsensitiveAndSubstringBased(t *testing.T) {
	p := newThemePicker(theme.DefaultName, nil)
	p.input.SetValue("MOCHA")
	p.filter()
	if len(p.matches) != 1 || p.matches[0] != "catppuccin-mocha" {
		t.Fatalf("matches = %v, want catppuccin-mocha", p.matches)
	}
}

func TestThemePickerNavigationClampsAndPreviews(t *testing.T) {
	p := newThemePicker(theme.DefaultName, nil)
	p.move(1)
	if p.preview != p.selected() || string(colorAccent) != mustPalettePrimary(t, p.selected()) {
		t.Fatalf("preview = %q, selected = %q, accent = %q", p.preview, p.selected(), colorAccent)
	}
	for i := 0; i < len(p.matches)+2; i++ {
		p.move(1)
	}
	if p.cursor < 0 || p.cursor >= len(p.matches) {
		t.Fatalf("cursor %d outside %d matches", p.cursor, len(p.matches))
	}
	applyTheme(theme.DefaultName)
}

func TestThemePickerCancelRestoresPreviousTheme(t *testing.T) {
	p := newThemePicker("dracula", nil)
	p.move(1)
	p.cancel()
	if string(colorAccent) != "#BD93F9" {
		t.Fatalf("cancel accent = %q, want Dracula purple", colorAccent)
	}
	applyTheme(theme.DefaultName)
}

func TestThemePickerApplyCallsPersistenceOnlyOnEnter(t *testing.T) {
	var saved string
	p := newThemePicker(theme.DefaultName, func(name string) error { saved = name; return nil })
	p.move(1)
	if saved != "" {
		t.Fatal("preview persisted before apply")
	}
	if err := p.apply(); err != nil {
		t.Fatal(err)
	}
	if saved != p.current {
		t.Fatalf("saved %q, current %q", saved, p.current)
	}
	applyTheme(theme.DefaultName)
}

func TestThemePickerEmptyResultsAreSafe(t *testing.T) {
	p := newThemePicker(theme.DefaultName, nil)
	p.input.SetValue("no-theme-can-match-this")
	p.filter()
	if len(p.matches) != 0 || p.selected() != "" {
		t.Fatalf("empty search state = matches %v selected %q", p.matches, p.selected())
	}
	if got := p.view(60, 12); !strings.Contains(got, "No themes match") {
		t.Fatalf("empty view = %q", got)
	}
	if cmd := p.handleKey(tea.KeyMsg{Type: tea.KeyDown}); cmd != nil {
		t.Fatal("down on empty result returned a command")
	}
}

func mustPalettePrimary(t *testing.T, name string) string {
	t.Helper()
	p, ok := theme.Lookup(name)
	if !ok {
		t.Fatalf("missing palette %q", name)
	}
	return p.Primary
}
