// Package userconfig stores Lato's user-level provider connections:
// which providers the user connected with /connect, their endpoints,
// API keys, and cached model lists.
//
// The store lives under the operating system's user configuration
// directory (via os.UserConfigDir): ~/.config/lato on Linux,
// ~/Library/Application Support/Lato on macOS, %AppData%\Lato on
// Windows. It is never stored inside a project repository. The file is
// written with restrictive permissions (0600) because it holds API
// keys; keys are additionally redactable through Redact so they can
// never reach logs, errors, or UI output.
package userconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"lato/internal/providers"
)

// Model is one cached model. IDs are opaque strings exactly as reported
// by the provider (or typed by the user); they are never split or
// rewritten. Custom marks models the user registered by hand via
// /model add — those survive discovery refreshes.
type Model struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Custom bool   `json:"custom,omitempty"`
}

// Connection is one configured provider. Registered providers use their
// registry ID ("openrouter"); custom providers use a slug of the name
// the user typed ("my-provider").
type Connection struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Class    string  `json:"class"` // providers.ClassOllama or ClassOpenAICompatible
	Endpoint string  `json:"endpoint,omitempty"`
	APIKey   string  `json:"api_key,omitempty"` // secret; file is 0600
	Custom   bool    `json:"custom,omitempty"`
	Source   string  `json:"source,omitempty"` // "", "opencode", "claude"
	Models   []Model `json:"models,omitempty"` // cached discovery result
}

// file is the on-disk shape of providers.json.
type file struct {
	Version     int           `json:"version"`
	Connections []*Connection `json:"connections"`
}

// Store holds parsed connections and knows where to persist them. A nil
// *Store behaves as an empty read-only store.
type Store struct {
	path        string
	Connections []*Connection
}

// Dir returns the Lato user-configuration directory, creating it with
// 0700 if needed. It uses os.UserConfigDir, so it resolves correctly on
// Linux, macOS, and Windows without hard-coded paths.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	dir := filepath.Join(base, "lato")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create lato config dir %s: %w", dir, err)
	}
	return dir, nil
}

// Path returns the full path of the connection store file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "providers.json"), nil
}

// Load reads the connection store. A missing file yields an empty store;
// a corrupt file is an error the caller can surface.
func Load() (*Store, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

// LoadFrom reads a store from an explicit path (tests use this).
func LoadFrom(path string) (*Store, error) {
	s := &Store{path: path}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read provider connections: %w", err)
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	s.Connections = f.Connections
	return s, nil
}

// save writes the store back with restrictive permissions. It creates
// parent directories when loading was deferred.
func (s *Store) save() error {
	if s.path == "" {
		p, err := Path()
		if err != nil {
			return err
		}
		s.path = p
	}
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create lato config dir: %w", err)
		}
	}
	out, err := json.MarshalIndent(file{Version: 1, Connections: s.Connections}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal provider connections: %w", err)
	}
	if err := os.WriteFile(s.path, append(out, '\n'), 0o600); err != nil {
		return fmt.Errorf("write provider connections: %w", err)
	}
	return nil
}

// Get returns a copy of the connection with the given ID.
func (s *Store) Get(id string) (Connection, bool) {
	for _, c := range s.safeConnections() {
		if c.ID == id {
			return *c, true
		}
	}
	return Connection{}, false
}

// List returns copies of every connection, ordered by ID.
func (s *Store) List() []Connection {
	all := s.safeConnections()
	out := make([]Connection, 0, len(all))
	for _, c := range all {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Put inserts or updates a connection by ID and persists the store.
func (s *Store) Put(c Connection) error {
	if s == nil {
		return fmt.Errorf("provider connections store is not loaded")
	}
	for _, existing := range s.Connections {
		if existing.ID == c.ID {
			*existing = c
			return s.save()
		}
	}
	s.Connections = append(s.Connections, &c)
	return s.save()
}

// Delete removes a connection by ID.
func (s *Store) Delete(id string) error {
	if s == nil {
		return fmt.Errorf("provider connections store is not loaded")
	}
	kept := s.Connections[:0:0]
	for _, c := range s.Connections {
		if c.ID != id {
			kept = append(kept, c)
		}
	}
	s.Connections = kept
	return s.save()
}

// safeConnections materializes the slice so a zero-value Store or JSON
// file without connections still reads safely.
func (s *Store) safeConnections() []*Connection {
	if s == nil {
		return nil
	}
	return s.Connections
}

// AddCustomModel registers a user-supplied model ID under an already
// configured provider. The ID is stored verbatim — no parsing,
// normalization, or splitting. The display name defaults to the model
// ID when empty, and a model with the same ID is never added twice.
func (s *Store) AddCustomModel(providerID, modelID, name string) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return fmt.Errorf("model ID cannot be empty")
	}
	c, ok := s.Get(providerID)
	if !ok {
		return fmt.Errorf("provider %q is not configured; run /connect first", providerID)
	}
	for _, m := range c.Models {
		if m.ID == modelID {
			return fmt.Errorf("model %q is already registered with %s", modelID, c.Name)
		}
	}
	if name = strings.TrimSpace(name); name == "" {
		name = modelID
	}
	c.Models = append(c.Models, Model{ID: modelID, Name: name, Custom: true})
	return s.Put(c)
}

// MergeDiscoveredModels replaces a provider's cached discovery results
// while preserving manually registered models. A custom model whose ID
// now appears in live discovery is kept once (as discovered) rather
// than duplicated.
func (s *Store) MergeDiscoveredModels(id string, discovered []Model) error {
	c, ok := s.Get(id)
	if !ok {
		return fmt.Errorf("provider %q is not configured; run /connect first", id)
	}

	seen := make(map[string]bool, len(discovered))
	fresh := make([]Model, 0, len(discovered)+len(c.Models))
	for _, m := range discovered {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		fresh = append(fresh, Model{ID: m.ID, Name: m.Name})
	}
	for _, m := range c.Models {
		if m.Custom && !seen[m.ID] {
			seen[m.ID] = true
			fresh = append(fresh, m)
		}
	}
	c.Models = fresh
	return s.Put(c)
}

// CustomNameToID converts a user-typed custom provider name into a
// stable slug ID ("My Provider!" → "my-provider").
func CustomNameToID(name string) string {
	id := make([]rune, 0, len(name))
	dash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			id = append(id, r)
			dash = false
		case r >= 'A' && r <= 'Z':
			id = append(id, r+('a'-'A'))
			dash = false
		default:
			if !dash && len(id) > 0 {
				id = append(id, '-')
				dash = true
			}
		}
	}
	for len(id) > 0 && id[len(id)-1] == '-' {
		id = id[:len(id)-1]
	}
	if len(id) == 0 {
		return "custom"
	}
	return string(id)
}

// Resolved is the effective endpoint/key pair for a provider after
// applying the precedence rules.
type Resolved struct {
	Endpoint string
	APIKey   string
	Found    bool // true when level 1/2 (explicit or imported config) supplied values
}

// Resolve applies Lato's deterministic credential precedence:
//
//  1. explicit user configuration (/connect) — the saved connection
//  2. imported configuration — also a saved connection (source-tagged)
//  3. environment variables named by the registry entry
//  4. provider defaults (registry endpoint, no key)
//
// fallbackEndpoint carries the config.yaml endpoint for providers with
// no saved connection.
func Resolve(s *Store, providerID, fallbackEndpoint string) Resolved {
	r := Resolved{Endpoint: fallbackEndpoint}
	if c, ok := s.Get(providerID); ok {
		if c.Endpoint != "" {
			r.Endpoint = c.Endpoint
		}
		r.APIKey = c.APIKey
		r.Found = true
		return r
	}
	if info, ok := providers.ByID(providerID); ok && info.APIKeyEnv != "" {
		r.APIKey = os.Getenv(info.APIKeyEnv)
	}
	return r
}
