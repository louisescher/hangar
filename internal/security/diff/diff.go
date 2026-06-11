// Package diff produces a git-style unified diff between two strings. It is the
// review text stored in the audit log on updates — human/agent-facing, never
// machine-applied. Ported from withastro/rosie (unified-diff.ts).
//
// Algorithm: trim the common leading/trailing lines, then run an LCS diff over
// only the region that changed. A size guard falls back to a wholesale
// delete+insert for pathological rewrites.
package diff

import (
	"strconv"
	"strings"
)

const (
	maxCells  = 4_000_000
	noNewline = "\\ No newline at end of file"
)

type opType int

const (
	opEq opType = iota
	opDel
	opIns
)

type op struct {
	typ  opType
	a, b int // index into old lines (a) / new lines (b); -1 when not applicable
}

// Unified returns a unified diff of oldStr vs newStr with the given number of
// context lines, labeled oldName/newName. Returns "" when the inputs are equal.
func Unified(oldName, newName, oldStr, newStr string, context int) string {
	if oldStr == newStr {
		return ""
	}
	oldLines := splitLines(oldStr)
	newLines := splitLines(newStr)
	ops := diffLines(oldLines, newLines)

	var changeIdx []int
	for i, o := range ops {
		if o.typ != opEq {
			changeIdx = append(changeIdx, i)
		}
	}
	if len(changeIdx) == 0 {
		return ""
	}

	// Group changes into hunks, merging groups separated by <= 2*context eq lines.
	var groups [][2]int
	g := 0
	for g < len(changeIdx) {
		end := g
		for end+1 < len(changeIdx) && changeIdx[end+1]-changeIdx[end]-1 <= 2*context {
			end++
		}
		first := changeIdx[g]
		last := changeIdx[end]
		groups = append(groups, [2]int{max(0, first-context), min(len(ops)-1, last+context)})
		g = end + 1
	}

	var out []string
	out = append(out, "--- "+oldName, "+++ "+newName)

	emit := func(prefix, line string) {
		if strings.HasSuffix(line, "\n") {
			out = append(out, prefix+line[:len(line)-1])
		} else {
			out = append(out, prefix+line, noNewline)
		}
	}

	for _, grp := range groups {
		start, end := grp[0], grp[1]
		oldCount, newCount, oldFirst, newFirst := 0, 0, -1, -1
		for i := start; i <= end; i++ {
			if ops[i].a >= 0 {
				if oldFirst < 0 {
					oldFirst = ops[i].a
				}
				oldCount++
			}
			if ops[i].b >= 0 {
				if newFirst < 0 {
					newFirst = ops[i].b
				}
				newCount++
			}
		}
		oldStart := oldFirst + 1
		if oldCount == 0 {
			oldStart = linesBefore(ops, start, true)
		}
		newStart := newFirst + 1
		if newCount == 0 {
			newStart = linesBefore(ops, start, false)
		}
		out = append(out, "@@ -"+fmtRange(oldStart, oldCount)+" +"+fmtRange(newStart, newCount)+" @@")

		for i := start; i <= end; i++ {
			switch ops[i].typ {
			case opEq:
				emit(" ", oldLines[ops[i].a])
			case opDel:
				emit("-", oldLines[ops[i].a])
			case opIns:
				emit("+", newLines[ops[i].b])
			}
		}
	}

	return strings.Join(out, "\n") + "\n"
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func diffLines(a, b []string) []op {
	var ops []op
	n, m := len(a), len(b)

	lo := 0
	for lo < n && lo < m && a[lo] == b[lo] {
		lo++
	}
	hiA, hiB := n, m
	for hiA > lo && hiB > lo && a[hiA-1] == b[hiB-1] {
		hiA--
		hiB--
	}

	for i := 0; i < lo; i++ {
		ops = append(ops, op{opEq, i, i})
	}

	midA, midB := hiA-lo, hiB-lo
	switch {
	case midA > 0 && midB == 0:
		for i := lo; i < hiA; i++ {
			ops = append(ops, op{opDel, i, -1})
		}
	case midB > 0 && midA == 0:
		for j := lo; j < hiB; j++ {
			ops = append(ops, op{opIns, -1, j})
		}
	case midA > 0 && midB > 0:
		if midA*midB > maxCells {
			for i := lo; i < hiA; i++ {
				ops = append(ops, op{opDel, i, -1})
			}
			for j := lo; j < hiB; j++ {
				ops = append(ops, op{opIns, -1, j})
			}
		} else {
			ops = lcsRegion(a, b, lo, hiA, lo, hiB, ops)
		}
	}

	for i := hiA; i < n; i++ {
		ops = append(ops, op{opEq, i, i - hiA + hiB})
	}
	return ops
}

func lcsRegion(a, b []string, aLo, aHi, bLo, bHi int, ops []op) []op {
	n := aHi - aLo
	m := bHi - bLo
	dp := make([][]int32, n+1)
	for i := range dp {
		dp[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[aLo+i] == b[bLo+j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[aLo+i] == b[bLo+j]:
			ops = append(ops, op{opEq, aLo + i, bLo + j})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, op{opDel, aLo + i, -1})
			i++
		default:
			ops = append(ops, op{opIns, -1, bLo + j})
			j++
		}
	}
	for i < n {
		ops = append(ops, op{opDel, aLo + i, -1})
		i++
	}
	for j < m {
		ops = append(ops, op{opIns, -1, bLo + j})
		j++
	}
	return ops
}

func linesBefore(ops []op, start int, side bool) int {
	c := 0
	for i := 0; i < start; i++ {
		if side && ops[i].a >= 0 {
			c++
		} else if !side && ops[i].b >= 0 {
			c++
		}
	}
	return c
}

func fmtRange(start, count int) string {
	if count == 1 {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "," + strconv.Itoa(count)
}
