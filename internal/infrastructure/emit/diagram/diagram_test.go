package diagram

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stacklok/doctopus/internal/application"
	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/graphmodel"
	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/domain/reference"
	"github.com/stacklok/doctopus/internal/infrastructure/emit"
)

// hostileTitle bundles every character that could break or inject into a
// Mermaid or DOT label.
const hostileTitle = "ev\"il]\n--> [x](y) `code` <script>alert(1)</script>; {a} | \\ #frag"

// buildHostileView constructs a minimal one-document corpus whose front-matter
// title is hostileTitle, runs the real analysis, and returns the View — so the
// escaping is exercised end-to-end against the frozen domain model.
func buildHostileView(t *testing.T) emit.View {
	t.Helper()
	c := corpus.NewCorpus()
	doc := &corpus.Document{
		ID:          "evil.md",
		FrontMatter: corpus.FrontMatter{Title: hostileTitle, Description: hostileTitle},
		Root: &corpus.Section{Level: 0, Children: []*corpus.Section{
			{Level: 1, Text: hostileTitle, Slug: "h", StartLine: 1, EndLine: 5},
		}},
	}
	if err := c.Add(doc); err != nil {
		t.Fatal(err)
	}
	g := graphmodel.BuildReferenceGraph(c, nil, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	res := application.Result{
		DocumentCount: c.Len(),
		HeadingCount:  c.HeadingCount(),
		Metrics:       m,
		Corpus:        c,
		BrokenEdges: []application.BrokenEdge{
			{Origin: "evil.md", Target: hostileTitle},
		},
	}
	return emit.BuildView(res)
}

func TestMermaid_HostileLabelEscaped(t *testing.T) {
	out := string(Mermaid(buildHostileView(t)))

	// A node label is delimited by ["..."]. The hostile characters must not leak
	// the raw forms that would break the parser or inject syntax.
	mustNotContain := []string{
		"\n--> ",   // newline + edge syntax inside output is an injection
		"[x](y)",   // raw brackets/parens (shape delimiters)
		"<script>", // raw HTML
		"alert(1)", // the parens of the payload must be neutralized
		"\"il]",    // a raw double-quote closing the label early
	}
	for _, frag := range mustNotContain {
		if strings.Contains(out, frag) {
			t.Errorf("mermaid output leaked unescaped fragment %q:\n%s", frag, out)
		}
	}
	// The fenced block must still be well-formed.
	if !strings.HasPrefix(out, "```mermaid\n") || !strings.HasSuffix(out, "```\n") {
		t.Errorf("mermaid block not well-formed:\n%s", out)
	}
	// A ';' is a statement separator only in Mermaid's UNQUOTED syntax. Our labels
	// are double-quoted and the '"' that would close the quoted context is itself
	// replaced, so the hostile title's ';' must survive verbatim inside the quoted
	// label (escapeMermaidLabel deliberately does not touch it). This pins the
	// assumption behind removing the old ';' -> ';' no-op.
	if !strings.Contains(hostileTitle, ";") {
		t.Fatal("hostileTitle must contain ';' to exercise the semicolon assumption")
	}
	if !strings.Contains(out, ";") {
		t.Errorf("expected the label's ';' to be preserved verbatim inside the quoted label:\n%s", out)
	}
	// Every line inside must be a comment, node, class, edge, or classDef — no
	// stray line from an injected newline (each content line is indented; the
	// fences and the `flowchart` directive are the only unindented lines).
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "", line == "```mermaid", line == "```", strings.HasPrefix(line, "flowchart"):
			continue
		case strings.HasPrefix(line, "  "):
			continue
		default:
			t.Errorf("unexpected top-level mermaid line (possible injection): %q", line)
		}
	}
}

func TestDOT_HostileLabelEscaped(t *testing.T) {
	out := string(DOT(buildHostileView(t)))

	// In DOT, only " and \ are special inside a quoted string. A raw, unescaped
	// double-quote would terminate the label early; assert it is escaped.
	if strings.Contains(out, "ev\"il") {
		t.Errorf("DOT leaked an unescaped double-quote:\n%s", out)
	}
	// A literal newline must never appear inside the digraph body (it would break
	// the statement); it is rendered as the two-character escape \n.
	body := strings.TrimPrefix(out, "digraph doctopus {\n")
	if strings.Contains(hostileTitle, "\n") && strings.Contains(body, "<script>\n") {
		t.Errorf("DOT leaked a raw newline in a label:\n%s", out)
	}
	// Balanced STRUCTURAL braces: exactly one '{' and one '}' outside of quoted
	// strings (braces inside a hostile label are harmless and must not be counted).
	openB, closeB := countUnquotedBraces(out)
	if openB != 1 || closeB != 1 {
		t.Errorf("DOT structural braces unbalanced: { = %d, } = %d\n%s", openB, closeB, out)
	}
	if !strings.HasPrefix(out, "digraph doctopus {\n") || !strings.HasSuffix(out, "}\n") {
		t.Errorf("DOT not well-formed:\n%s", out)
	}
}

func TestEscapeDOTString_QuotesAndBackslash(t *testing.T) {
	got := escapeDOTString(`a"b\c`)
	if got != `a\"b\\c` {
		t.Errorf("escapeDOTString = %q, want %q", got, `a\"b\\c`)
	}
	if escapeDOTString("x\ry") != "x y" {
		t.Errorf("CR not converted to space: %q", escapeDOTString("x\ry"))
	}
}

func TestEscapeMermaidLabel_NeutralizesDelimiters(t *testing.T) {
	got := escapeMermaidLabel(`"]#<>`)
	for _, bad := range []string{`"`, "]", "#", "<", ">"} {
		if strings.Contains(got, bad) {
			t.Errorf("escapeMermaidLabel left %q in %q", bad, got)
		}
	}
}

// TestMermaid_LargeGraphFallback: a graph above the threshold focuses on the
// orphans/broken neighborhood and emits a truncation note rather than every node.
func TestMermaid_LargeGraphFallback(t *testing.T) {
	v := buildLargeView(t)
	out := string(Mermaid(v))
	if !strings.Contains(out, "focused subgraph") {
		t.Errorf("large graph did not emit a truncation note:\n%s", firstLines(out, 5))
	}
	// The focused subgraph must contain far fewer node declarations than the full
	// corpus (every document would be `n_...[`).
	nodeDecls := strings.Count(out, "[\"")
	if nodeDecls == 0 || nodeDecls >= len(v.Docs) {
		t.Errorf("expected a focused subset of nodes, got %d declarations for %d docs", nodeDecls, len(v.Docs))
	}
}

// buildLargeView builds a corpus with > LargeGraphThreshold documents: a long
// chain of linked docs (reachable) plus one isolated orphan, so the focus set is
// small.
func buildLargeView(t *testing.T) emit.View {
	t.Helper()
	c := corpus.NewCorpus()
	n := LargeGraphThreshold + 50
	var refs []reference.Reference
	// index.md is a root; it links to chain/0001.md which links onward.
	addDoc(t, c, "index.md", "Index", 1)
	for i := 0; i < n; i++ {
		id := chainID(i)
		addDoc(t, c, id.String(), "Doc", 1)
	}
	// Build a chain index -> chain/0 -> chain/1 -> ...
	refs = append(refs, validRef("index.md", chainID(0), 1))
	for i := 0; i < n-1; i++ {
		refs = append(refs, validRef(chainID(i).String(), chainID(i+1), 2))
	}
	// One isolated orphan with no links at all.
	addDoc(t, c, "orphan.md", "Orphan", 1)

	g := graphmodel.BuildReferenceGraph(c, refs, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	return emit.BuildView(application.Result{
		DocumentCount: c.Len(),
		Metrics:       m,
		Corpus:        c,
	})
}

func chainID(i int) identity.DocumentID {
	return identity.DocumentID(fmt.Sprintf("chain/%04d.md", i))
}

func addDoc(t *testing.T, c *corpus.Corpus, id, title string, line int) {
	t.Helper()
	doc := &corpus.Document{
		ID:          identity.DocumentID(id),
		FrontMatter: corpus.FrontMatter{Title: title},
		Root: &corpus.Section{Level: 0, Children: []*corpus.Section{
			{Level: 1, Text: title, Slug: "h", StartLine: line, EndLine: line + 10},
		}},
	}
	if err := c.Add(doc); err != nil {
		t.Fatal(err)
	}
}

func validRef(origin string, target identity.DocumentID, line int) reference.Reference {
	return reference.Reference{
		RawReference: reference.RawReference{
			Origin: identity.DocumentID(origin), RawTarget: target.String(),
			Type: reference.RelativeLink, Line: line,
		},
		Target: reference.ResolvedTarget{Kind: reference.TargetDocument, DocumentID: target},
		Health: reference.Valid,
	}
}

// countUnquotedBraces counts '{' and '}' that appear outside DOT double-quoted
// strings, honoring the \" escape, so braces inside a label are not counted.
func countUnquotedBraces(s string) (open, closed int) {
	inStr := false
	escaped := false
	for _, r := range s {
		if inStr {
			switch {
			case escaped:
				escaped = false
			case r == '\\':
				escaped = true
			case r == '"':
				inStr = false
			}
			continue
		}
		switch r {
		case '"':
			inStr = true
		case '{':
			open++
		case '}':
			closed++
		}
	}
	return open, closed
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
