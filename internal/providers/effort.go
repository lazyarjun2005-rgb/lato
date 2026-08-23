// Provider-aware effort capabilities (M16).
//
// Lato never sends an effort parameter a provider has not declared
// support for. This table is the single authority on which mechanism
// (if any) each provider accepts, and which raw tokens that mechanism
// supports, ordered weakest → strongest so Lato's ladder maps onto
// whatever set a provider actually advertises.
//
// Extending support is a data change: add an entry here, nothing else.
// Providers absent from the table — including user-defined /connect
// providers — deliberately resolve to no mechanism: Lato-side
// orchestration still applies, but no speculative request fields are
// ever sent upstream.
package providers

import (
	"lato/internal/effort"
)

// EffortMechanism names where an effort value goes in a request body.
type EffortMechanism string

const (
	// EffortNone means this provider declares no effort parameter.
	// No request field is sent; effort applies through Lato-side
	// orchestration only.
	EffortNone EffortMechanism = ""

	// EffortReasoningField sends a top-level OpenAI-style field:
	// "reasoning_effort": "<token>".
	EffortReasoningField EffortMechanism = "reasoning_effort"

	// EffortReasoningObject sends the router-normalized object form:
	// "reasoning": {"effort": "<token>"}. OpenRouter documents this as
	// ignored by routed models that lack reasoning support, making it
	// safe for its heterogeneous catalog.
	EffortReasoningObject EffortMechanism = "reasoning"
)

// EffortCapability describes one provider's declared effort surface.
// Supported lists the raw tokens the mechanism accepts in ascending
// order of strength; it must contain at least one entry whenever
// Mechanism is not EffortNone.
type EffortCapability struct {
	Mechanism EffortMechanism
	Supported []string
}

// effortCapabilities is the authoritative capability table, keyed by
// registered provider ID.
//
// Rationale for the current entries:
//   - openrouter normalizes "reasoning" across hundreds of upstream
//     models and documents that unsupported models ignore it.
//   - ollama, lmstudio, nvidia, 9router, omniroute and custom /connect
//     providers serve heterogeneous backends with no advertised,
//     uniform effort contract; sending them reasoning parameters would
//     be guessing. Their effort therefore acts entirely through Lato's
//     orchestration layer.
var effortCapabilities = map[string]EffortCapability{
	"openrouter": {
		Mechanism: EffortReasoningObject,
		Supported: []string{"low", "medium", "high"},
	},
}

// EffortCapabilityFor returns the declared capability for a provider ID.
// Unregistered IDs (hand-edited configs, /connect customs) yield a
// no-mechanism capability.
func EffortCapabilityFor(providerID string) EffortCapability {
	if cap, ok := effortCapabilities[providerID]; ok {
		return cap
	}
	return EffortCapability{Mechanism: EffortNone}
}

// ResolveProviderEffort maps a Lato level onto a provider's declared
// capability. ok is false when the provider declares no mechanism —
// callers must then send nothing rather than guess.
//
// Mapping rule (derived from the capability, not hardcoded): Low maps
// to the weakest supported token, Medium/High to their positions when
// present, and Ultra/Lato-X walk toward the strongest token. A
// three-token set therefore clamps Ultra/Lato-X at its strongest
// ("high"); a five-token set ending in "max" lets Lato-X send "max".
func ResolveProviderEffort(providerID string, level effort.Level) (mechanism EffortMechanism, token string, ok bool) {
	cap := EffortCapabilityFor(providerID)
	if cap.Mechanism == EffortNone || len(cap.Supported) == 0 {
		return EffortNone, "", false
	}
	if !level.IsValid() {
		level = effort.Default
	}

	// Levels are 1-based; map to a 0-based position inside Supported.
	pos := int(level) - 1
	last := len(cap.Supported) - 1
	if pos > last {
		pos = last
	}
	return cap.Mechanism, cap.Supported[pos], true
}
