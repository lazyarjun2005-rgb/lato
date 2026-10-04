// Package tui implements Lato's interactive terminal chat interface,
// built on Bubble Tea. It is a presentation layer only: every message the
// user sends is answered by calling the exact same runtime.Run that `lato
// run` uses. This package adds no memory, tools, or behavior the runtime
// doesn't already have, it just makes talking to it feel like a chat
// session instead of one command per question.
package tui

import (
	"github.com/charmbracelet/lipgloss"

	"lato/internal/theme"
)

var (
	colorAccent    = lipgloss.Color("#0000FF")
	colorAssistant = lipgloss.Color("#0000FF")
	colorUser      = lipgloss.Color("#0000FF")
	colorMuted     = lipgloss.Color("#7A7A7A")
	colorError     = lipgloss.Color("#FF6B6B")
	colorWarning   = lipgloss.Color("#FFD166")
	colorSuccess   = lipgloss.Color("#65D1FF")
	colorInfo      = lipgloss.Color("#8AB4F8")
	colorBorder    = lipgloss.Color("#0000FF")
	colorSelected  = lipgloss.Color("#000033")
	colorText      = lipgloss.Color("#EAEAEA")
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

	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(p.SelectedFG)).Background(colorAccent).Padding(0, 1)
	headerMetaStyle = lipgloss.NewStyle().Foreground(colorMuted)
	userLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorUser)
	assistantLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAssistant)
	errorLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorError)
	systemLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(colorMuted)
	messageBodyStyle = lipgloss.NewStyle().Foreground(colorText)
	activityStyle = lipgloss.NewStyle().Foreground(colorMuted)
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
