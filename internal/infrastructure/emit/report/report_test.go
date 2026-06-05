package report

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stacklok/doctopus/internal/application"
	"github.com/stacklok/doctopus/internal/domain/corpus"
	"github.com/stacklok/doctopus/internal/domain/graphmodel"
	"github.com/stacklok/doctopus/internal/infrastructure/emit"
)

func TestEscapeCell_NeutralizesPipeAndNewline(t *testing.T) {
	// A hostile title containing a pipe and a newline must not break the table:
	// the pipe is escaped and the newline collapses to a space.
	got := escapeCell("a | b\nc")
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
