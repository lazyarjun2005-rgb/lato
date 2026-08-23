package builtin

import (
	"fmt"
	"strings"

	"lato/internal/command"
)

// Copy is the /copy command: it places Lato's output on the system
// clipboard as plain text.
//
//   - /copy            → the most recent complete response (default)
//   - /copy response   → same as /copy
//   - /copy last       → same as /copy
//   - /copy transcript → the whole visible conversation
//
// Copied text is the stored plain text, never terminal styling; ANSI is
// stripped defensively before writing.
type Copy struct{}

// NewCopy returns a ready-to-register /copy command.
func NewCopy() *Copy { return &Copy{} }

func (Copy) Name() string      { return "copy" }
func (Copy) Aliases() []string { return nil }
func (Copy) Description() string {
	return "Copy the last response (or the whole transcript with \"transcript\") to the clipboard."
}
func (Copy) Usage() string { return "/copy [response|last|transcript]" }

func (Copy) Execute(ctx command.Context, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("expected at most one argument, got %d", len(args))
	}

	target := "response"
	if len(args) == 1 {
		switch args[0] {
		case "response", "last":
			target = "response"
		case "transcript":
			target = "transcript"
		default:
			return fmt.Errorf("unknown target %q (usage: %s)", args[0], Copy{}.Usage())
		}
	}

	var text string
	if target == "transcript" {
		text = ctx.TranscriptText()
	} else {
		text = ctx.LatestResponse()
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("nothing to copy yet — ask Lato something first")
	}

	if err := ctx.WriteToClipboard(text); err != nil {
		return fmt.Errorf("copy failed: %w", err)
	}
	ctx.Println("✓ Copied %s (%d characters) to the clipboard.", copyTargetLabel(target), len(text))
	return nil
}

func copyTargetLabel(target string) string {
	if target == "transcript" {
		return "the transcript"
	}
	return "the latest response"
}
