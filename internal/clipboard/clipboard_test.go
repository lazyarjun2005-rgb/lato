package clipboard

import (
	"errors"
	"strings"
	"testing"
)

func TestWriteEmptyTextRejected(t *testing.T) {
	if err := Write(""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("Write(\"\") error = %v, want an empty-text error", err)
	}
	if err := Write("   \n\t"); err == nil {
		t.Fatal("whitespace-only text must be rejected as empty")
	}
}

func TestWriteUsesFirstAvailableLinuxTool(t *testing.T) {
	restore := stubClipboard(t, lookPathStub(map[string]bool{"wl-copy": true}), runnerRecorder().record)
	defer restore()

	if err := Write("hello clipboard"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
}

func TestWriteFallsBackThroughCandidates(t *testing.T) {
	rec := &recorder{}
	restore := stubClipboard(t,
		lookPathStub(map[string]bool{"wl-copy": false, "xclip": true}),
		rec.record,
	)
	defer restore()

	if err := Write("payload"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if len(rec.calls) == 0 || !strings.HasSuffix(rec.calls[0].name, "xclip") {
		t.Errorf("expected xclip to run, got %+v", rec.calls)
	}
	if len(rec.calls) > 0 && rec.calls[0].stdin != "payload" {
		t.Errorf("stdin = %q, want the copied text", rec.calls[0].stdin)
	}
}

// TestWriteFailureDoesNotLeakContent pins the security property: when
// every mechanism fails the error names the mechanisms but never the
// copied text.
func TestWriteFailureDoesNotLeakContent(t *testing.T) {
	secret := "sk-super-secret-content-42"
	restore := stubClipboard(t,
		lookPathStub(map[string]bool{}), // no Linux tools
		func(string, []string, string) error { return errors.New("boom") },
	)
	defer restore()
	oldWriteAll := writeAll
	writeAll = func(string) error { return errors.New("no backend") }
	defer func() { writeAll = oldWriteAll }()

	err := Write(secret)
	if err == nil {
		t.Fatal("expected an error when all mechanisms fail")
	}
	msg := err.Error()
	if strings.Contains(msg, secret) {
		t.Errorf("error message leaked the copied text: %q", msg)
	}
	if !strings.Contains(msg, "clipboard") {
		t.Errorf("error %q should mention the clipboard", msg)
	}
}

func TestWriteAtottoFallbackSucceeds(t *testing.T) {
	var got string
	restore := stubClipboard(t, lookPathStub(map[string]bool{}), func(string, []string, string) error {
		return errors.New("no tools")
	})
	defer restore()
	oldWriteAll := writeAll
	writeAll = func(s string) error { got = s; return nil }
	defer func() { writeAll = oldWriteAll }()

	if err := Write("via-atotto"); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got != "via-atotto" {
		t.Errorf("atotto fallback received %q", got)
	}
}

// --- helpers -------------------------------------------------------------

type recordedCall struct {
	name  string
	args  []string
	stdin string
}

type recorder struct{ calls []recordedCall }

func (r *recorder) record(name string, args []string, stdin string) error {
	r.calls = append(r.calls, recordedCall{name: name, args: args, stdin: stdin})
	return nil
}

func runnerRecorder() *recorder {
	return &recorder{}
}

func lookPathStub(available map[string]bool) func(string) (string, error) {
	return func(bin string) (string, error) {
		if available[bin] {
			return "/usr/bin/" + bin, nil
		}
		return "", errors.New("not found")
	}
}

func stubClipboard(t *testing.T,
	lp func(string) (string, error),
	run commandRunner,
) func() {
	t.Helper()
	oldLook, oldRun := lookPath, runCommand
	lookPath, runCommand = lp, run
	return func() { lookPath, runCommand = oldLook, oldRun }
}
