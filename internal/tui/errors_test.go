package tui

import (
	"errors"
	"strings"
	"testing"
)

// TestWithActionHintKnownFailures pins the actionable-error contract:
// known provider failure shapes gain a Why + Action block naming the
// fixing command, and the original cause is never replaced.
func TestWithActionHintKnownFailures(t *testing.T) {
	cases := []struct {
		name    string
		errText string
		want    []string
	}{
		{
			name:    "auth rejection",
			errText: `https://openrouter.ai/api/v1 returned status 401 for model "x/y"`,
			want:    []string{"status 401", "Why:", "/connect", "Action:"},
		},
		{
			name:    "model not found",
			errText: `ollama returned status 404 for model "llama3": model 'llama3' not found`,
			want:    []string{"status 404", "/model", "Action:"},
		},
		{
			name:    "rate limited",
			errText: "provider returned status 429 listing models",
			want:    []string{"status 429", "wait a moment"},
		},
		{
			name:    "unreachable",
			errText: "could not reach Ollama at http://localhost:11434 (is `ollama serve` running?): connection refused",
			want:    []string{"could not reach", "check that the provider is running"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := withActionHint(errors.New(c.errText))
			if !strings.HasPrefix(got, c.errText) {
				t.Errorf("hint replaced the original error:\n%s", got)
			}
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("hint missing %q:\n%s", w, got)
				}
			}
		})
	}
}

// TestWithActionHintPassthrough keeps unknown errors exactly as they
// are — no invented advice.
func TestWithActionHintPassthrough(t *testing.T) {
	err := errors.New("something entirely different happened")
	if got := withActionHint(err); got != err.Error() {
		t.Errorf("passthrough changed the message:\n%s", got)
	}
	if got := withActionHint(nil); got != "" {
		t.Errorf("nil error rendered as %q", got)
	}
}
