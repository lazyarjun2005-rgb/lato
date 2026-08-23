package builtin

import (
	"errors"
	"strings"
	"testing"
)

func TestCopyDefaultCopiesLatestResponse(t *testing.T) {
	ctx := &fakeContext{latestResponse: "## Result\n\nThe function returns 42."}
	if err := NewCopy().Execute(ctx, nil); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if ctx.clipboardText != ctx.latestResponse {
		t.Errorf("clipboard = %q, want the response", ctx.clipboardText)
	}
	if len(ctx.lines) == 0 || !strings.Contains(ctx.lines[0], "✓ Copied") {
		t.Errorf("confirmation missing: %v", ctx.lines)
	}
}

func TestCopyResponseAndLastAliases(t *testing.T) {
	for _, arg := range []string{"response", "last"} {
		ctx := &fakeContext{latestResponse: "answer"}
		if err := NewCopy().Execute(ctx, []string{arg}); err != nil {
			t.Fatalf("/copy %s error = %v", arg, err)
		}
		if ctx.clipboardText != "answer" {
			t.Errorf("/copy %s clipboard = %q", arg, ctx.clipboardText)
		}
	}
}

func TestCopyTranscript(t *testing.T) {
	ctx := &fakeContext{transcriptText: "You:\nhi\n\nLato:\nhello"}
	if err := NewCopy().Execute(ctx, []string{"transcript"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if ctx.clipboardText != ctx.transcriptText {
		t.Errorf("clipboard = %q, want transcript", ctx.clipboardText)
	}
}

func TestCopyEmptyResponseErrors(t *testing.T) {
	ctx := &fakeContext{latestResponse: ""}
	err := NewCopy().Execute(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "nothing to copy") {
		t.Fatalf("error = %v, want nothing-to-copy guidance", err)
	}
}

func TestCopyClipboardFailurePropagates(t *testing.T) {
	ctx := &fakeContext{
		latestResponse: "precious response",
		clipboardErr:   errors.New("no xclip installed"),
	}
	err := NewCopy().Execute(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "no xclip installed") {
		t.Fatalf("error = %v, want wrapped clipboard failure", err)
	}
	// The response must remain available for a retry.
	if ctx.latestResponse == "" {
		t.Error("response lost after failure")
	}
}

func TestCopyUnknownTarget(t *testing.T) {
	ctx := &fakeContext{}
	if err := NewCopy().Execute(ctx, []string{"everything"}); err == nil {
		t.Fatal("expected usage error for unknown target")
	}
	if ctx.clipboardText != "" {
		t.Error("clipboard written despite invalid arguments")
	}
}

// TestCopyMultilineMarkdownPreserved pins requirement 5: what is stored
// is exactly what lands on the clipboard, newlines included.
func TestCopyMultilineMarkdownPreserved(t *testing.T) {
	response := "# Title\n\n- one\n- two\n\n```go\nfunc main() {}\n```"
	ctx := &fakeContext{latestResponse: response}
	if err := NewCopy().Execute(ctx, []string{"last"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if ctx.clipboardText != response {
		t.Errorf("clipboard text differs from source markdown")
	}
}
