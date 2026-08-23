package session

import (
	"strings"
	"testing"
	"time"
)

func turnsFixture() *Session {
	s := New()
	s.Rename("Rewind Target")
	s.AddMessage("user", "question A")
	s.AddMessage("assistant", "answer A")
	s.AddMessage("user", "question B")
	s.AddMessage("assistant", "answer B")
	return s
}

func roles(s *Session) []string {
	out := make([]string, 0, len(s.Messages))
	for _, m := range s.Messages {
		out = append(out, m.Role[0:1]+":"+m.Content)
	}
	return out
}

// TestRewindOneCompletedTurn: the trailing user+assistant pair goes;
// earlier turns survive untouched.
func TestRewindOneCompletedTurn(t *testing.T) {
	s := turnsFixture()

	if err := s.Rewind(1); err != nil {
		t.Fatalf("Rewind(1) error = %v", err)
	}
	got := strings.Join(roles(s), ", ")
	want := "u:question A, a:answer A"
	if got != want {
		t.Errorf("after rewind 1 = [%s], want [%s]", got, want)
	}
}

// TestRewindMultipleTurns removes whole turn groups at once.
func TestRewindMultipleTurns(t *testing.T) {
	s := turnsFixture()

	if err := s.Rewind(2); err != nil {
		t.Fatalf("Rewind(2) error = %v", err)
	}
	if len(s.Messages) != 0 {
		t.Errorf("after rewind 2 = [%s], want empty", strings.Join(roles(s), ", "))
	}
}

// TestRewindIncompleteFinalTurn pins the boundary rule: an unanswered
// final request is removed by itself — never the preceding assistant.
func TestRewindIncompleteFinalTurn(t *testing.T) {
	s := turnsFixture()
	s.AddMessage("user", "question C") // assistant response never persisted

	if err := s.Rewind(1); err != nil {
		t.Fatalf("Rewind(1) error = %v", err)
	}
	got := strings.Join(roles(s), ", ")
	want := "u:question A, a:answer A, u:question B, a:answer B"
	if got != want {
		t.Errorf("after rewinding incomplete turn = [%s], want [%s]", got, want)
	}
}

// TestRewindValidationErrors covers zero/negative/over-count requests:
// each must fail without touching Messages.
func TestRewindValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		n       int
		wantErr string
	}{
		{"zero", 0, "at least 1"},
		{"negative", -1, "at least 1"},
		{"over count", 3, "cannot rewind 3 turns; conversation contains 2 turns"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := turnsFixture()
			before := roles(s)

			err := s.Rewind(tc.n)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
			if got := strings.Join(roles(s), ", "); got != strings.Join(before, ", ") {
				t.Errorf("messages mutated on failed rewind: [%s]", got)
			}
		})
	}
}

// TestRewindEmptyConversation: nothing to rewind is a clean refusal.
func TestRewindEmptyConversation(t *testing.T) {
	s := New()
	if err := s.Rewind(1); err == nil || !strings.Contains(err.Error(), "nothing to rewind") {
		t.Fatalf("error = %v", err)
	}
	if len(s.Messages) != 0 {
		t.Errorf("empty session mutated: %+v", s.Messages)
	}
}

// TestRewindPreservesIdentityAndPersists: ID/CreatedAt/Title survive,
// UpdatedAt advances, and Save→Load returns exactly the rewound history
// whose ProviderMessages carry only remaining turns.
func TestRewindPreservesIdentityAndPersists(t *testing.T) {
	isolateSessionDir(t)

	s := turnsFixture()
	id, created, title := s.ID, s.CreatedAt, s.Title
	before := s.UpdatedAt
	time.Sleep(2 * time.Millisecond)

	if err := s.Rewind(1); err != nil {
		t.Fatal(err)
	}
	if s.ID != id || !s.CreatedAt.Equal(created) || s.Title != title {
		t.Errorf("identity changed: id=%q created=%v title=%q", s.ID, s.CreatedAt, s.Title)
	}
	if !s.UpdatedAt.After(before) {
		t.Errorf("UpdatedAt = %v, want advanced past %v", s.UpdatedAt, before)
	}

	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(id)
	if err != nil {
		t.Fatal(err)
	}
	pm := reloaded.ProviderMessages()
	if len(pm) != 2 || pm[len(pm)-1].Content != "answer A" {
		t.Fatalf("persisted provider messages = %+v", pm)
	}
	for _, m := range pm {
		if strings.Contains(m.Content, "question B") || strings.Contains(m.Content, "answer B") {
			t.Errorf("rewound message returned after reload: %+v", pm)
		}
	}
}
