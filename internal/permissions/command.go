// Command safety classification for run_command actions.
//
// The classifier inspects a complete command line and answers whether it
// is safe to run unattended, needs explicit approval, or should be
// refused. It is deliberately conservative: this is not a shell sandbox,
// but obvious destructive or boundary-breaking commands must never
// silently execute.
//
// Compound structure matters. A command containing shell features —
// separators (; && ||), pipes, redirections, command substitution
// ($(...)  or backticks) outside quotes — is never classified by its
// first word: "go test ./... && rm -rf something" must not pass as a
// harmless test run. Such commands require confirmation, and destructive
// segments inside them are called out explicitly.
package permissions

import (
	"regexp"
	"strings"
)

// shellFeatures are the characters that make a bare token stream unsafe
// to judge word-by-word: separators, pipes, redirections, substitution.
const shellFeatureSet = ";|&<>$`\n\r"

// classifyCommand analyzes one full command line as requested through
// the run_command tool. It returns the action class, an approval
// decision, and a human-readable reason. The line is never executed
// here; classification only.
func classifyCommand(line string) (Class, Decision, string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return ClassCommandExecution, Deny, "the command is empty"
	}

	if feature := findShellFeature(line); feature != "" {
		// Judge every segment conservatively before deciding what to
		// report, so a destructive tail is named rather than hidden
		// behind a generic warning.
		reason := "command uses shell features (" + feature + ") and cannot be judged safely"
		if seg := destructiveSegment(line); seg != "" {
			return ClassHighRisk, Ask, reason + "; it contains a destructive part: " + seg
		}
		return ClassCommandExecution, Ask, reason
	}

	fields := splitArgs(line)
	if len(fields) == 0 {
		return ClassCommandExecution, Deny, "the command is empty"
	}
	if quoteRisk(line) {
		return ClassCommandExecution, Ask, "command has unbalanced quotes"
	}
	if strings.Contains(fields[0], "=") {
		return ClassCommandExecution, Ask, "command sets environment variables; not on the always-safe list"
	}

	program := baseProgram(fields[0])
	args := fields[1:]

	if why, risky := destructiveWords(program, args); risky {
		return ClassHighRisk, Ask, why
	}
	if isSafeCommand(program, args) {
		return ClassCommandExecution, Allow, program + " is a routine development command"
	}
	if secretShaped(line) {
		return ClassHighRisk, Ask, "command contains credential-like text"
	}
	return ClassCommandExecution, Ask, program + " is not on the known-safe list; review it before running"
}

// findShellFeature returns the first shell metacharacter appearing
// outside quotes, or "" when the line is a single plain invocation.
// Inside quotes these characters are data, not control flow.
func findShellFeature(line string) string {
	var quote rune
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case strings.ContainsRune(shellFeatureSet, r):
			return string(r)
		}
	}
	return ""
}

// destructiveSegment scans a compound command's separator-delimited
// segments and names the first one whose words look destructive, so
// permission prompts can show exactly which part is dangerous.
func destructiveSegment(line string) string {
	for _, seg := range splitSegments(line) {
		fields := splitArgs(seg)
		if len(fields) == 0 {
			continue
		}
		if _, risky := destructiveWords(baseProgram(fields[0]), fields[1:]); risky {
			return truncateSummary(seg, 80)
		}
	}
	return ""
}

// splitSegments splits a raw line on ; && || | so each segment can be
// inspected individually. It is heuristic (quotes are not tracked here;
// findShellFeature already routed quoted cases away when needed).
func splitSegments(line string) []string {
	parts := strings.FieldsFunc(line, func(r rune) bool {
		return r == ';' || r == '|' || r == '\n'
	})
	var out []string
	for _, p := range parts {
		// Split "a && b" chains too.
		for _, q := range strings.Split(p, "&&") {
			q = strings.TrimSpace(q)
			if q != "" {
				out = append(out, q)
			}
		}
	}
	if len(out) == 0 {
		out = append(out, line)
	}
	return out
}

// destructiveWords recognizes programs and git subcommands that destroy
// work: deletions, history rewrites, force pushes, privilege escalation,
// whole-device writes. Matching is structural (program + arguments),
// never a small fixed blacklist of exact strings.
func destructiveWords(program string, args []string) (string, bool) {
	switch program {
	case "rm", "rmdir", "unlink", "shred", "del", "erase", "rd",
		"remove-item", "ri", "deltree":
		target := strings.Join(args, " ")
		if target == "" {
			return "deletes files", true
		}
		return "deletes " + summarizeTarget(target), true

	case "dd", "mkfs", "mkfs.ext4", "mkfs.xfs", "mkfs.vfat", "diskutil",
		"format", "truncate":
		return "overwrites raw disks or files wholesale", true

	case "sudo", "su", "doas":
		return "runs with elevated privileges", true

	case "shutdown", "reboot", "halt", "poweroff", "kill", "killall", "pkill":
		return "stops processes or the machine", true
	}

	if program == "git" && len(args) > 0 {
		sub, rest := args[0], args[1:]
		switch sub {
		case "reset":
			for _, a := range rest {
				if a == "--hard" || a == "--merge" || a == "-H" {
					return "git reset discards committed and uncommitted work", true
				}
			}
		case "clean":
			for _, a := range rest {
				if strings.HasPrefix(a, "-") && strings.ContainsAny(a[1:], "fdx") {
					return "git clean permanently removes untracked files", true
				}
			}
		case "checkout":
			for _, a := range rest {
				if a == "--" || a == "." || a == "-f" || a == "--force" || a == "-B" {
					return "git checkout can discard local modifications", true
				}
			}
		case "restore":
			return "git restore discards uncommitted changes", true
		case "push":
			for _, a := range rest {
				if a == "--force" || a == "-f" {
					return "force push overwrites remote history", true
				}
			}
		case "branch", "tag":
			for _, a := range rest {
				if a == "-D" {
					return "force-deletes a branch or tag", true
				}
			}
		case "filter-branch", "rebase":
			return "rewrites commit history", true
		}
	}
	return "", false
}

// safePrograms are programs whose common development invocations may run
// without asking: build/test/format tools, read-only inspection
// commands, and read-only git operations.
var safePrograms = map[string]bool{
	"pwd": true, "ls": true, "dir": true, "cat": true, "head": true,
	"tail": true, "wc": true, "file": true, "stat": true, "tree": true,
	"which": true, "whereis": true, "echo": true, "printf": true,
	"date": true, "whoami": true, "hostname": true, "uname": true,
	"env": true, "printenv": true,

	"grep": true, "rg": true, "find": true, "fd": true, "ag": true,

	"go": true, "gofmt": true, "goimports": true, "gofumpt": true,
	"cargo": true, "rustc": true,
	"npm": true, "pnpm": true, "yarn": true, "node": true,
	"python": true, "python3": true, "pytest": true, "pip": true,
	"make": true, "just": true, "task": true,
	"tsc": true, "eslint": true, "prettier": true, "ruff": true,
	"golangci-lint": true, "staticcheck": true, "govulncheck": true,

	"git": true,
}

// goSubcommands allowed without confirmation. Everything else (clean,
// get with -u replacing modules, install writing outside the module,
// workroom surgery) falls back to Ask via the default below.
var goSubcommands = map[string]bool{
	"build": true, "test": true, "vet": true, "fmt": true, "run": true,
	"list": true, "doc": true, "version": true, "env": true,
	"generate": true, "work": true,
}

// gitReadonlySubcommands never modify tracked content.
var gitReadonlySubcommands = map[string]bool{
	"status": true, "diff": true, "log": true, "show": true, "ls-files": true,
	"rev-parse": true, "blame": true, "shortlog": true, "describe": true,
	"remote": true, "grep": true, "branch": true, "tag": true,
}

// findFlags that mutate or execute: -delete removes matches and -exec…
// runs arbitrary programs, so `find . -name x -delete` is not a plain
// listing despite `find` being otherwise safe.
var mutatingFindFlags = map[string]bool{"-delete": true, "-exec": true, "-execdir": true, "-ok": true, "-okdir": true}

// isSafeCommand reports whether program+args form an invocation that may
// run unattended. It applies per-program argument constraints on top of
// the safe-program table; anything unrecognized stays unsafe.
func isSafeCommand(program string, args []string) bool {
	if !safePrograms[program] {
		return false
	}

	switch program {
	case "go":
		if len(args) == 0 || !goSubcommands[args[0]] {
			return false
		}
	case "git":
		if len(args) == 0 {
			return false
		}
		sub := args[0]
		if sub == "config" && len(args) > 1 && args[1] == "--get" {
			return true
		}
		if !gitReadonlySubcommands[sub] {
			return false
		}
		// branch/tag creation and deletion change repo state; only
		// pure listings stay automatic. Force deletion was caught
		// earlier by the destructive pass.
		switch sub {
		case "branch", "tag":
			for _, a := range args[1:] {
				if strings.HasPrefix(a, "-") {
					return false
				}
			}
		}
	case "find":
		for _, a := range args {
			if mutatingFindFlags[a] {
				return false
			}
		}
	case "npm", "pnpm", "yarn", "python", "python3", "pip":
		// Package installation runs third-party code; keep it behind a
		// confirmation while test/build/lint/run stay frictionless.
		for _, a := range args {
			lower := strings.ToLower(a)
			if lower == "install" || lower == "add" || lower == "i" ||
				lower == "uninstall" || lower == "remove" ||
				strings.HasPrefix(lower, "--upgrade") {
				return false
			}
		}
	}
	return true
}

// baseProgram strips a leading path from the program name so /bin/rm
// and rm classify identically.
func baseProgram(first string) string {
	first = strings.ToLower(strings.TrimSpace(first))
	if i := strings.LastIndexAny(first, "/\\"); i >= 0 {
		first = first[i+1:]
	}
	return strings.TrimSuffix(first, ".exe")
}

// splitArgs splits a command line into fields honoring double and
// single quotes (same rules as Lato's process runner).
func splitArgs(line string) []string {
	var (
		fields []string
		cur    strings.Builder
		open   bool
		quote  rune
	)
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			open = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if open {
				fields = append(fields, cur.String())
				cur.Reset()
				open = false
			}
		default:
			cur.WriteRune(r)
			open = true
		}
	}
	if open {
		fields = append(fields, cur.String())
	}
	return fields
}

// quoteRisk reports unbalanced quotes: the runner would reject the
// command anyway, but classification still says so explicitly.
func quoteRisk(line string) bool {
	var quote rune
	for _, r := range line {
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
		}
	}
	return quote != 0
}

// credentialPatterns recognize secret-shaped text in command lines so
// such commands are never silently auto-allowed and their display is
// masked. Values are replaced, keys stay visible for context.
var credentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|passwd|authorization)\b\s*[:=]\s*"?([^\s"]+)`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{10,}\b`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{10,}`),
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

// Precompiled replacements for RedactSecrets, ordered so specific shapes
// (bearer values, key blocks) are masked before the generic key=value
// pass consumes their surrounding context.
var (
	redactPrivateKey = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`).ReplaceAllString
	redactBearer     = regexp.MustCompile(`(?i)(\bbearer\s+)[A-Za-z0-9._~+/=-]{10,}`).ReplaceAllString
	redactKeyValue   = regexp.MustCompile(`(?i)((?:api[_-]?key|secret|token|password|passwd|authorization)\b\s*[:=]\s*)"?([^\s"]+)`).ReplaceAllString
	redactSK         = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{10,}\b`).ReplaceAllString
)

// secretShaped reports whether s contains credential-like material.
func secretShaped(s string) bool {
	for _, re := range credentialPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// RedactSecrets masks credential-shaped values in s while keeping the
// surrounding text readable, so permission prompts and errors never
// expose secrets. Mirrors M11's protection rules for command context.
func RedactSecrets(s string) string {
	s = redactPrivateKey(s, "[redacted private key]")
	s = redactBearer(s, "${1}[redacted]")
	s = redactKeyValue(s, "${1}[redacted]")
	s = redactSK(s, "[redacted]")
	return s
}

// summarizeTarget renders a deletion target compactly for reasons.
func summarizeTarget(target string) string { return truncateSummary(target, 60) }

func truncateSummary(s string, n int) string {
	s = strings.TrimSpace(RedactSecrets(s))
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
