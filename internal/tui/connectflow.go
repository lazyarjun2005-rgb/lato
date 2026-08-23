// The /connect flow: an interactive provider-connection wizard built
// from the existing picker plus a small masked-input modal. It gathers
// endpoint/key material per provider, validates with a lightweight
// model discovery call, saves the connection to the user-level store,
// and hands off to model selection. API keys are never echoed to the
// transcript; only their redacted form is ever displayed.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"lato/internal/providers"
	"lato/internal/userconfig"
)

// customProviderOption is the extra row in the /connect picker.
const customProviderOptionID = "__custom__"

// inputStep is one prompt in the connection wizard.
type inputStep struct {
	title   string
	prompt  string
	initial string // pre-filled default (e.g. registry endpoint)
	masked  bool   // true for API keys: input echoes dots only
	apply   func(value string)
}

// connectFlow drives the wizard's step machine. It reuses selectPicker
// for the provider/candidate choice and inputModal for text entry.
type connectFlow struct {
	importing   bool // seeded from OpenCode/Claude candidates, not the registry
	unvalidated bool // saving a custom provider without probing (manual model)

	selectPicker *selectPicker // non-nil while choosing a provider/candidate
	input        *inputModal   // non-nil while gathering one value
	steps        []inputStep

	pending     userconfig.Connection   // assembled across steps
	candidates  []userconfig.Connection // import sources
	manualModel string                  // custom-provider fallback model ID
}

// newConnectFlow builds the wizard over every registered provider plus
// the custom option.
func newConnectFlow() *connectFlow {
	options := make([]selectOption, 0, len(providers.Registry)+1)
	for _, p := range providers.Registry {
		options = append(options, selectOption{
			Label: fmt.Sprintf("%s — %s", p.Name, firstSentence(p.Description)),
			ID:    p.ID,
		})
	}
	options = append(options, selectOption{Label: "Other / Custom Provider", ID: customProviderOptionID})
	return &connectFlow{
		selectPicker: &selectPicker{title: "Connect Provider", options: options},
	}
}

// newImportFlow seeds the wizard with connections detected from OpenCode
// or Claude Code configurations instead of the static registry. Nothing
// is saved until the user picks an entry and validation succeeds.
func newImportFlow(candidates []userconfig.Connection) *connectFlow {
	f := &connectFlow{importing: true, candidates: candidates}
	options := make([]selectOption, 0, len(candidates))
	for _, c := range candidates {
		options = append(options, selectOption{
			Label: fmt.Sprintf("%s → %s (key %s)", c.Name, c.Endpoint, userconfig.Redact(c.APIKey)),
			ID:    c.ID,
		})
	}
	f.selectPicker = &selectPicker{title: "Import Provider Connection", options: options}
	return f
}

func firstSentence(s string) string {
	if i := strings.IndexByte(s, '.'); i > 0 {
		s = s[:i]
	}
	return s
}

// handleKey processes one key press while the flow is active. ok=false
// means the flow finished (or was cancelled) and should be cleared.
func (f *connectFlow) handleKey(m *model, msg tea.KeyMsg) (bool, tea.Cmd) {
	if f.selectPicker != nil {
		switch msg.Type {
		case tea.KeyUp:
			f.selectPicker.moveUp()
			return true, nil
		case tea.KeyDown:
			f.selectPicker.moveDown()
			return true, nil
		case tea.KeyPgUp:
			f.selectPicker.pageUp()
			return true, nil
		case tea.KeyPgDown:
			f.selectPicker.pageDown()
			return true, nil
		case tea.KeyHome:
			f.selectPicker.home()
			return true, nil
		case tea.KeyEnd:
			f.selectPicker.end()
			return true, nil
		case tea.KeyEsc:
			return false, nil
		case tea.KeyEnter:
			opt := f.selectPicker.selected()
			f.selectPicker = nil
			if !f.beginProvider(opt.ID) {
				return true, f.validateCmd(m) // imports validate immediately
			}
			return true, textinput.Blink
		}
		return true, nil
	}

	if f.input != nil {
		switch msg.Type {
		case tea.KeyEsc:
			return false, nil
		case tea.KeyEnter:
			value := f.input.Value()
			step := f.steps[0]
			step.apply(strings.TrimSpace(value))
			f.steps = f.steps[1:]
			f.input = nil
			if len(f.steps) == 0 {
				if f.unvalidated {
					if f.manualModel == "" {
						return false, nil // empty model ID cancels the manual save
					}
					return false, f.saveUnvalidatedCmd(m)
				}
				return false, f.validateCmd(m)
			}
			f.input = newInputModal(f.steps[0])
			return true, textinput.Blink
		}
		f.input.Update(msg)
		return true, nil
	}

	return false, nil
}

// beginProvider queues the input prompts appropriate for the chosen
// provider and returns whether any inputs are needed. Imports need
// none: their configuration was already read from disk.
func (f *connectFlow) beginProvider(id string) bool {
	if f.importing {
		for _, c := range f.candidates {
			if c.ID == id {
				f.pending = c
				break
			}
		}
		return false
	}

	if id == customProviderOptionID {
		f.pending = userconfig.Connection{Custom: true, Class: providers.ClassOpenAICompatible}
		f.steps = []inputStep{
			{
				title: "Custom provider", prompt: "Provider name:",
				apply: func(v string) { f.pending.Name = v },
			},
			{
				title: "Custom provider", prompt: "Base URL:", initial: "http://localhost:1234/v1",
				apply: func(v string) { f.pending.Endpoint = strings.TrimRight(v, "/") },
			},
			{
				title: "Custom provider", prompt: "API key (optional):", masked: true,
				apply: func(v string) { f.pending.APIKey = v },
			},
		}
	} else {
		info, ok := providers.ByID(id)
		if !ok {
			return false
		}
		// The registry endpoint is the starting point for every
		// registered provider. Providers that prompt for a base URL
		// overwrite it below; key-only hosted providers (OpenRouter,
		// NVIDIA) keep it automatically.
		f.pending = userconfig.Connection{ID: id, Name: info.Name, Class: info.Class, Endpoint: info.Endpoint}
		var steps []inputStep
		if wantsEndpointPrompt(id) {
			steps = append(steps, inputStep{
				title: info.Name, prompt: "Base URL:", initial: info.Endpoint,
				apply: func(v string) { f.pending.Endpoint = strings.TrimRight(v, "/") },
			})
		}
		switch {
		case info.RequiresAPIKey():
			steps = append(steps, inputStep{
				title: info.Name, prompt: "API key:", masked: true,
				apply: func(v string) { f.pending.APIKey = v },
			})
		case isConfigurableRouter(id):
			// 9Router/OmniRoute installs often run unauthenticated.
			steps = append(steps, inputStep{
				title: info.Name, prompt: "API key (optional for local):", masked: true,
				apply: func(v string) { f.pending.APIKey = v },
			})
		}
		f.steps = steps
	}

	if len(f.steps) > 0 {
		f.input = newInputModal(f.steps[0])
		return true
	}
	return false
}

// wantsEndpointPrompt reports whether connecting this provider should
// ask for its base URL. Fixed cloud endpoints (OpenRouter, NVIDIA NIM)
// don't; everything else does, so local installs and self-hosted
// routers can be pointed anywhere. Key-only providers still get the
// registry endpoint seeded automatically.
func wantsEndpointPrompt(id string) bool {
	switch id {
	case "openrouter", "nvidia":
		return false
	default:
		return true
	}
}

// isConfigurableRouter reports whether the provider is a router whose
// local installs commonly run without authentication.
func isConfigurableRouter(id string) bool {
	return id == "9router" || id == "omniroute"
}

// finalize assigns derived fields before saving.
func (f *connectFlow) finalize() userconfig.Connection {
	conn := f.pending
	if conn.Custom && conn.ID == "" {
		conn.ID = userconfig.CustomNameToID(conn.Name)
	}
	conn.Endpoint = strings.TrimRight(conn.Endpoint, "/")
	return conn
}

// validateCmd runs the connection attempt asynchronously so the UI
// stays responsive during network probing.
func (f *connectFlow) validateCmd(m *model) tea.Cmd {
	conn := f.finalize()
	rt := m.runtime
	return func() tea.Msg {
		models, err := rt.ConnectProvider(conn)
		return connectResultMsg{conn: conn, models: models, err: err}
	}
}

// saveUnvalidatedCmd stores a custom provider whose endpoint could not
// be probed but for which the user supplied a model ID manually. A
// later /model refresh can still discover models once the endpoint
// works.
func (f *connectFlow) saveUnvalidatedCmd(m *model) tea.Cmd {
	conn := f.finalize()
	if f.manualModel != "" {
		conn.Models = []userconfig.Model{{ID: f.manualModel, Name: f.manualModel}}
	}
	rt := m.runtime
	return func() tea.Msg {
		err := rt.SaveUnvalidatedConnection(conn)
		return connectResultMsg{conn: conn, models: len(conn.Models), err: err, unvalidated: true}
	}
}

// offerManualModel converts a failed custom-provider validation into a
// manual model-ID prompt instead of discarding the entered values.
func (f *connectFlow) offerManualModel() {
	f.unvalidated = true
	f.steps = []inputStep{{
		title:  "Custom provider",
		prompt: "Model ID (saved without validation, empty cancels):",
		apply:  func(v string) { f.manualModel = v },
	}}
	f.input = newInputModal(f.steps[0])
}

// connectResultMsg carries the outcome of an asynchronous connection
// attempt back into the TUI.
type connectResultMsg struct {
	conn        userconfig.Connection
	models      int
	err         error
	unvalidated bool
}

// addModelFlow registers a user-supplied model ID under an already
// connected provider: pick the provider, type the exact model ID, give
// an optional display name. The ID is stored and later sent to the
// provider verbatim.
type addModelFlow struct {
	selectPicker *selectPicker // choose among connected providers
	input        *inputModal   // current prompt
	steps        []inputStep

	providerID string
	modelID    string
	name       string
}

// newAddModelFlow builds the wizard over every connected provider.
func newAddModelFlow(conns []userconfig.Connection) *addModelFlow {
	options := make([]selectOption, 0, len(conns))
	for _, c := range conns {
		options = append(options, selectOption{Label: c.Name, ID: c.ID})
	}
	return &addModelFlow{
		selectPicker: &selectPicker{title: "Add model to provider", options: options},
	}
}

// handleKey processes one key while the flow is active; ok=false means
// it finished or was cancelled.
func (f *addModelFlow) handleKey(m *model, msg tea.KeyMsg) (bool, tea.Cmd) {
	if f.selectPicker != nil {
		switch msg.Type {
		case tea.KeyUp:
			f.selectPicker.moveUp()
			return true, nil
		case tea.KeyDown:
			f.selectPicker.moveDown()
			return true, nil
		case tea.KeyPgUp:
			f.selectPicker.pageUp()
			return true, nil
		case tea.KeyPgDown:
			f.selectPicker.pageDown()
			return true, nil
		case tea.KeyEsc:
			return false, nil
		case tea.KeyEnter:
			f.providerID = f.selectPicker.selected().ID
			f.selectPicker = nil
			f.steps = f.inputSteps()
			f.input = newInputModal(f.steps[0])
			return true, textinput.Blink
		}
		return true, nil
	}

	if f.input != nil {
		switch msg.Type {
		case tea.KeyEsc:
			return false, nil
		case tea.KeyEnter:
			value := strings.TrimSpace(f.input.Value())
			f.steps[0].apply(value)
			f.steps = f.steps[1:]
			f.input = nil
			if len(f.steps) == 0 {
				return false, f.saveCmd(m)
			}
			f.input = newInputModal(f.steps[0])
			return true, textinput.Blink
		}
		f.input.Update(msg)
		return true, nil
	}

	return false, nil
}

func (f *addModelFlow) inputSteps() []inputStep {
	return []inputStep{
		{
			title: "Add model", prompt: "Model ID (sent exactly as typed):",
			apply: func(v string) { f.modelID = v },
		},
		{
			title: "Add model", prompt: "Display name (optional):",
			apply: func(v string) { f.name = v },
		},
	}
}

// registration returns the assembled registration values.
func (f *addModelFlow) registration() (providerID, modelID, name string) {
	return f.providerID, f.modelID, f.name
}

// saveCmd persists the registration without any network probing.
func (f *addModelFlow) saveCmd(m *model) tea.Cmd {
	id, modelID, name := f.registration()
	rt := m.runtime
	return func() tea.Msg {
		err := rt.RegisterModel(id, modelID, name)
		return addModelResultMsg{providerID: id, modelID: modelID, err: err}
	}
}

// addModelResultMsg reports the registration outcome.
type addModelResultMsg struct {
	providerID string
	modelID    string
	err        error
}

// inputModal is a minimal single-line prompt modal. Masked inputs echo
// dots so keys never appear on screen.
type inputModal struct {
	title  string
	prompt string
	input  textinput.Model
}

func newInputModal(step inputStep) *inputModal {
	in := textinput.New()
	in.Placeholder = step.initial
	in.Prompt = step.prompt + " "
	in.CharLimit = 512
	if step.masked {
		in.EchoMode = textinput.EchoPassword
		in.EchoCharacter = '•'
	}
	in.Focus()
	return &inputModal{title: step.title, prompt: step.prompt, input: in}
}

// Value returns the typed value, falling back to the placeholder so a
// pre-filled default (base URLs) needs no retyping.
func (im *inputModal) Value() string {
	v := im.input.Value()
	if v == "" {
		return im.input.Placeholder
	}
	return v
}

// SetValue replaces the typed text (tests and programmatic defaults).
func (im *inputModal) SetValue(s string) { im.input.SetValue(s) }

// Update forwards key input to the underlying text field.
func (im *inputModal) Update(msg tea.Msg) {
	var cmd tea.Cmd
	im.input, cmd = im.input.Update(msg)
	_ = cmd
}

func (im *inputModal) view(width, height int) string {
	var b strings.Builder
	b.WriteString(pickerTitleStyle.Render(im.title))
	b.WriteString("\n\n")
	b.WriteString(inputBorderStyle.Width(pickerWidth - 2).Render(im.input.View()))
	b.WriteString("\n\n")
	b.WriteString(pickerHelpStyle.Render("enter confirm · esc cancel"))
	box := pickerBorderStyle.Width(pickerWidth).Render(strings.TrimRight(b.String(), "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
