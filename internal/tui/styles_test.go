package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

import "lato/internal/theme"

func TestPrimaryPaletteUsesElectricBlue(t *testing.T) {
	const want = "#0000FF"

	for name, got := range map[string]string{
		"accent":    string(colorAccent),
		"assistant": string(colorAssistant),
		"border":    string(colorBorder),
	} {
		if got != want {
			t.Errorf("%s color = %q, want %q", name, got, want)
		}
	}
}

func TestSemanticPaletteRemainsDistinct(t *testing.T) {
	if string(colorError) != "#FF6B6B" {
		t.Errorf("error color = %q, want #FF6B6B", colorError)
	}
	if string(colorMuted) != "#7A7A7A" {
		t.Errorf("muted color = %q, want #7A7A7A", colorMuted)
	}
	if string(colorText) != "#EAEAEA" {
		t.Errorf("text color = %q, want #EAEAEA", colorText)
	}
}

func TestApplyingThemeUpdatesExistingStyles(t *testing.T) {
	applyTheme("dracula")
	defer applyTheme(theme.DefaultName)

	if string(colorAccent) != "#BD93F9" {
		t.Errorf("accent after theme = %q, want Dracula purple", colorAccent)
	}
	if string(colorError) != "#FF5555" || string(colorSuccess) != "#50FA7B" {
		t.Errorf("semantic colors not applied: error=%q success=%q", colorError, colorSuccess)
	}
	if got := string(userLabelStyle.GetForeground().(lipgloss.Color)); got != "#8BE9FD" {
		t.Errorf("user style foreground = %q, want Dracula cyan", got)
	}
}
