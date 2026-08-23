// Slash-command autocomplete (M16). The palette is a thin navigation
// layer over the existing command registry — the single source of truth
// for names, aliases, and descriptions. It never dispatches commands
// itself: accepting a suggestion fills the input with the canonical
// command line, which then flows through the normal dispatcher exactly
// as if the user had typed it.
//
// The component is deliberately lightweight: suggestions come from an
// in-memory registry snapshot, filtering is plain string matching, and
// nothing here touches the network, the model, or the repository.
package tui

import (
	"fmt"
	"strings"

	"lato/internal/command"
)

// maxPaletteRows bounds the suggestion area so the palette can never
// dominate the screen, even though every command is available through it.
const maxPaletteRows = 6

// commandSuggestion is one palette row.
type commandSuggestion struct {
	name        string // canonical name without "/"
	usage       string // full usage line, e.g. "/model [add]"
	description string
}

// slashPalette holds the filtered suggestion state for the current
// input prefix.
type slashPalette struct {
	all     []commandSuggestion // registry snapshot, registration order
	matches []commandSuggestion
	cursor  int
	queried bool // false while the input has no "/" token yet
}

// newSlashPalette snapshots the registry once. Commands added later to
// a live registry would not appear; Lato builds its registry once at
// startup, so this keeps the palette allocation-free afterwards.
func newSlashPalette(reg *command.Registry) *slashPalette {
	p := &slashPalette{}
	for _, cmd := range reg.All() {
		p.all = append(p.all, commandSuggestion{
			name:        cmd.Name(),
			usage:       cmd.Usage(),
			description: cmd.Description(),
		})
	}
	// Start disengaged: the palette is derived exclusively from current
	// input via sync(). Pre-populating matches here would show every
	// command on an empty prompt the moment Lato starts.
	return p
}

// sync refreshes the palette from the raw input line. The palette is
// engaged only while the input begins with a slash-command token that
// is still being typed: once a space appears, arguments are underway
// and completion steps aside.
//
// Matching is case-insensitive prefix-only, per the interaction spec:
// "/co" offers connect and copy; nothing unrelated ever appears.
func (p *slashPalette) sync(rawInput string) {
	if !strings.HasPrefix(rawInput, "/") || strings.ContainsAny(rawInput[1:], " \t") {
		p.matches = nil
		p.cursor = 0
		// "No matching commands" applies only while a command token is
		// actually being typed — never for plain text or once argument
		// entry has started.
		p.queried = strings.HasPrefix(rawInput, "/") && !strings.ContainsAny(rawInput[1:], " \t")
		return
	}
	query := strings.ToLower(strings.TrimSpace(rawInput[1:]))
	p.queried = true

	var matched []commandSuggestion
	for _, s := range p.all {
		if query == "" || strings.HasPrefix(s.name, query) {
			matched = append(matched, s)
		}
	}
	p.matches = matched
	if p.cursor >= len(p.matches) {
		p.cursor = 0
	}
}

// engaged reports whether the palette should be visible: the user is
// typing a command token AND there is something to suggest.
func (p *slashPalette) engaged() bool { return len(p.matches) > 0 }

// isEmptyQuery reports whether a slash token is being typed but nothing
// matches — the "no matching commands" state.
func (p *slashPalette) isEmptyQuery() bool { return p.queried && len(p.matches) == 0 }

// selected returns the highlighted suggestion.
func (p *slashPalette) selected() commandSuggestion {
	if len(p.matches) == 0 {
		return commandSuggestion{}
	}
	return p.matches[p.cursor]
}

// accept collapses the palette into the completed command line. The
// caller submits or continues editing that line through the normal
// input pipeline.
func (p *slashPalette) accept() string {
	s := p.selected()
	if s.name == "" {
		return ""
	}
	p.matches = nil
	p.cursor = 0
	return "/" + s.name
}

// moveUp/moveDown walk the visible suggestions cyclically.
func (p *slashPalette) moveUp() {
	if len(p.matches) == 0 {
		return
	}
	p.cursor--
	if p.cursor < 0 {
		p.cursor = len(p.matches) - 1
	}
}

func (p *slashPalette) moveDown() {
	if len(p.matches) == 0 {
		return
	}
	p.cursor++
	if p.cursor >= len(p.matches) {
		p.cursor = 0
	}
}

// close hides the palette until the next slash token starts.
func (p *slashPalette) close() {
	p.matches = nil
	p.cursor = 0
}

// view renders the compact suggestion strip shown above the input:
// a bordered list of "usage — description" rows with the selection
// cursor, or a graceful empty-state line.
func (p *slashPalette) view(width int) string {
	if p.isEmptyQuery() {
		return paletteStyle.Width(width - 2).Render("No matching commands")
	}
	if !p.engaged() {
		return ""
	}

	rows := len(p.matches)
	if rows > maxPaletteRows {
		rows = maxPaletteRows
	}

	var b strings.Builder
	for i := 0; i < rows; i++ {
		s := p.matches[i]
		cursor := "  "
		style := paletteMetaStyle
		if i == p.cursor {
			cursor = "› "
			style = paletteSelectedStyle
		}
		fmt.Fprintf(&b, "%s%s", cursor, style.Render(s.usage))
		b.WriteString("  ")
		b.WriteString(paletteDescStyle.Render(s.description))
		if i < rows-1 {
			b.WriteString("\n")
		}
	}
	if hidden := len(p.matches) - rows; hidden > 0 {
		fmt.Fprintf(&b, "\n  %d more — keep typing", hidden)
	}
	return paletteStyle.Width(width - 2).Render(strings.TrimRight(b.String(), "\n"))
}
