// Package config loads Lato's YAML configuration file.
//
// Config lives under Lato's user configuration directory —
// ~/.config/lato on Linux, ~/Library/Application Support/lato on macOS,
// %AppData%\Lato on Windows (overridable with LATO_HOME). If config.yaml
// doesn't exist, Load writes a sensible default file so a first-time
// user always has something working to edit.
package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"lato/internal/effort"
	"lato/internal/providers"
)

// legacyHomeName is the pre-M14 configuration directory directly under
// the user's home. It is read once for migration and never deleted.
const legacyHomeName = ".lato"

// Model describes how to reach a local model provider.
type Model struct {
	Provider string `yaml:"provider"`
	Endpoint string `yaml:"endpoint"`
	Name     string `yaml:"name"`
	Effort   string `yaml:"effort,omitempty"` // low|medium|high|ultra|lato-x
	APIKey   string `yaml:"-"`
}

// Agent describes the default agent's identity and base system prompt.
type Agent struct {
	Name         string `yaml:"name"`
	SystemPrompt string `yaml:"system_prompt"`
}

// Config is the top-level shape of config.yaml.
type Config struct {
	Model Model `yaml:"model"`
	Agent Agent `yaml:"agent"`
}

const defaultConfigTemplate = `model:
  provider: ollama
  endpoint: http://localhost:11434
  name: llama3
  # Agent effort: low | medium | high | ultra | lato-x (optional)
  # effort: high

agent:
  name: default
  system_prompt: |
    You are a helpful coding assistant.
`

// Dir returns Lato's user configuration directory, creating it with
// restrictive permissions if necessary.
//
// The directory follows the operating system convention, resolved via
// os.UserConfigDir: ~/.config/lato on Linux, ~/Library/Application
// Support/lato on macOS, and %AppData%\Lato on Windows. The LATO_HOME
// environment variable overrides the platform location entirely, which
// keeps portable setups and tests hermetic. A legacy ~/.lato home is
// migrated once by copying (never rewriting or deleting) its contents.
func Dir() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("LATO_HOME")); custom != "" {
		// An explicit location is taken literally: portable setups and
		// tests must not have legacy data pulled in behind their back.
		abs, err := filepath.Abs(custom)
		if err != nil {
			return "", fmt.Errorf("resolve LATO_HOME %q: %w", custom, err)
		}
		if err := os.MkdirAll(abs, 0o700); err != nil {
			return "", fmt.Errorf("create lato config dir %s: %w", abs, err)
		}
		return abs, nil
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	dir := filepath.Join(base, "lato")

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create lato config dir %s: %w", dir, err)
	}
	if err := migrateLegacyHome(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// migrateLegacyHome copies a pre-M14 ~/.lato home into the platform
// configuration directory, once, without touching anything that already
// exists there. The legacy directory is left in place so an old binary
// keeps working; nothing is ever rewritten or deleted. Migration is
// best-effort for skills: a failure to copy one skill file does not stop
// startup, but a failure to provide config.yaml is reported.
func migrateLegacyHome(dir string) error {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil // no home directory: nothing to migrate
	}
	legacy := filepath.Join(home, legacyHomeName)
	if legacy == dir {
		return nil // someone points LATO_HOME at the legacy location
	}
	if info, err := os.Stat(legacy); err != nil || !info.IsDir() {
		return nil // no legacy home
	}

	migrated := false

	cfgDst := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(cfgDst); os.IsNotExist(err) {
		if _, statErr := os.Stat(filepath.Join(legacy, "config.yaml")); statErr == nil {
			if err := copyFile(filepath.Join(legacy, "config.yaml"), cfgDst, 0o600); err != nil {
				return fmt.Errorf("migrate legacy config: %w", err)
			}
			migrated = true
		}
	}

	skillsMigrated, err := migrateLegacyDir(filepath.Join(legacy, "skills"), filepath.Join(dir, "skills"))
	if err != nil {
		return err
	}

	if migrated || skillsMigrated {
		fmt.Printf("Migrated Lato configuration from %s to %s\n", legacy, dir)
	}
	return nil
}

// migrateLegacyDir copies files from src to dst that do not exist in
// dst yet. It reports whether anything was copied.
func migrateLegacyDir(src, dst string) (bool, error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read legacy directory %s: %w", src, err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return false, fmt.Errorf("create %s: %w", dst, err)
	}

	copied := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		target := filepath.Join(dst, e.Name())
		if _, err := os.Stat(target); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return copied, fmt.Errorf("stat %s: %w", target, err)
		}
		if err := copyFile(filepath.Join(src, e.Name()), target, 0o644); err != nil {
			return copied, err
		}
		copied = true
	}
	return copied, nil
}

// copyFile copies src to dst when src exists. A missing source is not
// an error; the destination is simply left untouched.
func copyFile(src, dst string, perm os.FileMode) error {
	raw, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer raw.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		if os.IsExist(err) {
			return nil // never overwrite what is already in place
		}
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, raw); err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	return nil
}

// Path returns the path to config.yaml under the Lato home directory.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// Load reads config.yaml, creating it with default contents on first run.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(defaultConfigTemplate), 0o644); err != nil {
			return nil, fmt.Errorf("write default config to %s: %w", path, err)
		}
		fmt.Printf("Created default config at %s\n", path)
	} else if err != nil {
		return nil, fmt.Errorf("stat config file %s: %w", path, err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config at %s: %w", path, err)
	}

	// Effort is optional; empty keeps the balanced default. A typo is
	// reported with the valid values rather than silently reinterpreted.
	if cfg.Model.Effort != "" {
		level, err := effort.Parse(cfg.Model.Effort)
		if err != nil {
			return nil, fmt.Errorf("invalid config at %s: model.effort: %w", path, err)
		}
		cfg.Model.Effort = level.String()
	}

	// Source the API key from the environment, using the environment
	// variable declared by the active provider's registry entry. Keys
	// are never persisted (APIKey is tagged yaml:"-") and never
	// printed. Unregistered provider IDs keep the legacy NVIDIA_API_KEY
	// lookup so hand-edited configs keep working.
	cfg.Model.APIKey = resolveAPIKey(cfg.Model.Provider)

	return &cfg, nil
}

// resolveAPIKey reads the active provider's key from its declared
// environment variable (e.g. OPENROUTER_API_KEY for openrouter,
// NINEROUTER_KEY for 9router). Registered local providers declare no
// environment variable and yield "".
func resolveAPIKey(providerID string) string {
	if info, ok := providers.ByID(providerID); ok {
		if info.APIKeyEnv == "" {
			return ""
		}
		return os.Getenv(info.APIKeyEnv)
	}
	return os.Getenv("NVIDIA_API_KEY")
}

// Save writes the config back to config.yaml. APIKey is never
// round-tripped (it's tagged yaml:"-" and always sourced from the
// environment), so it's never written to disk.
func (c *Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}

	out, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write config file %s: %w", path, err)
	}
	return nil
}

// validate performs minimal sanity checks so failures surface early with a
// clear message instead of deep inside an HTTP call.
func (c *Config) validate() error {
	if c.Model.Provider == "" {
		return fmt.Errorf("model.provider is required (e.g. \"ollama\")")
	}
	if c.Model.Endpoint == "" {
		return fmt.Errorf("model.endpoint is required (e.g. \"http://localhost:11434\")")
	}
	if c.Model.Name == "" {
		return fmt.Errorf("model.name is required (e.g. \"llama3\")")
	}
	return nil
}
