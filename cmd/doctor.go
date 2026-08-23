package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"lato/internal/clipboard"
	"lato/internal/config"
	"lato/internal/install"
	"lato/internal/memory"
	"lato/internal/task"
	"lato/internal/userconfig"
	"lato/internal/workspace"
)

// doctorCmd helps verify a global `lato` installation and the local
// environment Lato depends on. It only reads: no setting is changed,
// no shell configuration is touched, and no secret is ever printed.
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check the installation, configuration, and environment",
	Long: `Doctor reports where the running lato binary lives, whether that
location is on your PATH (so plain "lato" works from any terminal),
where configuration, provider connections, memory, and tasks are
stored, which workspace you are in, and which clipboard helper would
be used. It changes nothing.`,
	Args: cobra.NoArgs,

	RunE: func(cmd *cobra.Command, args []string) error {
		return runDoctor()
	},
}

func runDoctor() error {
	fmt.Println("Lato environment check")
	fmt.Println()

	exe := ""
	if raw, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(raw); err == nil {
			exe = real
		} else {
			exe = raw
		}
	}
	fmt.Printf("  Executable   %s\n", orUnknown(exe))
	if exe != "" {
		dir := filepath.Dir(exe)
		if install.OnPath(dir) {
			fmt.Println("  PATH         ok — the executable's directory is on PATH")
		} else {
			fmt.Println("  PATH         warning — the executable's directory is NOT on PATH")
			for _, line := range install.Hint(install.LocalBinDir()) {
				fmt.Printf("                 %s\n", line)
			}
		}
	}

	if bin := install.LocalBinDir(); bin != "" {
		fmt.Printf("  Install dir  %s (go install . target)\n", bin)
	}

	cfgDir, err := config.Dir()
	switch {
	case err != nil:
		fmt.Printf("  Config       unavailable: %v\n", err)
	default:
		cfgPath := filepath.Join(cfgDir, "config.yaml")
		if _, statErr := os.Stat(cfgPath); statErr == nil {
			fmt.Printf("  Config       %s\n", cfgPath)
		} else {
			fmt.Printf("  Config       %s (missing; created on first start)\n", cfgPath)
		}
	}

	printProviders()
	printStoreDir("Memory", memory.Dir)
	printStoreDir("Tasks", task.Dir)

	ws := workspace.Discover()
	fmt.Printf("  Workspace    %s\n", ws.Root)
	fmt.Printf("  Working dir  %s\n", ws.CWD)

	if name, ok := clipboard.Backend(); ok {
		fmt.Printf("  Clipboard    %s available\n", name)
	} else {
		fmt.Printf("  Clipboard    not ready (%s); /copy will report this at use time\n", clipboardHint())
	}

	fmt.Println()
	fmt.Println(`Run "lato" in any project directory to start.`)
	return nil
}

// printProviders reports the provider connection store without ever
// showing endpoint credentials.
func printProviders() {
	path, err := userconfig.Path()
	if err != nil {
		fmt.Printf("  Providers    unavailable: %v\n", err)
		return
	}
	store, loadErr := userconfig.Load()
	switch {
	case loadErr != nil:
		fmt.Printf("  Providers    %s (unreadable: %v)\n", path, loadErr)
	case len(store.Connections) == 0:
		fmt.Printf("  Providers    %s (none configured — run /connect inside lato)\n", path)
	default:
		fmt.Printf("  Providers    %s (%d configured)\n", path, len(store.Connections))
	}
}

func printStoreDir(label string, resolve func() (string, error)) {
	dir, err := resolve()
	if err != nil {
		fmt.Printf("  %-12s unavailable: %v\n", label, err)
		return
	}
	fmt.Printf("  %-12s %s\n", label, dir)
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

func init() {
	rootCmd.AddCommand(doctorCmd)
}
