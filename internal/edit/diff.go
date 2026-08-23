// Unified-style diff rendering for edit results.
//
// The implementation is intentionally small and deterministic: a
// line-based longest-common-subsequence comparison between before and
// after content, flattened into one script of line operations, then
// grouped into hunks with a fixed context radius. It exists so the
// agent can see exactly what an edit changed; it is not a general
// patch framework and cannot be applied back onto a file.

package edit

import (
	"fmt"
	"strings"
)

// diffContext is how many unchanged lines surround each hunk of changes.
const diffContext = 3

// Diff renders the change between before and after as a compact
// unified-style patch against path (slash-separated, workspace-relative).
// Identical inputs yield "". Line endings are normalized to "\n" for
// comparison and output, so a CRLF checkout produces the same diff text
// as an LF one, and a pure line-ending change yields "".
func Diff(path, before, after string) string {
	if before == after {
		return ""
	}

	ops := lineOps(splitLines(before), splitLines(after))
	hs := hunks(ops)
	if len(hs) == 0 {
		return "" // line-ending-only difference; nothing meaningful
	}

	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", path, path)

	for _, h := range hs {
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", h.aStart, h.aCount, h.bStart, h.bCount)
		for _, op := range ops[h.lo:h.hi] {
			switch op.kind {
			case opEqual:
				fmt.Fprintf(&out, " %s\n", op.text)
			case opDelete:
				fmt.Fprintf(&out, "-%s\n", op.text)
			case opAdd:
				fmt.Fprintf(&out, "+%s\n", op.text)
			}
		}
	}
	return out.String()
}

// opKind classifies one line operation in the flattened script.
type opKind int

const (
	opDelete opKind = iota // line removed from before
	opEqual                // line present in both, in order
	opAdd                  // line added in after
)

// lineOp is one entry of the flattened script: a single line plus its
// position numbers in the source sequences (1-based; unused side is 0).
type lineOp struct {
	kind  opKind
	text  string
	aLine int // line number in before, for delete/equal
	bLine int // line number in after, for add/equal
}

// lineOps aligns a and b along their longest common subsequence and
// flattens the alignment into a script covering every line of both
// inputs exactly once. Replacements appear as deletes immediately
// followed by adds, which keeps removed and replacement lines paired
// in the rendered hunk. The classic O(len(a)*len(b)) table is fine
// here because edited files are bounded by MaxFileSize.
func lineOps(a, b []string) []lineOp {
	n, m := len(a), len(b)

	// lcs[i][j] = length of the common subsequence of a[i:], b[j:].
	lcs := make([][]int32, n+1)
	backing := make([]int32, (n+1)*(m+1))
	for i := range lcs {
		lcs[i] = backing[i*(m+1) : (i+1)*(m+1)]
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	ops := make([]lineOp, 0, n+m)
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			ops = append(ops, lineOp{kind: opEqual, text: a[i], aLine: i + 1, bLine: j + 1})
			i++
			j++
		case i < n && (j >= m || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, lineOp{kind: opDelete, text: a[i], aLine: i + 1})
			i++
		default:
			ops = append(ops, lineOp{kind: opAdd, text: b[j], bLine: j + 1})
			j++
		}
	}
	return ops
}

// hunk describes one rendered region of the script: ops[lo:hi], with
// 1-based starting line numbers and line counts for both sides.
type hunk struct {
	lo, hi         int // slice bounds into the op script
	aStart, aCount int // 1-based start line and line count in before
	bStart, bCount int // 1-based start line and line count in after
}

// posOf returns the op's line position for gap measurement. Equal and
// deleted lines are measured on the before side; added lines have no
// before position, so they use the after side — within one script the
// two sides advance in lockstep across equal runs, so gaps computed on
// either side are directly comparable.
func posOf(op lineOp) int {
	if op.aLine > 0 {
		return op.aLine
	}
	return op.bLine
}

// hunks clusters the script's changed lines into hunks separated by
// more than 2*diffContext unchanged lines, padding each hunk with up
// to diffContext unchanged lines of context on either side.
func hunks(ops []lineOp) []hunk {
	changed := make([]int, 0, 8)
	for idx, op := range ops {
		if op.kind != opEqual {
			changed = append(changed, idx)
		}
	}
	if len(changed) == 0 {
		return nil // line-ending-only difference; nothing meaningful
	}

	var out []hunk
	start := 0 // index into changed[] where the current cluster began
	for k := 1; k <= len(changed); k++ {
		separated := k == len(changed) ||
			posOf(ops[changed[k]])-posOf(ops[changed[k-1]]) > 2*diffContext+1
		if !separated {
			continue
		}

		first, last := changed[start], changed[k-1]
		lo := first
		ctxLines := 0
		for lo > 0 && ops[lo-1].kind == opEqual && ctxLines < diffContext {
			lo--
			ctxLines++
		}
		hi := last + 1
		ctxLines = 0
		for hi < len(ops) && ops[hi].kind == opEqual && ctxLines < diffContext {
			hi++
			ctxLines++
		}

		out = append(out, hunk{
			lo:     lo,
			hi:     hi,
			aStart: ops[lo].aLine,
			bStart: ops[lo].bLine,
		})
		start = k
	}

	// Fill counts from the padded ranges.
	for idx := range out {
		h := &out[idx]
		for _, op := range ops[h.lo:h.hi] {
			switch op.kind {
			case opAdd:
				h.bCount++
			case opDelete:
				h.aCount++
			case opEqual:
				h.aCount++
				h.bCount++
			}
		}
		// A window that begins with added lines (possible only at the
		// very start of a file) has no before-side line to anchor on.
		if h.aStart == 0 {
			h.aStart = 1
		}
	}
	return out
}

// splitLines splits s into lines without their terminators, tolerating
// both "\n" and "\r\n" endings. A trailing newline does not produce a
// phantom final empty line.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
