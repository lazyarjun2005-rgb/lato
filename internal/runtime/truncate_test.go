package runtime

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateOutput(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		maxBytes    int
		wantTrunc   bool
		wantContain string
		wantNotCont string
	}{
		{
			name:        "short string unchanged",
			input:       "hello world",
			maxBytes:    100,
			wantTrunc:   false,
			wantContain: "hello world",
		},
		{
			name:        "long string truncated",
			input:       strings.Repeat("a", 1000),
			maxBytes:    100,
			wantTrunc:   true,
			wantContain: "[output truncated",
			wantNotCont: strings.Repeat("a", 1000),
		},
		{
			name:        "unicode preserved",
			input:       "Hello 世界! " + strings.Repeat("x", 500),
			maxBytes:    200,
			wantTrunc:   true,
			wantContain: "[output truncated",
		},
		{
			name:        "maxBytes <= 0 returns original",
			input:       "hello world",
			maxBytes:    0,
			wantTrunc:   false,
			wantContain: "hello world",
		},
		{
			name:        "maxBytes too small for marker",
			input:       "hello world",
			maxBytes:    10,
			wantTrunc:   true,
			wantNotCont: "hello world",
		},
		{
			name:        "exact fit no truncation",
			input:       "hello",
			maxBytes:    5,
			wantTrunc:   false,
			wantContain: "hello",
		},
		{
			name:        "one byte over triggers truncation but marker too large",
			input:       "hello",
			maxBytes:    4,
			wantTrunc:   true,
			wantContain: "hell",
		},
		{
			name:        "marker preserved in output",
			input:       strings.Repeat("a", 200),
			maxBytes:    100,
			wantTrunc:   true,
			wantContain: "[output truncated: showing beginning and end]",
		},
		{
			name:        "both head and tail in truncated output",
			input:       "START" + strings.Repeat("x", 100) + "END",
			maxBytes:    80,
			wantTrunc:   true,
			wantContain: "START",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, truncated := TruncateOutput(tc.input, tc.maxBytes)
			if truncated != tc.wantTrunc {
				t.Errorf("TruncateOutput(%q, %d) truncated=%v, want %v", tc.input, tc.maxBytes, truncated, tc.wantTrunc)
			}
			if tc.wantContain != "" && !strings.Contains(result, tc.wantContain) {
				t.Errorf("result %q missing expected %q", result, tc.wantContain)
			}
			if tc.wantNotCont != "" && strings.Contains(result, tc.wantNotCont) {
				t.Errorf("result %q should not contain %q", result, tc.wantNotCont)
			}
		})
	}
}

func TestTruncateOutputUTF8(t *testing.T) {
	s := "Hello 世界! " + strings.Repeat("x", 200)
	result, truncated := TruncateOutput(s, 100)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if !utf8.ValidString(result) {
		t.Errorf("result is not valid UTF-8: %q", result)
	}
	if strings.HasSuffix(result, "\uFFFD") {
		t.Errorf("result ends with replacement character")
	}
}

func TestTruncateOutputSmallMaxBytes(t *testing.T) {
	result, truncated := TruncateOutput("hello world", 5)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if len(result) > 5 {
		t.Errorf("result length %d exceeds maxBytes 5", len(result))
	}
}
