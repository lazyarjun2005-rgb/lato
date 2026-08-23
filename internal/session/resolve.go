// Session name resolution for /resume.
//
// A user may address a session by its ID (exact or unique prefix,
// mirroring the picker's short-ID display and /task resume's prefix
// convention) or by its exact Title. Resolution is read-only: no file
// is renamed, modified, or created while resolving.
//
// Order is fixed and deliberate:
//  1. exact full-ID match wins immediately;
//  2. otherwise a UNIQUE ID-prefix match is accepted;
//  3. otherwise an exact Title match is required;
//  4. ambiguity never guesses — the caller is told to use the session
//     picker or a longer/full ID.
package session

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ResolveSession resolves input against sessions using the order above.
// The input is trimmed; matching itself is exact (no fuzzy logic).
func ResolveSession(input string, sessions []Session) (*Session, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, errors.New("session id or title is required")
	}

	// 1. Exact full-ID match.
	for i := range sessions {
		if sessions[i].ID == input {
			return &sessions[i], nil
		}
	}

	// 2. Unique ID-prefix match.
	var byPrefix []Session
	for _, s := range sessions {
		if strings.HasPrefix(s.ID, input) {
			byPrefix = append(byPrefix, s)
		}
	}
	switch len(byPrefix) {
	case 1:
		return &byPrefix[0], nil
	case 0:
	default:
		return nil, ambiguousError(input, byPrefix)
	}

	// 3. Exact title match.
	var byTitle []Session
	for _, s := range sessions {
		if s.Title == input {
			byTitle = append(byTitle, s)
		}
	}
	switch len(byTitle) {
	case 1:
		return &byTitle[0], nil
	case 0:
	default:
		return nil, ambiguousError(input, byTitle)
	}

	return nil, fmt.Errorf("session not found: %q — run /sessions to pick one", input)
}

// ambiguousError renders the never-guess refusal with enough context to
// disambiguate: short IDs plus titles.
func ambiguousError(input string, matches []Session) error {
	sorted := append([]Session(nil), matches...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	const maxListed = 5
	var b strings.Builder
	fmt.Fprintf(&b, "%d sessions match %q — do not guess; use the session picker (/sessions) or a longer ID:",
		len(sorted), input)
	for i, s := range sorted {
		if i == maxListed {
			fmt.Fprintf(&b, "\n  … %d more", len(sorted)-i)
			break
		}
		id := s.ID
		if len(id) > 8 {
			id = id[:8]
		}
		title := s.Title
		if strings.TrimSpace(title) == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&b, "\n  %s — %s", id, title)
	}
	return errors.New(b.String())
}
