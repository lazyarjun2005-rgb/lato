// Permission confirmation UI (M13). The uiAsker bridges the runtime's
// blocking Asker contract into the Bubble Tea event loop; the prompt
// itself is a compact modal that matches the existing picker styling.
// This package adds presentation only: what is allowed, for how long,
// and what happens on denial is decided entirely by the runtime and its
// permission policy.
package tui

import (
	"context"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"lato/internal/permissions"
	"lato/internal/runtime"
)

// uiAsker implements runtime.Asker against the running Bubble Tea
// program: it forwards the request as a message, then blocks until the
// user answers or the run context is canceled. Before bind() (or when
// no program is attached) it fails safe with a denial — a prompt that
// cannot be shown must never be treated as approval.
type uiAsker struct {
	mu      sync.Mutex
	program *tea.Program
}

func newUIAsker() *uiAsker { return &uiAsker{} }

// bind attaches the program once it exists. Safe to call concurrently;
// AskPermission may already be blocked waiting for earlier prompts.
func (a *uiAsker) bind(p *tea.Program) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.program = p
}

// AskPermission delivers req to the model and waits for an answer.
func (a *uiAsker) AskPermission(ctx context.Context, req runtime.PermissionRequest) runtime.PermissionChoice {
	a.mu.Lock()
	p := a.program
	a.mu.Unlock()
	if p == nil {
		return runtime.PermissionDeny // fail safe
	}

	p.Send(permAskMsg{req: &req})

	select {
	case choice := <-req.Answer():
		return choice
	case <-ctx.Done():
		req.Respond(runtime.PermissionDeny)
		return runtime.PermissionDeny
	}
}

// permAskMsg carries a pending permission request into Update.
type permAskMsg struct{ req *runtime.PermissionRequest }

// permPrompt is one visible confirmation. It owns nothing beyond
// display state; the decision is sent straight to the waiting asker.
type permPrompt struct {
	req *runtime.PermissionRequest
}

func newPermPrompt(req *runtime.PermissionRequest) *permPrompt {
	return &permPrompt{req: req}
}

// decide answers the request, reports the outcome in the transcript, and
// closes the modal.
func (m model) decide(c runtime.PermissionChoice) (tea.Model, tea.Cmd) {
	req := m.perm.req
	m.perm = nil

	switch c {
	case runtime.PermissionAllowOnce:
		m.appendActivity("✓ Approved once: " + req.Summary)
	case runtime.PermissionAllowTask:
		m.appendActivity("✓ Approved for this task: " + req.Summary)
	default:
		m.appendActivity("✗ Denied: " + req.Summary + "\nAction was NOT executed.")
	}
	req.Respond(c)
	m.refreshTranscript()
	return m, nil
}

// handlePermKey routes keys while a permission prompt is open. Esc and
// 'n' deny rather than quit: refusing must always be available.
func (m model) handlePermKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "1", "y", "a":
		return m.decide(runtime.PermissionAllowOnce)
	case "2", "t":
		return m.decide(runtime.PermissionAllowTask)
	case "3", "n", "d", "q", "esc":
		return m.decide(runtime.PermissionDeny)
	case "ctrl+c":
		next, cmd := m.decide(runtime.PermissionDeny)
		if mm, ok := next.(model); ok {
			mm.quitting = true
			return mm, tea.Batch(cmd, tea.Quit)
		}
		return next, tea.Quit
	}
	return m, nil // ignore everything else while deciding
}

// view renders the compact permission modal in the existing picker
// style. Everything shown was redacted by the classifier before it got
// here, so credential-shaped command content can never appear.
func (p *permPrompt) view(width, height int) string {
	var b strings.Builder
	b.WriteString(pickerTitleStyle.Render("Permission required"))
	b.WriteString("\n\n")

	classLine := classLabel(p.req.Class)
	if classLine != "" {
		b.WriteString(pickerMetaStyle.Render(classLine))
		b.WriteString("\n")
	}
	b.WriteString(pickerSelectedStyle.Width(pickerWidth - 4).Render(
		wrapText("Action:\n  "+p.req.Summary, pickerWidth-4)))
	if strings.TrimSpace(p.req.Reason) != "" && p.req.Reason != p.req.Summary {
		b.WriteString("\n")
		b.WriteString(pickerMetaStyle.Width(pickerWidth - 4).Render(
			wrapText("Reason:\n  "+p.req.Reason, pickerWidth-4)))
	}

	b.WriteString("\n\n")
	b.WriteString("[1] Allow once   [2] Allow for task   [3] Deny\n")
	b.WriteString(pickerHelpStyle.Render("1/y allow once · 2/t allow for task · 3/n/esc deny"))

	box := pickerBorderStyle.Width(pickerWidth).Render(strings.TrimRight(b.String(), "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// classLabel renders the risk class for the prompt header line.
func classLabel(c permissions.Class) string {
	switch c {
	case permissions.ClassHighRisk:
		return "Risk: destructive or outside the workspace"
	case permissions.ClassCommandExecution:
		return "Risk: command execution"
	case permissions.ClassWorkspaceWrite:
		return "Risk: workspace modification"
	case permissions.ClassReadOnly:
		return ""
	default:
		return ""
	}
}

// wrapText hard-wraps text to width so long commands stay readable in
// the modal. Plain-text only; no styling is embedded.
func wrapText(s string, width int) string {
	if width < 20 {
		width = 20
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			switch {
			case line == "":
				line = word
			case len(line)+1+len(word) <= width:
				line += " " + word
			default:
				out = append(out, line)
				line = "    " + word // continuation indent
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
