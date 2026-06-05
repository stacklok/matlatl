package report

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stacklok/doctopus/internal/application"
	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/graphmodel"
	"github.com/stacklok/doctopus/internal/domain/reference"
	"github.com/stacklok/doctopus/internal/infrastructure/emit"
)

func TestEscapeCell_NeutralizesPipeAndNewline(t *testing.T) {
	// A hostile title containing a pipe and a newline must not break the table:
	// the pipe is escaped and the newline collapses to a space. This exercises the
	// shared emit.EscapeTableCell helper the report and index both use.
	got := emit.EscapeTableCell("a | b\nc")
	if strings.Contains(got, "\n") {
		t.Errorf("newline survived cell escaping: %q", got)
	}
	if strings.Contains(got, "| b") && !strings.Contains(got, `\|`) {
		t.Errorf("pipe not escaped: %q", got)
	}
}

func TestMarkdown_HostileTitleDoesNotBreakTable(t *testing.T) {
	hostile := "Pwn|Title\n## Injected\n`x`<b>"
	c := corpus.NewCorpus()
	doc := &corpus.Document{
		ID:          "evil.md",
		FrontMatter: corpus.FrontMatter{Title: hostile, Description: hostile},
		Root: &corpus.Section{Level: 0, Children: []*corpus.Section{
			{Level: 1, Text: hostile, Slug: "h", StartLine: 1, EndLine: 3},
		}},
	}
	if err := c.Add(doc); err != nil {
		t.Fatal(err)
	}
	g := graphmodel.BuildReferenceGraph(c, nil, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	v := emit.BuildView(application.Result{DocumentCount: 1, Metrics: m, Corpus: c})

	out := string(Markdown(v))
	// No table row may contain a raw newline-injected heading.
	if strings.Contains(out, "\n## Injected") {
		// The hostile "## Injected" must only ever appear escaped, never as a real
		// heading line inside a cell.
		for _, line := range strings.Split(out, "\n") {
			if line == "## Injected" {
				t.Errorf("hostile title injected a real markdown heading:\n%s", out)
			}
		}
	}
}

// TestMarkdown_OrphanListEscapesHostileTitle pins writeDocList in LIST context
// (not the table context already covered above): an isolated orphan with a
// hostile title (pipe, quote, newline, backtick) must render as a bullet whose
// path is in an inline-code span and whose title is escaped as flowing Markdown.
// The hostile newline must NOT survive (it could inject a new list item / block),
// and the backtick must be neutralized so it cannot close the inline-code span.
func TestMarkdown_OrphanListEscapesHostileTitle(t *testing.T) {
	const hostile = "Quote\"Pipe|Tick`End\nInjected"
	c := corpus.NewCorpus()
	// A single document with no inbound/outbound links is an isolated orphan. No
	// root set is found, so reachability is indeterminate (orphans still listed).
	doc := &corpus.Document{
		ID:          "orphan.md",
		FrontMatter: corpus.FrontMatter{Title: hostile},
	}
	if err := c.Add(doc); err != nil {
		t.Fatal(err)
	}
	g := graphmodel.BuildReferenceGraph(c, nil, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	v := emit.BuildView(application.Result{DocumentCount: 1, Metrics: m, Corpus: c})

	if len(v.Orphans) == 0 {
		t.Fatal("setup: expected the lone unlinked document to be an isolated orphan")
	}

	out := string(Markdown(v))

	// The orphan bullet must be present, with the path in an inline-code span.
	if !strings.Contains(out, "- `orphan.md` — ") {
		t.Fatalf("orphan bullet not rendered in list form:\n%s", out)
	}
	// Locate the orphan bullet line and verify the hostile title is escaped there.
	var bullet string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "- `orphan.md`") {
			bullet = line
			break
		}
	}
	if bullet == "" {
		t.Fatalf("could not find the orphan bullet line:\n%s", out)
	}
	// The newline must have collapsed: the injected "Injected" text stays on the
	// same bullet line, never on its own line as a forged list item / block.
	if !strings.Contains(bullet, "Injected") {
		t.Errorf("title text lost from the bullet: %q", bullet)
	}
	for _, line := range strings.Split(out, "\n") {
		if line == "Injected" || line == "- Injected" {
			t.Errorf("hostile newline injected a separate line/bullet:\n%s", out)
		}
	}
	// The backtick in the title must be neutralized (EscapeMarkdownText escapes it
	// to \` in flowing text) so it cannot prematurely close the path code span.
	if !strings.Contains(bullet, "\\`") {
		t.Errorf("backtick in title not escaped in list context: %q", bullet)
	}
}

// TestMarkdown_EmptyDocListRendersNone pins writeDocList's empty branch: when
// there are no orphans, the section renders the literal "None." sentinel rather
// than an empty (confusing) list. A fully-linked two-document corpus with a
// README root has no isolated orphans and no unreachable docs.
func TestMarkdown_EmptyDocListRendersNone(t *testing.T) {
	c := corpus.NewCorpus()
	// README links to guide; guide links back. README is a directory-index root,
	// so reachability is determinate and nothing is orphaned/unreachable. The graph
	// is built from RESOLVED references (the second arg), so we wire the resolved
	// edges explicitly.
	readme := &corpus.Document{
		ID:          "README.md",
		FrontMatter: corpus.FrontMatter{Title: "Home"},
		Root: &corpus.Section{Level: 0, Children: []*corpus.Section{
			{Level: 1, Text: "Home", Slug: "home", StartLine: 1, EndLine: 2},
		}},
	}
	guide := &corpus.Document{
		ID:          "guide.md",
		FrontMatter: corpus.FrontMatter{Title: "Guide"},
	}
	if err := c.Add(readme); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(guide); err != nil {
		t.Fatal(err)
	}
	refs := []reference.Reference{
		{
			RawReference: reference.RawReference{Origin: "README.md", RawTarget: "guide.md", Type: reference.RelativeLink, Line: 2},
			Target:       reference.ResolvedTarget{Kind: reference.TargetDocument, DocumentID: "guide.md"},
			Health:       reference.Valid,
		},
		{
			RawReference: reference.RawReference{Origin: "guide.md", RawTarget: "README.md", Type: reference.RelativeLink, Line: 1},
			Target:       reference.ResolvedTarget{Kind: reference.TargetDocument, DocumentID: "README.md"},
			Health:       reference.Valid,
		},
	}
	g := graphmodel.BuildReferenceGraph(c, refs, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	v := emit.BuildView(application.Result{DocumentCount: 2, Metrics: m, Corpus: c})

	if len(v.Orphans) != 0 {
		t.Fatalf("setup: expected no orphans, got %v", v.Orphans)
	}

	out := string(Markdown(v))
	idx := strings.Index(out, "## Isolated orphans")
	if idx < 0 {
		t.Fatalf("orphans section missing:\n%s", out)
	}
	// The orphans section body must contain the "None." sentinel.
	section := out[idx:]
	if !strings.Contains(section, "None.") {
		t.Errorf("empty orphan list did not render the None. sentinel:\n%s", section)
	}
}

func TestUseColor_HonorsNoColorAndTTY(t *testing.T) {
	// NO_COLOR set → never, even with ColorAlways.
	withEnv := func(string) (string, bool) { return "", true }
	if useColor(ColorAlways, &bytes.Buffer{}, withEnv) {
		t.Error("NO_COLOR set should disable color even with ColorAlways")
	}
	// ColorNever → never.
	noEnv := func(string) (string, bool) { return "", false }
	if useColor(ColorNever, os.Stdout, noEnv) {
		t.Error("ColorNever should disable color")
	}
	// ColorAuto + non-TTY writer (bytes.Buffer) → no color.
	if useColor(ColorAuto, &bytes.Buffer{}, noEnv) {
		t.Error("ColorAuto on a non-TTY writer should not colorize")
	}
	// ColorAlways + no NO_COLOR → color.
	if !useColor(ColorAlways, &bytes.Buffer{}, noEnv) {
		t.Error("ColorAlways without NO_COLOR should colorize")
	}
}

func TestTerminal_NoColorWhenNotTTY(t *testing.T) {
	c := corpus.NewCorpus()
	doc := &corpus.Document{ID: "a.md", FrontMatter: corpus.FrontMatter{Title: "A"}}
	_ = c.Add(doc)
	g := graphmodel.BuildReferenceGraph(c, nil, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	v := emit.BuildView(application.Result{DocumentCount: 1, Metrics: m, Corpus: c})

	var buf bytes.Buffer
	if err := Terminal(&buf, v, TerminalOptions{Color: ColorAuto}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "\033[") {
		t.Errorf("terminal report colorized a non-TTY writer:\n%q", buf.String())
	}
}

func TestTerminal_QuietSummaryLine(t *testing.T) {
	c := corpus.NewCorpus()
	_ = c.Add(&corpus.Document{ID: "a.md", FrontMatter: corpus.FrontMatter{Title: "A"}})
	g := graphmodel.BuildReferenceGraph(c, nil, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	v := emit.BuildView(application.Result{DocumentCount: 1, HeadingCount: 0, Metrics: m, Corpus: c})

	var buf bytes.Buffer
	if err := Terminal(&buf, v, TerminalOptions{Color: ColorNever, Quiet: true}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "doctopus: 1 documents") || strings.Count(got, "\n") != 1 {
		t.Errorf("quiet mode should print one summary line, got: %q", got)
	}
}
