package index

import (
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
)

func buildView(t *testing.T, docs ...*corpus.Document) emit.View {
	t.Helper()
	return buildViewWithRefs(t, nil, docs...)
}

// buildViewWithRefs builds a View over the docs plus the given resolved
// references, so backlink rendering (the document-projection in-neighbours) can
// be exercised.
func buildViewWithRefs(t *testing.T, refs []reference.Reference, docs ...*corpus.Document) emit.View {
	t.Helper()
	c := corpus.NewCorpus()
	for _, d := range docs {
		if err := c.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	g := graphmodel.BuildReferenceGraph(c, refs, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	return emit.BuildView(application.Result{DocumentCount: c.Len(), Metrics: m, Corpus: c})
}

// linkRef is a resolved relative-link reference origin→targetDoc, for backlink tests.
func linkRef(origin, targetDoc string) reference.Reference {
	return reference.Reference{
		RawReference: reference.RawReference{
			Origin: identity.DocumentID(origin), RawTarget: targetDoc,
			Type: reference.RelativeLink, Line: 1,
		},
		Target: reference.ResolvedTarget{Kind: reference.TargetDocument, DocumentID: identity.DocumentID(targetDoc)},
		Health: reference.Valid,
	}
}

// docWithH1 is a minimal document with a single H1 (so it has a stable title).
func docWithH1(id, title string) *corpus.Document {
	return &corpus.Document{
		ID: identity.DocumentID(id),
		Root: &corpus.Section{Level: 0, Children: []*corpus.Section{
			{Level: 1, Text: title, Slug: "h1", StartLine: 1, EndLine: 5},
		}},
	}
}

// rowFor returns the index.md table row line for a document path, or "".
func rowFor(out, docPath string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "| `"+docPath+"`") {
			return line
		}
	}
	return ""
}

// backlinksCellOf extracts the Backlinks column (the 3rd data cell) of a row:
// `| Document | Description | Backlinks | Modified |`.
func backlinksCellOf(t *testing.T, row string) string {
	t.Helper()
	cells := strings.Split(strings.Trim(row, "|"), "|")
	if len(cells) < 4 {
		t.Fatalf("row has %d cells, want 4: %q", len(cells), row)
	}
	return strings.TrimSpace(cells[2])
}

// TestIndex_Backlinks: A links to B → B's row lists A as a backlink, A's row
// shows the no-backlinks marker. ADR 0016 (Nelson/Xanadu two-way links).
func TestIndex_Backlinks(t *testing.T) {
	a := docWithH1("a.md", "A")
	b := docWithH1("b.md", "B")
	out := string(Markdown(buildViewWithRefs(t, []reference.Reference{linkRef("a.md", "b.md")}, a, b)))

	bRow := rowFor(out, "b.md")
	if bRow == "" {
		t.Fatalf("no table row for b.md:\n%s", out)
	}
	if cell := backlinksCellOf(t, bRow); !strings.Contains(cell, "a.md") {
		t.Errorf("b.md Backlinks cell should list a.md, got: %q", cell)
	}
	aRow := rowFor(out, "a.md")
	if aRow == "" {
		t.Fatalf("no table row for a.md:\n%s", out)
	}
	// a.md has no inbound links → its Backlinks cell is the "-" marker.
	if cell := backlinksCellOf(t, aRow); cell != "-" {
		t.Errorf("a.md (no inbound links) Backlinks cell = %q, want \"-\"", cell)
	}
}

// TestIndex_BacklinksSelfExcluded: A links to itself AND to B → A must NOT appear
// in its own Backlinks cell (the projection excludes self-loops, ADR 0007/0016).
func TestIndex_BacklinksSelfExcluded(t *testing.T) {
	a := docWithH1("a.md", "A")
	b := docWithH1("b.md", "B")
	refs := []reference.Reference{linkRef("a.md", "a.md"), linkRef("a.md", "b.md")}
	out := string(Markdown(buildViewWithRefs(t, refs, a, b)))

	aRow := rowFor(out, "a.md")
	if aRow == "" {
		t.Fatalf("no table row for a.md:\n%s", out)
	}
	// a.md's only inbound edge would be its self-loop, which the projection drops,
	// so its Backlinks cell is the "-" marker (a.md is NOT its own backlink).
	if cell := backlinksCellOf(t, aRow); cell != "-" {
		t.Errorf("a.md must not be its own backlink (self-loop excluded); cell = %q, want \"-\"", cell)
	}
}

// TestIndex_DescriptionFallsBackToH1: a document with no front-matter description
// uses its first H1 text as the description.
func TestIndex_DescriptionFallsBackToH1(t *testing.T) {
	doc := &corpus.Document{
		ID: "guide.md",
		Root: &corpus.Section{Level: 0, Children: []*corpus.Section{
			{Level: 1, Text: "The First Heading", Slug: "the-first-heading", StartLine: 1, EndLine: 5},
		}},
	}
	out := string(Markdown(buildView(t, doc)))
	if !strings.Contains(out, "The First Heading") {
		t.Errorf("index did not fall back to the H1 description:\n%s", out)
	}
}

// TestIndex_FrontMatterDescriptionWins: an explicit front-matter description
// overrides the H1 fallback.
func TestIndex_FrontMatterDescriptionWins(t *testing.T) {
	doc := &corpus.Document{
		ID:          "guide.md",
		FrontMatter: corpus.FrontMatter{Description: "Explicit desc"},
		Root: &corpus.Section{Level: 0, Children: []*corpus.Section{
			{Level: 1, Text: "H1 Text", Slug: "h1-text", StartLine: 1, EndLine: 5},
		}},
	}
	out := string(Markdown(buildView(t, doc)))
	if !strings.Contains(out, "Explicit desc") {
		t.Errorf("front-matter description should win:\n%s", out)
	}
	if strings.Contains(out, "| H1 Text |") {
		t.Errorf("H1 should not be used when a front-matter description exists:\n%s", out)
	}
}

// TestIndex_HostilePathAndDescriptionEscaped: a pipe/backtick/newline in the
// path or description must not break the GFM table.
func TestIndex_HostilePathAndDescriptionEscaped(t *testing.T) {
	doc := &corpus.Document{
		ID:          "evil.md",
		FrontMatter: corpus.FrontMatter{Description: "a | b `c`\nd"},
	}
	out := string(Markdown(buildView(t, doc)))
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "| ") {
			// A data/header row must have exactly the right number of unescaped pipes
			// (5 for a 4-column table: Document, Description, Backlinks, Modified).
			// Escaped pipes (\|) are part of the cell.
			cleaned := strings.ReplaceAll(line, `\|`, "")
			if n := strings.Count(cleaned, "|"); n != 5 {
				t.Errorf("table row has %d unescaped pipes (want 5), row=%q", n, line)
			}
		}
	}
}

// TestIndex_HostileCategoryEscaped pins the MUST-FIX divergence: a hostile
// directory/category name (which becomes a Markdown heading) must be neutralized
// with the SAME shared escaper the report uses, so it cannot render as live
// markdown in index.md. The category is the document's directory, so we place a
// doc under a hostile directory path and assert every markdown-significant
// metacharacter is backslash-escaped in the emitted "## ..." heading — exactly
// as emit.EscapeMarkdownText (used by report.md) would produce.
func TestIndex_HostileCategoryEscaped(t *testing.T) {
	const hostileDir = "a*b_c[d]e\\f`g|h"
	doc := &corpus.Document{
		ID:          corpus.DocumentID(hostileDir + "/page.md"),
		FrontMatter: corpus.FrontMatter{Title: "Page"},
	}
	out := string(Markdown(buildView(t, doc)))

	// The heading line for the category must equal what the shared escaper yields.
	wantHeading := "## " + emit.EscapeMarkdownText(hostileDir)
	if !strings.Contains(out, wantHeading) {
		t.Errorf("category heading not escaped via the shared helper.\nwant line: %q\ngot:\n%s", wantHeading, out)
	}
	// Defense-in-depth: the raw, unescaped metacharacters must not survive in the
	// heading (they would render as live markdown). We check the emphasis pair and
	// the link brackets specifically.
	headingLine := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "## ") {
			headingLine = line
			break
		}
	}
	// The markdown-significant metacharacters must be neutralized. A bare pipe is
	// intentionally NOT escaped here: this is a heading, not a table cell, so '|'
	// is inert (EscapeMarkdownText escapes the cell-breaking pipe only via
	// EscapeTableCell). That asymmetry is the whole point of having two helpers.
	for _, raw := range []string{"a*b", "b_c", "c[d", "d]e", `e\f`, "f`g"} {
		if strings.Contains(headingLine, raw) {
			t.Errorf("hostile category heading leaked unescaped %q: %q", raw, headingLine)
		}
	}
}

func TestIndex_Empty(t *testing.T) {
	out := string(Markdown(buildView(t)))
	if !strings.Contains(out, "No documents") {
		t.Errorf("empty index should say so:\n%s", out)
	}
}
