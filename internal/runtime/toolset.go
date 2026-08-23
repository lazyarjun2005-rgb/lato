// Selective tool exposure. Tool definitions are rendered into every
// model prompt, and on local CPU-only models that schema costs real
// prompt-processing time (thousands of tokens before the first output
// token). A purely conversational turn cannot call tools, so sending
// ten JSON schemas with "hi" is pure overhead.
//
// The rule is deliberately conservative: tools are omitted only when
// the latest user message is clearly small talk. Every other message —
// including anything ambiguous — receives the full tool set, so no
// capability is ever lost when it matters.

package runtime

import (
	"strings"

	"lato/internal/providers"
	"lato/internal/tools"
)

// maxSmallTalkWords caps how long a message may be to qualify through
// word-level matching; longer messages always get tools.
const maxSmallTalkWords = 6

// smallTalkPhrases are exact multi-word small-talk messages (after
// normalization). Matching is by equality, never substring, so "what is
// your name" matches but "what is a pointer" does not.
var smallTalkPhrases = map[string]bool{
	"how are you":            true,
	"how are you doing":      true,
	"how is it going":        true,
	"hows it going":          true,
	"whats up":               true,
	"what is up":             true,
	"wassup":                 true,
	"who are you":            true,
	"what are you":           true,
	"what can you do":        true,
	"what do you do":         true,
	"what is your name":      true,
	"whats your name":        true,
	"tell me about yourself": true,
	"nice to meet you":       true,
	"good morning":           true,
	"good afternoon":         true,
	"good evening":           true,
	"good night":             true,
	"thanks a lot":           true,
	"thank you so much":      true,
	"many thanks":            true,
	"talk to you later":      true,
	"see you later":          true,
	"sounds good":            true,
	"that is all":            true,
	"thats all":              true,
	"never mind":             true,
	"nevermind":              true,
	"just kidding":           true,
}

// smallTalkWords are words that can only appear in small talk. If a
// short message consists entirely of these, it gets no tools. Action
// verbs, file/code nouns, and interrogatives are deliberately absent:
// any message containing one falls through to the full tool set.
var smallTalkWords = map[string]bool{
	// greetings & farewells
	"hi": true, "hello": true, "hey": true, "heya": true, "hiya": true,
	"yo": true, "sup": true, "howdy": true, "greetings": true,
	"bye": true, "goodbye": true, "later": true, "cya": true, "night": true,

	// gratitude & affirmations
	"thanks": true, "thank": true, "thx": true, "ty": true,
	"ok": true, "okay": true, "cool": true, "nice": true, "great": true,
	"awesome": true, "perfect": true, "good": true, "well": true,
	"yes": true, "yeah": true, "yep": true, "yup": true, "sure": true,
	"no": true, "nope": true, "nah": true, "right": true, "fine": true,

	// filler pronouns/copulas/filler that carry no intent on their own
	"i": true, "im": true, "me": true, "my": true, "we": true, "us": true,
	"you": true, "your": true, "it": true, "its": true, "this": true,
	"am": true, "is": true, "are": true, "was": true, "be": true,
	"a": true, "an": true, "the": true, "to": true, "and": true,
	"so": true, "very": true, "too": true, "also": true, "just": true,
	"now": true, "here": true, "there": true, "up": true, "out": true,
	"in": true, "on": true, "all": true, "done": true, "got": true,
	"gotcha": true, "lol": true, "haha": true, "hmm": true, "hm": true,
	"friend": true, "buddy": true, "man": true, "dude": true,
	"today": true, "morning": true, "afternoon": true, "evening": true,
	"day": true, "doing": true, "going": true, "again": true,
}

// conversationalTurn reports whether a user message is pure small talk
// that cannot involve any tool. Normalization lowercases, strips
// punctuation, and collapses whitespace; matching is then either an
// exact phrase or an all-words-in-set test on short messages.
func conversationalTurn(text string) bool {
	t := normalizeTurnText(text)
	if t == "" {
		return true // nothing actionable in an empty message
	}
	if smallTalkPhrases[t] {
		return true
	}
	words := strings.Fields(t)
	if len(words) == 0 || len(words) > maxSmallTalkWords {
		return false
	}
	for _, w := range words {
		if !smallTalkWords[w] {
			return false
		}
	}
	return true
}

// normalizeTurnText lowercases, removes punctuation (apostrophes fold
// into the word: "what's" → "whats"), and collapses whitespace.
func normalizeTurnText(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	lastSpace := false
	for _, r := range strings.ToLower(text) {
		switch {
		case unicodeIsLetterOrDigit(r):
			b.WriteRune(r)
			lastSpace = false
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if !lastSpace && b.Len() > 0 {
				b.WriteByte(' ')
				lastSpace = true
			}
		default:
			// punctuation and symbols are dropped
		}
	}
	return strings.TrimSpace(b.String())
}

func unicodeIsLetterOrDigit(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// lastUserMessage returns the most recent user message's text, or ""
// when the history contains none.
func lastUserMessage(history []providers.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == providers.UserRole {
			return history[i].Content
		}
	}
	return ""
}

// toolDefinitions selects the tool definitions offered to the model for
// this request. Conversational turns receive none — they cannot use
// tools, so the schemas would be dead weight in the prompt. Everything
// else receives the full set, unchanged.
func (r *Runtime) toolDefinitions(history []providers.Message) []tools.Definition {
	if conversationalTurn(lastUserMessage(history)) {
		return nil
	}
	return r.manager.Definitions()
}
