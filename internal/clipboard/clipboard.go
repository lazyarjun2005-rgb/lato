// Package clipboard writes text to the system clipboard.
//
// It is deliberately small and dependency-light: on Linux it prefers
// native Wayland/X11 tools (wl-copy, xclip, xsel) and falls back to
// github.com/atotto/clipboard (which also covers macOS pbcopy and
// Windows clip.exe). Text is never included in returned errors, so no
// copied content can leak into logs or diagnostics.
package clipboard

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"
)

// writeAll delegates to atotto (pbcopy/clip.exe/xclip/xsel) and is
// swappable in tests.
var writeAll = clipboard.WriteAll

// commandRunner runs one clipboard helper with text piped to stdin. It
// exists so tests can fake process execution.
type commandRunner func(name string, args []string, stdin string) error

var (
	lookPath                 = exec.LookPath
	runCommand commandRunner = func(name string, args []string, stdin string) error {
		cmd := exec.Command(name, args...)
		cmd.Stdin = strings.NewReader(stdin)
		return cmd.Run()
	}
)

// linuxCandidates lists clipboard helpers from most preferred to least:
// Wayland first, then X11 selections.
func linuxCandidates() []struct {
	name string
	args []string
} {
	return []struct {
		name string
		args []string
	}{
		{"wl-copy", nil},
		{"xclip", []string{"-selection", "clipboard"}},
		{"xsel", []string{"--clipboard", "--input"}},
	}
}

// Backend reports which clipboard mechanism Write would try first on
// this machine: the name of the helper program it found ("wl-copy",
// "xclip", "xsel", "pbcopy", "clip.exe") and whether one was located.
// It never executes anything and is meant for diagnostics (`lato
// doctor`); the fallback atotto backend, whose availability can only be
// proven by writing, is reported as "system" when no native helper was
// found.
func Backend() (string, bool) {
	switch runtime.GOOS {
	case "linux":
		for _, c := range linuxCandidates() {
			if _, err := lookPath(c.name); err == nil {
				return c.name, true
			}
		}
	case "darwin":
		if _, err := lookPath("pbcopy"); err == nil {
			return "pbcopy", true
		}
	case "windows":
		if _, err := lookPath("clip.exe"); err == nil {
			return "clip.exe", true
		}
	}
	return "system clipboard backend", false
}

// Write places text on the system clipboard. The full text is written;
// it is never trimmed or transformed, and never echoed in errors.
func Write(text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("nothing to copy: the text is empty")
	}

	var attempted []string

	if runtime.GOOS == "linux" {
		for _, c := range linuxCandidates() {
			path, err := lookPath(c.name)
			if err != nil {
				continue
			}
			if err := runCommand(path, c.args, text); err != nil {
				attempted = append(attempted, c.name)
				continue
			}
			return nil
		}
	}

	if err := writeAll(text); err != nil {
		attempted = append(attempted, "system clipboard backend")
		hint := ""
		switch runtime.GOOS {
		case "linux":
			hint = " — install one of: wl-copy, xclip, xsel"
		case "darwin":
			hint = " — pbcopy was not available"
		case "windows":
			hint = " — clip.exe was not available"
		}
		return fmt.Errorf("clipboard write failed via %s%s", strings.Join(attempted, ", "), hint)
	}
	return nil
}
