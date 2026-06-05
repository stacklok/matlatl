package index

import (
	"strings"
	"testing"

	"github.com/stacklok/doctopus/internal/application"
	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/graphmodel"
	"github.com/stacklok/doctopus/internal/infrastructure/emit"
)

func buildView(t *testing.T, docs ...*corpus.Document) emit.View {
	t.Helper()
	c := corpus.NewCorpus()
	for _, d := range docs {
		if err := c.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	g := graphmodel.BuildReferenceGraph(c, nil, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	return emit.BuildView(application.Result{DocumentCount: c.Len(), Metrics: m, Corpus: c})
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
			// (4 for a 3-column table). Escaped pipes (\|) are part of the cell.
			cleaned := strings.ReplaceAll(line, `\|`, "")
			if n := strings.Count(cleaned, "|"); n != 4 {
				t.Errorf("table row has %d unescaped pipes (want 4), row=%q", n, line)
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
