package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"lato/internal/theme"
)

const themePickerWidth = 58

type themePicker struct {
	input    textinput.Model
	all      []string
	matches  []string
	cursor   int
	offset   int
	previous string
	current  string
	preview  string
	save     func(string) error
}

func newThemePicker(current string, save func(string) error) *themePicker {
	in := textinput.New()
	in.Prompt = "Search: "
	in.Placeholder = "type a theme name"
	in.CharLimit = 80
	in.Width = themePickerWidth - 12
	in.Focus()
	p := &themePicker{input: in, all: theme.Names(), previous: current, current: current, preview: current, save: save}
	p.filter()
	return p
}

func (p *themePicker) filter() {
	query := strings.ToLower(strings.TrimSpace(p.input.Value()))
	p.matches = p.matches[:0]
	for _, name := range p.all {
		if query == "" || strings.Contains(name, query) {
			p.matches = append(p.matches, name)
		}
	}
	if p.cursor >= len(p.matches) {
		p.cursor = 0
	}
	if p.input.Value() == "" {
		for i, name := range p.matches {
			if name == p.current {
				p.cursor = i
				break
			}
		}
	}
	p.ensureVisible(24)
	if len(p.matches) > 0 {
		p.preview = p.matches[p.cursor]
		applyTheme(p.preview)
	}
}

func (p *themePicker) selected() string {
	if len(p.matches) == 0 {
		return ""
	}
	return p.matches[p.cursor]
}

func (p *themePicker) ensureVisible(rows int) {
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+rows {
		p.offset = p.cursor - rows + 1
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

func (p *themePicker) visibleRows(height int) int {
	rows := height - 12
	if rows < 3 {
		return 3
	}
	return rows
}

func (p *themePicker) move(delta int) {
	if len(p.matches) == 0 {
		return
	}
	p.cursor += delta
	if p.cursor < 0 {
		p.cursor = len(p.matches) - 1
	}
	if p.cursor >= len(p.matches) {
		p.cursor = 0
	}
	p.preview = p.selected()
	p.ensureVisible(24)
	applyTheme(p.preview)
}

func (p *themePicker) handleKey(msg tea.KeyMsg) tea.Cmd {
	if msg.Paste {
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		p.filter()
		return cmd
	}
	switch msg.Type {
	case tea.KeyUp:
		p.move(-1)
		return nil
	case tea.KeyDown:
		p.move(1)
		return nil
	case tea.KeyHome:
		p.cursor = 0
		p.preview = p.selected()
		p.ensureVisible(24)
		applyTheme(p.preview)
		return nil
	case tea.KeyEnd:
		if len(p.matches) > 0 {
			p.cursor = len(p.matches) - 1
			p.preview = p.selected()
			p.ensureVisible(24)
			applyTheme(p.preview)
		}
		return nil
	case tea.KeyPgUp:
		p.move(-p.visibleRows(24))
		return nil
	case tea.KeyPgDown:
		p.move(p.visibleRows(24))
		return nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.filter()
	return cmd
}

func (p *themePicker) apply() error {
	selected := p.selected()
	if selected == "" {
		return nil
	}
	p.current = selected
	if p.save != nil {
		if err := p.save(selected); err != nil {
			return err
		}
	}
	return nil
}

func (p *themePicker) cancel() { applyTheme(p.previous) }

func (p *themePicker) view(width, height int) string {
	rows := p.visibleRows(height)
	p.ensureVisible(rows)
	boxWidth := modalWidth(width, themePickerWidth)
	contentWidth := modalInnerWidth(boxWidth)
	p.input.Width = contentWidth - 2
	if p.input.Width < 1 {
		p.input.Width = 1
	}
	var b strings.Builder
	b.WriteString(pickerTitleStyle.Render("Themes"))
	b.WriteString("\n\n")
	b.WriteString(inputBorderStyle.Width(contentWidth).Render(p.input.View()))
	b.WriteString("\n\n")
	if len(p.matches) == 0 {
		b.WriteString(pickerMetaStyle.Render("No themes match your search."))
	} else {
		end := p.offset + rows
		if end > len(p.matches) {
			end = len(p.matches)
		}
		for i := p.offset; i < end; i++ {
			name := p.matches[i]
			prefix := "  "
			if i == p.cursor {
				prefix = "› "
			}
			marker := ""
			if name == p.current {
				marker = " ✓"
			}
			line := prefix + swatch(name) + " " + name + marker
			if i == p.cursor {
				b.WriteString(pickerSelectedStyle.Width(contentWidth).Render(trimModalText(line, contentWidth)))
			} else {
				b.WriteString(pickerMetaStyle.Width(contentWidth).Render(trimModalText(line, contentWidth)))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(previewLine(p.preview))
	b.WriteString("\n\n")
	b.WriteString(pickerHelpStyle.Render("↑/↓ navigate · type to search · enter apply · esc cancel"))
	box := pickerBorderStyle.Width(boxWidth).Render(strings.TrimRight(b.String(), "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func swatch(name string) string {
	p, ok := theme.Lookup(name)
	if !ok {
		return "·"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(p.Primary)).Render("●")
}

func previewLine(name string) string {
	p, ok := theme.Lookup(name)
	if !ok {
		return pickerMetaStyle.Render("Preview unavailable")
	}
	return fmt.Sprintf("Preview %s  %s %s %s %s %s",
		name,
		lipgloss.NewStyle().Foreground(lipgloss.Color(p.Primary)).Render("● primary"),
		lipgloss.NewStyle().Foreground(lipgloss.Color(p.Success)).Render("● success"),
		lipgloss.NewStyle().Foreground(lipgloss.Color(p.Warning)).Render("● warning"),
		lipgloss.NewStyle().Foreground(lipgloss.Color(p.Error)).Render("● error"),
		lipgloss.NewStyle().Foreground(lipgloss.Color(p.Muted)).Render("● muted"),
	)
}
