package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"lato/internal/effort"
	"lato/internal/providers"
)

// pickerScope distinguishes what a selectPicker is choosing between:
// providers, or the models belonging to one provider.
type pickerScope int

const (
	scopeProvider pickerScope = iota
	scopeModel
)

// selectOption is one row in a selectPicker: a friendly label, the real
// ID to switch to, and whether it's the currently active choice.
// Header rows are non-selectable section titles used by the grouped
// model picker.
type selectOption struct {
	Label   string
	ID      string
	Current bool
	Header  bool
}

// selectPicker is a small, self-contained component for choosing a
// provider or a model by friendly name, mirroring sessionPicker: it
// holds a snapshot of its options and a cursor, and never switches
// anything itself — the caller reads Selected() once Enter is pressed.
//
// For model pickers it also carries the M16 effort dimension: the
// effort ladder renders under the list and ←/→ walk it. Enter selects
// model + effort together; 's' applies the selection to this session
// only.
//
// Rendering is bounded: only the rows that fit the terminal height are
// drawn, in a window that follows the cursor automatically, so
// providers with hundreds of models stay usable.
type selectPicker struct {
	title    string
	options  []selectOption
	cursor   int
	scope    pickerScope
	provider string // providerID this picker's models belong to; only set for scopeModel

	// Effort state (model pickers only). effortCursor indexes
	// effort.All; withEffort gates the ←/→ handling and rendering.
	effortEnabled bool
	effortCursor  int

	offset   int // first visible row index
	pageSize int // rows per PageUp/PageDown; refreshed by view
}

// pickerChromeLines is the vertical space a picker spends on its frame
// instead of rows: title, blank separator, blank separator, help line,
// two border lines, and two padding lines.
const pickerChromeLines = 8

// minPickerVisibleRows keeps a few rows selectable even in a very short
// terminal rather than collapsing the list away entirely.
const minPickerVisibleRows = 3

// newProviderPicker builds a picker over every registered provider,
// with currentID's row highlighted as the active choice.
func newProviderPicker(currentID string) *selectPicker {
	options := make([]selectOption, len(providers.Registry))
	cursor := 0
	for i, p := range providers.Registry {
		options[i] = selectOption{Label: p.Name, ID: p.ID, Current: p.ID == currentID}
		if options[i].Current {
			cursor = i
		}
	}
	return &selectPicker{title: "Provider", options: options, cursor: cursor, scope: scopeProvider}
}

// newModelPicker builds a picker over the given models, with currentID's
// row highlighted as the active choice and currentEffort preselected.
// The caller fetches the list from the live provider; the picker never
// consults a static registry.
func newModelPicker(providerID, currentID string, models []providers.ModelInfo, currentEffort effort.Level) *selectPicker {
	options := make([]selectOption, len(models))
	cursor := 0
	for i, mi := range models {
		options[i] = selectOption{Label: mi.Name, ID: mi.ID, Current: mi.ID == currentID}
		if options[i].Current {
			cursor = i
		}
	}
	p := &selectPicker{
		title:    "Model (" + providers.DisplayName(providerID) + ")",
		options:  options,
		cursor:   cursor,
		scope:    scopeModel,
		provider: providerID,
	}
	p.enableEffort(currentEffort)
	return p
}

// modelGroup is one provider's section in the grouped /model picker.
type modelGroup struct {
	Name   string
	Models []providers.ModelInfo
}

// newGroupedModelPicker builds a picker whose rows are grouped under
// per-provider header titles. Model IDs stay opaque; grouping is purely
// presentational and derived from the connection store, never from
// parsing ID strings.
func newGroupedModelPicker(groups []modelGroup, currentID string, currentEffort effort.Level) *selectPicker {
	var options []selectOption
	cursor := 0
	for _, g := range groups {
		if len(g.Models) == 0 {
			continue
		}
		options = append(options, selectOption{Label: g.Name, Header: true})
		for _, mi := range g.Models {
			current := mi.ID == currentID
			options = append(options, selectOption{Label: mi.ID, ID: mi.ID, Current: current})
			if current {
				cursor = len(options) - 1
			}
		}
	}
	p := &selectPicker{
		title:   "Model",
		options: options,
		cursor:  cursor,
		scope:   scopeModel,
	}
	if len(options) > 0 {
		p.skipHeader(1) // never start on a section title
	}
	p.enableEffort(currentEffort)
	return p
}

// moveUp/moveDown move the selection cursor, clamped to the list bounds
// and skipping non-selectable header rows.
func (p *selectPicker) moveUp() {
	if p.cursor > 0 {
		p.cursor--
	}
	p.skipHeader(-1)
}

func (p *selectPicker) moveDown() {
	if p.cursor < len(p.options)-1 {
		p.cursor++
	}
	p.skipHeader(1)
}

// skipHeader advances the cursor in direction until it rests on a
// selectable row.
func (p *selectPicker) skipHeader(direction int) {
	for p.cursor >= 0 && p.cursor < len(p.options) && p.options[p.cursor].Header {
		next := p.cursor + direction
		if next < 0 || next >= len(p.options) {
			return // clamped against a header boundary; leave as-is
		}
		p.cursor = next
	}
}

// selected returns the option currently under the cursor.
func (p *selectPicker) selected() selectOption {
	return p.options[p.cursor]
}

// visibleRows returns how many option rows fit in a terminal of the
// given height.
func (p *selectPicker) visibleRows(height int) int {
	rows := height - pickerChromeLines
	if rows < minPickerVisibleRows {
		rows = minPickerVisibleRows
	}
	return rows
}

// ensureVisible shifts the scroll window so the cursor row is on
// screen, clamping at both ends of the list.
func (p *selectPicker) ensureVisible(rows int) {
	if rows <= 0 || len(p.options) == 0 {
		return
	}
	if p.offset > len(p.options)-rows {
		p.offset = len(p.options) - rows
	}
	if p.offset < 0 {
		p.offset = 0
	}
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+rows {
		p.offset = p.cursor - rows + 1
	}
}

// pageUp/pageDown/home/end move the selection in larger steps for long
// model lists. The page size is whatever view() last fit on screen.
func (p *selectPicker) pageUp() {
	if p.pageSize <= 0 {
		p.pageSize = minPickerVisibleRows
	}
	p.cursor -= p.pageSize
	p.clampCursor()
}

func (p *selectPicker) pageDown() {
	if p.pageSize <= 0 {
		p.pageSize = minPickerVisibleRows
	}
	p.cursor += p.pageSize
	p.clampCursor()
}

func (p *selectPicker) home() { p.cursor = 0; p.skipHeader(1) }

func (p *selectPicker) end() {
	p.cursor = len(p.options) - 1
	p.skipHeader(-1)
}

func (p *selectPicker) clampCursor() {
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor > len(p.options)-1 {
		p.cursor = len(p.options) - 1
	}
	p.skipHeader(1)
}

// enableEffort turns on the effort ladder row for model pickers,
// seeded with the currently active level.
func (p *selectPicker) enableEffort(current effort.Level) {
	p.effortEnabled = true
	p.effortCursor = int(current) - 1
	if p.effortCursor < 0 {
		p.effortCursor = int(effort.Default) - 1
	}
}

// moveEffortLeft/moveEffortRight walk the ladder, clamped at the ends.
func (p *selectPicker) moveEffortLeft() {
	if p.effortCursor > 0 {
		p.effortCursor--
	}
}

func (p *selectPicker) moveEffortRight() {
	if p.effortCursor < len(effort.All)-1 {
		p.effortCursor++
	}
}

// selectedEffort returns the level under the effort cursor.
func (p *selectPicker) selectedEffort() effort.Level {
	return effort.All[p.effortCursor]
}

// renderEffortRow draws the ladder with brackets around the active
// choice, e.g.  low → medium → [high] → ultra → lato-X. Plain text
// markers keep it readable without color.
func (p *selectPicker) renderEffortRow() string {
	parts := make([]string, len(effort.All))
	for i, lvl := range effort.All {
		if i == p.effortCursor {
			parts[i] = pickerSelectedStyle.Render("[" + lvl.String() + "]")
			continue
		}
		parts[i] = pickerMetaStyle.Render(lvl.String())
	}
	return "Effort: " + strings.Join(parts, " → ")
}

// view renders the picker as a centered modal over a width x height
// area. Only the rows fitting the height are rendered; the window
// follows the cursor so the highlighted choice is always visible.
func (p *selectPicker) view(width, height int) string {
	rows := p.visibleRows(height)
	p.pageSize = rows
	p.ensureVisible(rows)

	var b strings.Builder

	b.WriteString(pickerTitleStyle.Render(p.title))
	b.WriteString("\n\n")

	end := p.offset + rows
	if end > len(p.options) {
		end = len(p.options)
	}
	for i := p.offset; i < end; i++ {
		b.WriteString(p.renderRow(i, p.options[i]))
		b.WriteString("\n")
	}

	if p.effortEnabled {
		b.WriteString("\n")
		b.WriteString(p.renderEffortRow())
		b.WriteString("\n")
	}

	b.WriteString("\n")
	help := "↑/↓ model · ←/→ effort · enter choose · s session-only · esc cancel"
	if !p.effortEnabled {
		help = "↑/↓ select · enter choose · esc cancel"
	} else if len(p.options) > rows {
		help = fmt.Sprintf("%d/%d · ↑/↓ model · PgUp/PgDn page · ←/→ effort · enter choose", p.cursor+1, len(p.options))
	}
	b.WriteString(pickerHelpStyle.Render(help))

	box := pickerBorderStyle.Width(pickerWidth).Render(strings.TrimRight(b.String(), "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// renderRow formats a single option row: a selection cursor, the
// friendly label, and a checkmark if it's the active choice. Header
// rows render as section titles without a cursor.
func (p *selectPicker) renderRow(i int, opt selectOption) string {
	if opt.Header {
		return pickerTitleStyle.Render(opt.Label)
	}

	cursor := "  "
	if i == p.cursor {
		cursor = "› "
	}

	line := cursor + opt.Label
	if opt.Current {
		line += " " + pickerActiveStyle.Render("✓")
	}

	if i == p.cursor {
		return pickerSelectedStyle.Width(pickerWidth - 4).Render(line)
	}
	return pickerMetaStyle.Width(pickerWidth - 4).Render(line)
}
