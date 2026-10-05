// Package tui implements Lato's interactive terminal chat interface,
// built on Bubble Tea. It is a presentation layer only: every message the
// user sends is answered by calling the exact same runtime.Run that `lato
// run` uses. This package adds no memory, tools, or behavior the runtime
// doesn't already have, it just makes talking to it feel like a chat
// session instead of one command per question.
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"lato/internal/theme"
)

const (
	modalMaxWidth = 68
	modalMinWidth = 28
)

// modalWidth keeps floating dialogs inside the terminal while retaining a
// readable maximum width on large screens.
func modalWidth(terminalWidth, preferred int) int {
	if preferred <= 0 {
		preferred = modalMaxWidth
	}
	if terminalWidth <= 0 {
		return preferred
	}
	available := terminalWidth - 4
	if available < 1 {
		return 1
	}
	if available < modalMinWidth || preferred > available {
		return available
	}
	return preferred
}

func modalInnerWidth(outer int) int {
	if outer <= 6 {
		return 1
	}
	return outer - 6
}

func trimModalText(text string, width int) string {
	if width < 1 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(strings.TrimSpace(text))
}

var (
	colorAccent       = lipgloss.Color("#0000FF")
	colorAssistant    = lipgloss.Color("#0000FF")
	colorUser         = lipgloss.Color("#0000FF")
	colorMuted        = lipgloss.Color("#7A7A7A")
	colorError        = lipgloss.Color("#FF6B6B")
	colorWarning      = lipgloss.Color("#FFD166")
	colorSuccess      = lipgloss.Color("#65D1FF")
	colorInfo         = lipgloss.Color("#8AB4F8")
	colorBorder       = lipgloss.Color("#0000FF")
	colorSelected     = lipgloss.Color("#000033")
	colorText         = lipgloss.Color("#EAEAEA")
	colorPrompt       = lipgloss.Color("#0000FF")
	colorPlaceholder  = lipgloss.Color("#7A7A7A")
	markdownStyleName = "dark"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#0E0E12")).
			Background(colorAccent).
			Padding(0, 1)

	headerMetaStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	userLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	assistantLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAssistant)

	errorLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorError)

	systemLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorMuted)

	messageBodyStyle = lipgloss.NewStyle().
				Foreground(colorText)

	activityStyle = lipgloss.NewStyle().
			Foreground(colorMuted)
	activityTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorInfo)
	activityRunningStyle = lipgloss.NewStyle().Foreground(colorInfo)
	activitySuccessStyle = lipgloss.NewStyle().Foreground(colorSuccess)
	activityErrorStyle   = lipgloss.NewStyle().Foreground(colorError)
	todoTitleStyle       = lipgloss.NewStyle().Bold(true).Foreground(colorInfo)
	todoPendingStyle     = lipgloss.NewStyle().Foreground(colorMuted)
	todoActiveStyle      = lipgloss.NewStyle().Foreground(colorWarning)
	todoCompleteStyle    = lipgloss.NewStyle().Foreground(colorSuccess)
	todoFailedStyle      = lipgloss.NewStyle().Foreground(colorError)
	workspaceStyle       = lipgloss.NewStyle().Foreground(colorMuted)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	inputBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(colorBorder).
				Padding(0, 1)

	spinnerStyle = lipgloss.NewStyle().
			Foreground(colorAccent)
)

var (
	pickerBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(colorBorder).
				Padding(1, 2)

	pickerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent)

	pickerActiveStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAccent)

	pickerSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorText).
				Background(lipgloss.Color("#000033"))

	pickerMetaStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	pickerHelpStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// Slash-command palette (M16): a quiet strip above the input.
	paletteStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			Foreground(colorMuted)

	paletteSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorText)

	paletteMetaStyle = lipgloss.NewStyle().
				Foreground(colorText)

	paletteDescStyle = lipgloss.NewStyle().
				Foreground(colorMuted)
)

// applyTheme updates semantic colors and rebuilds all package styles in one
// place. Components consume these styles without naming theme colors.
func applyTheme(name string) {
	_, p := theme.Resolve(name)
	colorAccent = lipgloss.Color(p.Primary)
	colorAssistant = lipgloss.Color(p.Assistant)
	colorUser = lipgloss.Color(p.User)
	colorMuted = lipgloss.Color(p.Muted)
	colorError = lipgloss.Color(p.Error)
	colorWarning = lipgloss.Color(p.Warning)
	colorSuccess = lipgloss.Color(p.Success)
	colorInfo = lipgloss.Color(p.Info)
	colorBorder = lipgloss.Color(p.Border)
	colorSelected = lipgloss.Color(p.SelectedBG)
	colorText = lipgloss.Color(p.Text)
	colorPrompt = lipgloss.Color(p.Prompt)
	colorPlaceholder = lipgloss.Color(p.Muted)
	markdownStyleName = markdownThemeStyle(p.Text)

	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.SelectedFG)).Background(colorAccent).Padding(0, 1)
	headerMetaStyle = lipgloss.NewStyle().Foreground(colorMuted)
	userLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorUser)
	assistantLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAssistant)
	errorLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorError)
	systemLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorMuted)
	messageBodyStyle = lipgloss.NewStyle().Foreground(colorText)
	activityStyle = lipgloss.NewStyle().Foreground(colorMuted)
	activityTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorInfo)
	activityRunningStyle = lipgloss.NewStyle().Foreground(colorInfo)
	activitySuccessStyle = lipgloss.NewStyle().Foreground(colorSuccess)
	activityErrorStyle = lipgloss.NewStyle().Foreground(colorError)
	todoTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorInfo)
	todoPendingStyle = lipgloss.NewStyle().Foreground(colorMuted)
	todoActiveStyle = lipgloss.NewStyle().Foreground(colorWarning)
	todoCompleteStyle = lipgloss.NewStyle().Foreground(colorSuccess)
	todoFailedStyle = lipgloss.NewStyle().Foreground(colorError)
	workspaceStyle = lipgloss.NewStyle().Foreground(colorMuted)
	helpStyle = lipgloss.NewStyle().Foreground(colorMuted)
	inputBorderStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colorBorder).Padding(0, 1)
	spinnerStyle = lipgloss.NewStyle().Foreground(colorAccent)
	pickerBorderStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(colorBorder).Padding(1, 2)
	pickerTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	pickerActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	pickerSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.SelectedFG)).Background(colorSelected)
	pickerMetaStyle = lipgloss.NewStyle().Foreground(colorMuted)
	pickerHelpStyle = lipgloss.NewStyle().Foreground(colorMuted)
	paletteStyle = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderBottom(true).Foreground(colorMuted)
	paletteSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(colorText)
	paletteMetaStyle = lipgloss.NewStyle().Foreground(colorText)
	paletteDescStyle = lipgloss.NewStyle().Foreground(colorMuted)
}

func markdownThemeStyle(text string) string {
	if len(text) != 7 || text[0] != '#' {
		return "dark"
	}
	parse := func(s string) int {
		v := 0
		for _, r := range s {
			v *= 16
			switch {
			case r >= '0' && r <= '9':
				v += int(r - '0')
			case r >= 'a' && r <= 'f':
				v += int(r-'a') + 10
			case r >= 'A' && r <= 'F':
				v += int(r-'A') + 10
			}
		}
		return v
	}
	// Dark text indicates a light terminal palette. This deliberately uses
	// the semantic text role rather than theme names or a second theme list.
	return map[bool]string{true: "light", false: "dark"}[0.2126*float64(parse(text[1:3]))+0.7152*float64(parse(text[3:5]))+0.0722*float64(parse(text[5:7])) < 145]
}

func styleTextInput(in *textinput.Model) {
	in.PromptStyle = lipgloss.NewStyle().Foreground(colorPrompt)
	in.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	in.PlaceholderStyle = lipgloss.NewStyle().Foreground(colorPlaceholder)
	in.Cursor.TextStyle = lipgloss.NewStyle().Foreground(colorText)
}
