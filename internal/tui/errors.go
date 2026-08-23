// Error presentation helpers. Provider failures already carry precise
// causes; these helpers add an actionable next step so users know what
// to do, without hiding details or ever touching credential material.
package tui

import (
	"strings"
)

// withActionHint renders err and, when the message matches a known
// failure shape, appends a short reason plus an Action line naming the
// command that fixes it. Unknown errors pass through unchanged.
func withActionHint(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case containsAny(msg, "status 401", "status 403"):
		return msg + hintBlock("The provider rejected the credentials.",
			"run /connect <provider> to update the API key (keys are never displayed), or /provider to switch")
	case containsAny(msg, "status 404") && containsAny(msg, "model"):
		return msg + hintBlock("The selected model was not found on this provider.",
			"run /model to pick another model, or /model add to register it by ID")
	case containsAny(msg, "status 429"):
		return msg + hintBlock("The provider is rate-limiting requests.",
			"wait a moment and send the request again")
	case containsAny(msg, "could not reach"):
		return msg + hintBlock("The provider endpoint could not be reached.",
			"check that the provider is running and reachable, then retry; /connect can correct the endpoint")
	default:
		return msg
	}
}

func hintBlock(reason, action string) string {
	var b strings.Builder
	b.WriteString("\n\nWhy: ")
	b.WriteString(reason)
	b.WriteString("\nAction: ")
	b.WriteString(action)
	b.WriteString(".")
	return b.String()
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
