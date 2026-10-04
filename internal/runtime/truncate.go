// Output truncation (Phase 2E). Provides utilities for bounding tool
// output size while preserving both the beginning and end of long
// output with clear truncation markers.
package runtime

import (
	"unicode/utf8"
)

// TruncationMarker is the marker inserted when output is truncated.
const TruncationMarker = "\n... [output truncated: showing beginning and end] ...\n"

// TruncateOutput bounds the size of a tool output string to maxBytes.
// If the input exceeds maxBytes, it preserves the first and last
// portions with a clear truncation marker in between.
// The marker itself is included in the byte count.
// Returns the (possibly truncated) string and whether truncation occurred.
// Handles UTF-8 correctly (never splits a multi-byte character).
func TruncateOutput(s string, maxBytes int) (string, bool) {
	if maxBytes <= 0 {
		return s, false
	}

	// Fast path: already small enough
	if len(s) <= maxBytes {
		return s, false
	}

	// Reserve space for the marker
	markerBytes := len(TruncationMarker)
	if maxBytes <= markerBytes {
		// Too small for even the marker; just return truncated with marker
		return TruncateUTF8(s, maxBytes), true
	}

	available := maxBytes - markerBytes
	// Split available space between head and tail
	headBytes := available / 2
	tailBytes := available - headBytes

	// Ensure we don't exceed original string length
	if headBytes+tailBytes >= len(s) {
		return s, false
	}

	// Truncate head and tail at UTF-8 boundaries
	head := TruncateUTF8(s, headBytes)
	tail := TruncateUTF8(s[len(s)-tailBytes:], tailBytes)

	return head + TruncationMarker + tail, true
}

// TruncateUTF8 truncates a string to at most maxBytes while preserving
// valid UTF-8 (never splitting a multi-byte character).
func TruncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	// Truncate at byte boundary, then back up to valid UTF-8
	b := []byte(s[:maxBytes])
	for !utf8.Valid(b) && len(b) > 0 {
		b = b[:len(b)-1]
	}
	return string(b)
}

// TruncateOutputWithMarker is a lower-level function that allows a
// custom marker. Used when we want a different marker format.
func TruncateOutputWithMarker(s string, maxBytes int, marker string) (string, bool) {
	if maxBytes <= 0 {
		return s, false
	}

	if len(s) <= maxBytes {
		return s, false
	}

	markerBytes := len(marker)
	if maxBytes <= markerBytes {
		return TruncateUTF8(s, maxBytes), true
	}

	available := maxBytes - markerBytes
	headBytes := available / 2
	tailBytes := available - headBytes

	if headBytes+tailBytes >= len(s) {
		return s, false
	}

	head := TruncateUTF8(s, headBytes)
	tail := TruncateUTF8(s[len(s)-tailBytes:], tailBytes)

	return head + marker + tail, true
}
