package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"

	"lato/internal/effort"
	"lato/internal/providers"
)

func groupsFixture() []modelGroup {
	return []modelGroup{
		{Name: "Ollama", Models: []providers.ModelInfo{{ID: "qwen2.5:3b", Name: "qwen2.5:3b"}}},
		{Name: "OpenRouter", Models: []providers.ModelInfo{
			{ID: "vendor/model", Name: "vendor/model"},
			{ID: "cc/glm-4:variant", Name: "cc/glm-4:variant"},
		}},
	}
}

func TestGroupedPickerHeadersAreSkipped(t *testing.T) {
	p := newGroupedModelPicker(groupsFixture(), "", effort.Medium)
	if len(p.options) != 5 { // 2 headers + 3 models
		t.Fatalf("options = %d, want 5 (2 headers + 3 models)", len(p.options))
	}

	// The cursor must never rest on a header row.
	for i := 0; i < len(p.options); i++ {
		if p.selected().Header {
			t.Fatalf("cursor at %d rests on header %q", p.cursor, p.selected().Label)
		}
		p.moveDown()
	}
	p.moveUp()
	p.moveUp()
	if p.selected().Header {
		t.Fatal("cursor ended on a header after moving up")
	}
}

func TestGroupedPickerKeepsOpaqueIDs(t *testing.T) {
	p := newGroupedModelPicker(groupsFixture(), "cc/glm-4:variant", effort.Medium)
	sel := p.selected()
	if sel.ID != "cc/glm-4:variant" || !sel.Current {
		t.Fatalf("selected = %+v, want current opaque model ID cc/glm-4:variant", sel)
	}
	view := p.view(80, 24)
	for _, want := range []string{"Ollama", "OpenRouter", "vendor/model"} {
		if !strings.Contains(view, want) {
			t.Errorf("grouped view missing %q", want)
		}
	}
}

func TestGroupedPickerSkipsEmptyGroups(t *testing.T) {
	groups := append(groupsFixture(), modelGroup{Name: "Empty"})
	p := newGroupedModelPicker(groups, "", effort.Medium)
	if strings.Contains(p.view(80, 24), "Empty") {
		t.Error("empty group rendered a section header")
	}
}

func TestInputModalMaskedAndDefault(t *testing.T) {
	step := inputStep{title: "T", prompt: "Base URL:", initial: "http://localhost:20128/v1"}
	im := newInputModal(step)
	if im.Value() != step.initial {
		t.Errorf("Value() = %q, want placeholder default", im.Value())
	}

	masked := newInputModal(inputStep{title: "K", prompt: "API key:", masked: true})
	if masked.input.EchoMode != textinput.EchoPassword {
		t.Error("masked input must use password echo so keys never appear on screen")
	}
}

// --- viewport / scrolling behavior -------------------------------------

// bigList builds a picker over n plainly-labeled models.
func bigList(n int) *selectPicker {
	options := make([]selectOption, n)
	for i := 0; i < n; i++ {
		label := fmt.Sprintf("model-%03d", i)
		options[i] = selectOption{Label: label, ID: label}
	}
	return &selectPicker{title: "Model (test)", options: options, scope: scopeModel}
}

func TestPickerViewportShowsInitialSelection(t *testing.T) {
	p := bigList(200)
	p.cursor = 100

	view := p.view(80, 24)
	if !strings.Contains(view, "model-100") {
		t.Fatal("initial render does not show the selected model")
	}
	if strings.Contains(view, "model-000") || strings.Contains(view, "model-199") {
		t.Error("viewport rendered rows far outside the selection window")
	}
}

func TestPickerViewportScrollsDownWithCursor(t *testing.T) {
	p := bigList(200)
	rows := p.visibleRows(24)

	for i := 0; i < rows+5; i++ { // push the cursor past the first page
		p.moveDown()
	}
	view := p.view(80, 24)

	want := fmt.Sprintf("model-%03d", p.cursor)
	if !strings.Contains(view, want) {
		t.Fatalf("view after scrolling lacks selected row %q", want)
	}
	if strings.Contains(view, "model-000") {
		t.Error("top of list still rendered after the window scrolled down")
	}
	if p.offset+p.pageSize <= p.cursor {
		t.Errorf("cursor %d outside window [%d,%d)", p.cursor, p.offset, p.offset+p.pageSize)
	}
}

func TestPickerViewportScrollsBackUp(t *testing.T) {
	p := bigList(200)
	rows := p.visibleRows(24)
	for i := 0; i < rows*2; i++ {
		p.moveDown()
	}
	for i := p.cursor; i > 0; i-- {
		p.moveUp()
	}
	view := p.view(80, 24)

	if p.cursor != 0 || p.offset != 0 {
		t.Fatalf("cursor/offset = %d/%d, want 0/0 after returning to top", p.cursor, p.offset)
	}
	if !strings.Contains(view, "model-000") {
		t.Error("first model not visible after scrolling back up")
	}
}

func TestPickerLargeListNotFullyRendered(t *testing.T) {
	p := bigList(300)
	view := p.view(80, 20)

	if strings.Count(view, "\n") > 40 {
		t.Errorf("rendered %d lines for a 20-row terminal; list was not bounded", strings.Count(view, "\n"))
	}
	for i := 250; i < 300; i++ {
		if strings.Contains(view, fmt.Sprintf("model-%03d", i)) {
			t.Fatalf("unreachable row model-%03d rendered", i)
		}
	}
}

func TestPickerSelectedIndexStableAcrossScrolling(t *testing.T) {
	p := bigList(50)
	for down := 0; down < 30; down++ {
		p.moveDown()
		p.view(80, 15) // scroll window follows
		if got := p.selected().ID; got != fmt.Sprintf("model-%03d", down+1) {
			t.Fatalf("after %d downs selection = %q", down+1, got)
		}
	}
	for up := 29; up >= 0; up-- {
		p.moveUp()
		p.view(80, 15)
		if got := p.selected().ID; got != fmt.Sprintf("model-%03d", up) {
			t.Fatalf("after up, selection = %q, want model-%03d", got, up)
		}
	}
}

func TestPickerSmallTerminalStillShowsSelection(t *testing.T) {
	p := bigList(100)
	p.cursor = 60
	view := p.view(80, 6)
	if !strings.Contains(view, "model-060") {
		t.Fatal("selection invisible in a very short terminal")
	}
}

func TestPickerPageAndJumpKeys(t *testing.T) {
	p := bigList(200)
	p.view(80, 24) // establish page size
	page := p.pageSize

	p.pageDown()
	if p.cursor < page-2 || p.cursor > page+2 {
		t.Errorf("pageDown cursor = %d, want ~%d", p.cursor, page)
	}
	p.home()
	if p.cursor != 0 {
		t.Errorf("home cursor = %d, want 0", p.cursor)
	}
	p.end()
	if p.cursor != len(p.options)-1 {
		t.Errorf("end cursor = %d, want last", p.cursor)
	}
	p.pageUp()
	if p.cursor >= len(p.options)-1 {
		t.Error("pageUp from the end did not move the cursor")
	}
}

func TestGroupedPickerViewportKeepsHeadersSelectableFlow(t *testing.T) {
	g := groupsFixture()
	var options []selectOption
	for _, grp := range g {
		options = append(options, selectOption{Label: grp.Name, Header: true})
		for _, mi := range grp.Models {
			options = append(options, selectOption{Label: mi.ID, ID: mi.ID})
		}
	}
	p := &selectPicker{title: "Model", options: options, scope: scopeModel}
	p.skipHeader(1)

	view := p.view(80, 12) // small: cannot show all rows
	if p.selected().Header {
		t.Fatal("cursor rests on a header in a cramped viewport")
	}
	if !strings.Contains(view, p.selected().ID) {
		t.Fatal("selected grouped model not visible in cramped viewport")
	}
}
