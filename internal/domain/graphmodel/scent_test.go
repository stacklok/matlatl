package graphmodel

import (
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// titledDoc builds a document whose front-matter title is set (so scent scores
// against a known title) plus an H1 for the heading-fallback test.
func titledDoc(id, title string) *corpus.Document {
	root := &corpus.Section{Level: 0, StartLine: 1, EndLine: 100}
	return &corpus.Document{
		ID:          identity.DocumentID(id),
		Root:        root,
		FrontMatter: corpus.FrontMatter{Title: title},
	}
}

// anchorRef is validRef with an explicit anchor (display) text and line.
func anchorRef(origin, targetDoc, anchor string, line int) reference.Reference {
	r := validRef(origin, targetDoc)
	r.AnchorText = anchor
	r.Line = line
	return r
}

// scentFor builds a graph from the given docs + refs and returns its findings.
func scentFor(t *testing.T, docs []*corpus.Document, refs []reference.Reference) []ScentFinding {
	t.Helper()
	c := buildCorpus(t, docs...)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	return g.ComputeScent(c)
}

func findScent(findings []ScentFinding, source, target identity.DocumentID) (ScentFinding, bool) {
	for _, f := range findings {
		if f.Source == source && f.Target == target {
			return f, true
		}
	}
	return ScentFinding{}, false
}

// TestScent_ScentFreePhrase: a generic "click here" anchor scores 0 → flagged,
// with the suggestion equal to the target's title.
func TestScent_ScentFreePhrase(t *testing.T) {
	docs := []*corpus.Document{titledDoc("A.md", "Source"), titledDoc("B.md", "Installation Guide")}
	refs := []reference.Reference{anchorRef("A.md", "B.md", "click here", 5)}
	findings := scentFor(t, docs, refs)
	f, ok := findScent(findings, "A.md", "B.md")
	if !ok {
		t.Fatalf("expected a low-scent finding for the 'click here' anchor, got %+v", findings)
	}
	if f.Score != 0.0 {
		t.Errorf("scent-free phrase score = %v, want 0.0", f.Score)
	}
	if f.Suggestion != "Installation Guide" {
		t.Errorf("suggestion = %q, want target title 'Installation Guide'", f.Suggestion)
	}
	if f.Line != 5 {
		t.Errorf("line = %d, want 5", f.Line)
	}
}

// TestScent_ExactTitleMatch: an anchor equal to the target title scores 1.0 →
// NOT flagged.
func TestScent_ExactTitleMatch(t *testing.T) {
	docs := []*corpus.Document{titledDoc("A.md", "Source"), titledDoc("B.md", "Installation Guide")}
	refs := []reference.Reference{anchorRef("A.md", "B.md", "Installation Guide", 1)}
	findings := scentFor(t, docs, refs)
	if _, ok := findScent(findings, "A.md", "B.md"); ok {
		t.Errorf("an exact title-match anchor must NOT be flagged: %+v", findings)
	}
}

// TestScent_NumericAndEmpty: a numeric/punctuation-only anchor has no tokens →
// scores 0 → flagged.
func TestScent_NumericAndEmpty(t *testing.T) {
	docs := []*corpus.Document{titledDoc("A.md", "Source"), titledDoc("B.md", "Target Page")}
	refs := []reference.Reference{anchorRef("A.md", "B.md", "123 !!!", 1)}
	findings := scentFor(t, docs, refs)
	f, ok := findScent(findings, "A.md", "B.md")
	if !ok {
		t.Fatalf("a numeric/empty anchor must be flagged (no tokens → 0): %+v", findings)
	}
	if f.Score != 0.0 {
		t.Errorf("numeric anchor score = %v, want 0.0", f.Score)
	}
}

// TestScent_BacktickAnchorSkipped: a wholly backtick-wrapped code identifier is
// SKIPPED (no finding), even if it shares no tokens with the title.
func TestScent_BacktickAnchorSkipped(t *testing.T) {
	docs := []*corpus.Document{titledDoc("A.md", "Source"), titledDoc("B.md", "Configuration Reference")}
	refs := []reference.Reference{anchorRef("A.md", "B.md", "`SomeStruct`", 1)}
	findings := scentFor(t, docs, refs)
	if _, ok := findScent(findings, "A.md", "B.md"); ok {
		t.Errorf("a backtick-wrapped code identifier must be skipped: %+v", findings)
	}
}

// TestScent_HeadingsFallback: when the target's title yields no scoreable tokens
// (e.g. a one-letter title), the union of its heading texts is used. Here the
// anchor "deployment" matches a heading, so it is NOT flagged.
func TestScent_HeadingsFallback(t *testing.T) {
	target := &corpus.Document{
		ID: "B.md",
		Root: &corpus.Section{Level: 0, StartLine: 1, EndLine: 100, Children: []*corpus.Section{
			{Level: 1, Text: "Deployment topics", Slug: "deployment-topics", StartLine: 1, EndLine: 50},
		}},
		FrontMatter: corpus.FrontMatter{Title: "X"}, // single-char title → no tokens
	}
	docs := []*corpus.Document{titledDoc("A.md", "Source"), target}
	refs := []reference.Reference{anchorRef("A.md", "B.md", "deployment", 1)}
	findings := scentFor(t, docs, refs)
	if _, ok := findScent(findings, "A.md", "B.md"); ok {
		t.Errorf("anchor matching a heading (title-token fallback) must NOT be flagged: %+v", findings)
	}
}

// TestScent_ThresholdBoundary pins the < 0.20 cutoff. anchor {alpha,beta,gamma,
// delta} vs title {alpha} → Jaccard 1/4 = 0.25 ≥ 0.20 → NOT flagged. Dropping a
// matching token so it shares 1 of 5 → 0.20 is the boundary (NOT flagged, since
// the cutoff is strict <). A 1-of-6 union = 0.1667 < 0.20 → flagged.
func TestScent_ThresholdBoundary(t *testing.T) {
	// 0.25 case: anchor 4 tokens, title 1 token, 1 shared → union 4, inter 1.
	notFlagged := scentFor(t,
		[]*corpus.Document{titledDoc("A.md", "S"), titledDoc("B.md", "alpha")},
		[]reference.Reference{anchorRef("A.md", "B.md", "alpha beta gamma delta", 1)},
	)
	if _, ok := findScent(notFlagged, "A.md", "B.md"); ok {
		t.Errorf("score 0.25 (>= 0.20) must NOT be flagged: %+v", notFlagged)
	}
	// EXACTLY 0.20 case (the strict-< boundary): anchor 5 distinct content tokens
	// {alpha,beta,gamma,delta,epsilon}, title 1 token {alpha} that is shared, no
	// other overlap → inter 1, union 5 → 0.20. The cutoff is strict `<`, so 0.20 is
	// NOT flagged.
	boundary := scentFor(t,
		[]*corpus.Document{titledDoc("A.md", "S"), titledDoc("B.md", "alpha")},
		[]reference.Reference{anchorRef("A.md", "B.md", "alpha beta gamma delta epsilon", 1)},
	)
	if _, ok := findScent(boundary, "A.md", "B.md"); ok {
		t.Errorf("score exactly 0.20 (cutoff is strict <) must NOT be flagged: %+v", boundary)
	}
	// 0.1667 case: anchor 5 distinct tokens, title 1 shared + 0 extra... build a
	// union of 6 with 1 shared: anchor {alpha,beta,gamma,delta,epsilon} (5), title
	// {alpha,zeta} (2) → inter 1, union 6 → 0.1667 < 0.20 → flagged.
	flagged := scentFor(t,
		[]*corpus.Document{titledDoc("A.md", "S"), titledDoc("B.md", "alpha zeta")},
		[]reference.Reference{anchorRef("A.md", "B.md", "alpha beta gamma delta epsilon", 1)},
	)
	f, ok := findScent(flagged, "A.md", "B.md")
	if !ok {
		t.Fatalf("score 0.1667 (< 0.20) must be flagged: %+v", flagged)
	}
	if f.Score >= LowScentThreshold {
		t.Errorf("flagged score = %v, want < %v", f.Score, LowScentThreshold)
	}
}

// TestScent_Deterministic: findings are identical regardless of input ref order,
// and sorted by (Source, Line, Target, AnchorText).
func TestScent_Deterministic(t *testing.T) {
	docs := []*corpus.Document{
		titledDoc("A.md", "Alpha"), titledDoc("B.md", "Beta Page"), titledDoc("C.md", "Gamma Page"),
	}
	forward := []reference.Reference{
		anchorRef("A.md", "B.md", "here", 3),
		anchorRef("A.md", "C.md", "click here", 1),
	}
	shuffled := []reference.Reference{forward[1], forward[0]}
	f1 := scentFor(t, docs, forward)
	f2 := scentFor(t, docs, shuffled)
	if len(f1) != len(f2) {
		t.Fatalf("finding count differs: %d vs %d", len(f1), len(f2))
	}
	for i := range f1 {
		if f1[i] != f2[i] {
			t.Errorf("finding %d differs across input order: %+v vs %+v", i, f1[i], f2[i])
		}
	}
	// Sorted by (Source, Line, ...): the line-1 finding precedes the line-3 one.
	if len(f1) >= 2 && f1[0].Line > f1[1].Line {
		t.Errorf("findings not sorted by line: %+v", f1)
	}
}
