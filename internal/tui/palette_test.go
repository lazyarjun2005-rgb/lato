package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"lato/internal/config"
	"lato/internal/runtime"
	"lato/internal/session"
)

// newStartupModel mirrors production construction exactly as newModel
// does it: registry + palette created together, input focused, and NO
// explicit palette synchronization. It exists to catch initialization
// bugs that a fixture calling syncPalette() would mask.
func newStartupModel() model {
	reg := newRegistry()
	input := textinput.New()
	input.Prompt = "› "
	input.CharLimit = 4000
	input.Focus()
	return model{
		registry: reg,
		palette:  newSlashPalette(reg),
		entries:  []chatEntry{},
		input:    input,
	}
}

func TestNewSlashPaletteStartsDisengaged(t *testing.T) {
	p := newSlashPalette(newRegistry())
	if p.engaged() {
		t.Errorf("fresh palette engaged with %d suggestions; must start hidden", len(p.matches))
	}
	if p.isEmptyQuery() {
		t.Error("fresh palette reported an empty-query state")
	}
	if view := p.view(80); view != "" {
		t.Errorf("fresh palette rendered content:\n%s", view)
	}
}

// TestStartupModelShowsNoPalette pins the M16 regression fix: launching
// Lato with untouched input shows no suggestions at all.
func TestStartupModelShowsNoPalette(t *testing.T) {
	m := newStartupModel()

	if m.input.Value() != "" {
		t.Fatalf("startup input = %q, want empty", m.input.Value())
	}
	if m.palette.engaged() {
		t.Errorf("palette engaged at startup with %d suggestions", len(m.palette.matches))
	}

	footer := m.renderFooterWithPalette()
	for _, cmd := range []string{"/exit", "/clear", "/model", "/provider", "/effort", "/connect"} {
		if strings.Contains(footer, cmd) {
			t.Errorf("startup footer leaks %q:\n%s", cmd, footer)
		}
	}
	if strings.Contains(footer, "keep typing") || strings.Contains(footer, "No matching commands") {
		t.Errorf("startup footer shows palette chrome:\n%s", footer)
	}
	if m.paletteExtraLines() != 0 {
		t.Errorf("palette occupies %d layout rows at startup, want 0", m.paletteExtraLines())
	}
}

// TestRelaunchedModelStillHidden pins requirement 7: after a session
// that used the palette, constructing a fresh model (the relaunch case)
// starts hidden again. Interaction happens within one model — palette
// is per-model state.
func TestRelaunchedModelStillHidden(t *testing.T) {
	first := newStartupModel()
	first = typeInto(first, "/")
	if !first.palette.engaged() {
		t.Fatal("setup: / did not engage palette")
	}
	next, _ := first.handleKey(keyMsg(tea.KeyBackspace, ""))
	interacted := next.(model)
	if interacted.palette.engaged() {
		t.Fatal("setup: backspace did not disengage palette")
	}

	fresh := newStartupModel()
	if fresh.palette.engaged() {
		t.Error("relaunched model started with palette engaged")
	}
	if view := fresh.renderFooterWithPalette(); strings.Contains(view, "/exit") {
		t.Errorf("relaunched model rendered suggestions:\n%s", view)
	}
}

// TestPaletteInputLifecycle pins the full desired invariant as a table:
// palette state is derived exclusively from current input.
func TestPaletteInputLifecycle(t *testing.T) {
	cases := []struct {
		input     string
		engaged   bool
		emptyQ    bool
		firstName string // expected selected command, "" if not checked
	}{
		{input: "", engaged: false},
		{input: "/", engaged: true, firstName: "exit"},
		{input: "/mo", engaged: true, firstName: "model"},
		{input: "/model", engaged: true, firstName: "model"},
		{input: "/mem", engaged: true, firstName: "memory"},
		{input: "/model ", engaged: false},
		{input: "/xyz", engaged: false, emptyQ: true},
		{input: "hello", engaged: false},
	}
	for _, tc := range cases {
		m := newStartupModel()
		m.input.SetValue(tc.input)
		m.syncPalette()

		if got := m.palette.engaged(); got != tc.engaged {
			t.Errorf("input %q: engaged = %v, want %v", tc.input, got, tc.engaged)
		}
		if got := m.palette.isEmptyQuery(); got != tc.emptyQ {
			t.Errorf("input %q: emptyQuery = %v, want %v", tc.input, got, tc.emptyQ)
		}
		if tc.firstName != "" && m.palette.selected().name != tc.firstName {
			t.Errorf("input %q: selected = %q, want %q",
				tc.input, m.palette.selected().name, tc.firstName)
		}
	}
}

// TestBackspaceFromSlashDisengages pins the reported interaction path:
// "/" then backspace returns to the fully hidden state.
func TestBackspaceFromSlashDisengages(t *testing.T) {
	m := typeInto(newStartupModel(), "/")
	if !m.palette.engaged() {
		t.Fatal("setup: / did not engage palette")
	}
	next, _ := m.handleKey(keyMsg(tea.KeyBackspace, ""))
	done := next.(model)

	if done.input.Value() != "" {
		t.Fatalf("backspace left input %q", done.input.Value())
	}
	if done.palette.engaged() {
		t.Errorf("palette still engaged after clearing input")
	}
	if done.renderFooterWithPalette() != done.renderFooter() {
		t.Error("hidden palette still altered the footer rendering")
	}
}
func newPaletteTestModel() model {
	reg := newRegistry()
	input := textinput.New()
	input.Prompt = "› "
	input.CharLimit = 4000
	input.Focus()
	m := model{
		registry: reg,
		palette:  newSlashPalette(reg),
		entries:  []chatEntry{{Role: roleSystem, Content: "seed"}},
		input:    input,
	}
	m.syncPalette()
	return m
}

// typeInto feeds a string one rune at a time through handleKey, like a
// real keyboard would.
func typeInto(m model, s string) model {
	for _, r := range s {
		next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(string(r))})
		m = next.(model)
	}
	return m
}

func keyMsg(k tea.KeyType, s string) tea.KeyMsg {
	if s != "" {
		return tea.KeyMsg{Type: k, Runes: []rune(s)}
	}
	return tea.KeyMsg{Type: k}
}

// TestPaletteOpensOnSlash pins requirement 1: typing "/" immediately
// engages the palette over every registered command.
func TestPaletteOpensOnSlash(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/")
	if !m.palette.engaged() {
		t.Fatal("palette did not engage on \"/\"")
	}
	if len(m.palette.matches) != len(m.registry.All()) {
		t.Errorf("palette shows %d suggestions, want all %d registered",
			len(m.palette.matches), len(m.registry.All()))
	}
}

// TestPaletteFiltersByPrefix pins requirements 2–3 and J's ranking:
// exact prefixes only — unrelated commands never appear.
func TestPaletteFiltersByPrefix(t *testing.T) {
	cases := map[string][]string{
		"/m":   {"model", "memory"}, // registration order: model before memory
		"/mo":  {"model"},
		"/mem": {"memory"},
	}
	for in, want := range cases {
		m := typeInto(newPaletteTestModel(), in)
		if !m.palette.engaged() {
			t.Fatalf("%q: palette disengaged", in)
		}
		var got []string
		for _, s := range m.palette.matches {
			got = append(got, s.name)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%q filtered to %v, want %v", in, got, want)
		}
	}
}

// TestPaletteCoReturnsConnectAndCopy pins the spec example for "/co".
func TestPaletteCoReturnsConnectAndCopy(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/co")
	var got []string
	for _, s := range m.palette.matches {
		got = append(got, s.name)
	}
	// "code" joined the c-commands in Milestone 2; registration order
	// puts connect and copy before it.
	if strings.Join(got, ",") != "connect,copy,code" {
		t.Errorf("/co matched %v, want [connect copy code]", got)
	}
}

// TestPaletteCaseInsensitive pins requirement 6.
func TestPaletteCaseInsensitive(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/MO")
	if !m.palette.engaged() || m.palette.selected().name != "model" {
		t.Errorf("case-insensitive match failed: %+v", m.palette.matches)
	}
}

// TestPaletteNoSubstringNoise pins the prefix-only rule: a fragment
// that is not a prefix of any command shows the empty state instead of
// fuzzy noise.
func TestPaletteNoSubstringNoise(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/nnec")
	if m.palette.engaged() {
		t.Errorf("substring noise leaked into matches: %+v", m.palette.matches)
	}
	if !m.palette.isEmptyQuery() {
		t.Error("non-matching query not detected as empty")
	}
}

// TestPaletteNoMatchesState pins requirement 7 / Part O: graceful empty
// state, never a crash, never suggestions.
func TestPaletteNoMatchesState(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/xyz")
	if m.palette.engaged() {
		t.Error("empty query reported engaged")
	}
	if !m.palette.isEmptyQuery() {
		t.Error("empty query not detected")
	}
	view := m.renderFooterWithPalette()
	if !strings.Contains(view, "No matching commands") {
		t.Errorf("empty-state message missing:\n%s", view)
	}
}

// TestPaletteArrowNavigation pins requirement 8: ↓ advances, ↑ wraps
// backwards, selection stays inside bounds.
func TestPaletteArrowNavigation(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/")
	first := m.palette.cursor

	next, _ := m.handleKey(keyMsg(tea.KeyDown, ""))
	m = next.(model)
	if m.palette.cursor != first+1 {
		t.Errorf("down moved cursor %d→%d", first, m.palette.cursor)
	}

	next, _ = m.handleKey(keyMsg(tea.KeyUp, ""))
	m = next.(model)
	if m.palette.cursor != first {
		t.Errorf("up returned cursor to %d, want %d", m.palette.cursor, first)
	}

	// Wrap at the top.
	for i := 0; i < len(m.palette.matches)+1; i++ {
		next, _ = m.handleKey(keyMsg(tea.KeyUp, ""))
		m = next.(model)
	}
	if m.palette.cursor < 0 || m.palette.cursor >= len(m.palette.matches) {
		t.Fatalf("cursor escaped bounds: %d", m.palette.cursor)
	}
}

// TestEnterAcceptsAndExecutes pins requirements 9/L: Enter completes the
// suggestion into the input AND submits it through the normal
// dispatcher — /clear really clears.
func TestEnterAcceptsAndExecutes(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/cle")
	next, _ := m.handleKey(keyMsg(tea.KeyEnter, ""))
	done := next.(model)

	if len(done.entries) != 0 {
		t.Errorf("/clear did not execute: %d entries remain", len(done.entries))
	}
	if done.input.Value() != "" {
		t.Errorf("input not consumed after submit: %q", done.input.Value())
	}
	if done.palette.engaged() {
		t.Error("palette stayed engaged after submission")
	}
}

// TestTabFillsWithoutSubmitting pins the Tab variant: fill the canonical
// command, keep editing.
func TestTabFillsWithoutSubmitting(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/mem")
	next, _ := m.handleKey(keyMsg(tea.KeyTab, ""))
	done := next.(model)

	if done.input.Value() != "/memory" {
		t.Errorf("tab fill produced %q, want /memory", done.input.Value())
	}
	if len(done.entries) != 1 {
		t.Error("tab submitted instead of filling")
	}
}

// TestEscClosesPaletteKeepingText pins requirement 10: Esc dismisses the
// palette but leaves the typed text alone.
func TestEscClosesPaletteKeepingText(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/mo")
	next, _ := m.handleKey(keyMsg(tea.KeyEsc, ""))
	done := next.(model)

	if done.palette.engaged() {
		t.Error("esc left palette engaged")
	}
	if done.input.Value() != "/mo" {
		t.Errorf("esc destroyed input: %q", done.input.Value())
	}
	if done.quitting {
		t.Error("esc quit the app while the palette was open")
	}
}

// TestBackspaceContinuesFiltering pins requirement 11: deleting a
// character widens suggestions again.
func TestBackspaceContinuesFiltering(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/mod")
	if got := m.palette.selected().name; got != "model" {
		t.Fatalf("setup wrong: %q", got)
	}
	next, _ := m.handleKey(keyMsg(tea.KeyBackspace, ""))
	m = next.(model)
	if !m.palette.engaged() {
		t.Fatal("backspace closed the palette")
	}
	var got []string
	for _, s := range m.palette.matches {
		got = append(got, s.name)
	}
	if strings.Join(got, ",") != "model" {
		t.Errorf("after backspace matches = %v, want just model", got)
	}
}

// TestSpaceDisengagesPalette pins Part M: once arguments start, the
// palette steps aside.
func TestSpaceDisengagesPalette(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/model ")
	if m.palette.engaged() {
		t.Error("palette stayed engaged after argument space")
	}
}

// TestNormalChatNeverEngagesPalette pins requirement 17.
func TestNormalChatNeverEngagesPalette(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "hello world 2+2")
	if m.palette.engaged() || m.palette.isEmptyQuery() {
		t.Error("plain chat engaged the palette")
	}
}

// TestCtrlCQuitsEvenWithPaletteOpen pins requirement 18 / Part Q: the
// palette must not swallow Ctrl+C.
func TestCtrlCQuitsEvenWithPaletteOpen(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/mo")
	next, _ := m.handleKey(keyMsg(tea.KeyCtrlC, ""))
	done := next.(model)
	if !done.quitting {
		t.Error("ctrl+c did not quit while palette open")
	}
}

// TestAltCUnaffectedByPalette pins requirement 19: Alt+C reaches the
// copy shortcut while the palette is engaged.
func TestAltCUnaffectedByPalette(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/co")
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c"), Alt: true})
	done := next.(model)

	found := false
	for _, e := range done.entries {
		if strings.Contains(e.Content, "Nothing to copy yet") {
			found = true
		}
	}
	if !found {
		t.Error("alt+c shortcut did not run while palette was open")
	}
}

// TestEveryRegisteredCommandAppearsExactlyOnce pins requirements 14–15
// and Part N: the palette derives from the same registry metadata as
// /help, aliases are not duplicated as rows, and nothing is missing.
func TestEveryRegisteredCommandAppearsExactlyOnce(t *testing.T) {
	m := newPaletteTestModel()

	seen := map[string]int{}
	for _, s := range m.palette.all {
		seen[s.name]++
	}
	for _, cmd := range m.registry.All() {
		if seen[cmd.Name()] != 1 {
			t.Errorf("command %q appears %d times in palette source", cmd.Name(), seen[cmd.Name()])
		}
	}
	if len(seen) != len(m.registry.All()) {
		t.Errorf("palette snapshot has %d unique commands, registry has %d",
			len(seen), len(m.registry.All()))
	}

	// Aliases must not appear as separate palette rows.
	for _, s := range m.palette.all {
		if s.name == "?" {
			t.Errorf("alias %q leaked into the palette", s.name)
		}
	}
}

// TestAliasStillDispatches pins requirement 16: aliases bypass the
// palette but keep working through the dispatcher ("/?" is an alias of
// /help).
func TestAliasStillDispatches(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/?")
	next, _ := m.handleKey(keyMsg(tea.KeyEnter, ""))
	done := next.(model)

	found := false
	for _, e := range done.entries {
		if strings.Contains(e.Content, "Available commands") {
			found = true
		}
	}
	if !found {
		t.Error("/? alias did not produce help output")
	}
}

// TestHelpStillWorksAfterPaletteChanges pins requirement 13: the plain,
// fully-typed command keeps working.
func TestHelpStillWorksAfterPaletteChanges(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/help")
	next, _ := m.handleKey(keyMsg(tea.KeyEnter, ""))
	done := next.(model)

	found := false
	for _, e := range done.entries {
		if strings.Contains(e.Content, "Available commands") {
			found = true
		}
	}
	if !found {
		t.Error("/help no longer renders after palette integration")
	}
}

// TestBareSlashEnterDoesNothing pins a safety guard: "/" alone offers
// the full list but Enter must not auto-run the first suggestion
// (/exit would be a nasty surprise).
func TestBareSlashEnterDoesNothing(t *testing.T) {
	m := typeInto(newPaletteTestModel(), "/")
	next, _ := m.handleKey(keyMsg(tea.KeyEnter, ""))
	done := next.(model)

	if done.quitting {
		t.Fatal("bare / + Enter executed the highlighted command (exit)")
	}
	if len(done.entries) != 1 {
		t.Errorf("bare / + Enter changed state: %d entries", len(done.entries))
	}
}

// TestPaletteViewRendersSelectionCursor pins Part K's visual contract:
// the highlighted row carries "› ", others do not.
func TestPaletteViewRendersSelectionCursor(t *testing.T) {
	p := newSlashPalette(newRegistry())
	p.sync("/mo")

	view := p.view(80)
	lines := strings.Split(view, "\n")
	selected := 0
	for _, l := range lines {
		if strings.Contains(l, "› ") {
			selected++
		}
	}
	if selected != 1 {
		t.Errorf("view shows %d selected rows, want 1:\n%s", selected, view)
	}
	if !strings.Contains(view, "/model") {
		t.Errorf("view missing /model usage:\n%s", view)
	}
}

// TestPaletteViewCapsRows pins Part O's compactness rule: no matter how
// many commands exist, the strip stays bounded and says how many more
// there are.
func TestPaletteViewCapsRows(t *testing.T) {
	p := newSlashPalette(newRegistry())
	p.sync("/")

	view := p.view(100)
	if rows := strings.Count(view, "\n"); rows > maxPaletteRows+3 { // + hidden-hint + border
		t.Errorf("palette rendered %d lines, cap exceeded:\n%s", rows, view)
	}
	if !strings.Contains(view, "more") {
		t.Errorf("hidden-count hint missing:\n%s", view)
	}
}

// TestFullStartupRenderShowsNoPalette exercises the production path end
// to end: real config, real runtime, real model construction, real
// WindowSizeMsg — then asserts the rendered screen contains zero
// suggestion rows.
func TestFullStartupRenderShowsNoPalette(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LATO_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("APPDATA", filepath.Join(dir, "AppData", "Roaming"))

	cfg := `model:
  provider: ollama
  endpoint: http://localhost:11434
  name: test-model
agent:
  name: default
  system_prompt: test
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	cfgLoaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	rt, err := runtime.New()
	if err != nil {
		t.Fatal(err)
	}

	m := newModel(cfgLoaded, session.New(), newUIAsker(), rt)
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	done := sized.(model)

	view := done.View()
	for _, leaked := range []string{"/exit", "/clear", "/model", "/provider", "/effort", "/connect", "/memory", "keep typing"} {
		if strings.Contains(view, leaked) {
			t.Errorf("startup screen leaks %q:\n%s", leaked, view)
		}
	}
	if !strings.Contains(view, "Ask Lato something") {
		t.Errorf("startup view missing input placeholder:\n%s", view)
	}
}
