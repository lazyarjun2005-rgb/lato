// Package session provides structures and methods for managing user sessions,
// including message handling and conversion to provider-specific formats.
// It defines the Session and Message types, along with a method to convert session messages
// into a format compatible with external providers.
package session

import (
	"fmt"
	"lato/internal/providers"
	"strings"
	"time"
)

// Message represents a single message in a session, including its role, content, and timestamp.
type Message struct {
	Role    string    `json:"role"`
	Content string    `json:"content"`
	Time    time.Time `json:"time"`
}

// Session represents a user session, containing an ID, timestamps for
// creation and updates, a list of messages exchanged during the session,
// and an optional human-readable title. Title is additive schema: older
// session files without it load normally with an empty title.
type Session struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	Messages []Message `json:"messages"`
}

// Rename gives the session a persistent human-readable title and bumps
// UpdatedAt, so renamed sessions surface correctly in recency-sorted
// listings. Persistence happens through the next Save call.
func (s *Session) Rename(title string) {
	s.Title = strings.TrimSpace(title)
	s.UpdatedAt = time.Now()
}

// ClearMessages empties the conversation history while preserving the
// session itself: ID, CreatedAt, Title, and all other fields stay as
// they are; only Messages is reset (to an empty, non-nil slice) and
// UpdatedAt advances. Persistence happens through the next Save call.
func (s *Session) ClearMessages() {
	s.Messages = []Message{}
	s.UpdatedAt = time.Now()
}

// Rewind removes the most recent n conversation turns. A turn starts
// at each persisted user message and extends through any following
// assistant messages, so an incomplete final user turn (a request whose
// response was never persisted) is removed exactly by itself.
//
// Validation happens before any mutation: n must be ≥ 1, the
// conversation must contain at least one turn, and n may not exceed the
// available turn count — a failed call leaves Messages untouched.
// Persistence happens through the next Save call.
func (s *Session) Rewind(n int) error {
	if n < 1 {
		return fmt.Errorf("turn count must be at least 1, got %d", n)
	}

	starts := make([]int, 0, len(s.Messages))
	for i, msg := range s.Messages {
		if msg.Role == "user" {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		return fmt.Errorf("conversation is empty; nothing to rewind")
	}
	if n > len(starts) {
		return fmt.Errorf("cannot rewind %d turns; conversation contains %d turns", n, len(starts))
	}

	cut := starts[len(starts)-n]
	s.Messages = s.Messages[:cut]
	s.UpdatedAt = time.Now()
	return nil
}

// Markdown renders the persisted conversation as a deterministic
// Markdown document: a fixed header, the optional title line, then one
// section per message in conversation order. Only user and assistant
// messages exist in a session; anything unexpected is skipped. No
// credentials, configuration, or runtime state can appear here by
// construction — the renderer reads nothing but this Session.
func (s *Session) Markdown() string {
	var b strings.Builder
	b.WriteString("# Lato Session\n")
	if t := strings.TrimSpace(s.Title); t != "" {
		fmt.Fprintf(&b, "\n**Title:** %s\n", t)
	}
	for _, msg := range s.Messages {
		var role string
		switch msg.Role {
		case "user":
			role = "User"
		case "assistant":
			role = "Assistant"
		default:
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n%s\n", role, strings.TrimRight(msg.Content, "\n"))
	}
	return b.String()
}

// maxExportSlug caps the title-derived part of a default export name so
// pathological titles cannot produce unwieldy paths.
const maxExportSlug = 40

// DefaultExportFilename derives a safe, deterministic export name for
// this session: a sanitized form of the title when one is set,
// otherwise the short session ID. The result never contains path
// separators or other filesystem-dangerous characters.
func (s *Session) DefaultExportFilename() string {
	slug := sanitizeFileSlug(s.Title)
	if slug == "" {
		id := s.ID
		if len(id) > 8 {
			id = id[:8]
		}
		slug = id
	}
	return "lato-session-" + slug + ".md"
}

// sanitizeFileSlug reduces a title to [A-Za-z0-9_-] with runs of other
// characters collapsed to single dashes; everything else (separators,
// unicode, control characters) disappears, so the result is always a
// single safe path element.
func sanitizeFileSlug(title string) string {
	var b strings.Builder
	lastDash := true // leading separators are dropped via trim below
	for _, r := range strings.TrimSpace(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-_")
	if len(out) > maxExportSlug {
		out = strings.TrimRight(out[:maxExportSlug], "-_")
	}
	return out
}

// ProviderMessages converts the session's messages into a slice of providers.Message,
func (s *Session) ProviderMessages() []providers.Message {
	messages := make([]providers.Message, 0, len(s.Messages))

	for _, msg := range s.Messages {
		messages = append(messages, providers.Message{
			Role:    providers.Role(msg.Role),
			Content: msg.Content,
		})
	}

	return messages
}
