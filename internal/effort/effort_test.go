package effort

import (
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	cases := map[string]Level{
		"low":     Low,
		"LOW":     Low,
		"medium":  Medium,
		"MEDIUM":  Medium,
		"med":     Medium,
		"default": Medium,
		"high":    High,
		"High":    High,
		"ultra":   Ultra,
		"ULTRA":   Ultra,
		"lato-x":  LatoX,
		"LATO-X":  LatoX,
		"lato_x":  LatoX,
		"latox":   LatoX,
		"x":       LatoX,
	}
	for in, want := range cases {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = (%v, %v), want %v", in, got, err, want)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	for _, in := range []string{"", "max", "extreme", "high-medium", "1", "lato"} {
		if got, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %v, want error", in, got)
		} else if !strings.Contains(err.Error(), "valid:") {
			t.Errorf("Parse(%q) error %q does not name valid choices", in, err)
		}
	}
}

// TestParseTrimsWhitespace pins that surrounding whitespace is accepted,
// matching how users actually type.
func TestParseTrimsWhitespace(t *testing.T) {
	if got, err := Parse("  high "); err != nil || got != High {
		t.Errorf(`Parse("  high ") = (%v, %v), want high`, got, err)
	}
}

func TestLadderOrder(t *testing.T) {
	if !(Low < Medium && Medium < High && High < Ultra && Ultra < LatoX) {
		t.Fatal("ladder order is not strictly increasing")
	}
	if len(All) != 5 {
		t.Fatalf("All = %v, want 5 levels", All)
	}
}

func TestPrevNextClamped(t *testing.T) {
	if Low.Prev() != Low {
		t.Error("Low.Prev must clamp at Low")
	}
	if LatoX.Next() != LatoX {
		t.Error("LatoX.Next must clamp at LatoX")
	}
	if Medium.Next() != High || Medium.Prev() != Low {
		t.Error("Medium neighbors wrong")
	}

	// Walking the full ladder from the bottom visits every level exactly
	// once and terminates at LatoX.
	var got []Level
	for l := Low; ; l = l.Next() {
		got = append(got, l)
		if l == LatoX {
			break
		}
		if len(got) > len(All) {
			t.Fatal("Next() did not terminate at LatoX")
		}
	}
	if len(got) != len(All) {
		t.Errorf("walked ladder = %v, want %v", got, All)
	}
}

func TestStringDisplayNames(t *testing.T) {
	cases := map[Level]string{
		Low:    "low",
		Medium: "medium",
		High:   "high",
		Ultra:  "ultra",
		LatoX:  "lato-X", // exact casing matters to the UI
	}
	for l, want := range cases {
		if got := l.String(); got != want {
			t.Errorf("String(%d) = %q, want %q", int(l), got, want)
		}
	}
}

func TestDefaultAndValidity(t *testing.T) {
	if Default != Medium {
		t.Error("Default must be Medium (pre-M16 behavior)")
	}
	if !Default.IsValid() {
		t.Error("Default not valid")
	}
	// The zero value is deliberately invalid so an uninitialized Level
	// can never silently select a real mode.
	if Level(0).IsValid() || Level(-1).IsValid() || Level(42).IsValid() {
		t.Error("out-of-range levels reported valid")
	}
}
