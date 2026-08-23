package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"lato/internal/config"
	"lato/internal/runtime"
	"lato/internal/session"
)

func Start(sess *session.Session) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Runtime construction is separated from the chat model so a
	// configuration problem surfaces as a clean, actionable error
	// instead of a panic mid-startup. A provider that cannot be built
	// (e.g. a missing API key) is not fatal: the TUI still opens and
	// explains how to fix it.
	rt, err := runtime.New()
	if err != nil {
		return fmt.Errorf("lato cannot start: %w", err)
	}

	// The asker is created before the model so the runtime can attach
	// it during construction, and bound to the program right after so
	// permission prompts can reach the event loop (M13).
	asker := newUIAsker()
	m := newModel(cfg, sess, asker, rt)

	program := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	asker.bind(program)

	_, err = program.Run()
	return err
}
