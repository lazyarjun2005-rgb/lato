// Clipboard support for the chat transcript. Entries store raw plain
// text — all terminal styling is applied at render time — so copying
// entry contents yields clean text; ANSI stripping runs anyway as a
// defensive guarantee that escape sequences can never reach the system
// clipboard.
package tui

import (
	"regexp"
	"strings"

	"lato/internal/clipboard"
)

// ansiPattern matches CSI sequences (colors, cursor movement) and OSC
// sequences (window title, hyperlinks).
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// stripANSI removes terminal escape sequences from s.
func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

// LatestResponse returns the most recent complete response together
// with its tool activity lines ("✓ run_command completed"), as plain
// text, or ok=false when there is nothing to copy yet.
//
// The trailing block is collected by walking backwards over adjacent
// assistant/activity entries until a user/system/error boundary; this
// keeps repeat /copy calls working even after system confirmations are
// appended to the transcript.
func (m *model) LatestResponse() string {
	end := len(m.entries)
	for end > 0 && !isCopyableRole(m.entries[end-1].Role) {
		end-- // skip trailing system confirmations/errors
	}
	start := end
	for start > 0 && isCopyableRole(m.entries[start-1].Role) {
		start--
	}

	var parts []string
	for _, e := range m.entries[start:end] {
		if c := strings.TrimSpace(e.Content); c != "" {
			parts = append(parts, c)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return stripANSI(strings.Join(parts, "\n\n"))
}

// TranscriptText returns the whole visible conversation as plain,
// labeled text, or ok=false when the transcript is empty.
func (m *model) TranscriptText() string {
	labels := map[role]string{
		roleUser:      "You",
		roleAssistant: "Lato",
		roleActivity:  "tool",
		roleError:     "Error",
		roleSystem:    "System",
	}
	var parts []string
	for _, e := range m.entries {
		c := strings.TrimSpace(stripANSI(e.Content))
		if c == "" {
			continue
		}
		parts = append(parts, labels[e.Role]+":\n"+c)
	}
	return strings.Join(parts, "\n\n")
}

func isCopyableRole(r role) bool {
	return r == roleAssistant || r == roleActivity
}

// writeClipboard is the seam tests use to observe or fail clipboard
// writes without touching the real system.
var writeClipboard = clipboard.Write

// WriteToClipboard places text on the system clipboard. Errors carry no
// copied content.
func (m *model) WriteToClipboard(text string) error {
	return writeClipboard(text)
}
