// Package index renders index.md: a navigable flat index of every document by
// canonical DocumentID, with a one-line description (front matter → first H1
// fallback), category/section, and mod-date. It is a dual-purpose human TOC and
// agent-navigation surface (the curated/importance-ordered index pattern). It is
// infrastructure: it reads the frozen emit.View and never mutates the domain
// (ADR 0004), and output is deterministic.
package index

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
)

// IndexMarkdownName is the conventional index filename.
const IndexMarkdownName = "index.md"

// Markdown renders index.md from the View. Documents are grouped by category
// (their directory), categories sorted, documents within a category sorted by
// DocumentID — fully deterministic. Each entry lists the canonical DocumentID,
// its description, and its mod-date. Every emitted string is escaped via the
// shared emit escape helpers (the SAME ones the Markdown report uses) so a
// hostile title/path/category cannot break the GFM table or inject markdown
// (ADR 0003).
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
	slices.Sort(cats)

	for _, cat := range cats {
		// The category label is an attacker-influenced directory name and is
		// rendered as a Markdown heading, so it gets the same flowing-text escaping
		// the report uses (a hostile category must not render as live markdown).
		fmt.Fprintf(&b, "## %s\n\n", emit.EscapeMarkdownText(emit.CategoryLabel(cat)))
		b.WriteString("| Document | Description | Backlinks | Modified |\n| --- | --- | --- | --- |\n")
		for _, d := range byCat[cat] {
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n",
				emit.EscapeInlineCode(d.ID.String()),
				emit.EscapeTableCell(d.Description),
				emit.EscapeTableCell(backlinksCell(v, d.ID)),
				emit.EscapeTableCell(formatModTime(d.ModTime)))
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// backlinksCell renders the documents that link TO id as a comma-separated list
// of their paths (ADR 0016, Nelson/Xanadu two-way links), or "-" when nothing
// links to it. The list is the document projection's in-neighbours (already
// sorted by path, self-excluded), so the cell is deterministic.
func backlinksCell(v emit.View, id identity.DocumentID) string {
	in := v.Backlinks(id)
	if len(in) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(in))
	for _, src := range in {
		parts = append(parts, src.String())
	}
	return strings.Join(parts, ", ")
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
