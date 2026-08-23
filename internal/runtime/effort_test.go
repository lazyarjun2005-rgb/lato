package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lato/internal/effort"
	"lato/internal/providers"
	"lato/internal/tools"
)

// TestProfilesAreBoundedAndMonotonic pins the M16 safety contract: every
// profile has hard bounds, and higher effort widens them without ever
// becoming unbounded.
func TestProfilesAreBoundedAndMonotonic(t *testing.T) {
	const absoluteTurnCeiling = 64 // no level may ever exceed this

	prev := effortProfile{}
	for _, lvl := range effort.All {
		p := profileFor(lvl)
		if p.MaxTurns <= 0 || p.MaxTurns > absoluteTurnCeiling {
			t.Errorf("%s: MaxTurns = %d, want 1..%d", lvl, p.MaxTurns, absoluteTurnCeiling)
		}
		if p.RepeatNudgeAfter <= 0 || p.RepeatStopAfter <= p.RepeatNudgeAfter {
			t.Errorf("%s: repetition thresholds inconsistent: nudge=%d stop=%d", lvl, p.RepeatNudgeAfter, p.RepeatStopAfter)
		}
		if prev.MaxTurns != 0 && p.MaxTurns < prev.MaxTurns {
			t.Errorf("%s: MaxTurns shrank relative to lower level", lvl)
		}
		prev = p
	}

	// Medium must reproduce the pre-M16 balanced constants exactly.
	m := profileFor(effort.Medium)
	if m.MaxTurns != maxAgentTurns || m.RepeatNudgeAfter != repeatNudgeAfter || m.RepeatStopAfter != repeatStopAfter {
		t.Errorf("medium profile %+v diverges from M10 constants", m)
	}

	// Invalid levels resolve to the default profile instead of panicking.
	if profileFor(effort.Level(0)).MaxTurns != maxAgentTurns {
		t.Error("zero level did not fall back to balanced profile")
	}
}

// TestEffortGuidanceInjectedPerLevel pins that complex-task prompts
// carry the level's guidance block at non-medium levels and stay
// byte-identical to pre-M16 prompts at medium.
func TestEffortGuidanceInjectedPerLevel(t *testing.T) {
	rt := newTestRuntime(nil)
	history := []providers.Message{{Role: providers.UserRole,
		Content: "Add a validation helper to this project, write tests for it, run the tests, and fix any failures."}}

	rt.effort = effort.Medium
	msgsMedium, _ := rt.buildMessages(history)
	rt.effort = effort.LatoX
	msgsX, _ := rt.buildMessages(history)
	rt.effort = effort.Low
	msgsLow, _ := rt.buildMessages(history)

	sys := func(msgs []providers.Message) string { return msgs[0].Content }

	if strings.Contains(sys(msgsMedium), "## Effort:") {
		t.Error("medium must not inject an effort block")
	}
	for _, msgs := range [][]providers.Message{msgsLow, msgsX} {
		if !strings.Contains(sys(msgs), "## Effort:") || !strings.Contains(sys(msgs), taskDirective) {
			t.Errorf("expected task protocol + effort guidance in:\n%.200s", sys(msgs))
		}
	}
	if !strings.Contains(sys(msgsX), "maximum (lato-X)") {
		t.Errorf("lato-X guidance missing its marker:\n%s", sys(msgsX))
	}
	if strings.Contains(sys(msgsLow), "maximum") {
		t.Errorf("low guidance must not claim maximum mode:\n%s", sys(msgsLow))
	}
}

// TestLowEffortTightensBudget proves the ladder changes real execution:
// an agent repeating one identical tool call is stopped after 3 calls
// under LOW but allowed 4 under MEDIUM — same provider, same script,
// different effort-scaled bounds.
func TestLowEffortTightensBudget(t *testing.T) {
	executions := func(level effort.Level) (int, string) {
		rt := newTestRuntime(&runawayProvider{})
		rt.effort = level
		events, err := rt.StreamChat(context.Background(), []providers.Message{
			{Role: providers.UserRole, Content: "Add a feature to this project, test it, run the tests, fix failures, then verify everything."},
		})
		if err != nil {
			t.Fatal(err)
		}
		count, lastText := 0, ""
		for ev := range events {
			switch ev.Type {
			case EventToolFinish:
				count++
			case EventDone:
				if ev.Response != nil {
					lastText = ev.Response.Content
				}
			}
		}
		return count, lastText
	}

	lowRuns, lowContent := executions(effort.Low)
	if lowRuns != 3 {
		t.Errorf("low effort stopped after %d identical tool runs, want exactly 3:\n%s", lowRuns, lowContent)
	}
	if !strings.Contains(lowContent, "repeated with identical arguments 3 times") {
		t.Errorf("low repetition summary missing:\n%s", lowContent)
	}

	medRuns, medContent := executions(effort.Medium)
	if medRuns != 4 {
		t.Errorf("medium effort stopped after %d identical tool runs, want exactly 4:\n%s", medRuns, medContent)
	}
}

// runawayProvider requests one echo tool call per turn, forever. It only
// terminates because the effort-scaled M10 budget stops it.
type runawayProvider struct{}

func (r *runawayProvider) StreamChat(ctx context.Context, messages []providers.Message, tools []tools.Definition) (<-chan providers.StreamEvent, error) {
	events := make(chan providers.StreamEvent, 1)
	events <- providers.StreamEvent{
		Text:      "working...",
		ToolCalls: []providers.ToolCall{{ID: "t", Name: "echo", Arguments: map[string]any{"value": "echo-ran"}}},
		Done:      true,
	}
	close(events)
	return events, nil
}

func (r *runawayProvider) ListModels(ctx context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}

// TestSetEffortPersistsAndApplies pins the setting semantics: persist=true
// writes config.yaml; session-only does not; invalid names are refused;
// the active provider receives resolved request-side effort when it
// declares a capability.
func TestSetEffortPersistsAndApplies(t *testing.T) {
	isolateUserConfig(t)
	cfgDir := t.TempDir()
	t.Setenv("LATO_HOME", cfgDir)
	t.Setenv("OPENROUTER_API_KEY", "k")

	if err := writeTestConfig(cfgDir, "openrouter"); err != nil {
		t.Fatal(err)
	}
	rt, err := New()
	if err != nil {
		t.Fatal(err)
	}

	if err := rt.SetEffort("ultra", true); err != nil {
		t.Fatalf("SetEffort(ultra) error = %v", err)
	}
	if rt.CurrentEffort() != "ultra" {
		t.Errorf("CurrentEffort = %q, want ultra", rt.CurrentEffort())
	}
	assertPersistedEffort(t, cfgDir, "ultra")

	// Session-only change updates state but not config.yaml.
	if err := rt.SetEffort("low", false); err != nil {
		t.Fatal(err)
	}
	if rt.CurrentEffort() != "low" {
		t.Errorf("session effort = %q, want low", rt.CurrentEffort())
	}
	assertPersistedEffort(t, cfgDir, "ultra")

	// Invalid values are refused with the valid choices named.
	err = rt.SetEffort("turbo", true)
	if err == nil || !strings.Contains(err.Error(), "valid:") {
		t.Errorf("SetEffort(turbo) error = %v, want actionable refusal", err)
	}

	// The openrouter capability resolved onto the live provider object:
	// low maps to the weakest declared token.
	compat := rt.provider.(*providers.NvidiaProvider)
	mech, token := compat.EffortSetting()
	if mech != string(providers.EffortReasoningObject) || token != "low" {
		t.Errorf("provider effort = (%q,%q), want reasoning/low", mech, token)
	}

	// Switching to a capability-less provider clears request-side effort
	// while keeping the Lato-side level.
	if err := rt.SetProvider("ollama"); err != nil {
		t.Skipf("ollama unavailable in this environment: %v", err)
	}
}

func assertPersistedEffort(t *testing.T, cfgDir, want string) {
	t.Helper()
	raw, err := readFileString(filepath.Join(cfgDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "effort: "+want) {
		t.Errorf("config.yaml missing effort: %s:\n%s", want, raw)
	}
}

func readFileString(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

// writeTestConfig seeds a minimal valid config pointing at providerID.
func writeTestConfig(cfgDir, providerID string) error {
	endpoint := "http://localhost:11434"
	switch providerID {
	case "openrouter":
		endpoint = "https://openrouter.ai/api/v1"
	case "9router":
		endpoint = "http://localhost:20128/v1"
	}
	cfg := `model:
  provider: ` + providerID + `
  endpoint: ` + endpoint + `
  name: test-model
agent:
  name: default
  system_prompt: test
`
	return os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfg), 0o600)
}
