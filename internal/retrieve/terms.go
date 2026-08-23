package retrieve

import "strings"

// Terms extracts the meaningful search terms from a question. It
// lowercases, drops stop-words and very short words, keeps identifiers
// intact ("fmt.Println" stays one term), and caps the result so a long
// question cannot blow up scoring.
func Terms(question string) []string {
	fields := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(question)), func(r rune) bool {
		return !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || r == '_' || r == '.')
	})

	var terms []string
	for _, f := range fields {
		if len(f) < 3 {
			continue
		}
		f = strings.Trim(f, ".")
		if f == "" || stopWords[f] {
			continue
		}
		if !containsString(terms, f) {
			terms = append(terms, f)
		}
		if len(terms) >= maxTerms {
			break
		}
	}
	return terms
}

// stopWords are common English and code-question filler words that carry
// no retrieval signal.
var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "but": true,
	"not": true, "you": true, "all": true, "any": true, "can": true,
	"how": true, "what": true, "when": true, "where": true, "which": true,
	"who": true, "why": true, "does": true, "did": true, "was": true,
	"this": true, "that": true, "these": true, "those": true, "there": true,
	"with": true, "from": true, "into": true, "its": true, "his": true,
	"her": true, "our": true, "their": true, "has": true, "have": true,
	"had": true, "were": true, "been": true, "being": true,
	"will": true, "would": true, "could": true, "should": true, "may": true,
	"might": true, "must": true, "shall": true, "about": true, "work": true,
	"works": true, "working": true, "used": true, "using": true, "use": true,
	"find": true, "show": true, "tell": true, "explain": true, "describe": true, "code": true, "repo": true, "repository": true, "project": true,
	"codebase": true, "function": true, "file": true, "line": true,
}
