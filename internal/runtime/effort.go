// Agent-side effort orchestration (M16).
//
// A profile is how a Lato effort level changes the EXISTING M10 loop:
// turn budget, repetition thresholds, and the guidance block injected
// into complex-task prompts. Profiles scale the depth of orchestration;
// they never remove safety. Even lato-X keeps hard bounds, repetition
// detection, permission gating, and honest completion fully intact —
// it is maximum effort inside the existing architecture, not an
// unbounded mode.
package runtime

import (
	"lato/internal/effort"
)

// effortProfile is the bounded orchestration shape derived from one
// effort level. Every field stays within M10's safety model:
//
//	MaxTurns         cap on model turns per user request (hard stop)
//	RepeatNudgeAfter identical consecutive calls before a steering hint
//	RepeatStopAfter  identical consecutive calls before a clean stop
//	Directive        prompt guidance for complex tasks ("" = none)
type effortProfile struct {
	MaxTurns         int
	RepeatNudgeAfter int
	RepeatStopAfter  int
	Directive        string
}

// effortProfiles maps each ladder level to its orchestration bounds.
// The values are explicit and reviewed: higher levels widen the budget
// modestly and loosen the repetition nudge slightly, but every level
// terminates. Medium reproduces the pre-M16 constants exactly.
var effortProfiles = map[effort.Level]effortProfile{
	effort.Low: {
		MaxTurns:         6,
		RepeatNudgeAfter: 2,
		RepeatStopAfter:  3,
		Directive: "Work at low effort: minimize tool calls and analysis, execute directly, " +
			"and keep responses concise. Verify only what correctness genuinely requires.",
	},
	effort.Medium: {
		MaxTurns:         12,
		RepeatNudgeAfter: 3,
		RepeatStopAfter:  4,
		Directive:        "", // balanced mode: no extra guidance beyond M10's protocol
	},
	effort.High: {
		MaxTurns:         18,
		RepeatNudgeAfter: 3,
		RepeatStopAfter:  4,
		Directive: "Work at high effort: inspect the repository thoroughly before editing, " +
			"verify changes with real commands (format, build, tests), and when something fails, " +
			"diagnose the actual cause before retrying a different approach.",
	},
	effort.Ultra: {
		MaxTurns:         24,
		RepeatNudgeAfter: 4,
		RepeatStopAfter:  5,
		Directive: "Work at ultra effort: plan deeply before acting, inspect broadly enough to be " +
			"certain of context, add targeted checks beyond basic verification, and replan " +
			"deliberately whenever a result differs from what you expected.",
	},
	effort.LatoX: {
		MaxTurns:         32,
		RepeatNudgeAfter: 4,
		RepeatStopAfter:  5,
		Directive: "Work at maximum (lato-X) effort while staying strictly within your turn budget: " +
			"decompose complex work into explicit ordered steps, re-inspect files when uncertainty is high, " +
			"cross-check edge cases, verify every layer you touched, and recover from setbacks by " +
			"understanding them first. Thoroughness never overrides safety or honesty about results.",
	},
}

// profile returns the orchestration profile for r's current effort.
func (r *Runtime) profile() effortProfile {
	return profileFor(r.effort)
}

// profileFor resolves a level to its bounded profile; unknown levels
// fall back to balanced rather than panicking.
func profileFor(level effort.Level) effortProfile {
	if p, ok := effortProfiles[level]; ok && level.IsValid() {
		return p
	}
	return effortProfiles[effort.Default]
}
