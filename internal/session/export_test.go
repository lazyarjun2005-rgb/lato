package session

import (
	"strings"
	"testing"
)

func multiTurnSession() *Session {
	s := New()
	s.Rename("Auth Debugging")
	s.AddMessage("user", "first question")
	s.AddMessage("assistant", "first answer")
	s.AddMessage("user", "second question")
	return s
}

// TestMarkdownRendersConversationInOrder pins the document format and
// ordering: header, title line, then one section per persisted message.
func TestMarkdownRendersConversationInOrder(t *testing.T) {
	doc := multiTurnSession().Markdown()

	if !strings.HasPrefix(doc, "# Lato Session\n") {
		t.Errorf("missing main heading:\n%s", doc)
	}
	if !strings.Contains(doc, "**Title:** Auth Debugging") {
		t.Errorf("title line missing:\n%s", doc)
	}
	u1 := strings.Index(doc, "## User")
	a1 := strings.Index(doc, "## Assistant")
	u2 := strings.LastIndex(doc, "## User")
	if u1 == -1 || a1 == -1 || u2 == -1 || !(u1 < a1 && a1 < u2) {
		t.Errorf("sections out of order:\n%s", doc)
	}
	for _, want := range []string{"first question", "first answer", "second question"} {
		if !strings.Contains(doc, want) {
			t.Errorf("content %q missing:\n%s", want, doc)
		}
	}
}

// TestMarkdownUntitledOmitsTitleLine keeps untitled exports clean.
func TestMarkdownUntitledOmitsTitleLine(t *testing.T) {
	s := New()
	s.AddMessage("user", "hello")

	doc := s.Markdown()
	if strings.Contains(doc, "Title:") {
		t.Errorf("untitled export contains a title line:\n%s", doc)
	}
	if !strings.Contains(doc, "## User") || !strings.Contains(doc, "hello") {
		t.Errorf("conversation content missing:\n%s", doc)
	}
}

// TestMarkdownSkipsUnknownRoles is the defensive rule: only known
// conversation roles are rendered.
func TestMarkdownSkipsUnknownRoles(t *testing.T) {
	s := New()
	s.Messages = append(s.Messages,
		Message{Role: "system-ish", Content: "internal noise"},
		Message{Role: "user", Content: "real"},
	)

	doc := s.Markdown()
	if strings.Contains(doc, "internal noise") {
		t.Errorf("unknown role leaked into export:\n%s", doc)
	}
	if !strings.Contains(doc, "real") {
		t.Errorf("known content lost:\n%s", doc)
	}
}

// TestDefaultExportFilenameFromTitle: safe slug from a messy title.
func TestDefaultExportFilenameFromTitle(t *testing.T) {
	s := New()
	s.Title = `My Auth/Debug: "v2"? <final>`
	got := s.DefaultExportFilename()
	want := "lato-session-My-Auth-Debug-v2-final.md"
	if got != want {
		t.Errorf("filename = %q, want %q", got, want)
	}
}

// TestDefaultExportFilenameFallbackToID: no usable title → short ID.
func TestDefaultExportFilenameFallbackToID(t *testing.T) {
	s := New()
	s.ID = "01234567-rest-of-uuid"

	fallback := &Session{ID: "01234567-rest-of-uuid"}
	if got := fallback.DefaultExportFilename(); got != "lato-session-01234567.md" {
		t.Errorf("fallback filename = %q", got)
	}

	// A unicode-only title sanitizes to nothing → ID fallback.
	s.Title = "会議のメモ"
	if got := s.DefaultExportFilename(); got != "lato-session-"+s.ID[:8]+".md" {
		t.Errorf("unicode-title filename = %q, want ID fallback", got)
	}
}

// TestDefaultExportFilenameLengthCap: pathological titles stay bounded.
func TestDefaultExportFilenameLengthCap(t *testing.T) {
	s := New()
	s.Title = strings.Repeat("word ", 30)

	got := s.DefaultExportFilename()
	slug := strings.TrimSuffix(strings.TrimPrefix(got, "lato-session-"), ".md")
	if len(slug) > maxExportSlug {
		t.Errorf("slug length = %d, want ≤ %d (%q)", len(slug), maxExportSlug, slug)
	}
	if strings.HasSuffix(slug, "-") || strings.HasSuffix(slug, "_") {
		t.Errorf("slug ends with separator: %q", slug)
	}
}

// TestDefaultExportFilenameNeverContainsSeparators: path-dangerous
// characters can never survive into the single filename element.
func TestDefaultExportFilenameNeverContainsSeparators(t *testing.T) {
	dangerous := []string{"../../etc/passwd", "a/b\\c", "..", "con::x|*?.md"}
	for _, title := range dangerous {
		s := &Session{ID: "abcdefgh1234", Title: title}
		got := s.DefaultExportFilename()
		if strings.ContainsAny(got, "/\\:*?\"<>|") {
			t.Errorf("title %q produced unsafe filename %q", title, got)
		}
		if strings.Contains(got, "..") {
			t.Errorf("title %q produced traversal in %q", title, got)
		}
	}
}
