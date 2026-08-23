package install

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestOnPathInExactEntryOnly pins that PATH membership is decided per
// entry, never by string prefix: a directory must match its own entry
// and nothing else.
func TestOnPathInExactEntryOnly(t *testing.T) {
	sep := string(filepath.ListSeparator)
	pathEnv := strings.Join([]string{"/usr/local/bin", "/home/user/go/bin", "/home/user/.local/bin"}, sep)

	if !OnPathIn("/home/user/go/bin", pathEnv) {
		t.Error("listed bin dir not found on PATH")
	}
	if OnPathIn("/home/user/go", pathEnv) {
		t.Error("parent of a PATH entry matched by prefix")
	}
	if OnPathIn("/usr/local/bin-extra", pathEnv) {
		t.Error("sibling with a shared prefix matched")
	}
	if OnPathIn("", pathEnv) {
		t.Error("empty directory reported as on PATH")
	}
}

func TestOnPathInWindowsSeparators(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PATH syntax only applies on Windows")
	}
	pathEnv := `C:\Users\dev\go\bin;C:\Users\dev\.local\bin`
	if !OnPathIn(`C:\Users\dev\go\bin`, pathEnv) {
		t.Error("Windows-style entry not matched")
	}
	if !OnPathIn(`c:/users/dev/.local/bin`, pathEnv) {
		t.Error("case and separator folding failed")
	}
	if OnPathIn(`D:\elsewhere`, pathEnv) {
		t.Error("unrelated drive matched")
	}
}

func TestOnPathInUnixSeparators(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix PATH syntax does not apply on Windows")
	}
	pathEnv := "/opt/lato/bin:/opt/lato:/usr/bin"
	if !OnPathIn("/opt/lato/bin", pathEnv) {
		t.Error("/opt/lato/bin should be on PATH")
	}
	if OnPathIn("/opt/lato/other", pathEnv) {
		t.Error("prefix confusion: /opt/lato matched /opt/lato/other")
	}
}

func TestHintSilentWhenOnPath(t *testing.T) {
	dir := t.TempDir()
	if got := Hint(dir); len(got) != 0 && OnPath(dir) {
		t.Errorf("Hint produced output for a directory already on PATH: %v", got)
	}
}

func TestHintNamesDirectoryAndExport(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nowhere", "bin")
	got := Hint(missing)
	if len(got) == 0 {
		t.Fatal("expected setup hint for an off-PATH directory")
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, missing) {
		t.Errorf("hint %q does not name the install directory", joined)
	}
	switch runtime.GOOS {
	case "windows":
		if !strings.Contains(joined, "setx") {
			t.Errorf("Windows hint %q does not suggest a way to update PATH", joined)
		}
	default:
		if !strings.Contains(joined, "export PATH=") {
			t.Errorf("Unix hint %q does not include the export line", joined)
		}
	}
}

// TestLocalBinDirPrefersGoBin pins the precedence GOBIN > GOPATH/bin >
// ~/.local/bin using a fake HOME so the developer's real environment
// cannot influence the result.
func TestLocalBinDirPrefersGoBin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")

	want := filepath.Join(home, "go", "bin")
	if runtime.GOOS == "windows" {
		want = filepath.Join(home, "go", "bin")
	}
	dirs := BinDirs()
	if len(dirs) == 0 || dirs[0] != want {
		t.Fatalf("BinDirs() = %v, want first entry %q", dirs, want)
	}
	if LocalBinDir() != want {
		t.Errorf("LocalBinDir() = %q, want %q", LocalBinDir(), want)
	}

	local := filepath.Join(home, ".local", "bin")
	found := false
	for _, d := range dirs {
		if d == local {
			found = true
		}
	}
	if !found {
		t.Errorf("~/.local/bin missing from candidates: %v", dirs)
	}

	t.Setenv("GOPATH", filepath.Join(home, "gopath"))
	t.Setenv("GOBIN", filepath.Join(home, "custom-bin"))
	dirs = BinDirs()
	if dirs[0] != filepath.Join(home, "custom-bin") {
		t.Errorf("GOBIN not preferred: %v", dirs)
	}
}
