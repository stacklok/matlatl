package mdparser

import "testing"

// TestLineIndex_LineAt covers lineAt's boundary behavior: offset 0, exactly at a
// newline byte, just past a newline, past the end of the source, a negative
// offset (clamped to line 1), and the empty-source case.
func TestLineIndex_LineAt(t *testing.T) {
	// "a\nbb\nccc" — line starts at offsets 0, 2, 5.
	//  index: 0='a' 1='\n' 2='b' 3='b' 4='\n' 5='c' 6='c' 7='c'
	src := []byte("a\nbb\nccc")
	li := newLineIndex(src)
	if got := li.lineCount(); got != 3 {
		t.Fatalf("lineCount() = %d, want 3", got)
	}

	cases := []struct {
		name string
		off  int
		want int
	}{
		{"start of file", 0, 1},
		{"within line 1", 1, 1},   // the '\n' byte still belongs to line 1
		{"start of line 2", 2, 2}, // first byte after the first '\n'
		{"within line 2", 3, 2},
		{"at second newline", 4, 2},
		{"start of line 3", 5, 3},
		{"within line 3", 7, 3},
		{"past end of source", 999, 3}, // clamps to the last line
		{"negative offset", -10, 1},    // clamps up to line 1
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := li.lineAt(tc.off); got != tc.want {
				t.Errorf("lineAt(%d) = %d, want %d", tc.off, got, tc.want)
			}
		})
	}
}

// TestLineIndex_EmptySource: an empty buffer is a single (empty) line; every
// offset maps to line 1.
func TestLineIndex_EmptySource(t *testing.T) {
	li := newLineIndex(nil)
	if got := li.lineCount(); got != 1 {
		t.Fatalf("lineCount() on empty src = %d, want 1", got)
	}
	for _, off := range []int{-1, 0, 1, 100} {
		if got := li.lineAt(off); got != 1 {
			t.Errorf("lineAt(%d) on empty src = %d, want 1", off, got)
		}
	}
}

// TestLineIndex_TrailingNewline: a trailing '\n' creates a final empty line, and
// an offset at/after it maps to that last line.
func TestLineIndex_TrailingNewline(t *testing.T) {
	src := []byte("x\n")
	li := newLineIndex(src)
	if got := li.lineCount(); got != 2 {
		t.Fatalf("lineCount() = %d, want 2", got)
	}
	if got := li.lineAt(2); got != 2 {
		t.Errorf("lineAt(2) = %d, want 2 (the empty trailing line)", got)
	}
}
