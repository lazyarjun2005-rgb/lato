package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveAPIKeyUsesProviderEnv pins the Milestone 9 contract: the
// active provider's registry entry decides which environment variable
// supplies the API key. Local providers need no key.
func TestResolveAPIKeyUsesProviderEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "or-key")
	t.Setenv("NINEROUTER_KEY", "nine-key")
	t.Setenv("OMNIROUTE_KEY", "omni-key")
	t.Setenv("NVIDIA_API_KEY", "nvda-key")

	cases := map[string]string{
		"openrouter": "or-key",
		"9router":    "nine-key",
		"omniroute":  "omni-key",
		"nvidia":     "nvda-key",
		"ollama":     "", // local: no key needed
		"lmstudio":   "", // local: no key needed
	}
	for providerID, want := range cases {
		if got := resolveAPIKey(providerID); got != want {
			t.Errorf("resolveAPIKey(%q) = %q, want %q", providerID, got, want)
		}
	}
}

// TestResolveAPIKeyMissingKeyYieldsEmpty verifies an unset variable is
// reported as empty (construction then fails fast with a clear error,
// rather than sending a half-configured request).
func TestResolveAPIKeyMissingKeyYieldsEmpty(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	if got := resolveAPIKey("openrouter"); got != "" {
		t.Errorf("resolveAPIKey(openrouter) with unset env = %q, want empty", got)
	}
}

// isolateConfig points Lato's configuration directory at a fresh
// temporary directory for the duration of one test, covering the
// platform-specific environment variables every supported OS consults.
func isolateConfig(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	cfgDir := filepath.Join(base, "config", "lato")
	t.Setenv("LATO_HOME", cfgDir)
	return cfgDir
}

// TestSaveDoesNotPersistAPIKey verifies the in-memory-only key rule:
// whatever is resolved from the environment must never reach
// config.yaml on disk.
func TestSaveDoesNotPersistAPIKey(t *testing.T) {
	cfgDir := isolateConfig(t)

	cfg := &Config{
		Model: Model{Provider: "openrouter", Endpoint: "https://openrouter.ai/api/v1", Name: "vendor/model", APIKey: "super-secret"},
		Agent: Agent{Name: "default"},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(cfgDir, "config.yaml"))
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if strings.Contains(string(raw), "super-secret") {
		t.Error("API key was written to config.yaml; keys must stay in memory only")
	}
}

// TestLoadCreatesDefaultConfigUnderPlatformDir pins the M14 storage
// layout: first run creates config.yaml inside the OS configuration
// directory (or LATO_HOME), never inside a repository and never at a
// hard-coded Unix path.
func TestLoadCreatesDefaultConfigUnderPlatformDir(t *testing.T) {
	cfgDir := isolateConfig(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Model.Provider == "" || cfg.Model.Endpoint == "" || cfg.Model.Name == "" {
		t.Fatalf("default config incomplete: %+v", cfg.Model)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "config.yaml")); err != nil {
		t.Fatalf("default config not created under %s: %v", cfgDir, err)
	}
}

// TestDirMigratesLegacyHome verifies a pre-M14 ~/.lato home is copied
// into the platform location once: config.yaml and skill files land in
// the new place, existing new-style files are never overwritten, and
// the legacy directory is left untouched.
func TestDirMigratesLegacyHome(t *testing.T) {
	base := t.TempDir()
	legacy := filepath.Join(base, ".lato")
	if err := os.MkdirAll(filepath.Join(legacy, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacyCfg := "model:\n  provider: ollama\n  endpoint: http://localhost:11434\n  name: legacy-model\nagent:\n  name: default\n  system_prompt: legacy\n"
	if err := os.WriteFile(filepath.Join(legacy, "config.yaml"), []byte(legacyCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "skills", "old-skill.md"), []byte("# Old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LATO_HOME", "")
	t.Setenv("HOME", base)                              // Linux/macOS
	t.Setenv("XDG_CONFIG_HOME", "")                     // reset Linux override
	t.Setenv("USERPROFILE", base)                       // Windows
	t.Setenv("AppData", filepath.Join(base, "appdata")) // Windows UserConfigDir

	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("migrated config unreadable: %v", err)
	}
	if string(got) != legacyCfg {
		t.Errorf("migrated config was rewritten:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "old-skill.md")); err != nil {
		t.Errorf("legacy skill not migrated: %v", err)
	}

	// The legacy home must survive migration untouched.
	if _, err := os.Stat(filepath.Join(legacy, "config.yaml")); err != nil {
		t.Errorf("legacy home was modified during migration: %v", err)
	}

	// Migration is idempotent and never overwrites newer files.
	fresh := "model:\n  provider: lmstudio\n  endpoint: http://localhost:1234\n  name: new-model\nagent:\n  name: default\n  system_prompt: new\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(fresh), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Dir(); err != nil {
		t.Fatalf("second Dir() error = %v", err)
	}
	got, err = os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != fresh {
		t.Error("migration overwrote an existing config file")
	}
}

// TestLATOHomeOverrideIsHonored verifies the escape hatch used by tests
// and portable installs: LATO_HOME wins over the platform location.
func TestLATOHomeOverrideIsHonored(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-lato")
	t.Setenv("LATO_HOME", custom)

	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	if dir != custom {
		t.Errorf("Dir() = %q, want %q", dir, custom)
	}
}

// TestEffectiveLimitsMaxToolOutput verifies the max tool output limit is
// normalized correctly with defaults for zero/negative values.
func TestEffectiveLimitsMaxToolOutput(t *testing.T) {
	cfg := &Config{Limits: Limits{MaxToolOutput: 123}}
	if cfg.EffectiveLimits().MaxToolOutput != 123 {
		t.Fatalf("expected 123, got %d", cfg.EffectiveLimits().MaxToolOutput)
	}

	cfg = &Config{Limits: Limits{MaxToolOutput: 0}}
	if cfg.EffectiveLimits().MaxToolOutput != 64<<10 {
		t.Fatalf("expected default 64 KiB, got %d", cfg.EffectiveLimits().MaxToolOutput)
	}

	cfg = &Config{Limits: Limits{MaxToolOutput: -5}}
	if cfg.EffectiveLimits().MaxToolOutput != 64<<10 {
		t.Fatalf("expected default 64 KiB for negative, got %d", cfg.EffectiveLimits().MaxToolOutput)
	}
}

// TestEffectiveLimitsContextBudget verifies the Phase 3A history byte
// budget is normalized with a default for zero/negative values, so an
// unset or hand-edited key can never silently mean "unlimited".
func TestEffectiveLimitsContextBudget(t *testing.T) {
	cfg := &Config{Limits: Limits{ContextBudget: 4096}}
	if got := cfg.EffectiveLimits().ContextBudget; got != 4096 {
		t.Fatalf("explicit ContextBudget = %d, want 4096", got)
	}

	for _, set := range []int{0, -5} {
		cfg = &Config{Limits: Limits{ContextBudget: set}}
		if got := cfg.EffectiveLimits().ContextBudget; got != 128<<10 {
			t.Fatalf("ContextBudget %d = %d, want default %d", set, got, 128<<10)
		}
	}
}

// TestEffectiveLimitsMaxHistoryTurns verifies the Phase 3A turn-count
// budget is normalized with a default for zero/negative values.
func TestEffectiveLimitsMaxHistoryTurns(t *testing.T) {
	cfg := &Config{Limits: Limits{MaxHistoryTurns: 3}}
	if got := cfg.EffectiveLimits().MaxHistoryTurns; got != 3 {
		t.Fatalf("explicit MaxHistoryTurns = %d, want 3", got)
	}

	for _, set := range []int{0, -2} {
		cfg = &Config{Limits: Limits{MaxHistoryTurns: set}}
		if got := cfg.EffectiveLimits().MaxHistoryTurns; got != 20 {
			t.Fatalf("MaxHistoryTurns %d = %d, want default 20", set, got)
		}
	}
}

// TestEffectiveLimitsLeaveUnsetHistoryKeysForTrim pins that the two
// Phase 3A budgets default independently of the Phase 2B/2E limits: the
// yaml keys are omitempty, so a config that sets only the older limits
// must still arrive with both history bounds applied.
func TestEffectiveLimitsLeaveUnsetHistoryKeysForTrim(t *testing.T) {
	cfg := &Config{Limits: Limits{MaxToolCalls: 7, MaxToolOutput: 999}}
	l := cfg.EffectiveLimits()
	if l.ContextBudget != 128<<10 {
		t.Errorf("ContextBudget = %d, want default %d", l.ContextBudget, 128<<10)
	}
	if l.MaxHistoryTurns != 20 {
		t.Errorf("MaxHistoryTurns = %d, want default 20", l.MaxHistoryTurns)
	}
	if l.MaxToolCalls != 7 || l.MaxToolOutput != 999 {
		t.Errorf("older limits were disturbed: %+v", l)
	}
}
