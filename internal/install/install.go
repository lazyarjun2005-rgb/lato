// Package install reasons about Lato's global command-line installation:
// where a user-local lato binary lives, whether that location is on the
// PATH, and what to do when it is not.
//
// It deliberately contains no installer logic of its own. Installing
// Lato means placing a built executable in one of the directories
// listed here — `go install .` does exactly that into the Go bin
// directory — and the scripts in scripts/ automate it. Nothing in this
// package edits shell configuration files; it only reports actionable
// instructions for the user to run themselves.
package install

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// BinDirs lists candidate user-local binary directories, most preferred
// first, deduplicated:
//
//  1. $GOBIN when set
//  2. $GOPATH/bin (or $HOME/go/bin when GOPATH is unset) — where
//     `go install` places binaries
//  3. $HOME/.local/bin — the generic user-local location on Unix
func BinDirs() []string {
	home, err := os.UserHomeDir()

	var dirs []string
	add := func(d string) {
		d = strings.TrimSpace(d)
		if d == "" {
			return
		}
		abs, err := filepath.Abs(d)
		if err != nil || !filepath.IsAbs(abs) {
			return // relative or unresolvable candidates are not usable install targets
		}
		d = filepath.Clean(abs)
		for _, existing := range dirs {
			if sameDir(existing, d) {
				return
			}
		}
		dirs = append(dirs, d)
	}

	add(os.Getenv("GOBIN"))

	gopath := os.Getenv("GOPATH")
	if gopath == "" && err == nil {
		gopath = filepath.Join(home, "go")
	}
	// GOPATH may list several directories; binaries install into the first.
	if first := filepath.SplitList(gopath)[0]; first != "" {
		add(filepath.Join(first, "bin"))
	}

	if err == nil {
		add(filepath.Join(home, ".local", "bin"))
	}
	return dirs
}

// LocalBinDir returns the directory `go install` places the lato binary
// in on this machine ($GOBIN, $GOPATH/bin, or $HOME/go/bin), or ""
// when no home directory can be resolved.
func LocalBinDir() string {
	dirs := BinDirs()
	if len(dirs) == 0 {
		return ""
	}
	return dirs[0]
}

// OnPath reports whether dir appears in the PATH environment variable.
// Comparison uses cleaned absolute forms and folds case on Windows,
// where paths are case-insensitive; string-prefix tricks are never
// used, so /opt/lato/bin does not match an entry of /opt/lato.
func OnPath(dir string) bool {
	return OnPathIn(dir, os.Getenv("PATH"))
}

// OnPathIn is OnPath against an explicit PATH-style value (tests use
// this to exercise platform-specific separator behavior).
func OnPathIn(dir, pathEnv string) bool {
	dir = cleanPath(dir)
	if dir == "" {
		return false
	}
	for _, entry := range filepath.SplitList(pathEnv) {
		if sameDir(cleanPath(entry), dir) {
			return true
		}
	}
	return false
}

// ExecutableDir returns the directory containing the running lato
// binary, resolved through symlinks so a ~/.local/bin/lato link that
// points at a repository build reports the link's directory, not the
// target's. It returns "" when the executable path cannot be resolved.
func ExecutableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Dir(exe)
}

// Hint returns actionable setup lines for making `lato` invocable from
// any terminal when binDir is not yet on PATH. The lines never modify
// anything by themselves; they are commands the user chooses to run.
// An empty result means nothing needs fixing.
func Hint(binDir string) []string {
	if binDir == "" || OnPath(binDir) {
		return nil
	}
	switch runtime.GOOS {
	case "windows":
		return []string{
			"Lato is installed at " + binDir + ", which is not on your PATH.",
			"To make `lato` available in every terminal, run:",
			"  setx PATH \"%PATH%;" + binDir + "\"",
			"(then open a new terminal), or add the directory through",
			"Settings → System → About → Advanced system settings → Environment Variables.",
		}
	default:
		shell := "your shell configuration (~/.bashrc, ~/.zshrc, or equivalent)"
		return []string{
			"Lato is installed at " + binDir + ", which is not on your PATH.",
			"To make `lato` available in every terminal, add this line to " + shell + ":",
			"  export PATH=\"$PATH:" + binDir + "\"",
			"Then restart your shell or run: source " + shellConfig(),
		}
	}
}

func shellConfig() string {
	switch runtime.GOOS {
	case "darwin":
		return "~/.zshrc"
	default:
		return "~/.bashrc"
	}
}

// cleanPath normalizes one PATH entry for comparison.
func cleanPath(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = filepath.Clean(p)
	}
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	return filepath.Clean(abs)
}

// sameDir compares two cleaned paths, folding case on Windows.
func sameDir(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
