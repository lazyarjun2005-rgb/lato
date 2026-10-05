package tui

import "testing"

func TestDisplayWorkspacePath(t *testing.T) {
	cases := []struct {
		name, path, want string
	}{
		{"linux", "/home/arjun/projects/lato", "/home/arjun/projects/lato"},
		{"windows", `C:\Users\Arjun\Projects\lato`, "C:/Users/Arjun/Projects/lato"},
		{"unc", `\\server\share\projects\lato`, "//server/share/projects/lato"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := displayWorkspacePath(tc.path, 80); got != tc.want {
				t.Fatalf("displayWorkspacePath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDisplayWorkspacePathTruncatesNarrowWidth(t *testing.T) {
	got := displayWorkspacePath(`C:\Users\Arjun\Projects\very-long-project`, 20)
	if len([]rune(got)) > 20 {
		t.Fatalf("path length = %d, want <= 20: %q", len([]rune(got)), got)
	}
}
