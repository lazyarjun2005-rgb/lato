package tui

import (
	"testing"
	"time"

	"lato/internal/session"
)

func TestSessionEntriesRestoresOrderedActivityOnce(t *testing.T) {
	now := time.Now()
	sess := &session.Session{
		Messages: []session.Message{{Role: "user", Content: "inspect", Time: now}},
		Activity: []session.Activity{
			{Kind: "tool", Label: "read_file", Status: "completed", StartedAt: now.Add(time.Second)},
			{Kind: "tool", Label: "run_command", Status: "failed", Error: "exit code 1", StartedAt: now.Add(2 * time.Second)},
		},
	}
	entries := sessionEntries(sess)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	if entries[1].Role != roleActivity || entries[2].Role != roleActivity {
		t.Fatalf("activity roles not restored: %+v", entries)
	}
	if entries[1].Content != "✓ read_file" || entries[2].Content != "✗ run_command · exit code 1" {
		t.Fatalf("activity order/content incorrect: %+v", entries)
	}
	if got := len(sessionEntries(sess)); got != 3 {
		t.Fatalf("reopening equivalent session duplicated entries: %d", got)
	}
}

func TestSessionEntriesWithoutActivityRemainMessagesOnly(t *testing.T) {
	sess := &session.Session{Messages: []session.Message{{Role: "assistant", Content: "hello"}}}
	entries := sessionEntries(sess)
	if len(entries) != 1 || entries[0].Role != roleAssistant {
		t.Fatalf("legacy session entries = %+v", entries)
	}
}
