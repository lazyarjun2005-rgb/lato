package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"lato/internal/effort"
	"lato/internal/providers"
)

type modelChoice struct {
	providerID   string
	providerName string
	model        providers.ModelInfo
	current      bool
}

type modelPicker struct {
	input          textinput.Model
	all            []modelChoice
	matches        []int
	cursor         int
	offset         int
	currentModel   string
	current        effort.Level
	effortCursor   int
	activeProvider string
}

func newSearchableModelPicker(groups []modelGroup, activeProvider, currentModel string, currentEffort effort.Level) *modelPicker {
	in := textinput.New()
	in.Prompt = "Search: "
	in.Placeholder = "model or provider"
	in.CharLimit = 120
	in.Width = pickerWidth - 12
	in.Focus()

	p := &modelPicker{input: in, currentModel: currentModel, current: currentEffort, activeProvider: activeProvider}
	p.effortCursor = int(currentEffort) - 1
	if p.effortCursor < 0 || p.effortCursor >= len(effort.All) {
		p.effortCursor = int(effort.Default) - 1
	}
	for _, group := range groups {
		providerID := group.ID
		if providerID == "" {
			providerID = group.Name
			if info, ok := providerByDisplayName(group.Name); ok {
				providerID = info.ID
			}
		}
		for _, model := range group.Models {
			p.all = append(p.all, modelChoice{
				providerID: providerID, providerName: group.Name, model: model,
				current: providerID == activeProvider && model.ID == currentModel,
			})
		}
	}
	p.filter()
	return p
}

func providerByDisplayName(name string) (providers.ProviderInfo, bool) {
	for _, info := range providers.Registry {
		if info.Name == name {
			return info, true
		}
	}
	return providers.ProviderInfo{}, false
}

func (p *modelPicker) filter() {
	query := strings.ToLower(strings.TrimSpace(p.input.Value()))
	p.matches = p.matches[:0]
	for i, choice := range p.all {
		if query == "" || strings.Contains(strings.ToLower(choice.model.ID), query) ||
			strings.Contains(strings.ToLower(choice.model.Name), query) ||
			strings.Contains(strings.ToLower(choice.providerName), query) {
			p.matches = append(p.matches, i)
		}
	}
	if p.cursor >= len(p.matches) {
		p.cursor = 0
	}
	if query == "" {
		for i, index := range p.matches {
			if p.all[index].current {
				p.cursor = i
				break
			}
		}
	}
	p.ensureVisible(24)
}

func (p *modelPicker) selected() (modelChoice, bool) {
	if len(p.matches) == 0 {
		return modelChoice{}, false
	}
	return p.all[p.matches[p.cursor]], true
}

func (p *modelPicker) visibleRows(height int) int {
	rows := height - 14
	if rows < minPickerVisibleRows {
		return minPickerVisibleRows
	}
	return rows
}

func (p *modelPicker) ensureVisible(rows int) {
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

func (p *modelPicker) move(delta int) {
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
	p.ensureVisible(24)
}

func (p *modelPicker) handleKey(msg tea.KeyMsg) tea.Cmd {
	if msg.Paste {
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		p.filter()
		return cmd
	}
	switch msg.Type {
	case tea.KeyLeft:
		if p.effortCursor > 0 {
			p.effortCursor--
		}
		return nil
	case tea.KeyRight:
		if p.effortCursor < len(effort.All)-1 {
			p.effortCursor++
		}
		return nil
	case tea.KeyUp:
		p.move(-1)
		return nil
	case tea.KeyDown:
		p.move(1)
		return nil
	case tea.KeyHome:
		p.cursor = 0
		p.ensureVisible(24)
		return nil
	case tea.KeyEnd:
		if len(p.matches) > 0 {
			p.cursor = len(p.matches) - 1
			p.ensureVisible(24)
		}
		return nil
	case tea.KeyPgUp:
		p.move(-p.visibleRows(24))
		return nil
	case tea.KeyPgDown:
		p.move(p.visibleRows(24))
		return nil
	}
	if msg.String() == "h" || msg.String() == "l" {
		if msg.String() == "h" && p.effortCursor > 0 {
			p.effortCursor--
		}
		if msg.String() == "l" && p.effortCursor < len(effort.All)-1 {
			p.effortCursor++
		}
		return nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.filter()
	return cmd
}

func (p *modelPicker) view(width, height int) string {
	rows := p.visibleRows(height)
	p.ensureVisible(rows)
	var b strings.Builder
	b.WriteString(pickerTitleStyle.Render("Models"))
	b.WriteString("\n\n")
	b.WriteString(inputBorderStyle.Width(pickerWidth - 4).Render(p.input.View()))
	b.WriteString("\n\n")
	if len(p.matches) == 0 {
		b.WriteString(pickerMetaStyle.Render("No models match your search."))
	} else {
		end := p.offset + rows
		if end > len(p.matches) {
			end = len(p.matches)
		}
		for i := p.offset; i < end; i++ {
			choice := p.all[p.matches[i]]
			prefix := "  "
			if i == p.cursor {
				prefix = "› "
			}
			marker := ""
			if choice.current {
				marker = " ✓"
			}
			line := prefix + choice.model.ID + "  " + pickerMetaStyle.Render("["+choice.providerName+"]") + marker
			style := pickerMetaStyle
			if i == p.cursor {
				style = pickerSelectedStyle
			}
			b.WriteString(style.Width(pickerWidth - 4).Render(line))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	if choice, ok := p.selected(); ok {
		b.WriteString(fmt.Sprintf("Selected: %s · %s", choice.model.Name, choice.providerName))
	}
	b.WriteString("\nEffort: ")
	for i, level := range effort.All {
		if i == p.effortCursor {
			b.WriteString(pickerSelectedStyle.Render("[" + level.String() + "]"))
		} else {
			b.WriteString(pickerMetaStyle.Render(level.String()))
		}
		if i < len(effort.All)-1 {
			b.WriteString(" · ")
		}
	}
	b.WriteString("\n\n")
	b.WriteString(pickerHelpStyle.Render("↑/↓ navigate · type to search · enter apply · esc cancel"))
	box := pickerBorderStyle.Width(pickerWidth).Render(strings.TrimRight(b.String(), "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func (p *modelPicker) effort() effort.Level { return effort.All[p.effortCursor] }
