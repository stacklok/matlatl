package report

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stacklok/matlatl/internal/application"
	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/reference"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
)

// okfResult builds a minimal View-ready Result in OKF mode over a one-doc corpus,
// with the given okf-* findings, for the report render tests (ADR 0023).
func okfResult(t *testing.T, conformant bool, findings ...analysis.Finding) application.Result {
	t.Helper()
	c := corpus.NewCorpus()
	if err := c.Add(&corpus.Document{ID: "a.md"}); err != nil {
		t.Fatal(err)
	}
	c.Freeze()
	g := graphmodel.BuildReferenceGraph(c, nil, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	res := application.Result{
		DocumentCount: 1,
		Report:        analysis.NewAnalysisReport(findings),
		Metrics:       m,
		Corpus:        c,
		OKFMode:       true,
		OKFConformant: conformant,
	}
	for _, f := range findings {
		switch f.Kind {
		case analysis.OKFMissingFrontmatter:
			res.OKFMissingFrontmatterCount++
		case analysis.OKFMissingType:
			res.OKFMissingTypeCount++
		case analysis.OKFReservedFileStructure:
			res.OKFReservedFileStructureCount++
		}
	}
	return res
}

// TestReports_OKFConformant asserts the terminal + markdown reports render the
// CONFORMANT verdict and NO violations section (ADR 0023).
func TestReports_OKFConformant(t *testing.T) {
	v := emit.BuildView(okfResult(t, true))

	var term bytes.Buffer
	if err := Terminal(&term, v, TerminalOptions{Color: ColorNever}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(term.String(), "OKF v0.1: CONFORMANT") {
		t.Errorf("terminal missing CONFORMANT verdict:\n%s", term.String())
	}
	if strings.Contains(term.String(), "NOT CONFORMANT") || strings.Contains(term.String(), "fix-prompt <path> --okf") {
		t.Errorf("conformant terminal should have no violations section:\n%s", term.String())
	}

	md := string(Markdown(v))
	if !strings.Contains(md, "OKF v0.1: CONFORMANT") {
		t.Errorf("markdown missing CONFORMANT verdict:\n%s", md)
	}
	if strings.Contains(md, "| File | Line | Rule | Reason |") {
		t.Errorf("conformant markdown should have no violations table:\n%s", md)
	}
}

// TestReports_OKFNotConformant asserts the terminal + markdown reports name the
// offending files (rule + reason), not just the count (ADR 0023 item 4).
func TestReports_OKFNotConformant(t *testing.T) {
	f := analysis.Finding{
		ID: "okf-missing-type:a.md", Kind: analysis.OKFMissingType, Severity: analysis.Error,
		Location: analysis.Location{Document: "a.md", Line: 1},
		Message:  "\"a.md\" does not declare a non-empty OKF `type` (rule R2)",
		Details:  map[string]string{"targetDocument": "a.md", "reason": "`type` is empty (OKF §4.1)"},
	}
	v := emit.BuildView(okfResult(t, false, f))

	var term bytes.Buffer
	if err := Terminal(&term, v, TerminalOptions{Color: ColorNever}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"OKF v0.1: NOT CONFORMANT",
		"a.md:1", "missing-type", "`type` is empty",
		"fix-prompt <path> --okf",
	} {
		if !strings.Contains(term.String(), want) {
			t.Errorf("terminal NOT CONFORMANT section missing %q:\n%s", want, term.String())
		}
	}

	md := string(Markdown(v))
	// The reason's backticks are escaped in the GFM table cell, so match on the
	// backtick-free part of the reason.
	for _, want := range []string{
		"OKF v0.1: NOT CONFORMANT",
		"| File | Line | Rule | Reason |",
		"a.md", "missing-type", "is empty (OKF §4.1)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown NOT CONFORMANT table missing %q:\n%s", want, md)
		}
	}
}

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

// TestMarkdown_FarFromRootTableEscapesHostileID pins the populated far-from-root
// section (ADR 0021): a doc whose ID carries a pipe + newline must render as a
// table row whose cell is EscapeTableCell-neutralized, so it cannot break the GFM
// table or forge a new row. FarFromRootThreshold 1 makes the distance-1 doc far.
func TestMarkdown_FarFromRootTableEscapesHostileID(t *testing.T) {
	const hostile = "weird|pipe\nrow.md"
	c := corpus.NewCorpus()
	readme := &corpus.Document{
		ID:   "README.md",
		Root: &corpus.Section{Level: 0, Children: []*corpus.Section{{Level: 1, Text: "Home", Slug: "home", StartLine: 1, EndLine: 2}}},
	}
	far := &corpus.Document{ID: hostile, FrontMatter: corpus.FrontMatter{Title: "Deep"}}
	if err := c.Add(readme); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(far); err != nil {
		t.Fatal(err)
	}
	// README -> hostile (distance 1). Resolved edge wired directly (bypasses parsing).
	refs := []reference.Reference{{
		RawReference: reference.RawReference{Origin: "README.md", RawTarget: hostile, Type: reference.RelativeLink, Line: 2},
		Target:       reference.ResolvedTarget{Kind: reference.TargetDocument, DocumentID: hostile},
		Health:       reference.Valid,
	}}
	g := graphmodel.BuildReferenceGraph(c, refs, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{FarFromRootThreshold: 1})
	v := emit.BuildView(application.Result{DocumentCount: 2, Metrics: m, Corpus: c})

	if len(v.FarFromRoot) == 0 {
		t.Fatal("setup: expected the distance-1 doc to be far-from-root at threshold 1")
	}
	out := string(Markdown(v))
	idx := strings.Index(out, "## Far from root")
	if idx < 0 {
		t.Fatalf("Far from root section missing:\n%s", out)
	}
	section := out[idx:]
	// The hostile newline must not survive as a raw row break, and the pipe must be
	// escaped so it cannot forge an extra column.
	for _, line := range strings.Split(section, "\n") {
		if line == "row.md |" || strings.HasPrefix(line, "row.md") {
			t.Errorf("hostile newline forged a new table row:\n%s", section)
		}
	}
	if !strings.Contains(section, "\\|") {
		t.Errorf("pipe in the far-from-root doc ID was not escaped:\n%s", section)
	}
}

// TestMarkdown_FarFromRootIndeterminate pins the indeterminate branch: with no
// root set, the Far from root section states it is indeterminate rather than
// listing docs or "None." (ADR 0021).
func TestMarkdown_FarFromRootIndeterminate(t *testing.T) {
	c := corpus.NewCorpus()
	// No README/index/type:index → indeterminate root set.
	if err := c.Add(&corpus.Document{ID: "a.md"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(&corpus.Document{ID: "b.md"}); err != nil {
		t.Fatal(err)
	}
	g := graphmodel.BuildReferenceGraph(c, nil, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{})
	v := emit.BuildView(application.Result{DocumentCount: 2, Metrics: m, Corpus: c})

	if !v.ReachabilityIndeterminate {
		t.Fatal("setup: expected indeterminate reachability")
	}
	out := string(Markdown(v))
	idx := strings.Index(out, "## Far from root")
	if idx < 0 {
		t.Fatalf("Far from root section missing:\n%s", out)
	}
	if !strings.Contains(out[idx:], "Indeterminate: no root set found") {
		t.Errorf("indeterminate far-from-root section missing the indeterminate note:\n%s", out[idx:])
	}
}

// TestTerminal_FarFromRootSections renders the populated and indeterminate
// terminal branches of the Far from root section (ADR 0021).
func TestTerminal_FarFromRootSections(t *testing.T) {
	// Populated: README -> deep (distance 1), threshold 1 ⇒ deep is far.
	c := corpus.NewCorpus()
	readme := &corpus.Document{
		ID:   "README.md",
		Root: &corpus.Section{Level: 0, Children: []*corpus.Section{{Level: 1, Text: "H", Slug: "h", StartLine: 1, EndLine: 2}}},
	}
	deep := &corpus.Document{ID: "deep.md"}
	if err := c.Add(readme); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(deep); err != nil {
		t.Fatal(err)
	}
	refs := []reference.Reference{{
		RawReference: reference.RawReference{Origin: "README.md", RawTarget: "deep.md", Type: reference.RelativeLink, Line: 2},
		Target:       reference.ResolvedTarget{Kind: reference.TargetDocument, DocumentID: "deep.md"},
		Health:       reference.Valid,
	}}
	g := graphmodel.BuildReferenceGraph(c, refs, graphmodel.BuildOptions{})
	m := graphmodel.Analyze(g, c, graphmodel.AnalyzeOptions{FarFromRootThreshold: 1})
	v := emit.BuildView(application.Result{DocumentCount: 2, Metrics: m, Corpus: c})

	var buf bytes.Buffer
	if err := Terminal(&buf, v, TerminalOptions{Color: ColorNever}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "Far from root") || !strings.Contains(got, "deep.md (1 hops)") {
		t.Errorf("terminal far-from-root section missing the populated entry:\n%s", got)
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
	if !strings.HasPrefix(got, "matlatl: 1 documents") || strings.Count(got, "\n") != 1 {
		t.Errorf("quiet mode should print one summary line, got: %q", got)
	}
}
