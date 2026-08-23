// Package retrieve turns a natural-language question into bounded,
// evidence-backed source excerpts from the repository index, so the
// model can answer repository questions from actual code instead of
// README-and-tree guesswork.
//
// Retrieval is deterministic and local: one pass over the cached index
// (no rescan of the disk, no model calls, no network) scores files
// against the question's own terms, extracts short excerpts around the
// matching lines, summarizes the Go symbols of the winning files, and
// follows Go imports to directly related files. Output is strictly
// bounded so even a question over a large repository yields a compact
// prompt block, never a repository dump.
package retrieve

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"lato/internal/index"
)

// Bounds for one evidence block. They exist so a question over any
// repository — however large or repetitive — produces a compact,
// high-value prompt injection.
// These bounds are deliberately tight: prompt tokens dominate end-to-end
// latency on local CPU-only models (thousands of prompt tokens can cost
// minutes at ~7 tok/s prompt processing), so evidence must earn its place.
const (
	maxTerms           = 8       // question terms considered
	maxFiles           = 3       // files in the evidence block
	maxExcerptsPerFile = 3       // separate excerpt ranges per file
	excerptContext     = 3       // lines of context on each side of a match
	maxExcerptLines    = 12      // total excerpt lines per file
	maxSymbolsShown    = 5       // symbol summary lines per file
	maxSymbolBonus     = 12      // cap on symbol-match score per file
	maxBodyBonus       = 15      // cap on content-match score per file
	maxRelated         = 2       // import-related files across the block
	maxEvidenceBytes   = 2 << 10 // hard cap on the rendered block
)

// Evidence is the retrieval result for one question. It is produced by
// ForQuestion and rendered with Text; an evidence with no files renders
// as "" and callers should inject nothing.
type Evidence struct {
	Question string
	Repo     string // repository name, for the block header
	Files    []FileEvidence
}

// FileEvidence is one file worth of source evidence: where it matched,
// what it declares, and short excerpts around the matches.
type FileEvidence struct {
	Path     string // slash-separated, relative to the workspace root
	Lang     string
	Symbols  []index.Symbol // declaration summary, declaration order
	Excerpts []Excerpt
	Related  []RelatedFile // files related through Go imports
}

// Excerpt is a contiguous, line-numbered range of the file's source.
type Excerpt struct {
	Start int      // 1-based first line
	Lines []string // raw lines, Start..Start+len(Lines)-1
}

// RelatedFile names a file reached by following an import from an
// evidence file, with its own symbol summary.
type RelatedFile struct {
	Path    string // slash-separated, relative to the workspace root
	Via     string // the import path that led here
	Package string // Go package name, "" if unknown
	Symbols []index.Symbol
}

// Empty reports whether retrieval found nothing worth injecting.
func (e *Evidence) Empty() bool { return e == nil || len(e.Files) == 0 }

// scored pairs a candidate file with its relevance score and the body
// lines that matched, so excerpts can be cut without rescanning.
type scored struct {
	file  index.File
	score int
	lines []int // matching body lines, ascending
}

// ForQuestion scores the indexed repository against question and
// returns the resulting Evidence. It never fails and never returns an
// error: a question with no usable terms or no matches simply yields an
// empty Evidence. The index must already be built (see index.Builder);
// nothing here touches the disk.
func ForQuestion(idx *index.Index, repo, question string) *Evidence {
	ev := &Evidence{Question: strings.TrimSpace(question), Repo: repo}
	if idx == nil || !idx.Built() || ev.Question == "" {
		return ev
	}

	terms := Terms(ev.Question)
	if len(terms) == 0 {
		return ev
	}

	files := idx.Files()
	results := make([]*scored, 0, len(files))

	for i := range files {
		f := &files[i]
		if f.Binary {
			continue
		}
		s := scoreFile(f, terms)
		if s.score > 0 {
			results = append(results, s)
		}
	}

	sort.Slice(results, func(a, b int) bool {
		if results[a].score != results[b].score {
			return results[a].score > results[b].score
		}
		return results[a].file.Path < results[b].file.Path
	})
	if len(results) > maxFiles {
		results = results[:maxFiles]
	}

	primary := map[string]bool{}
	for _, s := range results {
		primary[s.file.Path] = true
	}

	relatedSeen := map[string]bool{}
	for _, s := range results {
		fe := FileEvidence{
			Path:     s.file.Path,
			Lang:     s.file.Lang,
			Symbols:  topSymbols(s.file.Symbols, maxSymbolsShown),
			Excerpts: excerpts(s.file.Body, s.lines),
		}
		if s.file.Lang == "Go" {
			for _, rel := range relatedFiles(idx, &s.file, primary, relatedSeen) {
				fe.Related = append(fe.Related, rel)
			}
		}
		ev.Files = append(ev.Files, fe)
	}
	return ev
}

// scoreFile computes one file's relevance to terms in a single pass and
// records the body lines that matched, so excerpts can be cut without a
// second scan. Scores mirror the search ranking's intuition: a symbol
// or name hit identifies purpose and outvalues an incidental content
// hit; per-category caps keep a file that repeats one term from
// dominating the block.
func scoreFile(f *index.File, terms []string) *scored {
	s := &scored{file: *f}

	nameLower := strings.ToLower(f.Name)
	pathLower := strings.ToLower(f.Path)
	symbolHits := 0
	for _, t := range terms {
		if strings.Contains(nameLower, t) {
			s.score += 4
		}
		if strings.Contains(pathLower, t) {
			s.score += 2
		}
		for _, sym := range f.Symbols {
			if symbolHits >= maxSymbolBonus {
				break
			}
			if strings.Contains(strings.ToLower(sym.Name), t) {
				s.score += 4
				symbolHits += 4
			}
		}
	}

	if f.Body == "" {
		return s
	}

	// One lowercased copy of the body serves every term; lines are
	// scanned once with a set-membership test per line.
	termSet := make(map[string]bool, len(terms))
	for _, t := range terms {
		termSet[t] = true
	}
	bodyBonus := 0
	for i, raw := range strings.Split(f.Body, "\n") {
		if bodyBonus >= maxBodyBonus {
			break
		}
		if containsAnyTerm(strings.ToLower(raw), termSet) {
			s.score += 3
			bodyBonus += 3
			s.lines = append(s.lines, i+1)
		}
	}
	return s
}

// containsAnyTerm reports whether line contains any of the terms.
func containsAnyTerm(line string, terms map[string]bool) bool {
	for t := range terms {
		if strings.Contains(line, t) {
			return true
		}
	}
	return false
}

// topSymbols returns up to n symbols in declaration order.
func topSymbols(syms []index.Symbol, n int) []index.Symbol {
	if len(syms) > n {
		return syms[:n]
	}
	return syms
}

// excerpts cuts bounded, line-numbered source ranges around the given
// match lines. Nearby matches merge into one range; the total stays
// within maxExcerptLines, and ranges beyond the indexed body are
// dropped (a truncated body still yields its available prefix).
func excerpts(body string, matchLines []int) []Excerpt {
	if len(matchLines) == 0 || body == "" {
		return nil
	}
	lines := strings.Split(body, "\n")
	total := len(lines)

	var out []Excerpt
	used := 0
	for _, m := range matchLines {
		if used >= maxExcerptLines || len(out) >= maxExcerptsPerFile {
			break
		}
		start := m - excerptContext
		if start < 1 {
			start = 1
		}
		end := m + excerptContext
		if end > total {
			end = total
		}
		if start > end {
			continue
		}

		// Merge with the previous range when they touch or overlap.
		if last := len(out) - 1; last >= 0 && start <= out[last].Start+len(out[last].Lines) {
			prev := &out[last]
			mergedEnd := end
			if cap := prev.Start + maxExcerptLines - used; mergedEnd > cap {
				mergedEnd = cap
			}
			if mergedEnd > prev.Start+len(prev.Lines) {
				prev.Lines = append(prev.Lines, lines[prev.Start+len(prev.Lines)-1:mergedEnd-1]...)
				used += mergedEnd - (prev.Start + len(prev.Lines))
			}
			continue
		}

		budget := maxExcerptLines - used
		if end-start+1 > budget {
			end = start + budget - 1
		}
		if end < m {
			continue // not enough budget to reach the match itself
		}
		out = append(out, Excerpt{Start: start, Lines: append([]string(nil), lines[start-1:end]...)})
		used += end - start + 1
	}
	return out
}

// relatedFiles follows f's Go imports to other indexed files: an import
// path's last meaningful segment is matched against candidate files'
// containing directory or package name. Files already in the evidence
// block are skipped, seen tracks the global cap across all evidence
// files, and each result carries a small symbol summary.
func relatedFiles(idx *index.Index, f *index.File, primary, seen map[string]bool) []RelatedFile {
	if len(seen) >= maxRelated {
		return nil
	}

	var targets []string
	for _, imp := range f.Imports {
		p := strings.Trim(imp, `"`)
		seg := path.Base(p)
		// Ignore module-version segments (…/util/v2 → util).
		if isVersionSegment(seg) {
			seg = path.Base(path.Dir(p))
		}
		if seg != "" && seg != "." && !isCommonSegment(seg) {
			targets = append(targets, seg)
		}
	}
	if len(targets) == 0 {
		return nil
	}

	var out []RelatedFile
	for i := range idx.Files() {
		if len(seen) >= maxRelated {
			break
		}
		g := &idx.Files()[i]
		if g.Path == f.Path || g.Lang != "Go" || primary[g.Path] || seen[g.Path] {
			continue
		}
		base := path.Base(path.Dir(g.Path))
		for _, seg := range targets {
			if seg != base && !containsString(g.Packages, seg) {
				continue
			}
			seen[g.Path] = true
			out = append(out, RelatedFile{
				Path:    g.Path,
				Via:     firstImportVia(f.Imports, seg),
				Package: firstPackage(g.Packages),
				Symbols: topSymbols(g.Symbols, 3),
			})
			break
		}
	}
	return out
}

// firstImportVia returns the quoted import whose path ends in seg, for
// the "via" annotation.
func firstImportVia(imports []string, seg string) string {
	for _, imp := range imports {
		p := strings.Trim(imp, `"`)
		if path.Base(p) == seg || path.Base(path.Dir(p)) == seg {
			return p
		}
	}
	return ""
}

func firstPackage(pkgs []string) string {
	if len(pkgs) == 0 {
		return ""
	}
	return pkgs[0]
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// isVersionSegment reports whether seg looks like a Go module major-
// version suffix ("v2", "v10").
func isVersionSegment(seg string) bool {
	if len(seg) < 2 || seg[0] != 'v' {
		return false
	}
	for _, r := range seg[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isCommonSegment suppresses import segments so generic they would
// relate almost every file ("internal", "pkg", the standard library).
func isCommonSegment(seg string) bool {
	switch seg {
	case "internal", "pkg", "cmd", "com", "org", "github", "golang.org", "std":
		return true
	}
	return strings.Contains(seg, ".") // hosts and domains: "example.com"
}

// Text renders the evidence as a compact prompt block. Empty evidence
// renders as "", so callers can inject the result unconditionally.
func (e *Evidence) Text() string {
	if e.Empty() {
		return ""
	}

	var b strings.Builder
	if e.Repo != "" {
		fmt.Fprintf(&b, "Source evidence from %q for: %s\n", e.Repo, e.Question)
	} else {
		fmt.Fprintf(&b, "Source evidence for: %s\n", e.Question)
	}

	for _, f := range e.Files {
		fmt.Fprintf(&b, "\n--- %s", f.Path)
		if f.Lang != "" {
			fmt.Fprintf(&b, " (%s)", f.Lang)
		}
		b.WriteString(" ---\n")

		if len(f.Symbols) > 0 {
			b.WriteString("Declarations: ")
			for i, s := range f.Symbols {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%s %s (line %d)", s.Kind, s.Name, s.Pos)
			}
			b.WriteString("\n")
		}

		for _, ex := range f.Excerpts {
			fmt.Fprintf(&b, "%s:%d:\n", f.Path, ex.Start)
			for i, line := range ex.Lines {
				fmt.Fprintf(&b, "%6d | %s\n", ex.Start+i, strings.TrimRight(line, "\r"))
			}
		}

		for _, rel := range f.Related {
			fmt.Fprintf(&b, "Related via import %s: %s", rel.Via, rel.Path)
			if rel.Package != "" {
				fmt.Fprintf(&b, " (package %s)", rel.Package)
			}
			b.WriteString("\n")
			for _, s := range rel.Symbols {
				fmt.Fprintf(&b, "  %s %s (line %d)\n", s.Kind, s.Name, s.Pos)
			}
		}
	}

	out := b.String()
	if len(out) > maxEvidenceBytes {
		out = out[:maxEvidenceBytes]
		if i := strings.LastIndexByte(out, '\n'); i > 0 {
			out = out[:i]
		}
		out += "\n... [evidence truncated to fit the context bound]\n"
	}
	return strings.TrimRight(out, "\n")
}
