package context

import (
	"os"
	"strings"
)

// firstLines returns the first n lines of the file at path, or "" if the
// file is missing, unreadable, or empty. On Linux/Mac a missing README is
// perfectly normal; callers simply get "".
func firstLines(path string, n int) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(content), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}

	// Trim leading and trailing blank lines so the summary starts and
	// ends with meaningful content.
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}

	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}

// parseGoMod parses the module-level facts out of a go.mod file into a
// GoMod. It returns nil if the file is missing or unreadable. Parsing is
// deliberately loose: it scans for "module", "go", and "require" lines
// without invoking the Go tool. A "require" block enumerates its
// dependency lines until the closing parenthesis; an unparenthesized
// "require path version" line is captured directly.
func parseGoMod(path string) *GoMod {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	gm := &GoMod{}
	inBlock := false
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			// Nothing to do; blank lines separate directives.
		case strings.HasPrefix(line, "module "):
			gm.Module = strings.TrimSpace(strings.TrimPrefix(line, "module "))
		case strings.HasPrefix(line, "go "):
			gm.Go = strings.TrimSpace(strings.TrimPrefix(line, "go "))
		case line == "require (":
			inBlock = true
		case line == ")":
			inBlock = false
		case strings.HasPrefix(line, "require "):
			rest := strings.TrimSpace(strings.TrimPrefix(line, "require "))
			if v := dependencyOf(rest); v != "" {
				gm.Requires = append(gm.Requires, v)
			}
		case inBlock:
			if v := dependencyOf(line); v != "" {
				gm.Requires = append(gm.Requires, v)
			}
		}
	}

	if gm.Module == "" && gm.Go == "" && len(gm.Requires) == 0 {
		return nil
	}
	return gm
}

// dependencyOf parses a dependency line into its "path version" form.
// Returns "" for tool directives or when either part is missing. A
// trailing //indirect comment is stripped from the version.
func dependencyOf(line string) string {
	if strings.HasPrefix(line, "tool ") {
		return "" // tool directives aren't module dependencies we want listed
	}
	idx := strings.Index(line, " ")
	if idx < 0 {
		return ""
	}
	path := strings.TrimSpace(line[:idx])
	version := strings.TrimSpace(line[idx+1:])
	if i := strings.Index(version, "//"); i >= 0 {
		version = strings.TrimSpace(version[:i])
	}
	if path == "" || version == "" {
		return ""
	}
	return path + " " + version
}
