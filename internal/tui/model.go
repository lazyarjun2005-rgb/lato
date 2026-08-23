package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"lato/internal/command"
	"lato/internal/config"
	"lato/internal/effort"
	"lato/internal/providers"
	"lato/internal/runtime"
	"lato/internal/session"
)

// minTranscriptHeight is the smallest the scrollable transcript area is
// ever allowed to shrink to, so a very short terminal window still shows
// something usable instead of a zero-height viewport.
const minTranscriptHeight = 3

// headerHeight and footerHeight are the fixed number of terminal rows
// consumed by the header line and the input box + help line,
// respectively. Used to size the transcript viewport on every resize.
const (
	headerHeight = 2
	footerHeight = 3
)

// model is Lato's interactive chat state. It is a thin presentation
// layer: it renders a transcript and forwards each submitted message through
// the runtime's streaming agent loop. See the package doc for what it
// deliberately does not add.
type model struct {
	runtime  *runtime.Runtime
	session  *session.Session
	registry *command.Registry
	stream   <-chan runtime.Event

	assistantBuffer string

	agentName    string
	providerName string
	modelName    string
	effortName   string

	entries  []chatEntry
	viewport viewport.Model
	input    textinput.Model
	spinner  spinner.Model

	// picker is non-nil while the /sessions modal is open. It owns
	// nothing beyond its own selection state; switching the active
	// session is still handled by the model.
	picker *sessionPicker

	// selectPicker is non-nil while the /provider or /model modal is
	// open. Like picker, it owns nothing beyond its own selection
	// state.
	selectPicker *selectPicker

	// flow is non-nil while the /connect (or /connect import) wizard is
	// active. It owns its own pickers and inputs; the model only routes
	// keys and renders.
	flow *connectFlow

	// addFlow is non-nil while the /model add wizard is active.
	addFlow *addModelFlow

	// perm is non-nil while a permission confirmation (M13) is on
	// screen. It routes every key until answered; the decision goes to
	// the waiting runtime through the asker.
	perm *permPrompt

	// asker is the runtime's interactive confirmation bridge; it is
	// bound to the tea.Program in Start so prompts can reach Update.
	asker *uiAsker

	// palette is the slash-command autocomplete layer (M16). It is a
	// pure view of the command registry plus the current input prefix;
	// accepting a suggestion routes through the normal dispatcher.
	palette *slashPalette

	// pendingStream holds a stream started by a command (/task resume);
	// handleKey promotes it to the live stream after dispatch returns.
	pendingStream <-chan runtime.Event

	width, height int
	waiting       bool // true while a runTask command is in flight
	status        string
	quitting      bool
	ready         bool // true once the first WindowSizeMsg has arrived
}

// newModel builds the initial chat model. cfg is only used to label the
// session header (which agent/provider/model it's talking to); requests use
// rt, built by the caller so startup failures never panic here. The asker
// wires permission confirmations into this program; it is bound to the
// tea.Program by Start.
func newModel(cfg *config.Config, sess *session.Session, asker *uiAsker, r *runtime.Runtime) model {
	input := textinput.New()
	input.Placeholder = "Ask Lato something…"
	input.Prompt = "› "
	input.CharLimit = 4000
	input.Focus()

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = spinnerStyle

	entries := sessionEntries(sess)
	if startErr := r.StartError(); startErr != nil {
		// The provider could not be built at startup. Keep the TUI up:
		// the message tells the user exactly how to recover in place.
		entries = append(entries, chatEntry{
			Role: roleSystem,
			Content: "Model provider unavailable: " + startErr.Error() +
				"\nRun /connect to configure a provider, /model to pick another, or check your configuration.",
		})
	}

	r.SetAsker(asker)

	registry := newRegistry()

	return model{
		agentName:    cfg.Agent.Name,
		providerName: cfg.Model.Provider,
		modelName:    cfg.Model.Name,
		effortName:   r.CurrentEffort(),
		input:        input,
		spinner:      spin,
		viewport:     viewport.New(0, 0),
		runtime:      r,
		entries:      entries,
		session:      sess,
		registry:     registry,
		palette:      newSlashPalette(registry),
		asker:        asker,
	}
}

// sessionEntries converts a session's saved messages into the transcript
// entries the viewport renders. Both the initial model construction and
// switching sessions via the picker go through this single function, so
// the conversion logic never has to be kept in sync in two places.
func sessionEntries(sess *session.Session) []chatEntry {
	entries := make([]chatEntry, 0, len(sess.Messages))

	for _, msg := range sess.Messages {
		var entryRole role

		switch msg.Role {
		case "user":
			entryRole = roleUser
		case "assistant":
			entryRole = roleAssistant
		default:
			continue
		}

		entries = append(entries, chatEntry{
			Role:    entryRole,
			Content: msg.Content,
		})
	}

	return entries
}

// Init satisfies tea.Model. There's nothing to load asynchronously at
// startup — config was already loaded before the program started — so
// this only starts the input cursor blinking.
func (m model) Init() tea.Cmd {
	return textinput.Blink
}

// Update satisfies tea.Model.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.layout()
		return m, nil

	case tea.KeyMsg:
		// A pending permission decision outranks every other modal:
		// a blocked action must never be bypassable by opening pickers.
		if m.perm != nil {
			return m.handlePermKey(msg)
		}
		if m.flow != nil {
			return m.handleFlowKey(msg)
		}
		if m.addFlow != nil {
			return m.handleAddModelKey(msg)
		}
		if m.picker != nil {
			return m.handlePickerKey(msg)
		}
		if m.selectPicker != nil {
			return m.handleSelectPickerKey(msg)
		}
		return m.handleKey(msg)

	case permAskMsg:
		// A tool call needs explicit confirmation (M13). Record the
		// request in the transcript so /copy captures it, then show the
		// compact decision modal.
		m.appendActivity("Permission required: " + msg.req.Summary)
		m.perm = newPermPrompt(msg.req)
		m.refreshTranscript()
		return m, nil

	case connectResultMsg:
		return m.handleConnectResult(msg)

	case addModelResultMsg:
		return m.handleAddModelResult(msg)

	case streamEventMsg:
		switch msg.Event.Type {
		case runtime.EventText:
			if msg.Event.Text == "" {
				return m, waitForChunk(m.stream)
			}
			m.status = ""
			m.appendAssistantText(msg.Event.Text)
			m.assistantBuffer += msg.Event.Text
		case runtime.EventThinking:
			// Provider thinking content remains available to other runtime
			// consumers. The terminal presents a concise, transient status.
			if msg.Event.Thinking == "" {
				m.status = "Thinking"
			}
		case runtime.EventToolStart:
			m.finishAssistantStream()
			m.status = formatToolStart(msg.Event.ToolCall)
		case runtime.EventToolFinish:
			m.status = ""
			m.appendActivity(formatToolFinish(msg.Event.ToolResult))
		case runtime.EventMemory:
			m.appendActivity(fmt.Sprintf("Memory: %d relevant project fact(s)", msg.Event.Count))
		}

		m.refreshTranscript()
		return m, waitForChunk(m.stream)

	case streamDoneMsg:
		m.waiting = false
		m.stream = nil
		m.status = ""
		m.finishAssistantStream()
		if text := strings.TrimSpace(m.assistantBuffer); text != "" {
			m.session.AddMessage("assistant", text)
			_ = m.session.Save()
		}

		m.assistantBuffer = ""
		m.refreshTranscript()
		return m, nil

	case streamErrMsg:
		m.waiting = false
		m.status = ""
		m.finishAssistantStream()

		m.entries = append(m.entries, chatEntry{
			Role:    roleError,
			Content: withActionHint(msg.err),
		})

		m.stream = nil

		m.refreshTranscript()

		return m, nil

	case spinner.TickMsg:
		if !m.waiting {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	// Anything not handled above (mouse events, cursor-blink ticks, etc.)
	// is forwarded to the viewport and input so their own internal
	// animations and scrolling keep working.
	var cmds []tea.Cmd

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	var inputCmd tea.Cmd
	m.input, inputCmd = m.input.Update(msg)
	cmds = append(cmds, inputCmd)

	return m, tea.Batch(cmds...)
}

func (m *model) appendAssistantText(text string) {
	if len(m.entries) == 0 || m.entries[len(m.entries)-1].Role != roleAssistant {
		m.entries = append(m.entries, chatEntry{Role: roleAssistant, Streaming: true})
	}
	m.entries[len(m.entries)-1].Content += text
}

func (m *model) finishAssistantStream() {
	for i := len(m.entries) - 1; i >= 0; i-- {
		if m.entries[i].Role == roleAssistant && m.entries[i].Streaming {
			m.entries[i].Streaming = false
			return
		}
	}
}

func (m *model) appendActivity(text string) {
	if text == "" {
		return
	}
	m.entries = append(m.entries, chatEntry{Role: roleActivity, Content: text})
}

// handleKey processes keyboard input: global shortcuts first, then
// message submission, then falls back to normal text-input editing.
func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Copy shortcuts. Alt+C works everywhere; Ctrl+Shift+C is honored
	// only when the terminal reports it as a distinct key (kitty-style
	// enhanced keyboards) — on legacy terminals it never reaches the
	// application, and the /copy command always works.
	switch msg.String() {
	case "alt+c", "ctrl+shift+c":
		return m.copyLatestToClipboard()
	}

	// Slash-command palette (M16). It intercepts only list-navigation
	// and acceptance keys while engaged; Ctrl+C, Esc-to-quit semantics,
	// copy shortcuts, and ordinary typing are untouched.
	if m.palette != nil && m.palette.engaged() {
		switch msg.String() {
		case "up":
			m.palette.moveUp()
			return m, nil
		case "down":
			m.palette.moveDown()
			return m, nil
		case "tab":
			return m.acceptPalette(false)
		case "enter":
			return m.acceptPalette(true)
		case "esc":
			m.palette.close()
			return m, nil
		}
	}

	switch msg.Type {

	case tea.KeyCtrlC, tea.KeyEsc:
		m.quitting = true
		return m, tea.Quit

	case tea.KeyEnter:
		if m.waiting {
			// A response is already in flight; ignore extra submits
			// instead of queuing or dropping the in-progress request.
			return m, nil
		}
		return m.submitInput()
	}

	var cmd tea.Cmd
	before := m.input.Value()
	m.input, cmd = m.input.Update(msg)

	// Any edit to the input re-evaluates the palette: "/" opens it,
	// a space or clearing the line closes it, backspace keeps filtering.
	if m.input.Value() != before {
		m.syncPalette()
	}
	return m, cmd
}

// syncPalette refreshes the autocomplete state from the raw input.
func (m *model) syncPalette() {
	if m.palette == nil {
		return
	}
	m.palette.sync(m.input.Value())
}

// acceptPalette completes the highlighted suggestion into the input.
// With submit=true the completed command line goes through the exact
// same dispatcher as hand-typed input; nothing bypasses command
// execution semantics. A bare "/" never auto-accepts: with no direction
// from the user, running the first suggestion (e.g. /exit) would be a
// surprise.
func (m model) acceptPalette(submit bool) (tea.Model, tea.Cmd) {
	if strings.TrimSpace(m.input.Value()) == "/" {
		return m, nil
	}
	line := m.palette.accept()
	if line == "" {
		return m, nil
	}
	m.input.SetValue(line)
	if !submit {
		return m, nil // Tab: fill only, let the user keep typing arguments
	}
	return m.submitInput()
}

// submitInput sends whatever is currently in the input box through the
// existing pipeline: slash commands via the registry dispatcher,
// everything else as chat.
func (m model) submitInput() (tea.Model, tea.Cmd) {
	task := strings.TrimSpace(m.input.Value())
	if task == "" {
		return m, nil
	}
	m.input.Reset()
	m.syncPalette() // palette closes once the token leaves the input

	if isCommand, err := command.Dispatch(&m, m.registry, task); isCommand {
		if err != nil {
			m.entries = append(m.entries, chatEntry{Role: roleError, Content: err.Error()})
		}
		m.refreshTranscript()
		if m.quitting {
			return m, tea.Quit
		}
		if m.pendingStream != nil {
			stream := m.pendingStream
			m.pendingStream = nil
			m.stream = stream
			return m, tea.Batch(m.spinner.Tick, waitForChunk(stream))
		}
		return m, nil
	}

	m.entries = append(m.entries, chatEntry{Role: roleUser, Content: task})
	m.session.AddMessage("user", task)

	if err := m.session.Save(); err != nil {
		m.entries = append(m.entries, chatEntry{
			Role:    roleError,
			Content: fmt.Sprintf("failed to save session: %v", err),
		})
	}

	m.waiting = true
	m.refreshTranscript()

	stream, err := m.runtime.Stream(
		context.Background(),
		m.session.ProviderMessages(),
	)
	if err != nil {
		m.waiting = false
		m.entries = append(m.entries, chatEntry{Role: roleError, Content: withActionHint(err)})
		m.refreshTranscript()
		return m, nil
	}

	m.stream = stream

	return m, tea.Batch(
		m.spinner.Tick,
		waitForChunk(stream),
	)
}

// copyLatestToClipboard implements the Alt+C / Ctrl+Shift+C shortcut:
// copy the latest response and report in the transcript, mirroring the
// /copy command without going through command dispatch.
func (m model) copyLatestToClipboard() (tea.Model, tea.Cmd) {
	text := m.LatestResponse()
	if text == "" {
		m.entries = append(m.entries, chatEntry{
			Role:    roleSystem,
			Content: "Nothing to copy yet — ask Lato something first.",
		})
		m.refreshTranscript()
		return m, nil
	}
	if err := m.WriteToClipboard(text); err != nil {
		m.entries = append(m.entries, chatEntry{Role: roleError, Content: "copy failed: " + err.Error()})
	} else {
		m.entries = append(m.entries, chatEntry{
			Role:    roleSystem,
			Content: "✓ Copied the latest response (" + itoa(len(text)) + " characters) to the clipboard.",
		})
	}
	m.refreshTranscript()
	return m, nil
}

func itoa(n int) string { return strconv.Itoa(n) }

// handleFlowKey routes keys into the /connect wizard and clears it
// when the wizard reports completion or cancellation.
func (m model) handleFlowKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	active, cmd := m.flow.handleKey(&m, msg)
	if !active {
		m.flow = nil
	}
	return m, cmd
}

// handleAddModelKey routes keys into the /model add wizard.
func (m model) handleAddModelKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	active, cmd := m.addFlow.handleKey(&m, msg)
	if !active {
		m.addFlow = nil
	}
	return m, cmd
}

// handleAddModelResult finishes /model add: confirm in the transcript
// (never echoing credentials — none are involved here) and reopen the
// grouped model picker so the new entry is immediately visible.
func (m model) handleAddModelResult(msg addModelResultMsg) (tea.Model, tea.Cmd) {
	m.addFlow = nil
	if msg.err != nil {
		m.Println("⚠ %s", msg.err)
		m.refreshTranscript()
		return m, nil
	}
	m.Println("✓ Added %q to %s. Open /model to select it.", msg.modelID, providers.DisplayName(msg.providerID))
	m.openModelPickerFor(m.providerName)
	m.refreshTranscript()
	return m, nil
}

// handleConnectResult finishes the /connect wizard: report the outcome
// (keys redacted), switch to the provider on success, and offer model
// selection. A failed custom-provider validation falls back to a
// manual model-ID prompt instead of discarding the entered values.
func (m model) handleConnectResult(msg connectResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.Println("⚠ %s", msg.err)
		if msg.conn.Custom && !msg.unvalidated && m.flow != nil {
			m.flow.offerManualModel()
			return m, textinput.Blink
		}
		m.flow = nil
		m.refreshTranscript()
		return m, nil
	}

	m.flow = nil
	name := msg.conn.Name
	if name == "" {
		name = msg.conn.ID
	}
	note := ""
	if msg.unvalidated {
		note = " (saved without validation)"
	}
	m.Println("✓ Connected %s — %d model(s) available%s.", name, msg.models, note)

	if err := m.SetProvider(msg.conn.ID); err != nil {
		m.Println("⚠ could not switch to %s: %v", msg.conn.ID, err)
		m.refreshTranscript()
		return m, nil
	}
	m.openModelPickerFor(msg.conn.ID)
	m.refreshTranscript()
	return m, nil
}

// handlePickerKey processes keys while the /sessions modal is open. It
// never touches the runtime or transcript directly except on Enter,
// where it hands off to switchToSession.
func (m model) handlePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp:
		m.picker.moveUp()
		return m, nil

	case tea.KeyDown:
		m.picker.moveDown()
		return m, nil

	case tea.KeyEsc:
		m.picker = nil
		return m, nil

	case tea.KeyEnter:
		selected := m.picker.selected()
		m.picker = nil
		return m.switchToSession(selected.ID)
	}

	switch msg.String() {
	case "k":
		m.picker.moveUp()
	case "j":
		m.picker.moveDown()
	case "q":
		m.picker = nil
	}
	return m, nil
}

// handleSelectPickerKey processes keys while the /provider or /model
// modal is open. Model pickers carry the M16 effort ladder: ←/→ walk
// it, Enter applies model + effort (persisted), and 's' applies them
// to this session only.
func (m model) handleSelectPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.selectPicker.effortEnabled {
		switch msg.Type {
		case tea.KeyLeft:
			m.selectPicker.moveEffortLeft()
			return m, nil
		case tea.KeyRight:
			m.selectPicker.moveEffortRight()
			return m, nil
		}
		switch msg.String() {
		case "h":
			m.selectPicker.moveEffortLeft()
			return m, nil
		case "l", "s":
			if msg.String() == "l" {
				m.selectPicker.moveEffortRight()
				return m, nil
			}
			// 's': session-only application of the current selection.
			opt := m.selectPicker.selected()
			lvl := m.selectPicker.selectedEffort()
			m.selectPicker = nil
			return m.applyModelChoice(opt.ID, lvl, false)
		}
	}

	switch msg.Type {
	case tea.KeyUp:
		m.selectPicker.moveUp()
		return m, nil

	case tea.KeyDown:
		m.selectPicker.moveDown()
		return m, nil

	case tea.KeyPgUp:
		m.selectPicker.pageUp()
		return m, nil

	case tea.KeyPgDown:
		m.selectPicker.pageDown()
		return m, nil

	case tea.KeyHome:
		m.selectPicker.home()
		return m, nil

	case tea.KeyEnd:
		m.selectPicker.end()
		return m, nil

	case tea.KeyEsc:
		m.selectPicker = nil
		return m, nil

	case tea.KeyEnter:
		opt := m.selectPicker.selected()
		scope := m.selectPicker.scope
		var lvl effort.Level
		if scope == scopeModel {
			lvl = m.selectPicker.selectedEffort()
		}
		m.selectPicker = nil
		if scope == scopeProvider {
			return m.chooseProvider(opt.ID)
		}
		return m.applyModelChoice(opt.ID, lvl, true)
	}

	switch msg.String() {
	case "k":
		m.selectPicker.moveUp()
	case "j":
		m.selectPicker.moveDown()
	case "q":
		m.selectPicker = nil
	}
	return m, nil
}

// switchToSession loads the session with id from disk and replaces the
// active in-memory session, transcript, and viewport in place. It never
// spawns a new runtime or restarts the program: the same runtime keeps
// running, just pointed at different conversation history from here on.
func (m model) switchToSession(id string) (tea.Model, tea.Cmd) {
	if m.session != nil && id == m.session.ID {
		return m, nil // already the active session; nothing to do
	}

	sess, err := session.Load(id)
	if err != nil {
		m.entries = append(m.entries, chatEntry{
			Role:    roleError,
			Content: fmt.Sprintf("failed to load session: %v", err),
		})
		m.refreshTranscript()
		return m, nil
	}

	// A stream from the previous session is no longer relevant once we've
	// switched conversations; drop it rather than let it keep appending
	// to the new transcript.
	m.stream = nil
	m.waiting = false
	m.status = ""
	m.assistantBuffer = ""

	m.session = sess
	m.entries = sessionEntries(sess)
	m.refreshTranscript()

	return m, nil
}

// layout recomputes the viewport and input widths/heights after a resize.
// While the slash palette is engaged its rows are subtracted from the
// transcript area so the whole UI always fits the terminal exactly.
func (m *model) layout() {
	transcriptHeight := m.height - headerHeight - footerHeight - m.paletteExtraLines()
	if transcriptHeight < minTranscriptHeight {
		transcriptHeight = minTranscriptHeight
	}

	m.viewport.Width = m.width
	m.viewport.Height = transcriptHeight

	// Account for the input box's rounded border + horizontal padding.
	inputWidth := m.width - 4
	if inputWidth < 1 {
		inputWidth = 1
	}
	m.input.Width = inputWidth

	m.refreshTranscript()
}

// paletteExtraLines reports how many terminal rows the engaged palette
// strip currently occupies (0 when hidden).
func (m *model) paletteExtraLines() int {
	if m.palette == nil {
		return 0
	}
	pv := m.palette.view(m.width)
	if pv == "" {
		return 0
	}
	return strings.Count(pv, "\n") + 1
}

// refreshTranscript re-renders all entries into the viewport. Once there's
// a conversation, it scrolls to the bottom so the newest message is
// visible; while empty (showing the LATO splash), it stays at the
// top so the whole banner is visible instead of its lower half.
func (m *model) refreshTranscript() {
	if m.viewport.Width == 0 {
		return // not sized yet; layout() will call this again once it is
	}
	m.viewport.SetContent(renderTranscript(m.entries, m.viewport.Width))
	if len(m.entries) == 0 {
		m.viewport.GotoTop()
	} else {
		m.viewport.GotoBottom()
	}
}

// View satisfies tea.Model.
func (m model) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "Starting Lato…\n"
	}

	// A permission decision blocks the whole run; it takes over the
	// screen until answered, like the other modals.
	if m.perm != nil {
		return m.perm.view(m.width, m.height)
	}

	if m.picker != nil {
		return m.picker.view(m.width, m.height)
	}
	if m.flow != nil {
		if m.flow.selectPicker != nil {
			return m.flow.selectPicker.view(m.width, m.height)
		}
		if m.flow.input != nil {
			return m.flow.input.view(m.width, m.height)
		}
		return lipgloss.JoinVertical(
			lipgloss.Left,
			m.renderHeader(),
			m.viewport.View(),
			m.renderFooter(),
		)
	}
	if m.addFlow != nil {
		if m.addFlow.selectPicker != nil {
			return m.addFlow.selectPicker.view(m.width, m.height)
		}
		if m.addFlow.input != nil {
			return m.addFlow.input.view(m.width, m.height)
		}
	}
	if m.selectPicker != nil {
		return m.selectPicker.view(m.width, m.height)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.renderHeader(),
		m.viewport.View(),
		m.renderFooterWithPalette(),
	)
}

// renderFooterWithPalette places the slash-command suggestion strip
// between the transcript and the input box while it is engaged, and
// falls back to the plain footer otherwise.
func (m model) renderFooterWithPalette() string {
	if m.palette == nil {
		return m.renderFooter()
	}
	pv := m.palette.view(m.width)
	if pv == "" {
		return m.renderFooter()
	}
	return lipgloss.JoinVertical(lipgloss.Left, pv, m.renderFooter())
}

func (m model) renderHeader() string {
	title := headerStyle.Render(" Lato ")
	meta := headerMetaStyle.Render(
		fmt.Sprintf("agent: %s  ·  %s/%s · %s", m.agentName, m.providerName, m.modelName, m.effortName),
	)
	return lipgloss.JoinHorizontal(lipgloss.Top, title, " ", meta)
}

func (m model) renderFooter() string {
	inputBox := inputBorderStyle.Width(m.width - 2).Render(m.input.View())

	status := "enter send · esc quit"
	if m.waiting {
		activity := m.status
		if activity == "" {
			activity = "Working"
		}
		status = fmt.Sprintf("%s %s", m.spinner.View(), activity)
	}

	return lipgloss.JoinVertical(lipgloss.Left, inputBox, helpStyle.Render(status))
}
