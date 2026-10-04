// History budgeting (Phase 3A). Bounds the measured conversation history
// handed to a provider on a single model turn, so a long session cannot
// grow that history without limit.
//
// SCOPE: this budget applies to conversation history only. It is NOT a
// bound on the provider request as a whole. Three inputs to a request are
// outside its byte accounting and can exceed MaxBytes freely:
//
//   - The leading system message. buildMessages concatenates the agent
//     prompt with injected repository context, project memory, the task
//     directive, and the effort guide into a single system message, and
//     splitLeadingSystem pins it unconditionally.
//   - Tool definitions, which travel beside the message list rather than
//     inside it.
//   - Tool-call arguments, which messageSize deliberately does not measure
//     (serializing a map on every turn would allocate; see messageSize).
//
// The bound is also a BYTE budget, not a token budget. Lato supports
// several providers with different tokenizers and has no model-token
// estimator available, so reporting a byte limit as a token limit would be
// a claim the code cannot support. Measured bytes are used instead: they
// are deterministic, provider-independent, and every provider ultimately
// serializes the same text to bytes.
//
// Trimming never sacrifices message structure. A provider requires every
// tool result to follow the assistant message that requested it, so the
// history is first split into indivisible units -- a plain message, or an
// assistant message together with all of its tool results -- and only
// whole units are ever evicted. The result therefore never contains an
// orphaned tool result and never contains an assistant tool call whose
// results were dropped.
package runtime

import (
	"slices"

	"lato/internal/providers"
)

// HistoryBudget bounds the conversation history retained for one model
// turn. Both fields are optional: a value of zero or less means "no limit"
// for that dimension, so a zero-valued HistoryBudget never trims.
type HistoryBudget struct {
	// MaxBytes is the largest total measured size of the retained
	// history. Zero or negative disables the byte bound.
	MaxBytes int

	// MaxTurns is the largest number of conversation turns to retain.
	// One turn is one user message together with every message that
	// follows it up to (but not including) the next user message.
	// Zero or negative disables the turn bound.
	MaxTurns int
}

// Per-message accounting constants. They approximate the structural cost
// of a message beyond its literal text, so that a history made of many
// tiny messages is still bounded by the byte budget.
const (
	// messageOverheadBytes covers role, separators, and provider framing
	// for one message.
	messageOverheadBytes = 8

	// toolCallOverheadBytes covers one tool call entry beyond its
	// identifier and name.
	toolCallOverheadBytes = 32
)

// historyUnit is one indivisible, safely retainable run of messages.
//
// A unit is either a single non-tool-result message, or an assistant
// message that issued tool calls together with every tool result that
// belongs to it. Evicting part of a unit would leave either an orphaned
// tool result or an assistant tool call without its results, so units are
// the smallest thing TrimHistory is allowed to drop.
type historyUnit struct {
	lo    int // first message index, inclusive
	hi    int // last message index, exclusive
	size  int // measured bytes of messages[lo:hi]
	turns int // user messages in messages[lo:hi]
}

// TrimHistory returns the portion of msgs that fits within budget.
//
// Guarantees:
//
//   - msgs is never modified, and the returned slice never aliases it.
//   - Relative message order is preserved.
//   - Every retained assistant tool call keeps all of its tool results,
//     and every retained tool result keeps the assistant message that
//     requested it. Units are evicted atomically, so neither an orphaned
//     tool result nor an incomplete tool-call group can be produced.
//   - Leading system messages are always retained: they carry the agent's
//     instructions, and a budget must never cost the model its prompt.
//   - The unit holding the current user request is always retained, even
//     when it alone exceeds the byte budget. When the history contains no
//     user message there is no such unit and nothing is pinned but the
//     newest one.
//   - The newest unit is always retained. During a tool loop that is the
//     assistant message and its results, so the model can never be asked
//     to act on a tool call whose output it has not seen.
//   - The result is never empty when msgs is non-empty and at least one
//     message can be retained safely.
//
// Units are selected individually rather than as one contiguous range.
// That distinction is what lets the byte budget bind during a tool loop:
// every message a single request produces sits after the unit holding that
// request, so retaining a contiguous range from the request onward would
// pin the whole loop and leave the budget with nothing to evict. At most
// one gap is ever introduced -- between the current request and the
// retained recent run -- and it is preferable to sending unbounded
// history.
//
// Two pins can exceed MaxBytes: the unit holding the current request, and
// the newest unit. Neither can be dropped without harming the run, so when
// they do not both fit, MaxBytes is exceeded rather than either pin
// broken. Everything between them is still evicted normally.
func TrimHistory(msgs []providers.Message, budget HistoryBudget) []providers.Message {
	if len(msgs) == 0 {
		return nil
	}

	prefix, body := splitLeadingSystem(msgs)
	if len(body) == 0 || (budget.MaxBytes <= 0 && budget.MaxTurns <= 0) {
		return cloneMessages(msgs)
	}

	units := historyUnits(body)
	if len(units) == 0 {
		// Every message is a tool result with no owning assistant
		// message, so nothing can be retained without orphaning it.
		// Returning the input unchanged is the least destructive option.
		return cloneMessages(msgs)
	}

	// Two units are pinned. The first holds the current user request, so a
	// budget can never silently drop what the user just asked. The second
	// is the newest unit, which during a tool loop is the assistant message
	// the model is currently reacting to together with its results.
	//
	// floor is -1 when the history contains no user message, in which case
	// there is no current request to pin and only the newest unit survives.
	floor := lastUserUnit(units)
	freshest := len(units) - 1

	// Retain the most recent contiguous run of units that fits, then pin the
	// current request separately if the run stopped short of it. Keeping the
	// run contiguous means the model never sees an arbitrary hole punched
	// through the middle of a recent exchange.
	keep := make([]bool, len(units))
	keep[freshest] = true
	total := units[freshest].size
	turns := units[freshest].turns
	for i := freshest - 1; i >= 0; i-- {
		u := units[i]
		if budget.MaxBytes > 0 && total+u.size > budget.MaxBytes {
			break
		}
		if budget.MaxTurns > 0 && turns+u.turns > budget.MaxTurns {
			break
		}
		keep[i] = true
		total += u.size
		turns += u.turns
	}

	// Applied after the walk so the pinned unit is retained even when the
	// budget ran out long before reaching it.
	if floor >= 0 {
		keep[floor] = true
	}

	out := make([]providers.Message, 0, len(prefix)+len(body))
	out = append(out, prefix...)
	for i, u := range units {
		if keep[i] {
			out = append(out, body[u.lo:u.hi]...)
		}
	}
	return out
}

// splitLeadingSystem separates the run of system messages at the head of
// msgs from the conversation that follows. Those messages carry the
// agent's instructions and the injected repository evidence, so they are
// preserved unconditionally rather than being treated as evictable
// history. A non-system leading message is part of the conversation.
func splitLeadingSystem(msgs []providers.Message) (prefix, body []providers.Message) {
	n := 0
	for n < len(msgs) && msgs[n].Role == providers.SystemRole {
		n++
	}
	return msgs[:n], msgs[n:]
}

// historyUnits splits body into indivisible units, ordered by index and
// non-overlapping.
//
// Tool results are attributed to the assistant message that issued them by
// matching ToolCallID, which is the only link the runtime records: the
// runtime sets ToolCalls on assistant messages and ToolCallID on tool
// results, and never the reverse. A tool result whose owner cannot be
// found cannot be retained safely and is given no unit, so it is dropped.
func historyUnits(body []providers.Message) []historyUnit {
	units := make([]historyUnit, 0, len(body))

	// Walking backwards is what makes tool grouping possible: the first
	// tool result seen from the end is the last of its group, so the unit
	// spans its owning assistant message through that result.
	for i := len(body) - 1; i >= 0; {
		if body[i].Role != providers.ToolRole {
			units = append(units, newHistoryUnit(body, i, i+1))
			i--
			continue
		}

		owner := toolCallOwner(body, i)
		if owner < 0 {
			// Orphaned tool result: skip it and keep scanning backwards.
			i--
			continue
		}
		// owner < i always holds: results follow the assistant message
		// that requested them.
		units = append(units, newHistoryUnit(body, owner, i+1))
		i = owner - 1
	}

	// The backward walk produced descending units; TrimHistory walks and
	// slices them in ascending order.
	slices.Reverse(units)
	return units
}

// newHistoryUnit measures messages[lo:hi].
func newHistoryUnit(body []providers.Message, lo, hi int) historyUnit {
	u := historyUnit{lo: lo, hi: hi}
	for i := lo; i < hi; i++ {
		u.size += messageSize(body[i])
		if body[i].Role == providers.UserRole {
			u.turns++
		}
	}
	return u
}

// toolCallOwner returns the index of the assistant message that issued the
// tool call named by msgs[i].ToolCallID, or -1 when no earlier assistant
// message requested it. The scan is bounded by the messages actually
// present, so a mismatched or empty identifier simply yields -1.
func toolCallOwner(msgs []providers.Message, i int) int {
	if i < 0 || i >= len(msgs) || msgs[i].ToolCallID == "" {
		return -1
	}
	for k := i - 1; k >= 0; k-- {
		if msgs[k].Role != providers.AssistantRole {
			continue
		}
		for _, tc := range msgs[k].ToolCalls {
			if tc.ID == msgs[i].ToolCallID {
				return k
			}
		}
	}
	return -1
}

// lastUserUnit returns the index of the unit holding the most recent user
// message, or -1 when the conversation contains none. A user message is
// never part of a tool-call unit, so it always begins one.
//
// -1 matters: returning 0 instead would pin the OLDEST unit as though it
// were the current request, and that unit could then exceed MaxBytes
// without limit whenever the history held no user message.
func lastUserUnit(units []historyUnit) int {
	for i := len(units) - 1; i >= 0; i-- {
		if units[i].turns > 0 {
			return i
		}
	}
	return -1
}

// messageSize returns the measured size of m in bytes.
//
// Content dominates the cost, so it is measured exactly. Tool call
// arguments are deliberately excluded: measuring them would require
// serializing a map on every turn, and they originate from the model's own
// output rather than from history. They are therefore outside MaxBytes
// accounting and are documented as such in the package comment.
func messageSize(m providers.Message) int {
	n := len(m.Content) + messageOverheadBytes + len(m.Name) + len(m.ToolCallID)
	for _, tc := range m.ToolCalls {
		n += toolCallOverheadBytes + len(tc.ID) + len(tc.Name)
	}
	return n
}

// cloneMessages returns an independent copy of msgs so callers never
// share a backing array with the runtime's history.
func cloneMessages(msgs []providers.Message) []providers.Message {
	out := make([]providers.Message, len(msgs))
	copy(out, msgs)
	return out
}
