// Package effort defines Lato's user-facing effort ladder:
//
//	low → medium → high → ultra → lato-X
//
// Effort is not a cosmetic label. A level changes two distinct things,
// and the architecture keeps them separate:
//
//  1. PROVIDER REQUEST CONFIGURATION — whether (and how) the active
//     provider accepts an effort/reasoning parameter, resolved through
//     the provider capability layer in internal/providers. Providers
//     that declare no such mechanism receive no extra request field.
//
//  2. LATO AGENT ORCHESTRATION — the bounds and guidance of the existing
//     M10 loop (turn budget, repetition thresholds, planning/verification
//     directive), resolved by the runtime. This applies on every provider.
//
// The ladder is strictly bounded at every level: higher effort deepens
// orchestration inside M10's safety architecture, it never removes it.
package effort

import (
	"fmt"
	"strings"
)

// Level is one rung of Lato's effort ladder, ordered from fastest to
// most thorough. The numeric order is meaningful: Prev/Next walk it and
// the runtime's orchestration profile scales with it.
type Level int

const (
	Low    Level = iota + 1 // 1: fastest, lightest orchestration
	Medium                  // 2: balanced (the zero-config default)
	High                    // 3: serious coding default recommendation
	Ultra                   // 4: deep bounded agentic mode
	LatoX                   // 5: maximum bounded agentic mode
)

// The zero value of Level is deliberately INVALID: an uninitialized
// Level can never silently select a real mode. Code receiving a
// zero-valued Level must resolve it through profileFor-style fallbacks
// to Default.

// Default is the effort used when nothing else is configured: balanced
// behavior identical to pre-M16 releases.
const Default = Medium

// All lists every level in ladder order (fastest first).
var All = []Level{Low, Medium, High, Ultra, LatoX}

// String returns the display name shown in the UI and header:
// low, medium, high, ultra, lato-X.
func (l Level) String() string {
	switch l {
	case Low:
		return "low"
	case Medium:
		return "medium"
	case High:
		return "high"
	case Ultra:
		return "ultra"
	case LatoX:
		return "lato-X"
	default:
		return fmt.Sprintf("effort.Level(%d)", int(l))
	}
}

// Parse resolves a user-typed effort name (case-insensitive). Accepted
// spellings include the display names plus a few forgiving variants of
// lato-x; anything else is an error naming the valid choices.
func Parse(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		return Low, nil
	case "medium", "med", "default":
		return Medium, nil
	case "high":
		return High, nil
	case "ultra":
		return Ultra, nil
	case "lato-x", "lato_x", "latox", "lato x", "x":
		return LatoX, nil
	default:
		return Default, fmt.Errorf("unknown effort %q (valid: low, medium, high, ultra, lato-x)", s)
	}
}

// Prev returns the next-lower level, clamped at Low.
func (l Level) Prev() Level {
	if l <= Low {
		return Low
	}
	return l - 1
}

// Next returns the next-higher level, clamped at LatoX.
func (l Level) Next() Level {
	if l >= LatoX {
		return LatoX
	}
	return l + 1
}

// IsValid reports whether l is one of the defined levels.
func (l Level) IsValid() bool {
	return l >= Low && l <= LatoX
}
