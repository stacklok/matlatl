package mdparser

import "slices"

// lineIndex maps a byte offset in a source buffer to a 1-based line number. It
// precomputes the start offset of each line for O(log n) lookups.
type lineIndex struct {
	// lineStarts[i] is the byte offset where line (i+1) begins.
	lineStarts []int
}

// newLineIndex builds a lineIndex over src.
func newLineIndex(src []byte) *lineIndex {
	starts := make([]int, 0, 16)
	starts = append(starts, 0)
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &lineIndex{lineStarts: starts}
}

// lineCount returns the number of lines in the source (>= 1).
func (l *lineIndex) lineCount() int {
	return len(l.lineStarts)
}

// lineAt returns the 1-based line number containing byte offset off.
func (l *lineIndex) lineAt(off int) int {
	if off < 0 {
		off = 0
	}
	// Find the insertion point for off+1; the line index is that position. This
	// yields the last lineStart <= off.
	i, _ := slices.BinarySearch(l.lineStarts, off+1)
	if i <= 0 {
		return 1
	}
	return i
}
