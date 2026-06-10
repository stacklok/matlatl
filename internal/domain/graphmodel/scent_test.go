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

// sectionedDoc builds a document with a front-matter title and the given
// (text, slug) headings as top-level sections, in order.
func sectionedDoc(id, title string, headings ...[2]string) *corpus.Document {
	root := &corpus.Section{Level: 0, StartLine: 1, EndLine: 100}
	line := 1
	for _, h := range headings {
		root.Children = append(root.Children, &corpus.Section{
			Level: 1, Text: h[0], Slug: h[1], StartLine: line, EndLine: line + 4, Parent: root,
		})
		line += 5
	}
	return &corpus.Document{
		ID:          identity.DocumentID(id),
		Root:        root,
		FrontMatter: corpus.FrontMatter{Title: title},
	}
}

// sectionRef is anchorRef resolved to a SECTION target (an anchored link like
// `file.md#slug`), so the edge lands on the section vertex.
func sectionRef(origin, targetDoc, slug, anchor string, line int) reference.Reference {
	r := anchorRef(origin, targetDoc, anchor, line)
	r.Target.Kind = reference.TargetSection
	r.Target.Anchor = slug
	return r
}

// TestScent_DocTargetPerHeadingMax: a document-targeted anchor is scored against
// the title AND each heading individually (the max wins, ADR 0016 amendment).
// The anchor "deployment topics" exactly matches ONE heading among several;
// against the per-heading candidate it scores 1.0 → NOT flagged. The old
// heading-UNION fallback would have scored 2/6 = 0.33 only because this corpus is
// tiny — per-heading is the contract (a union's size depresses Jaccard).
func TestScent_DocTargetPerHeadingMax(t *testing.T) {
	target := sectionedDoc("B.md", "X", // single-char title → no title tokens
		[2]string{"Overview material", "overview-material"},
		[2]string{"Deployment topics", "deployment-topics"},
		[2]string{"Troubleshooting guidance", "troubleshooting-guidance"},
	)
	docs := []*corpus.Document{titledDoc("A.md", "Source"), target}
	refs := []reference.Reference{anchorRef("A.md", "B.md", "deployment topics", 1)}
	findings := scentFor(t, docs, refs)
	if _, ok := findScent(findings, "A.md", "B.md"); ok {
		t.Errorf("anchor exactly matching one heading (per-heading max) must NOT be flagged: %+v", findings)
	}
}

// TestScent_DocTargetMatchesLaterHeading: the per-heading candidates cover a
// NON-FIRST heading even when the title is perfectly tokenizable — the heading
// vocabulary is additive, not a title fallback.
func TestScent_DocTargetMatchesLaterHeading(t *testing.T) {
	target := sectionedDoc("B.md", "Operations Handbook",
		[2]string{"Overview", "overview"},
		[2]string{"Capacity planning checklist", "capacity-planning-checklist"},
	)
	docs := []*corpus.Document{titledDoc("A.md", "Source"), target}
	refs := []reference.Reference{anchorRef("A.md", "B.md", "capacity planning checklist", 3)}
	findings := scentFor(t, docs, refs)
	if _, ok := findScent(findings, "A.md", "B.md"); ok {
		t.Errorf("anchor matching a later heading must NOT be flagged: %+v", findings)
	}
}

// TestScent_SectionTargetMatchesFragment: an anchored link `guide.md#installation`
// labelled "installation" is scored against the FRAGMENT's heading (plus the
// title) and scores 1.0 → NOT flagged (the pre-amendment behavior flagged it).
func TestScent_SectionTargetMatchesFragment(t *testing.T) {
	target := sectionedDoc("guide.md", "Atrium UI",
		[2]string{"Installation", "installation"},
		[2]string{"Configuration", "configuration"},
	)
	docs := []*corpus.Document{titledDoc("A.md", "Source"), target}
	refs := []reference.Reference{sectionRef("A.md", "guide.md", "installation", "installation", 2)}
	findings := scentFor(t, docs, refs)
	if _, ok := findScent(findings, "A.md", "guide.md"); ok {
		t.Errorf("an anchored link matching its fragment's heading must NOT be flagged: %+v", findings)
	}
}

// TestScent_SectionTargetJunkAnchor: a junk anchor on a section-targeted edge is
// still flagged, and the Suggestion is the FRAGMENT's heading text (the
// preferred candidate for section targets), not the document title.
func TestScent_SectionTargetJunkAnchor(t *testing.T) {
	target := sectionedDoc("guide.md", "Atrium UI",
		[2]string{"Installation", "installation"},
	)
	docs := []*corpus.Document{titledDoc("A.md", "Source"), target}
	refs := []reference.Reference{sectionRef("A.md", "guide.md", "installation", "relative link", 4)}
	findings := scentFor(t, docs, refs)
	f, ok := findScent(findings, "A.md", "guide.md")
	if !ok {
		t.Fatalf("a junk anchor on a section-targeted edge must stay flagged: %+v", findings)
	}
	if f.Suggestion != "Installation" {
		t.Errorf("suggestion = %q, want the fragment's heading 'Installation'", f.Suggestion)
	}
}

// TestScent_SectionTargetWrongSection: an anchored link labelled after a
// DIFFERENT section than the one it points at stays flagged — the anchored link
// is held to its actual destination (only the fragment's heading + the title are
// candidates), and the suggestion names the fragment's heading.
func TestScent_SectionTargetWrongSection(t *testing.T) {
	target := sectionedDoc("guide.md", "Atrium UI",
		[2]string{"Installation", "installation"},
		[2]string{"Configuration", "configuration"},
	)
	docs := []*corpus.Document{titledDoc("A.md", "Source"), target}
	refs := []reference.Reference{sectionRef("A.md", "guide.md", "installation", "Configuration", 6)}
	findings := scentFor(t, docs, refs)
	f, ok := findScent(findings, "A.md", "guide.md")
	if !ok {
		t.Fatalf("an anchor naming a DIFFERENT section than its fragment must stay flagged: %+v", findings)
	}
	if f.Suggestion != "Installation" {
		t.Errorf("suggestion = %q, want the fragment's heading 'Installation'", f.Suggestion)
	}
}

// TestScent_SameDocSectionDialect: a same-document anchored link labelled
// "§ Composition" (empty § prefix) pointing at #composition strips to the
// heading text and scores 1.0 → NOT flagged.
func TestScent_SameDocSectionDialect(t *testing.T) {
	doc := sectionedDoc("page.md", "Front Door",
		[2]string{"Composition", "composition"},
		[2]string{"Routing", "routing"},
	)
	refs := []reference.Reference{sectionRef("page.md", "page.md", "composition", "§ Composition", 8)}
	findings := scentFor(t, []*corpus.Document{doc}, refs)
	if _, ok := findScent(findings, "page.md", "page.md"); ok {
		t.Errorf("a same-doc '§ Heading' link matching its fragment must NOT be flagged: %+v", findings)
	}
}

// TestScent_SectionDialectPrefixStrip: a "file.md § Heading" anchor whose prefix
// names the target file is scored on the heading part only (doc-targeted, no
// fragment) → matches a heading → NOT flagged.
func TestScent_SectionDialectPrefixStrip(t *testing.T) {
	target := sectionedDoc("frontdoor.md", "Front Door",
		[2]string{"Overview", "overview"},
		[2]string{"TxToken claim shape", "txtoken-claim-shape"},
	)
	docs := []*corpus.Document{titledDoc("A.md", "Source"), target}
	refs := []reference.Reference{
		anchorRef("A.md", "frontdoor.md", "frontdoor.md § TxToken claim shape", 2),
	}
	findings := scentFor(t, docs, refs)
	if _, ok := findScent(findings, "A.md", "frontdoor.md"); ok {
		t.Errorf("a 'file.md § Heading' anchor naming its target's heading must NOT be flagged: %+v", findings)
	}
}

// TestScent_SectionDialectStaleHeading: a "file.md § Gone Heading" anchor whose
// heading no longer exists in the target STAYS flagged — that is rot, the
// finding's true positive — and the suggestion is the best real candidate.
func TestScent_SectionDialectStaleHeading(t *testing.T) {
	target := sectionedDoc("frontdoor.md", "Front Door",
		[2]string{"Routing rules", "routing-rules"},
	)
	docs := []*corpus.Document{titledDoc("A.md", "Source"), target}
	refs := []reference.Reference{
		anchorRef("A.md", "frontdoor.md", "frontdoor.md § Legacy session pinning", 3),
	}
	findings := scentFor(t, docs, refs)
	f, ok := findScent(findings, "A.md", "frontdoor.md")
	if !ok {
		t.Fatalf("a stale '§ heading' reference must STAY flagged (it is rot): %+v", findings)
	}
	if f.Suggestion != "Front Door" && f.Suggestion != "Routing rules" {
		t.Errorf("suggestion = %q, want a real candidate (title or heading)", f.Suggestion)
	}
}

// TestScent_SectionDialectMismatchedPrefix: a "otherfile.md § Heading" anchor
// whose prefix names a DIFFERENT file than the target keeps its misleading
// prefix (no strip) and stays flagged even though the heading part matches
// perfectly — stripped, this anchor would score 1.0; the kept path pollution
// (5 tokens) drags the Jaccard to 1/6 < 0.20. Mutation proof: drop the
// basename-match guard in effectiveAnchor and this flips to unflagged.
func TestScent_SectionDialectMismatchedPrefix(t *testing.T) {
	target := sectionedDoc("frontdoor.md", "Front Door",
		[2]string{"Routing", "routing"},
	)
	docs := []*corpus.Document{titledDoc("A.md", "Source"), target}
	refs := []reference.Reference{
		anchorRef("A.md", "frontdoor.md", "docs/design/modules/ui.md § Routing", 5),
	}
	findings := scentFor(t, docs, refs)
	if _, ok := findScent(findings, "A.md", "frontdoor.md"); !ok {
		t.Errorf("a '§' anchor with a MISMATCHED file prefix must keep the pollution and stay flagged: %+v", findings)
	}
}

// TestScent_SectionDialectPhraseAfterStrip: "file.md § here" strips to "here",
// which is a scent-free phrase → score 0.0 → flagged.
func TestScent_SectionDialectPhraseAfterStrip(t *testing.T) {
	target := sectionedDoc("guide.md", "Guide",
		[2]string{"Here be dragons", "here-be-dragons"},
	)
	docs := []*corpus.Document{titledDoc("A.md", "Source"), target}
	refs := []reference.Reference{anchorRef("A.md", "guide.md", "guide.md § here", 7)}
	findings := scentFor(t, docs, refs)
	f, ok := findScent(findings, "A.md", "guide.md")
	if !ok {
		t.Fatalf("'file.md § here' must strip to the scent-free phrase 'here' and stay flagged: %+v", findings)
	}
	if f.Score != 0.0 {
		t.Errorf("scent-free phrase (after strip) score = %v, want 0.0", f.Score)
	}
}

// TestEffectiveAnchor is the table test for the "file.md § Heading" dialect
// strip (ADR 0016 amendment): the prefix is stripped only when it names the
// TARGET file (basename match, ".md" optional, backticks trimmed) and the
// remainder is scoreable; an empty prefix always strips; everything else (no §,
// empty remainder, mismatched prefix) leaves the raw anchor untouched.
func TestEffectiveAnchor(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		target identity.DocumentID
		want   string
	}{
		{"no section sign", "plain anchor text", "guide.md", "plain anchor text"},
		{"prefix matches target", "guide.md § Installation", "guide.md", " Installation"},
		{"prefix matches deep path target", "docs/a/b/guide.md § Installation", "x/y/guide.md", " Installation"},
		{"prefix without .md matches", "guide § Installation", "docs/guide.md", " Installation"},
		{"backticked prefix matches", "`guide.md` § Installation", "guide.md", " Installation"},
		{"empty prefix strips", "§ Composition", "page.md", " Composition"},
		{"mismatched prefix kept", "ui.md § Installation", "frontdoor.md", "ui.md § Installation"},
		{"empty remainder kept", "guide.md §", "guide.md", "guide.md §"},
		{"punctuation-only remainder kept", "guide.md § !!", "guide.md", "guide.md § !!"},
		{"case-insensitive basename match", "GUIDE.MD § Setup steps", "guide.md", " Setup steps"},
	}
	for _, tc := range cases {
		if got := effectiveAnchor(tc.raw, tc.target); got != tc.want {
			t.Errorf("%s: effectiveAnchor(%q, %q) = %q, want %q", tc.name, tc.raw, tc.target, got, tc.want)
		}
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

// TestScent_SkipsLinelessEdge: a reference with Line 0 (e.g. a synthetic
// directory-expansion vouch edge, ADR 0008) is never flagged — a scent finding
// must point at an authored link with a source line (ADR 0016).
func TestScent_SkipsLinelessEdge(t *testing.T) {
	docs := []*corpus.Document{titledDoc("A.md", "Source"), titledDoc("B.md", "Target Page")}
	// A scent-free anchor (would normally be flagged) but with Line 0 → skipped.
	refs := []reference.Reference{anchorRef("A.md", "B.md", "click here", 0)}
	findings := scentFor(t, docs, refs)
	if _, ok := findScent(findings, "A.md", "B.md"); ok {
		t.Errorf("a Line-0 (synthetic / lineless) edge must not be flagged: %+v", findings)
	}
}

// TestScent_StableIdentifierExempt: an anchor naming the target's stable
// identifier (e.g. "ADR 0010") is exempt, but a bare path-/filename-like anchor
// is NOT — the load-bearing distinction (ADR 0016).
func TestScent_StableIdentifierExempt(t *testing.T) {
	// "ADR 0010" → docs/adr/0010-agent-scaffolding.md (id segment "0010"): the
	// anchor names the target's stable ID, so it is NOT flagged even though its
	// Jaccard vs the title "Agent scaffolding" is ~0.
	exempt := scentFor(t,
		[]*corpus.Document{
			titledDoc("README.md", "Home"),
			titledDoc("docs/adr/0010-agent-scaffolding.md", "Agent scaffolding"),
		},
		[]reference.Reference{anchorRef("README.md", "docs/adr/0010-agent-scaffolding.md", "ADR 0010", 4)},
	)
	if _, ok := findScent(exempt, "README.md", "docs/adr/0010-agent-scaffolding.md"); ok {
		t.Errorf("a stable-ID anchor 'ADR 0010' must be exempt: %+v", exempt)
	}

	// A bare path anchor "docs/dev-guide.md" → docs/dev-guide.md ("matlatl
	// developer guide"): path-like, so NOT exempt → still flagged.
	flagged := scentFor(t,
		[]*corpus.Document{
			titledDoc("README.md", "Home"),
			titledDoc("docs/dev-guide.md", "matlatl developer guide"),
		},
		[]reference.Reference{anchorRef("README.md", "docs/dev-guide.md", "docs/dev-guide.md", 7)},
	)
	if _, ok := findScent(flagged, "README.md", "docs/dev-guide.md"); !ok {
		t.Errorf("a bare-path anchor 'docs/dev-guide.md' must stay flagged (not ID-exempt): %+v", flagged)
	}

	// Path-like guard, exercised genuinely: target "adr/0002-bar.md" HAS a stable
	// identifier ("0002"), and the anchor "adr/0002-bar.md" CONTAINS that token —
	// so only the path-like guard (`/` / `.md`) keeps it flagged. Title "Second
	// Choice Record" shares no token with the path, so the score is low enough to
	// reach the exemption check. Mutation proof: delete the path-guard in
	// namesTargetIdentifier and this case flips to exempt (and fails).
	pathWithID := scentFor(t,
		[]*corpus.Document{
			titledDoc("README.md", "Home"),
			titledDoc("adr/0002-bar.md", "Second Choice Record"),
		},
		[]reference.Reference{anchorRef("README.md", "adr/0002-bar.md", "adr/0002-bar.md", 9)},
	)
	if _, ok := findScent(pathWithID, "README.md", "adr/0002-bar.md"); !ok {
		t.Errorf("a path-like anchor containing the ID token ('adr/0002-bar.md') must stay flagged "+
			"(the path-like guard, not the ID check, decides): %+v", pathWithID)
	}
}

// TestIdentifierSegment is a table test for the stable-identifier extraction:
// a "-"/"_"-delimited leading segment of the basename (ext stripped, lowercased)
// is the identifier only when it has length > 1 AND contains a digit (ADR 0016).
func TestIdentifierSegment(t *testing.T) {
	cases := []struct {
		target identity.DocumentID
		want   string
	}{
		{"0010-agent-scaffolding.md", "0010"},
		{"docs/adr/0001-x.md", "0001"},
		{"dev-guide.md", ""},
		{"README.md", ""},
		{"v1-foo.md", "v1"},
	}
	for _, tc := range cases {
		if got := identifierSegment(tc.target); got != tc.want {
			t.Errorf("identifierSegment(%q) = %q, want %q", tc.target, got, tc.want)
		}
	}
}
