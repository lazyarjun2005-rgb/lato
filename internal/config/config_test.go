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
