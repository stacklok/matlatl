// Package index renders index.md: a navigable flat index of every document by
// canonical DocumentID, with a one-line description (front matter → first H1
// fallback), category/section, and mod-date. It is a dual-purpose human TOC and
// agent-navigation surface (the curated/importance-ordered index pattern). It is
// infrastructure: it reads the frozen emit.View and never mutates the domain
// (ADR 0004), and output is deterministic.
package index

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/stacklok/doctopus/internal/infrastructure/emit"
)

// IndexMarkdownName is the conventional index filename.
const IndexMarkdownName = "index.md"

// Markdown renders index.md from the View. Documents are grouped by category
// (their directory), categories sorted, documents within a category sorted by
// DocumentID — fully deterministic. Each entry lists the canonical DocumentID,
// its description, and its mod-date. Cell content is escaped so a hostile
// title/path cannot break the GFM table (ADR 0003).
func Markdown(v emit.View) []byte {
	var b strings.Builder
	b.WriteString("# Documentation index\n\n")
	fmt.Fprintf(&b, "%d document(s).\n\n", len(v.Docs))

	if len(v.Docs) == 0 {
		b.WriteString("_No documents._\n")
		return []byte(b.String())
	}

	// Group by category (directory). v.Docs is already sorted by DocumentID, so
	// within each category the order is stable.
	byCat := map[string][]emit.DocView{}
	for _, d := range v.Docs {
		byCat[d.Category] = append(byCat[d.Category], d)
	}
	cats := make([]string, 0, len(byCat))
	for cat := range byCat {
		cats = append(cats, cat)
	}
	sort.Strings(cats)

	for _, cat := range cats {
		fmt.Fprintf(&b, "## %s\n\n", escapeText(categoryLabel(cat)))
		b.WriteString("| Document | Description | Modified |\n| --- | --- | --- |\n")
		for _, d := range byCat[cat] {
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n",
				escapeInlineCode(d.ID.String()),
				escapeCell(d.Description),
				escapeCell(formatModTime(d.ModTime)))
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}

func categoryLabel(cat string) string {
	if cat == "." || cat == "" {
		return "(root)"
	}
	return cat
}

// formatModTime renders a mod-time as a stable RFC3339 UTC date-time, or "-" for
// the zero time. UTC keeps the index byte-stable regardless of the runner's
// local timezone (determinism contract).
func formatModTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

// escapeCell mirrors the report package's GFM-cell escaping (kept local so the
// index package has no cross-emitter dependency): neutralize the pipe, newlines
// and inline-markdown characters that could break the table or inject markdown.
func escapeCell(s string) string {
	if s == "" {
		return ""
	}
	r := strings.NewReplacer(
		`\`, `\\`,
		"\r\n", " ",
		"\n", " ",
		"\r", " ",
		"|", `\|`,
		"`", "\\`",
		"*", `\*`,
		"_", `\_`,
		"<", `\<`,
		">", `\>`,
		"[", `\[`,
		"]", `\]`,
	)
	return r.Replace(s)
}

func escapeText(s string) string {
	r := strings.NewReplacer(
		"\r\n", " ",
		"\n", " ",
		"\r", " ",
		"<", `\<`,
		">", `\>`,
	)
	return r.Replace(s)
}

// escapeInlineCode neutralizes backticks and newlines so a path cannot break a
// single-backtick code span.
func escapeInlineCode(s string) string {
	r := strings.NewReplacer(
		"`", "ʼ",
		"\r\n", " ",
		"\n", " ",
		"\r", " ",
	)
	return r.Replace(s)
}
