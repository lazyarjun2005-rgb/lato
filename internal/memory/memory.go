// Package memory provides persistent, project-specific memory for Lato:
// small durable facts about the workspace that survive restarts and are
// injected into prompts only when relevant to the current request.
//
// Storage is local-first and lives under the operating system's user
// configuration directory (~/.config/lato/memory on Linux,
// %AppData%\Lato\memory on Windows), keyed by a stable hash of the
// project's absolute root so two projects never share memory. The store
// is bounded in entry count and size, rejects obvious credentials, and
// ranks retrieval deterministically by lexical relevance.
package memory

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Kind distinguishes who supplied a fact. User-provided memory is never
// silently overwritten by weaker discovered evidence.
type Kind string

const (
	// KindUser marks facts the human entered explicitly.
	KindUser Kind = "user"
	// KindDiscovered marks facts inferred from repository inspection.
	KindDiscovered Kind = "discovered"
)

// Bounded-memory limits. These keep memory useful without letting it
// grow into an uncontrolled dump or bloat model prompts.
const (
	maxEntries      = 64  // stored entries per project
	maxEntryChars   = 400 // characters per fact
	maxInjectCount  = 8   // entries per prompt injection
	maxInjectBytes  = 2048
	minTokenLength  = 2
	duplicateAction = "update"
)

var validCategories = map[string]bool{
	"architecture": true,
	"technology":   true,
	"convention":   true,
	"command":      true,
	"structure":    true,
	"decision":     true,
	"fact":         true,
}

// Entry is one remembered project fact.
type Entry struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Category  string    `json:"category"`
	Kind      Kind      `json:"kind"` // user | discovered
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store holds one project's memories. A nil *Store behaves as an empty,
// read-only store.
type Store struct {
	path      string
	ProjectID string  `json:"project_id"`
	NextID    int     `json:"next_id"`
	Entries   []Entry `json:"entries"`
}

// ProjectID derives a stable, path-leak-free identity from the
// workspace root: a SHA-256 prefix of the normalized absolute path.
// Distinct projects never share memory, even when their directory names
// match; case is folded on Windows where paths are case-insensitive.
func ProjectID(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	abs = filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:16]
}

// dir returns the memory directory under Lato's user configuration.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	p := filepath.Join(base, "lato", "memory")
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", fmt.Errorf("create memory directory: %w", err)
	}
	return p, nil
}

// Load opens the memory store for a project, creating an empty one when
// no file exists yet.
func Load(projectID string) (*Store, error) {
	d, err := Dir()
	if err != nil {
		return nil, err
	}
	return LoadFrom(filepath.Join(d, projectID+".json"), projectID)
}

// LoadFrom opens a store at an explicit path (tests use this).
func LoadFrom(path, projectID string) (*Store, error) {
	s := &Store{path: path, ProjectID: projectID}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read project memory: %w", err)
	}
	if err := json.Unmarshal(raw, s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if s.ProjectID == "" {
		s.ProjectID = projectID
	}
	return s, nil
}

// save persists the store with restrictive permissions; the file lives
// outside any repository.
func (s *Store) save() error {
	if s.ProjectID == "" {
		return errors.New("memory store has no project identity; cannot persist")
	}
	if s.path == "" {
		d, err := Dir()
		if err != nil {
			return err
		}
		s.path = filepath.Join(d, s.ProjectID+".json")
	}
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal memory: %w", err)
	}
	return os.WriteFile(s.path, out, 0o600)
}

// Add remembers a fact. Exact duplicates (normalized content) update
// the existing entry instead of adding a second copy; a discovered fact
// is superseded when the user states the same thing explicitly. Content
// that looks like a credential is rejected.
func (s *Store) Add(content, category string, kind Kind) (Entry, error) {
	content = strings.TrimSpace(content)
	switch {
	case s == nil:
		return Entry{}, errors.New("project memory store is not available")
	case content == "":
		return Entry{}, errors.New("memory content cannot be empty")
	case len(content) > maxEntryChars:
		return Entry{}, fmt.Errorf("memory content is %d characters; limit is %d", len(content), maxEntryChars)
	}
	if err := rejectSecrets(content); err != nil {
		return Entry{}, err
	}
	category = normalizeCategory(category)

	key := normalizeForDedupe(content)
	for i := range s.Entries {
		e := &s.Entries[i]
		if normalizeForDedupe(e.Content) != key {
			continue
		}
		if kind == KindUser && e.Kind != KindUser {
			// User evidence supersedes weaker discovered evidence.
			e.Content = content
			e.Category = category
			e.Kind = KindUser
			e.UpdatedAt = time.Now().UTC()
			return *e, s.save()
		}
		e.UpdatedAt = time.Now().UTC()
		return *e, s.save()
	}

	if len(s.Entries) >= maxEntries {
		return Entry{}, fmt.Errorf("project memory is full (%d entries); remove some with /memory remove", maxEntries)
	}

	now := time.Now().UTC()
	s.NextID++
	e := Entry{
		ID:        newID(),
		Content:   content,
		Category:  category,
		Kind:      kind,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.Entries = append(s.Entries, e)
	return e, s.save()
}

// Update replaces the content of one entry by ID (exact or unambiguous
// prefix). The entry keeps its kind unless upgraded to user.
func (s *Store) Update(idOrPrefix, content string) (Entry, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Entry{}, errors.New("memory content cannot be empty")
	}
	if len(content) > maxEntryChars {
		return Entry{}, fmt.Errorf("memory content is %d characters; limit is %d", len(content), maxEntryChars)
	}
	if err := rejectSecrets(content); err != nil {
		return Entry{}, err
	}

	i, err := s.resolve(idOrPrefix)
	if err != nil {
		return Entry{}, err
	}
	e := &s.Entries[i]
	e.Content = content
	e.UpdatedAt = time.Now().UTC()
	return *e, s.save()
}

// Promote upgrades an existing entry to user-provided.
func (s *Store) Promote(idOrPrefix string) (Entry, error) {
	i, err := s.resolve(idOrPrefix)
	if err != nil {
		return Entry{}, err
	}
	s.Entries[i].Kind = KindUser
	s.Entries[i].UpdatedAt = time.Now().UTC()
	return s.Entries[i], s.save()
}

// Remove deletes one entry by ID (exact or unambiguous prefix).
func (s *Store) Remove(idOrPrefix string) error {
	i, err := s.resolve(idOrPrefix)
	if err != nil {
		return err
	}
	s.Entries = append(s.Entries[:i], s.Entries[i+1:]...)
	return s.save()
}

// Clear removes every entry for the project.
func (s *Store) Clear() (int, error) {
	n := len(s.Entries)
	s.Entries = nil
	return n, s.save()
}

// List returns all entries, oldest first.
func (s *Store) List() []Entry {
	out := make([]Entry, len(s.Entries))
	copy(out, s.Entries)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// resolve finds one entry by exact ID or unique prefix.
func (s *Store) resolve(idOrPrefix string) (int, error) {
	if idOrPrefix == "" {
		return -1, errors.New("memory id cannot be empty")
	}
	matches := []int{}
	for i := range s.Entries {
		id := s.Entries[i].ID
		if id == idOrPrefix || strings.HasPrefix(id, idOrPrefix) {
			matches = append(matches, i)
		}
	}
	switch len(matches) {
	case 0:
		return -1, fmt.Errorf("no memory with id %q", idOrPrefix)
	case 1:
		return matches[0], nil
	default:
		return -1, fmt.Errorf("id %q is ambiguous across %d entries; use more characters", idOrPrefix, len(matches))
	}
}

func newID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("m%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// stopWords are too generic to make memory relevant.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true,
	"is": true, "are": true, "was": true, "to": true, "of": true,
	"in": true, "on": true, "for": true, "with": true, "this": true,
	"that": true, "it": true, "use": true, "uses": true, "using": true,
	"what": true, "do": true, "does": true, "how": true, "my": true,
	"our": true, "we": true, "you": true,
}

func tokenize(s string) map[string]bool {
	tokens := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !('a' <= r && r <= 'z' || '0' <= r && r <= '9')
	}) {
		if len(f) >= minTokenLength && !stopWords[f] {
			tokens[f] = true
		}
	}
	return tokens
}

// Relevant returns up to limit entries lexically related to query,
// ranked by term overlap, then user-kind, then recency. Entries with no
// overlap are excluded entirely — irrelevant memory is never injected.
func (s *Store) Relevant(query string, limit int) []Entry {
	if s == nil || limit <= 0 {
		return nil
	}
	qt := tokenize(query)
	if len(qt) == 0 {
		return nil
	}

	type scored struct {
		e     Entry
		score int
	}
	var hits []scored
	for _, e := range s.Entries {
		ct := tokenize(e.Content + " " + e.Category)
		score := 0
		for t := range qt {
			if ct[t] {
				score += 2
			}
		}
		if score == 0 {
			continue
		}
		hits = append(hits, scored{e: e, score: score})
	}
	if len(hits) == 0 {
		return nil
	}

	sort.Slice(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if (a.e.Kind == KindUser) != (b.e.Kind == KindUser) {
			return a.e.Kind == KindUser
		}
		return a.e.UpdatedAt.After(b.e.UpdatedAt)
	})

	out := make([]Entry, 0, limit)
	total := 0
	for _, h := range hits {
		if len(out) >= limit || total+len(h.e.Content) > maxInjectBytes {
			break
		}
		out = append(out, h.e)
		total += len(h.e.Content)
	}
	return out
}

// RenderBlock formats retrieved entries as a compact prompt block.
func RenderBlock(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Project memory\n")
	total := 0
	for _, e := range entries {
		line := fmt.Sprintf("- [%s] %s", e.Category, e.Content)
		if total+len(line) > maxInjectBytes {
			break
		}
		total += len(line)
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// normalizeCategory maps arbitrary categories onto the known set.
func normalizeCategory(c string) string {
	c = strings.ToLower(strings.TrimSpace(c))
	if c == "" {
		return "fact"
	}
	if validCategories[c] {
		return c
	}
	return "fact"
}

// normalizeForDedupe folds case, whitespace, and trailing punctuation so
// trivial rewordings do not create duplicate facts.
func normalizeForDedupe(content string) string {
	var b strings.Builder
	lastSpace := true // also trims the leading edge
	for _, r := range strings.ToLower(content) {
		switch {
		case r == ' ' || r == '\t' || r == '\n':
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		case r == '.' || r == '!' || r == '?':
			// dropped
		default:
			b.WriteRune(r)
			lastSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

// secretPatterns detect obvious credentials. Matching content is
// rejected outright rather than sanitized, because partial storage of a
// secret is still a leak.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{10,}`),
	regexp.MustCompile(`(?i)\bapi[_-]?key\b\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)\b(password|passwd|secret|token|bearer)\b\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

// rejectSecrets refuses content containing credential-shaped text. The
// matched text itself is never echoed back.
func rejectSecrets(content string) error {
	for _, re := range secretPatterns {
		if re.MatchString(content) {
			return errors.New("memory content looks like a credential and was rejected")
		}
	}
	return nil
}

// ContainsSecret reports whether s looks like credential material.
// Task/session persistence uses this to avoid storing secrets.
func ContainsSecret(s string) bool {
	return rejectSecrets(s) != nil
}

// RedactIfSecret replaces s with a placeholder when it looks like a
// credential; otherwise s is returned unchanged.
func RedactIfSecret(s string) string {
	if ContainsSecret(s) {
		return "[redacted]"
	}
	return s
}
