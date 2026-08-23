package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"lato/internal/command"
	"lato/internal/command/builtin"
	"lato/internal/effort"
	"lato/internal/index"
	"lato/internal/memory"
	"lato/internal/providers"
	"lato/internal/runtime"
	"lato/internal/session"
	"lato/internal/task"
	"lato/internal/userconfig"
	"lato/internal/workspace"
)

// newRegistry builds the set of slash commands available in the interactive chat.
func newRegistry() *command.Registry {
	reg := command.NewRegistry()
	reg.Register(builtin.NewExit())
	reg.Register(builtin.NewClear())
	reg.Register(builtin.NewModel())
	reg.Register(builtin.NewProvider())
	reg.Register(builtin.NewEffort())
	reg.Register(builtin.NewFast())
	reg.Register(builtin.NewConnect())
	reg.Register(builtin.NewImportCmd())
	reg.Register(builtin.NewCopy())
	reg.Register(builtin.NewExport())
	reg.Register(builtin.NewMemory())
	reg.Register(builtin.NewTask())
	reg.Register(builtin.NewPermissions())
	reg.Register(builtin.NewHelp(reg))
	reg.Register(builtin.NewSessions())
	reg.Register(builtin.NewRename())
	reg.Register(builtin.NewResume())
	reg.Register(builtin.NewRewind())
	reg.Register(builtin.NewWorkspace())
	reg.Register(builtin.NewIndex())
	reg.Register(builtin.NewVersion())
	reg.Register(builtin.NewStatus())
	reg.Register(builtin.NewDoctor())
	reg.Register(builtin.NewSkills())
	for _, c := range builtin.NewDevCommands() {
		reg.Register(c)
	}
	return reg
}

// The methods below satisfy command.Context, which is the only thing
// slash commands know about the session. They translate that narrow
// interface into real changes to the transcript and the runtime, so
// commands themselves never import Bubble Tea or lato/internal/runtime.

// Println appends a formatted system-style line to the transcript.
func (m *model) Println(format string, args ...any) {
	m.entries = append(m.entries, chatEntry{
		Role:    roleSystem,
		Content: fmt.Sprintf(format, args...),
	})
}

// Clear empties the transcript, e.g. for /clear.
func (m *model) Clear() {
	m.entries = nil
}

// Quit marks the session to end; handleKey turns this into a tea.Quit.
func (m *model) Quit() {
	m.quitting = true
}

// Workspace reports the workspace description discovered at startup.
func (m *model) Workspace() workspace.Info {
	return m.runtime.Workspace()
}

// Index reports the repository index, building it lazily on first use so
// /index never pays the walk cost unless the user asks for it.
func (m *model) Index() *index.Index {
	return m.runtime.Index()
}

// Model reports the currently active model name.
func (m *model) Model() string { return m.modelName }

// Provider reports the currently active provider name.
func (m *model) Provider() string { return m.providerName }

// CurrentEffort reports the active effort label.
func (m *model) CurrentEffort() string { return m.runtime.CurrentEffort() }

// SetEffort switches the effort level, updating the header label once
// the runtime accepts it.
func (m *model) SetEffort(level string, persist bool) error {
	if err := m.runtime.SetEffort(level, persist); err != nil {
		return err
	}
	m.effortName = m.runtime.CurrentEffort()
	return nil
}

// SetModel switches the runtime to a new model and, only once that
// succeeds, updates the label shown in the header.
func (m *model) SetModel(name string) error {
	if err := m.runtime.SetModel(name); err != nil {
		return err
	}
	m.modelName = name
	return nil
}

// SetProvider switches the runtime to a new provider and, only once that
// succeeds, updates the label shown in the header.
func (m *model) SetProvider(name string) error {
	if err := m.runtime.SetProvider(name); err != nil {
		return err
	}
	m.providerName = name
	return nil
}

// OpenSessionPicker opens the /sessions modal over sessions, which the
// caller already loaded from disk. It never reads sessions itself.
func (m *model) OpenSessionPicker(sessions []session.Session) {
	m.picker = newSessionPicker(sessions, m.session.ID)
}

// OpenProviderPicker opens the /provider modal.
func (m *model) OpenProviderPicker() {
	m.selectPicker = newProviderPicker(m.providerName)
}

// OpenConnectFlow starts the /connect wizard: choose a provider, enter
// its endpoint/key, validate, save.
func (m *model) OpenConnectFlow() {
	m.flow = newConnectFlow()
}

// OpenImportFlow starts the /connect import wizard over detected
// OpenCode and Claude Code gateway configurations.
func (m *model) OpenImportFlow() {
	candidates := m.runtime.DetectImportCandidates()
	if len(candidates) == 0 {
		m.Println("No importable provider configurations found (looked for opencode.json and Claude Code settings).")
		return
	}
	m.flow = newImportFlow(candidates)
}

// OpenAddModelFlow starts the /model add wizard over every connected
// provider, so dashboard-only model IDs can be registered by hand.
func (m *model) OpenAddModelFlow() {
	conns := m.runtime.Connections()
	if len(conns) == 0 {
		m.Println("No providers are connected yet. Run /connect first.")
		return
	}
	m.addFlow = newAddModelFlow(conns)
}

// --- project memory -----------------------------------------------------

// MemorySummary renders the project's stored memories as a compact
// listing, or "" when nothing is remembered yet.
func (m *model) MemorySummary() string {
	entries := m.runtime.Memory().List()
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%s [%s/%s] %s\n", e.ID, e.Kind, e.Category, e.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}

// RememberFact stores a user-provided durable fact.
func (m *model) RememberFact(text string) error {
	_, err := m.runtime.Memory().Add(text, "", memory.KindUser)
	return err
}

// ForgetMemory deletes one memory entry by ID or unique prefix.
func (m *model) ForgetMemory(id string) error { return m.runtime.Memory().Remove(id) }

// ClearMemory removes every memory for this project.
func (m *model) ClearMemory() error {
	_, err := m.runtime.Memory().Clear()
	return err
}

// and reports the outcome in the transcript. Keys never appear in the
// report.
// --- task continuity -----------------------------------------------------

// TaskList renders the project's known tasks, resumable ones first.
func (m *model) TaskList() string {
	all := m.runtime.TaskStore().All()
	if len(all) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Tasks:")
	for _, t := range all {
		fmt.Fprintf(&b, "\n%s — %s (%s)", shortID(t.ID), t.Title(), t.Status)
		if done, total := t.Progress(); total > 0 {
			fmt.Fprintf(&b, " · %d/%d", done, total)
		}
	}
	resumable := m.runtime.ResumableTasks()
	if len(resumable) > 0 {
		fmt.Fprintf(&b, "\n\nResume with /task resume %sor /resume-style requests.",
			map[bool]string{true: "", false: "<id> "}[len(resumable) == 1])
	}
	return b.String()
}

func shortID(id string) string {
	if len(id) > 6 {
		return id[:6]
	}
	return id
}

// resolveResumable applies the same selection rules as the runtime for
// user-facing resume commands, producing friendly errors.
func resolveResumable(rt *runtime.Runtime, id string) (task.Task, error) {
	rs := rt.ResumableTasks()
	switch {
	case len(rs) == 0:
		return task.Task{}, fmt.Errorf("no resumable task for this project")
	case id == "" && len(rs) == 1:
		return rs[0], nil
	case id == "":
		var b strings.Builder
		fmt.Fprintf(&b, "%d resumable tasks — choose one:", len(rs))
		for _, t := range rs {
			fmt.Fprintf(&b, "\n  /task resume %s — %s", shortID(t.ID), t.Title())
		}
		return task.Task{}, errors.New(b.String())
	default:
		for _, t := range rs {
			if t.ID == id || strings.HasPrefix(t.ID, id) {
				return t, nil
			}
		}
		return task.Task{}, fmt.Errorf("no resumable task matching %q", id)
	}
}

// resumeAnnouncement renders the compact state summary shown when a
// task is resumed, so the user sees exactly where work picks up. All
// fields come from the persisted task record; nothing is invented.
func resumeAnnouncement(t task.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Resuming task:\n  %s", t.Title())
	if done, total := t.Progress(); total > 0 {
		fmt.Fprintf(&b, "\nPrevious state:\n  %d/%d steps completed", done, total)
	}
	if t.LastAction != "" {
		fmt.Fprintf(&b, "\nLast action:\n  %s", t.LastAction)
	}
	next := ""
	if t.NextAction != "" {
		next = t.NextAction
	} else if step, ok := t.NextPending(); ok {
		next = step.Title
	}
	if next != "" {
		fmt.Fprintf(&b, "\nNext:\n  %s", next)
	}
	return b.String()
}

// ResumeTask validates the request and starts streaming the resumed
// task. Actual stream wiring happens in handleKey via pendingStream,
// because slash-command dispatch cannot return a tea.Cmd.
func (m *model) ResumeTask(idOrEmpty string) error {
	chosen, err := resolveResumable(m.runtime, idOrEmpty)
	if err != nil {
		return err
	}

	m.Println("%s", resumeAnnouncement(chosen))

	marker := fmt.Sprintf("(resuming task %s: %s)", shortID(chosen.ID), chosen.Title())
	m.entries = append(m.entries, chatEntry{Role: roleUser, Content: marker})
	if m.session != nil {
		m.session.AddMessage("user", "continue task "+chosen.ID)
		_ = m.session.Save()
	}

	stream, err := m.runtime.ResumeStream(context.Background(), chosen.ID)
	if err != nil {
		m.waiting = false
		return err
	}
	m.pendingStream = stream
	m.waiting = true
	m.refreshTranscript()
	return nil
}

// AbandonTask retires one task without touching repository files.
func (m *model) AbandonTask(id string) error {
	if _, err := m.runtime.TaskStore().Get(id); err != nil {
		return err
	}
	return m.runtime.TaskStore().SetStatus(id, task.StatusAbandoned)
}

// --- skills ----------------------------------------------------------------

// SkillsSummary renders the discovered skill catalog as a compact
// listing, or "" when no skill files exist. Bodies are not shown: they
// load on demand through the agent's load_skill tool.
func (m *model) SkillsSummary() string {
	catalog := m.runtime.SkillCatalog()
	if len(catalog) == 0 {
		return ""
	}
	var b strings.Builder
	for _, s := range catalog {
		fmt.Fprintf(&b, "%s — %s", s.ID, s.Name)
		if s.Description != "" {
			fmt.Fprintf(&b, ": %s", s.Description)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// --- prompt submission ------------------------------------------------------

// SubmitPrompt sends prompt into the existing agent loop exactly as if
// the user had typed it: it becomes a real user turn in the transcript
// and the persisted session, and the answer streams through the same
// runtime path as normal chat. Commands never see the stream itself;
// submitInput promotes pendingStream into the live pipeline after
// dispatch returns, so all loop behavior stays in one place.
func (m *model) SubmitPrompt(prompt string) error {
	if strings.TrimSpace(prompt) == "" {
		return errors.New("cannot submit an empty prompt")
	}
	if m.session == nil {
		return errors.New("no active session")
	}

	m.entries = append(m.entries, chatEntry{Role: roleUser, Content: prompt})
	m.session.AddMessage("user", prompt)
	if err := m.session.Save(); err != nil {
		m.entries = append(m.entries, chatEntry{
			Role:    roleError,
			Content: fmt.Sprintf("failed to save session: %v", err),
		})
	}

	stream, err := m.runtime.Stream(context.Background(), m.session.ProviderMessages())
	if err != nil {
		return err
	}

	m.pendingStream = stream
	m.waiting = true
	m.refreshTranscript()
	return nil
}

// --- session identity -------------------------------------------------------

// RenameSession gives the active session a persistent human-readable
// title and saves it immediately, so /sessions and future launches see
// the new name. The transcript is not modified: the confirmation is the
// command's own Println output.
func (m *model) RenameSession(title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("session title cannot be empty")
	}
	if m.session == nil {
		return errors.New("no active session")
	}
	m.session.Rename(title)
	if err := m.session.Save(); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

// ResumeSession resolves idOrTitle against saved sessions — exact ID,
// unique ID prefix, or exact Title, never fuzzy and never guessing —
// and switches to it through the same core the /sessions picker uses.
// Resolution is read-only: nothing is renamed or rewritten here.
func (m *model) ResumeSession(idOrTitle string) error {
	sessions, err := session.List()
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	chosen, err := session.ResolveSession(idOrTitle, sessions)
	if err != nil {
		return err
	}
	return m.applySessionSwitch(chosen.ID)
}

// RewindConversation removes the most recent conversation turns and
// persists the result before anything is finalized:
//
//  1. refuse while a stream is active (history must never shift under
//     a running request);
//  2. snapshot the live state;
//  3. let session.Rewind validate and mutate (its validation errors
//     leave Messages untouched);
//  4. persist — on failure the snapshot is restored, so memory and
//     disk never diverge and no false success can be reported;
//  5. only then rebuild the transcript from the persisted source of
//     truth, exactly like a session switch.
func (m *model) RewindConversation(turns int) (int, error) {
	if m.waiting {
		return 0, errors.New("cannot rewind conversation while Lato is busy")
	}
	if m.session == nil {
		return 0, errors.New("no active session")
	}

	snapshotMsgs := append([]session.Message(nil), m.session.Messages...)
	snapshotUpdated := m.session.UpdatedAt

	if err := m.session.Rewind(turns); err != nil {
		return 0, err
	}
	if err := m.session.Save(); err != nil {
		m.session.Messages = snapshotMsgs
		m.session.UpdatedAt = snapshotUpdated
		return 0, fmt.Errorf("save session: %w", err)
	}

	m.entries = sessionEntries(m.session)
	m.refreshTranscript()
	return turns, nil
}

// ClearConversation resets the current conversation: the visible
// transcript and the session's persisted Messages are emptied, while
// the session itself (ID, CreatedAt, Title) survives untouched. The
// next request starts from clean history.
//
// Refused while a stream is active — history must never be pulled out
// from under a running agent loop, and there is nothing to cancel here.
// If persistence fails after the in-memory reset, the error is
// reported; the divergence heals on the next successful Save.
func (m *model) ClearConversation() error {
	if m.waiting {
		return errors.New("cannot clear conversation while Lato is busy")
	}
	if m.session == nil {
		return errors.New("no active session")
	}

	m.Clear()
	m.session.ClearMessages()
	if err := m.session.Save(); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

// ExportConversation writes the current conversation to path as
// Markdown and returns the destination actually written. An empty path
// selects a deterministic, sanitized default derived from the session's
// title (falling back to its short ID).
//
// Safety rules: an existing destination is never overwritten; the
// parent directory must already exist (no surprise directory trees);
// only persisted session messages are rendered — credentials,
// configuration, memory, tasks, and runtime state are unreachable from
// here by construction.
func (m *model) ExportConversation(path string) (string, error) {
	if m.session == nil {
		return "", errors.New("no active session")
	}
	if len(m.session.Messages) == 0 {
		return "", errors.New("nothing to export yet — the conversation is empty")
	}

	dest := strings.TrimSpace(path)
	if dest == "" {
		dest = m.session.DefaultExportFilename()
	}

	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("refusing to overwrite existing destination %s", dest)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat %s: %w", dest, err)
	}

	dir := filepath.Dir(dest)
	if d, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("destination directory does not exist: %s", dir)
	} else if !d.IsDir() {
		return "", fmt.Errorf("destination directory is not a directory: %s", dir)
	}

	if err := os.WriteFile(dest, []byte(m.session.Markdown()), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", dest, err)
	}
	return dest, nil
}

// --- permissions (M13) ----------------------------------------------------

// PermissionsSummary renders the concise policy status shown by
// /permissions: workspace boundary, pending state, and active grants.
func (m *model) PermissionsSummary() string { return m.runtime.Permissions() }

// ResetPermissions drops every temporary approval. Grants never persist
// to disk, so this only affects the running process.
func (m *model) ResetPermissions() int { return m.runtime.ResetPermissions() }

// and reports the outcome in the transcript. Keys never appear in the
// report.
func (m *model) RefreshModels() error {
	refreshed, problems := m.runtime.RefreshConnectionModels()
	m.Println("✓ Model lists refreshed for %d provider(s).", refreshed)
	for _, p := range problems {
		m.Println("⚠ %s", p)
	}
	return nil
}

// OpenModelPicker opens the /model modal, scoped to the currently
// active provider. The options come from the live provider, not a
// baked-in list.
func (m *model) OpenModelPicker() {
	m.openModelPickerFor(m.providerName)
}

// openModelPickerFor fetches the models available on the active
// provider and opens the /model picker over them. A provider that
// reports exactly one model is auto-selected instead of prompting. When
// other providers are also connected, their cached model lists appear
// as grouped sections beneath the active provider's live list.
func (m *model) openModelPickerFor(provider string) {
	models, err := m.runtime.Models(context.Background())
	if err != nil {
		m.Println("⚠ %v", err)
		return
	}

	switch len(models) {
	case 0:
		// No models reported; leave the current model as-is.
	case 1:
		if err := m.SetModel(models[0].ID); err != nil {
			m.Println("⚠ %v", err)
			return
		}
		if len(m.runtime.Connections()) <= 1 {
			return // single provider: auto-selection is the whole answer
		}
	default:
	}

	if groups, ok := m.buildModelGroups(provider, models); ok {
		m.selectPicker = newGroupedModelPicker(groups, m.modelName, m.runtime.Effort())
		return
	}

	switch len(models) {
	case 0:
		// leave picker closed; the warning above already explains why
	default:
		m.selectPicker = newModelPicker(provider, m.modelName, models, m.runtime.Effort())
	}
}

// buildModelGroups assembles the grouped /model listing: the active
// provider first (with its freshly fetched list), then every other
// configured provider's cached discovery. Custom models registered via
// /model add always appear under their provider; ones whose ID is now
// also returned by live discovery are not duplicated.
func (m *model) buildModelGroups(activeID string, live []providers.ModelInfo) ([]modelGroup, bool) {
	return buildModelGroupList(activeID, live, m.runtime.Connections())
}

// buildModelGroupList is the pure grouping logic.
func buildModelGroupList(activeID string, live []providers.ModelInfo, conns []userconfig.Connection) ([]modelGroup, bool) {
	liveIDs := make(map[string]bool, len(live))
	for _, mi := range live {
		liveIDs[mi.ID] = true
	}
	// Surface manually registered models alongside the active
	// provider's live list so they are immediately selectable.
	addedCustom := false
	for _, c := range conns {
		if c.ID != activeID {
			continue
		}
		for _, cm := range c.Models {
			if cm.Custom && !liveIDs[cm.ID] {
				live = append(live, providers.ModelInfo{ID: cm.ID, Name: cm.Name})
				addedCustom = true
			}
		}
	}

	groups := []modelGroup{{Name: providers.DisplayName(activeID), Models: live}}
	for _, conn := range conns {
		if conn.ID == activeID || len(conn.Models) == 0 {
			continue
		}
		g := modelGroup{Name: conn.Name}
		for _, cm := range conn.Models {
			g.Models = append(g.Models, providers.ModelInfo{ID: cm.ID, Name: cm.Name})
		}
		groups = append(groups, g)
	}
	if len(groups) == 1 && !addedCustom {
		return nil, false
	}
	return groups, true
}

// chooseProvider switches to the provider with the given ID and prints
// a confirmation. Unless the new provider has exactly one known model,
// it also opens the model picker automatically, so picking a provider
// never leaves the user stuck on a stale model.
//
// Providers that need credentials but have neither a saved /connect
// configuration nor an environment key are refused with guidance
// instead of failing mid-request.
func (m model) chooseProvider(id string) (tea.Model, tea.Cmd) {
	if !m.runtime.IsConfigured(id) {
		m.Println("%s is not configured. Run /connect %s to set it up.", providers.DisplayName(id), id)
		m.refreshTranscript()
		return m, nil
	}
	if err := m.SetProvider(id); err != nil {
		m.entries = append(m.entries, chatEntry{Role: roleError, Content: err.Error()})
		m.refreshTranscript()
		return m, nil
	}
	m.Println("✓ Provider: %s", providers.DisplayName(id))

	m.openModelPickerFor(id)
	m.refreshTranscript()
	return m, nil
}

// applyModelChoice applies a model+effort selection from the picker.
// persist=false keeps the change session-only: nothing is written to
// config.yaml, and the previous defaults come back next launch. The
// header labels update only after the runtime accepted the switch.
func (m model) applyModelChoice(id string, lvl effort.Level, persist bool) (tea.Model, tea.Cmd) {
	var err error
	if persist {
		err = m.runtime.SetModelWithEffort(id, lvl.String())
	} else {
		err = m.runtime.SetSessionModelWithEffort(id, lvl.String())
	}
	if err != nil {
		m.entries = append(m.entries, chatEntry{Role: roleError, Content: err.Error()})
		m.refreshTranscript()
		return m, nil
	}

	m.modelName = id
	m.effortName = m.runtime.CurrentEffort()
	scope := ""
	if !persist {
		scope = " (session only)"
	}
	m.Println("✓ Model: %s · %s%s", id, m.effortName, scope)
	m.refreshTranscript()
	return m, nil
}
