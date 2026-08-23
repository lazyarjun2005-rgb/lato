// Package runtime wires together config, skills, the agent, and a model
// provider to execute a single task. This is the "harness" itself: it
// contains no business logic of its own beyond ordering these steps.
package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"lato/internal/agent"
	"lato/internal/config"
	contextpkg "lato/internal/context"
	"lato/internal/effort"
	"lato/internal/index"
	"lato/internal/memory"
	"lato/internal/permissions"
	"lato/internal/providers"
	"lato/internal/retrieve"
	"lato/internal/skills"
	"lato/internal/task"
	"lato/internal/tools"
	"lato/internal/tools/builtin"
	"lato/internal/tools/repository"
	"lato/internal/workspace"
)

// indexCache holds one built index and the workspace root it was built
// for, so the runtime can reuse it until the root changes.
type IndexCacheUnusedPlaceholder struct{}

// Runtime is the main execution point for Lato.
type Runtime struct {
	cfg       *config.Config
	provider  providers.ModelProvider
	agent     *agent.Agent
	manager   *tools.Manager
	skills    *skills.Store
	workspace workspace.Info
	index     *indexCache

	// effort is the active rung of Lato's ladder (M16). It scales the
	// M10 orchestration profile and, when the active provider declares
	// a capability, the request-side reasoning parameter.
	effort effort.Level

	// providerErr records why the initial provider could not be built
	// (missing credential, unsupported ID, ...). Startup deliberately
	// survives it so configuration can be fixed interactively through
	// /connect or /model; requests fail with this error until then.
	providerErr error

	// perms is the centralized safety policy (M13) consulted before
	// every tool execution; asker resolves confirmation prompts. askMu
	// guards asker, which the TUI attaches after construction.
	perms *permissions.Policy
	asker Asker
	askMu sync.RWMutex
}

// indexCache pairs a built index with the workspace root it was indexed
// from, so the index is invalidated whenever the workspace changes.
type indexCache struct {
	root string
	idx  *index.Index
}

func New() (*Runtime, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	latoHome, err := config.Dir()
	if err != nil {
		return nil, fmt.Errorf("resolve lato home: %w", err)
	}

	// Build the skill store once for the lifetime of this Runtime.
	skillStore, err := skills.New(latoHome)
	if err != nil {
		return nil, fmt.Errorf("load skill store: %w", err)
	}

	a := agent.New(
		cfg.Agent.Name,
		cfg.Agent.SystemPrompt,
		skills.FormatCatalog(skillStore.Catalog()),
	)

	manager, err := builtin.NewManager()
	if err != nil {
		return nil, fmt.Errorf("create tool manager: %w", err)
	}
	if err := manager.Register(newLoadSkillTool(skillStore)); err != nil {
		return nil, fmt.Errorf("register load_skill tool: %w", err)
	}

	// Discover the workspace once at startup so later milestones can
	// read a cached description instead of re-scanning the disk.
	ws := workspace.Discover()

	rt := &Runtime{
		cfg:       cfg,
		agent:     a,
		manager:   manager,
		skills:    skillStore,
		workspace: ws,
	}

	// The permission policy guards the discovered workspace root for
	// the lifetime of this Runtime (M13). Without an attached asker it
	// fails safe: anything needing confirmation is refused.
	rt.perms = permissions.NewPolicy(ws.Root)

	// Project memory tools let the agent persist durable discoveries;
	// they operate on the user-level store for the discovered workspace.
	if err := memory.Register(manager, rt); err != nil {
		return nil, fmt.Errorf("register memory tools: %w", err)
	}

	// The initial provider is built through the runtime method so saved
	// /connect credentials take precedence from the first request on.
	// A provider that cannot be constructed — missing credential,
	// unsupported ID, unknown class — does not sink startup: the TUI
	// still launches and the reason is reported as an actionable error
	// on the first request (and via StartError), so it can be fixed
	// interactively with /connect or /model.
	rt.effort = effortFromConfig(cfg)
	provider, err := rt.newProvider(cfg, rt.effort)
	if err != nil {
		rt.providerErr = err
	} else {
		rt.provider = provider
	}

	// The repository tools wrap the runtime's index, so they must be
	// registered after the Runtime exists; runtime itself satisfies the
	// Store interface they need. This keeps /index, context building, and
	// search all backed by the same cached index.
	if err := repository.Register(manager, rt); err != nil {
		return nil, fmt.Errorf("register repository tools: %w", err)
	}

	// The editing tools are scoped to the discovered workspace root and
	// invalidate the cached index whenever a write lands.
	if err := rt.RegisterEditTools(); err != nil {
		return nil, fmt.Errorf("register editing tools: %w", err)
	}

	// Command execution is pinned to the discovered workspace root and
	// invalidates the cached index whenever a run actually started (a
	// command may modify workspace files behind Lato's back).
	if err := rt.RegisterShellTools(); err != nil {
		return nil, fmt.Errorf("register shell tools: %w", err)
	}

	return rt, nil
}

func (r *Runtime) CurrentModel() string    { return r.cfg.Model.Name }     // CurrentModel returns the name of the model currently in use.
func (r *Runtime) CurrentProvider() string { return r.cfg.Model.Provider } // CurrentProvider returns the name of the provider currently in use.

// StartError reports why the initially configured model provider could
// not be built, or nil when startup was fully healthy. It lets the UI
// surface a fixable problem instead of refusing to start.
func (r *Runtime) StartError() error { return r.providerErr }

// activeProvider returns the current provider, or the recorded startup
// failure when none could be built. Every request path goes through
// here so a broken configuration surfaces as an error event rather
// than a nil-pointer crash.
func (r *Runtime) activeProvider() (providers.ModelProvider, error) {
	if r.provider == nil {
		if r.providerErr != nil {
			return nil, r.providerErr
		}
		return nil, fmt.Errorf("no model provider is configured — run /connect to set one up")
	}
	return r.provider, nil
}

// Models asks the active provider for the models currently available on
// it, so the /model picker reflects reality instead of a baked-in list.
func (r *Runtime) Models(ctx context.Context) ([]providers.ModelInfo, error) {
	provider, err := r.activeProvider()
	if err != nil {
		return nil, err
	}
	return provider.ListModels(ctx)
}

// Workspace returns the workspace description captured when this Runtime
// was created.
func (r *Runtime) Workspace() workspace.Info {
	return r.workspace
}

// SetModel switches the active model, keeping the current provider and
// effort, and takes effect starting with the next request.
func (r *Runtime) SetModel(name string) error {
	return r.SetModelWithEffort(name, "")
}

// SetModelWithEffort switches model and effort in one atomic step. An
// empty level keeps the current effort; otherwise it must be a valid
// ladder name ("low"…"lato-X"). The provider is only rebuilt once both
// values validate, and config is saved only after construction
// succeeds — a typo can never corrupt either setting.
func (r *Runtime) SetModelWithEffort(name, level string) error {
	if name == "" {
		return fmt.Errorf("model name cannot be empty")
	}
	newLevel := r.effort
	if !newLevel.IsValid() {
		newLevel = effort.Default
	}
	if level != "" {
		parsed, err := effort.Parse(level)
		if err != nil {
			return err
		}
		newLevel = parsed
	}

	cfg := *r.cfg
	cfg.Model.Name = name
	cfg.Model.Effort = newLevel.String()
	provider, err := r.newProvider(&cfg, newLevel)
	if err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	r.cfg = &cfg
	r.provider = provider
	r.effort = newLevel
	r.providerErr = nil
	return nil
}

// SetSessionModelWithEffort applies a model+effort pair to this
// session only: config.yaml is never written, so the persisted default
// comes back on the next launch.
func (r *Runtime) SetSessionModelWithEffort(name, level string) error {
	if name == "" {
		return fmt.Errorf("model name cannot be empty")
	}
	newLevel := r.effort
	if !newLevel.IsValid() {
		newLevel = effort.Default
	}
	if level != "" {
		parsed, err := effort.Parse(level)
		if err != nil {
			return err
		}
		newLevel = parsed
	}

	cfg := *r.cfg
	cfg.Model.Name = name
	cfg.Model.Effort = newLevel.String()
	provider, err := r.newProvider(&cfg, newLevel)
	if err != nil {
		return err
	}
	r.provider = provider
	r.effort = newLevel
	r.providerErr = nil
	return nil
}

// CurrentEffort returns the display label of the active effort.
func (r *Runtime) CurrentEffort() string { return r.effort.String() }

// Effort returns the active effort level.
func (r *Runtime) Effort() effort.Level { return r.effort }

// SetEffort changes the effort level. With persist=true the choice is
// saved as the default for future sessions; with false it applies to
// this session only. Either way the provider is rebuilt so request-side
// effort (where supported) and orchestration bounds move together.
func (r *Runtime) SetEffort(level string, persist bool) error {
	parsed, err := effort.Parse(level)
	if err != nil {
		return err
	}

	cfg := *r.cfg
	cfg.Model.Effort = parsed.String()
	provider, err := r.newProvider(&cfg, parsed)
	if err != nil {
		return err
	}
	if persist {
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		r.cfg = &cfg
	}
	r.provider = provider
	r.effort = parsed
	r.providerErr = nil
	return nil
}

// SetProvider switches the active provider, keeping the current model
// name, and takes effect starting with the next request. If the
// provider is registered, its default endpoint is adopted too, so
// switching providers never leaves the endpoint pointed at the
// previous one.
func (r *Runtime) SetProvider(name string) error {
	if name == "" {
		return fmt.Errorf("provider name cannot be empty")
	}
	cfg := *r.cfg
	cfg.Model.Provider = name
	if info, ok := providers.ByID(name); ok {
		cfg.Model.Endpoint = info.Endpoint
	} else if conn, ok := r.Connection(name); ok && conn.Endpoint != "" {
		// Custom providers take their endpoint from the saved connection.
		cfg.Model.Endpoint = conn.Endpoint
	}
	// newProvider applies the credential precedence (saved /connect
	// configuration over environment over config.yaml), so the endpoint
	// stored here is only a display/default value; secrets stay out of
	// config.yaml either way (APIKey is yaml:"-"). Effort is preserved:
	// capability resolution adapts it to the target provider.
	provider, err := r.newProvider(&cfg, r.effort)
	if err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	r.cfg = &cfg
	r.provider = provider
	r.providerErr = nil
	return nil
}

// effortFromConfig resolves the configured effort string, falling back
// to the balanced default when unset. Load already validates the value;
// this is defensive for hand-constructed configs.
func effortFromConfig(cfg *config.Config) effort.Level {
	if cfg == nil || cfg.Model.Effort == "" {
		return effort.Default
	}
	if lvl, err := effort.Parse(cfg.Model.Effort); err == nil {
		return lvl
	}
	return effort.Default
}

// applyProviderEffort configures provider-side effort on providers that
// declared support. Providers without a capability entry receive
// nothing — Lato-side orchestration still applies in full.
func applyProviderEffort(p providers.ModelProvider, providerID string, level effort.Level) {
	if !level.IsValid() {
		level = effort.Default
	}
	ea, ok := p.(providers.EffortAware)
	if !ok {
		return
	}
	if mech, token, ok := providers.ResolveProviderEffort(providerID, level); ok {
		ea.ApplyEffort(string(mech), token)
	}
}

func (r *Runtime) contextFor(history []providers.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != providers.UserRole {
			continue
		}
		question := history[i].Content

		// Whole-repository questions get the static description block.
		// Code questions ("how does X work?", "where is Y used?") get,
		// in addition, deterministic source evidence retrieved from the
		// cached index — actual excerpts, declarations, and import
		// relationships — so the model reasons from real code.
		if !contextpkg.RepositoryQuestion(question) && !contextpkg.LooksLikeCodeQuestion(question) {
			return ""
		}
		ctx := contextpkg.NewBuilder(r.workspace).Build()
		parts := []string{ctx.Text(), r.repositorySnapshot(question)}
		if evidence := retrieve.ForQuestion(r.Index(), r.workspace.Repository, question); !evidence.Empty() {
			parts = append(parts, evidence.Text())
		}
		return strings.Join(parts, "\n\n")
	}
	return ""
}

// repositorySnapshot renders a compact, bounded summary of the indexed
// repository for injection when the model asks about the workspace. It
// names the files most relevant to the user's question — scored against
// the question's own words when it has them — without dumping the tree,
// so the prompt stays focused. For code questions, source-level evidence
// is appended separately by contextFor (see internal/retrieve).
func (r *Runtime) repositorySnapshot(query string) string {
	idx := r.Index()
	stats, ok := idx.Stats()
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString("Repository index:\n")
	fmt.Fprintf(&b, "  Files: %d\n", stats.Files)
	fmt.Fprintf(&b, "  Directories: %d\n", stats.Directories)
	if langs := languagesSummary(stats.Languages); langs != "" {
		fmt.Fprintf(&b, "  Languages: %s\n", langs)
	}
	fmt.Fprintf(&b, "  Go packages: %d\n", stats.GoPackages)
	fmt.Fprintf(&b, "  Symbols: %d\n", stats.Symbols)

	if relevant := r.RelevantFiles(10, query); len(relevant) > 0 {
		b.WriteString("  Important files:\n")
		for _, f := range relevant {
			if f.Lang == "" {
				fmt.Fprintf(&b, "    - %s\n", f.Path)
			} else {
				fmt.Fprintf(&b, "    - %s (%s)\n", f.Path, f.Lang)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// languagesSummary renders a sorted "Lang (n)" list.
func languagesSummary(m map[string]int) string {
	if len(m) == 0 {
		return ""
	}
	langs := make([]string, 0, len(m))
	for l := range m {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	parts := make([]string, len(langs))
	for i, l := range langs {
		parts[i] = fmt.Sprintf("%s (%d)", l, m[l])
	}
	return strings.Join(parts, ", ")
}

// StreamChat runs the agent loop and emits its structured events as they
// happen. Model text, tool calls, tool results, the final response, and
// M12 task checkpoints all flow through this single execution path.
func (r *Runtime) StreamChat(ctx context.Context, messages []providers.Message) (<-chan Event, error) {
	if ctx == nil {
		return nil, fmt.Errorf("stream context cannot be nil")
	}

	if isResumeRequest(lastUserMessage(messages)) {
		events := make(chan Event)
		go func() {
			defer close(events)
			emit := func(event Event) bool {
				select {
				case events <- event:
					return true
				case <-ctx.Done():
					return false
				}
			}
			r.handleResumeRequest(ctx, emit)
		}()
		return events, nil
	}

	return r.stream(ctx, messages, nil)
}

// stream is the shared execution body for fresh requests and resumed
// tasks alike: one loop, one event channel, optional task tracking.
func (r *Runtime) stream(ctx context.Context, history []providers.Message, existing *task.Task) (<-chan Event, error) {
	events := make(chan Event)
	go func() {
		defer close(events)
		emit := func(event Event) bool {
			select {
			case events <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}
		msgs, memCount := r.buildMessages(history)
		if memCount > 0 {
			emit(Event{Type: EventMemory, Count: memCount})
		}
		trk := newTaskTracker(r, lastUserMessage(history), existing)
		r.run(ctx, msgs, emit, trk)
	}()

	return events, nil
}

// Stream is kept as a compatibility alias for StreamChat.
func (r *Runtime) Stream(ctx context.Context, messages []providers.Message) (<-chan Event, error) {
	return r.StreamChat(ctx, messages)
}

// Run executes the same streaming agent loop as StreamChat and returns its
// final response after consuming the emitted events.
func (r *Runtime) Run(messages []providers.Message) (providers.Response, error) {
	return r.RunContext(context.Background(), messages)
}

// RunContext is Run with caller-controlled cancellation and deadlines.
func (r *Runtime) RunContext(ctx context.Context, messages []providers.Message) (providers.Response, error) {
	events, err := r.StreamChat(ctx, messages)
	if err != nil {
		return providers.Response{}, err
	}

	for event := range events {
		switch event.Type {
		case EventDone:
			if event.Response == nil {
				return providers.Response{}, fmt.Errorf("runtime completed without a response")
			}
			return *event.Response, nil
		case EventError:
			return providers.Response{}, event.Err
		}
	}

	if err := ctx.Err(); err != nil {
		return providers.Response{}, err
	}
	return providers.Response{}, fmt.Errorf("runtime stopped without a completion event")
}

// run is the only agent loop. It turns provider chunks into runtime events,
// executes requested tools, appends their results to the conversation, and
// repeats until a model turn does not request a tool.
//
// M10 bounds the cycle: each user request gets at most maxAgentTurns
// model turns, and identical consecutive tool calls are steered once,
// then stopped — so autonomous execution can never spin forever.
func (r *Runtime) run(ctx context.Context, messages []providers.Message, emit func(Event) bool, trk *taskTracker) {
	// The effort profile (M16) scales the EXISTING loop's bounds: turn
	// budget and repetition thresholds come from the active level. The
	// safety model is identical at every level — bounded turns, repeat
	// detection, permission gate.
	prof := r.profile()

	// The tool set is chosen once per request from the user's latest
	// message and stays fixed for the whole tool loop: mid-task turns
	// must keep seeing the same capabilities they started with.
	definitions := r.toolDefinitions(messages)

	turns := 0
	var lastSignature string
	repeats := 0
	nudged := false
	var toolsUsed []string
	stalls := 0

	for {
		if turns >= prof.MaxTurns {
			finishWithStatus(emit, withPausePreview(trk, budgetSummary(turns, toolsUsed)))
			return
		}
		turns++

		response, err := r.runModelTurn(ctx, messages, definitions, emit)
		if err != nil {
			emit(Event{Type: EventError, Err: err})
			return
		}

		trk.observePlan(response.Content)
		trk.observeProgress(response.Content)

		if len(response.ToolCalls) == 0 {
			// A turn without tool calls normally ends the request — but
			// mid-task it is often just narration: the model spoke,
			// neither acted nor concluded, and plan steps remain open.
			// Steer it back into the SAME run instead of finalizing and
			// forcing the user to type "continue" (M16 regression fix).
			// Bounded by maxStallContinuations at every effort level.
			if trk.needsContinuation(response.Content) {
				if stalls < maxStallContinuations && turns < prof.MaxTurns {
					stalls++
					messages = append(messages,
						providers.Message{
							Role:    providers.AssistantRole,
							Content: response.Content,
						},
						providers.Message{
							Role:    providers.SystemRole,
							Content: continuationNudge,
						},
					)
					continue
				}
				// Continuations exhausted mid-task: pause honestly. The
				// task stays resumable and success is never claimed
				// over an unconcluded run.
				finishWithStatus(emit, withPausePreview(trk,
					"Paused: the model stopped mid-task without completing or concluding. "+
						"Progress above is preserved — continue with a follow-up message."))
				return
			}
			response.Content += trk.finish(response.Content, len(toolsUsed))
			emit(Event{Type: EventDone, Response: &response})
			return
		}
		// NOTE: stalls is deliberately NOT reset on progress — the
		// continuation allowance is absolute per request, keeping the
		// worst case identical at every effort level.

		messages = append(messages, providers.Message{
			Role:      providers.AssistantRole,
			Content:   response.Content,
			ToolCalls: response.ToolCalls,
		})

		for _, tc := range response.ToolCalls {
			call := tc
			if !emit(Event{Type: EventToolStart, ToolCall: &call}) {
				return
			}

			// Permission gate (M13): classify → check → allow/deny/ask.
			// Every tool call from every provider passes through here.
			started := time.Now()
			result, execErr := r.executeTool(ctx, tc, trk)
			switch {
			case execErr != nil && ctx.Err() != nil:
				// The request itself was cancelled while the tool ran;
				// there is no later model turn to inform. Reporting is
				// best-effort (the emit channel may already be closing);
				// consumers also observe cancellation through ctx.
				emit(Event{Type: EventError, Err: ctx.Err()})
				return
			case execErr != nil:
				// Recoverable execution failure (M16 regression fix): a
				// tool error is information for the model, not the end of
				// the request. It becomes the tool's structured result so
				// it joins the conversation and the SAME loop continues —
				// the model can correct its arguments, choose another
				// tool, or conclude. Still bounded by the effort profile's
				// turn budget and the repetition guard below.
				result = tools.Result{
					IsError: true,
					Content: fmt.Sprintf("tool %q failed: %v", tc.Name, execErr),
				}
			}

			toolResult := &ToolResult{
				ToolCallID: tc.ID,
				Name:       tc.Name,
				Arguments:  tc.Arguments,
				Content:    result.Content,
				IsError:    result.IsError,
				Success:    !result.IsError,
				Duration:   time.Since(started),
			}
			if !emit(Event{Type: EventToolFinish, ToolResult: toolResult}) {
				return
			}

			messages = append(messages, providers.Message{
				Role:       providers.ToolRole,
				Name:       tc.Name,
				Content:    result.Content,
				ToolCallID: tc.ID,
			})

			toolsUsed = append(toolsUsed, tc.Name)
			trk.observeTool(tc.Name, tc.Arguments, *toolResult)

			// Loop guard: identical consecutive calls with no progress.
			sig := toolSignature(tc.Name, tc.Arguments)
			if sig == lastSignature {
				repeats++
			} else {
				lastSignature = sig
				repeats = 1
			}
			switch {
			case repeats == prof.RepeatNudgeAfter && !nudged:
				nudged = true
				messages = append(messages, providers.Message{
					Role:    providers.SystemRole,
					Content: steeringMessage,
				})
			case repeats >= prof.RepeatStopAfter:
				finishWithStatus(emit, withPausePreview(trk, repeatSummary(tc.Name, prof.RepeatStopAfter)))
				return
			}
		}
	}
}

// finishWithStatus ends an autonomous run cleanly: a visible status line
// followed by a normal completion event carrying the same text, so the
// TUI and session persistence treat it like any final answer.
func finishWithStatus(emit func(Event) bool, status string) {
	emit(Event{Type: EventText, Text: "\n" + status + "\n"})
	emit(Event{Type: EventDone, Response: &providers.Response{Content: status}})
}

// runModelTurn streams one provider response and returns the assembled model
// response. It deliberately knows nothing about tool execution.
func (r *Runtime) runModelTurn(ctx context.Context, messages []providers.Message, definitions []tools.Definition, emit func(Event) bool) (providers.Response, error) {
	provider, err := r.activeProvider()
	if err != nil {
		return providers.Response{}, err
	}

	if !emit(Event{Type: EventThinking}) {
		return providers.Response{}, context.Canceled
	}

	stream, err := provider.StreamChat(ctx, messages, definitions)
	if err != nil {
		return providers.Response{}, fmt.Errorf("model call failed: %w", err)
	}

	var response providers.Response
	for event := range stream {
		if event.Err != nil {
			return providers.Response{}, fmt.Errorf("model stream failed: %w", event.Err)
		}

		if event.Thinking != "" {
			if !emit(Event{Type: EventThinking, Thinking: event.Thinking}) {
				return providers.Response{}, context.Canceled
			}
		}

		if event.Text != "" {
			response.Content += event.Text
			if !emit(Event{Type: EventText, Text: event.Text}) {
				return providers.Response{}, context.Canceled
			}
		}

		response.ToolCalls = append(response.ToolCalls, event.ToolCalls...)
	}

	if err := ctx.Err(); err != nil {
		return providers.Response{}, err
	}
	return response, nil
}

func Run(messages []providers.Message) (providers.Response, error) {
	rt, err := New()
	if err != nil {
		return providers.Response{}, fmt.Errorf("create runtime: %w", err)
	}

	return rt.Run(messages)
}

// newProvider selects and constructs a ModelProvider based on
// cfg.Model.Provider. The provider's registry entry decides which
// implementation speaks for it (native Ollama client or the shared
// OpenAI-compatible client), so new OpenAI-shaped providers are a
// registry change, not a code change here.
//
// level is the active effort: when the target provider declares an
// effort capability, the matching request parameter is applied here so
// every construction path (startup, /model, /effort) behaves
// identically. Providers without a capability receive nothing.
//
// Credentials follow Lato's precedence: saved /connect configuration
// first, then environment variables, then config.yaml values, then
// registry defaults. Custom providers configured through /connect are
// built from their stored entry even though they are absent from the
// static registry.
func (r *Runtime) newProvider(cfg *config.Config, level effort.Level) (providers.ModelProvider, error) {
	id := cfg.Model.Provider

	info, registered := providers.ByID(id)
	class := info.Class

	conn, connected := r.Connection(id)
	switch {
	case registered:
	case connected:
		// Custom providers live only in the user's connection store.
		class = conn.Class
	default:
		return nil, fmt.Errorf("unsupported model provider %q (known providers: ollama, lmstudio, nvidia, openrouter, 9router, omniroute)", id)
	}

	endpoint, apiKey := cfg.Model.Endpoint, cfg.Model.APIKey
	if connected {
		if conn.Endpoint != "" {
			endpoint = conn.Endpoint
		}
		if conn.APIKey != "" {
			apiKey = conn.APIKey
		}
	}

	// Fail before any HTTP call when the selected provider needs a
	// credential that neither the connection store nor the environment
	// provides. A saved connection always satisfies this check: /connect
	// validated it explicitly (local routers often run without a key).
	if requiresKey(id) && apiKey == "" && !connected {
		name := info.Name
		if name == "" {
			name = id
		}
		env := info.APIKeyEnv
		if env == "" {
			env = "an API key"
		}
		return nil, fmt.Errorf("%s is not set — run /connect %s or set it", env, id)
	}

	switch class {
	case providers.ClassOllama:
		p := providers.NewOllamaProvider(endpoint, cfg.Model.Name)
		applyProviderEffort(p, id, level)
		return p, nil
	case providers.ClassOpenAICompatible:
		p := providers.NewOpenAICompatible(endpoint, cfg.Model.Name, apiKey, nil)
		applyProviderEffort(p, id, level)
		return p, nil
	default:
		return nil, fmt.Errorf("provider %q has unknown class %q", id, class)
	}
}
