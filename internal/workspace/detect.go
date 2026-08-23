package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

// detectLanguage picks the primary programming language from the
// project-root manifest files, falling back to the most common source
// extension counted during the tree walk. It is purely file-based and
// deterministic: no AI, no external commands.
func detectLanguage(present map[string]bool, ext map[string]int) string {
	if present["go.mod"] || present["go.work"] {
		return "Go"
	}
	if present["Cargo.toml"] {
		return "Rust"
	}
	if present["pyproject.toml"] || present["requirements.txt"] || present["setup.py"] {
		return "Python"
	}
	if present["package.json"] {
		if present["tsconfig.json"] {
			return "TypeScript"
		}
		return "JavaScript"
	}
	if present["pom.xml"] || present["build.gradle"] || present["build.gradle.kts"] {
		return "Java"
	}
	if present[".csproj"] || present[".sln"] {
		return "C#"
	}

	// No manifest: fall back to the most common source extension.
	best, bestCount := "", 0
	for extName, count := range ext {
		lang, ok := extensionLanguages[extName]
		if !ok || count <= bestCount {
			continue
		}
		best, bestCount = lang, count
	}
	return best
}

// extensionLanguages maps source-file extensions to their language,
// consulted only when no manifest identifies the project.
var extensionLanguages = map[string]string{
	".go":    "Go",
	".py":    "Python",
	".rs":    "Rust",
	".ts":    "TypeScript",
	".tsx":   "TypeScript",
	".js":    "JavaScript",
	".jsx":   "JavaScript",
	".cs":    "C#",
	".java":  "Java",
	".kt":    "Kotlin",
	".rb":    "Ruby",
	".php":   "PHP",
	".c":     "C",
	".h":     "C",
	".cpp":   "C++",
	".cc":    "C++",
	".cxx":   "C++",
	".hpp":   "C++",
	".swift": "Swift",
	".sh":    "Shell",
}

// detectFramework reports a well-known framework for the detected
// language, based on root files or manifest contents. Returns "" when
// there is no reliable signal.
func detectFramework(lang, root string) string {
	switch lang {
	case "JavaScript", "TypeScript":
		// Framework is inferred from the presence of a project's config
		// file at the root. Check the most specific marker first.
		switch {
		case pathExists(filepath.Join(root, "nuxt.config.ts")) || pathExists(filepath.Join(root, "nuxt.config.js")):
			return "Nuxt"
		case pathExists(filepath.Join(root, "svelte.config.js")) || pathExists(filepath.Join(root, "svelte.config.ts")):
			return "SvelteKit"
		case pathExists(filepath.Join(root, "next.config.js")) || pathExists(filepath.Join(root, "next.config.mjs")) || pathExists(filepath.Join(root, "next.config.ts")):
			return "Next.js"
		case pathExists(filepath.Join(root, "vite.config.ts")) || pathExists(filepath.Join(root, "vite.config.js")):
			return "Vite"
		}
	case "Python":
		if pathExists(filepath.Join(root, "manage.py")) {
			return "Django"
		}
		if pathExists(filepath.Join(root, "app.py")) {
			return "Flask"
		}
	case "Rust":
		if pathExists(filepath.Join(root, "Cargo.toml")) {
			if c := readFileString(filepath.Join(root, "Cargo.toml")); strings.Contains(c, "actix-web") {
				return "Actix Web"
			} else if strings.Contains(c, "axum") {
				return "Axum"
			} else if strings.Contains(c, "rocket") {
				return "Rocket"
			}
		}
	}
	return ""
}

// detectModule returns the module/package identifier from the language's
// manifest, or "" if there is none or it cannot be read.
func detectModule(lang, root string) string {
	switch lang {
	case "Go":
		return readGoModule(filepath.Join(root, "go.mod"))
	case "Rust":
		return readKeyValue(filepath.Join(root, "Cargo.toml"), "name")
	case "Python":
		return readPyName(filepath.Join(root, "pyproject.toml"))
	case "JavaScript", "TypeScript":
		return readJSONField(filepath.Join(root, "package.json"), "name")
	case "Java":
		return readXMLField(filepath.Join(root, "pom.xml"), "artifactId")
	case "C#":
		return readXMLFieldFirst(filepath.Join(root, ".csproj"))
	}
	return ""
}

// detectBuildSystem reports the build system implied by the language and
// its root files.
func detectBuildSystem(lang, root string, present map[string]bool) string {
	switch lang {
	case "Go":
		if present["go.work"] {
			return "Go workspaces"
		}
		if present["go.mod"] {
			return "Go modules"
		}
		return ""
	case "Rust":
		return "Cargo"
	case "Python":
		if present["pyproject.toml"] {
			if c := readFileString(filepath.Join(root, "pyproject.toml")); strings.Contains(c, "poetry") {
				return "Poetry"
			}
			return "PEP 517"
		}
		if present["setup.py"] {
			return "Setuptools"
		}
		return ""
	case "JavaScript", "TypeScript":
		if present["package.json"] {
			if s := readFileString(filepath.Join(root, "package.json")); strings.Contains(s, `"next"`) {
				return "Next.js"
			}
			return "npm"
		}
		return ""
	case "Java":
		switch {
		case present["build.gradle"] || present["build.gradle.kts"]:
			return "Gradle"
		case present["pom.xml"]:
			return "Maven"
		}
	case "C#":
		return ".NET SDK"
	}
	return ""
}

// detectPackageManager reports the package manager implied by lockfiles
// and manifests, or "" when none is found.
func detectPackageManager(lang, root string, present map[string]bool) string {
	switch lang {
	case "Go":
		if present["go.mod"] || present["go.work"] {
			return "Go modules"
		}
		return ""
	case "Rust":
		return "Cargo"
	case "Python":
		switch {
		case present["pyproject.toml"]:
			if c := readFileString(filepath.Join(root, "pyproject.toml")); strings.Contains(c, "poetry") {
				return "Poetry"
			} else if strings.Contains(c, "uv") {
				return "uv"
			}
			return "pip"
		case present["requirements.txt"]:
			return "pip"
		}
	case "JavaScript", "TypeScript":
		switch {
		case present["pnpm-lock.yaml"]:
			return "pnpm"
		case present["yarn.lock"]:
			return "Yarn"
		case present["package-lock.json"]:
			return "npm"
		case present["package.json"]:
			return "npm"
		}
	case "C#":
		return "NuGet"
	}
	return ""
}

// readGoModule returns the module path from a go.mod file: the first
// token after "module" on a line that starts with the word module.
func readGoModule(path string) string {
	for _, line := range strings.Split(readFileString(path), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// readKeyValue returns the value of the first `key = "value"` assignment
// in a TOML-ish file. Used for Cargo.toml's package name.
func readKeyValue(path, key string) string {
	for _, line := range strings.Split(readFileString(path), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+" =") {
			value := strings.TrimSpace(strings.TrimPrefix(line, key+" ="))
			return strings.Trim(value, `"'`)
		}
	}
	return ""
}

// readPyName returns the name from a pyproject.toml file's [project]
// table, or "" if there is none.
func readPyName(path string) string {
	inProject := false
	for _, line := range strings.Split(readFileString(path), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inProject = strings.HasPrefix(line, "[project")
			continue
		}
		if inProject && strings.HasPrefix(line, "name") && strings.Contains(line, "=") {
			value := strings.TrimSpace(strings.SplitN(line, "=", 2)[1])
			return strings.Trim(value, `"'`)
		}
	}
	return ""
}

// readJSONField returns the string value of key in a JSON file, without
// pulling in a JSON parser. Falls back to "" on any malformed input.
func readJSONField(path, key string) string {
	s := readFileString(path)
	idx := strings.Index(s, `"`+key+`"`)
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(key)+2:]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[colon+1:])
	if !strings.HasPrefix(rest, `"`) {
		return ""
	}
	end := strings.Index(rest[1:], `"`)
	if end < 0 {
		return ""
	}
	return rest[1 : 1+end]
}

// readXMLField returns the text content of the first occurrence of
// <tag>…</tag> in an XML file. Used for pom.xml's artifactId.
func readXMLField(path, tag string) string {
	s := readFileString(path)
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i+len(open):], close)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(s[i+len(open) : i+len(open)+j])
}

// readXMLFieldFirst returns the first <ProjectName> element in a .csproj
// file, falling back to the first <AssemblyName>.
func readXMLFieldFirst(path string) string {
	if v := readXMLField(path, "ProjectName"); v != "" {
		return v
	}
	return readXMLField(path, "AssemblyName")
}

func readFileString(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
