// Package doctor renders Lato's environment report: where the running
// binary lives, whether that location is on PATH, where configuration,
// provider connections, memory, and tasks are stored, which workspace
// is active, and which clipboard helper would be used.
//
// It only reads: no setting is changed, no shell configuration is
// touched, and no secret is ever printed. The report is rendered into
// a caller-provided writer so both `lato doctor` (stdout) and the
// /doctor slash command (chat transcript) share one implementation.
package doctor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"lato/internal/clipboard"
	"lato/internal/config"
	"lato/internal/install"
	"lato/internal/memory"
	"lato/internal/task"
	"lato/internal/userconfig"
	"lato/internal/workspace"
)

// Report writes the environment check to w. ws is the workspace to
// describe: command-line callers pass a fresh Discover(), the
// interactive TUI passes its runtime's cached workspace so invoking
// the command never rescans the repository.
func Report(w io.Writer, ws workspace.Info) {
	fmt.Fprintln(w, "Lato environment check")
	fmt.Fprintln(w)

	exe := ""
	if raw, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(raw); err == nil {
			exe = real
		} else {
			exe = raw
		}
	}
	fmt.Fprintf(w, "  Executable   %s\n", orUnknown(exe))
	if exe != "" {
		dir := filepath.Dir(exe)
		if install.OnPath(dir) {
			fmt.Fprintln(w, "  PATH         ok — the executable's directory is on PATH")
		} else {
			fmt.Fprintln(w, "  PATH         warning — the executable's directory is NOT on PATH")
			for _, line := range install.Hint(install.LocalBinDir()) {
				fmt.Fprintf(w, "                 %s\n", line)
			}
		}
	}

	if bin := install.LocalBinDir(); bin != "" {
		fmt.Fprintf(w, "  Install dir  %s (go install . target)\n", bin)
	}

	cfgDir, err := config.Dir()
	switch {
	case err != nil:
		fmt.Fprintf(w, "  Config       unavailable: %v\n", err)
	default:
		cfgPath := filepath.Join(cfgDir, "config.yaml")
		if _, statErr := os.Stat(cfgPath); statErr == nil {
			fmt.Fprintf(w, "  Config       %s\n", cfgPath)
		} else {
			fmt.Fprintf(w, "  Config       %s (missing; created on first start)\n", cfgPath)
		}
	}

	printProviders(w)
	printStoreDir(w, "Memory", memory.Dir)
	printStoreDir(w, "Tasks", task.Dir)

	fmt.Fprintf(w, "  Workspace    %s\n", ws.Root)
	fmt.Fprintf(w, "  Working dir  %s\n", ws.CWD)

	if name, ok := clipboard.Backend(); ok {
		fmt.Fprintf(w, "  Clipboard    %s available\n", name)
	} else {
		fmt.Fprintf(w, "  Clipboard    not ready (%s); /copy will report this at use time\n", clipboardHint())
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, `Run "lato" in any project directory to start.`)
}

// printProviders reports the provider connection store without ever
// showing endpoint credentials.
func printProviders(w io.Writer) {
	path, err := userconfig.Path()
	if err != nil {
		fmt.Fprintf(w, "  Providers    unavailable: %v\n", err)
		return
	}
	store, loadErr := userconfig.Load()
	switch {
	case loadErr != nil:
		fmt.Fprintf(w, "  Providers    %s (unreadable: %v)\n", path, loadErr)
	case len(store.Connections) == 0:
		fmt.Fprintf(w, "  Providers    %s (none configured — run /connect inside lato)\n", path)
	default:
		fmt.Fprintf(w, "  Providers    %s (%d configured)\n", path, len(store.Connections))
	}
}

func printStoreDir(w io.Writer, label string, resolve func() (string, error)) {
	dir, err := resolve()
	if err != nil {
		fmt.Fprintf(w, "  %-12s unavailable: %v\n", label, err)
		return
	}
	fmt.Fprintf(w, "  %-12s %s\n", label, dir)
}

func clipboardHint() string {
	switch runtime.GOOS {
	case "linux":
		return "install wl-copy, xclip, or xsel"
	case "darwin":
		return "pbcopy was not found"
	case "windows":
		return "clip.exe was not found"
	default:
		return "no system clipboard helper found"
	}
}

func orUnknown(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}
