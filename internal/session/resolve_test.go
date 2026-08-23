package session

import (
	"strings"
	"testing"
)

func resolveFixture() []Session {
	return []Session{
		{ID: "8f31c2a1-1111-2222-3333-444444444444", Title: "Demo Authentication Project"},
		{ID: "8f31ffff-1111-2222-3333-444444444444", Title: "College AI Project"},
		{ID: "aaaaaaaa-1111-2222-3333-444444444444", Title: "College AI Project"}, // duplicate title
		{ID: "bbbbbbbb-1111-2222-3333-444444444444"},                              // legacy untitled
	}
}

// TestResolveSessionOrder pins the mandated resolution order: exact ID
// beats everything, unique prefix next, exact title last.
func TestResolveSessionOrder(t *testing.T) {
	sessions := resolveFixture()

	// Exact full ID.
	got, err := ResolveSession("aaaaaaaa-1111-2222-3333-444444444444", sessions)
	if err != nil || got.ID != "aaaaaaaa-1111-2222-3333-444444444444" {
		t.Fatalf("exact ID: got %v, %v", got, err)
	}

	// Unique prefix — note it shares the "8f31" stem with the other ID,
	// so the longer distinguishing prefix is required.
	got, err = ResolveSession("8f31c2a1", sessions)
	if err != nil || got.Title != "Demo Authentication Project" {
		t.Fatalf("unique prefix: got %+v, %v", got, err)
	}

	// Exact title (single holder).
	got, err = ResolveSession("Demo Authentication Project", sessions)
	if err != nil || got.ID != "8f31c2a1-1111-2222-3333-444444444444" {
		t.Fatalf("exact title: got %+v, %v", got, err)
	}

	// Whitespace around the input is tolerated; matching stays exact.
	got, err = ResolveSession("  College AI Project  ", sessions)
	if err == nil {
		t.Errorf("trimmed-but-duplicated title resolved to %+v; want ambiguity error", got)
	}
}

// TestResolveSessionAmbiguityNeverGuesses covers both ambiguity shapes:
// shared ID prefixes and duplicate titles.
func TestResolveSessionAmbiguityNeverGuesses(t *testing.T) {
	sessions := resolveFixture()

	_, err := ResolveSession("8f31", sessions) // two IDs share this stem
	if err == nil || !strings.Contains(err.Error(), "do not guess") ||
		!strings.Contains(err.Error(), "/sessions") {
		t.Fatalf("prefix ambiguity error = %v", err)
	}
	if !strings.Contains(err.Error(), "8f31c2a1") || !strings.Contains(err.Error(), "8f31ffff") {
		t.Errorf("candidates not listed:\n%v", err)
	}

	_, err = ResolveSession("College AI Project", sessions) // duplicate title
	if err == nil || !strings.Contains(err.Error(), "2 sessions match") {
		t.Fatalf("title ambiguity error = %v", err)
	}
}

// TestResolveSessionNotFoundAndEmpty: clear failures, no guessing.
func TestResolveSessionNotFoundAndEmpty(t *testing.T) {
	sessions := resolveFixture()

	_, err := ResolveSession("does-not-exist", sessions)
	if err == nil || !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("not-found error = %v", err)
	}

	_, err = ResolveSession("   ", sessions)
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("empty-input error = %v", err)
	}
}

// TestResolveSessionLegacyUntitledByID: legacy sessions without titles
// remain resolvable by ID and never match an arbitrary non-empty input
// through their empty Title field.
func TestResolveSessionLegacyUntitledByID(t *testing.T) {
	sessions := resolveFixture()

	got, err := ResolveSession("bbbbbbbb", sessions)
	if err != nil || got.ID != "bbbbbbbb-1111-2222-3333-444444444444" {
		t.Fatalf("legacy by prefix: got %+v, %v", got, err)
	}
}

// TestResolveIsReadOnly: resolution must not mutate the catalog.
func TestResolveIsReadOnly(t *testing.T) {
	sessions := resolveFixture()
	titlesBefore := make([]string, len(sessions))
	for i, s := range sessions {
		titlesBefore[i] = s.ID + "|" + s.Title
	}

	if _, err := ResolveSession("Demo Authentication Project", sessions); err != nil {
		t.Fatal(err)
	}
	for i, s := range sessions {
		if got := s.ID + "|" + s.Title; got != titlesBefore[i] {
			t.Fatalf("resolver modified entry %d: %q → %q", i, titlesBefore[i], got)
		}
	}
}
