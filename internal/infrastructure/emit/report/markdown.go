package report

import (
	"fmt"
	"strings"

	"github.com/stacklok/doctopus/internal/domain/analysis"
	"github.com/stacklok/doctopus/internal/domain/identity"
	"github.com/stacklok/doctopus/internal/infrastructure/emit"
)

// ReportMarkdownName is the conventional committable report filename.
const ReportMarkdownName = "report.md"

// Markdown renders a committable GitHub-flavored Markdown report from the View:
// a corpus-overview table, a broken-link/anchor table (file, line, target,
// suggested fix), orphan and unreachable lists with remediation, a hub/authority
// table, and a knowledge-gap section. Every cell is escaped (escapeCell) so a
// hostile document title/path cannot break a GFM table or inject markdown
// (ADR 0003). Output is deterministic (the View is sorted).
func Markdown(v emit.View) []byte {
	var b strings.Builder
	c := v.Counts

	b.WriteString("# doctopus report\n\n")

	// Corpus overview table.
	b.WriteString("## Corpus overview\n\n")
	b.WriteString("| Metric | Count |\n| --- | --- |\n")
	writeRow(&b, "Documents", c.Documents)
	writeRow(&b, "Headings", c.Headings)
	writeRow(&b, "References", c.References)
	writeRow(&b, "Components", c.Components)
	writeRow(&b, "Broken links", c.BrokenLink)
	writeRow(&b, "Broken anchors", c.BrokenAnchor)
	writeRow(&b, "Ambiguous links", c.Ambiguous)
	writeRow(&b, "Orphans", c.Orphan)
	writeRow(&b, "Unreachable", c.Unreachable)
	writeRow(&b, "Knowledge gaps", c.KnowledgeGap)
	b.WriteString("\n")

	// Broken links + anchors table (combined; Kind column distinguishes).
	b.WriteString("## Broken links and anchors\n\n")
	broken := make([]analysis.Finding, 0, len(v.BrokenLinks)+len(v.BrokenAnchors))
	broken = append(broken, v.BrokenLinks...)
	broken = append(broken, v.BrokenAnchors...)
	if len(broken) == 0 {
		b.WriteString("None. :tada:\n\n")
	} else {
		b.WriteString("| File | Line | Kind | Detail | Suggested fix |\n")
		b.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, f := range broken {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				escapeCell(f.Location.Document.String()),
				escapeCell(lineCell(f.Location.Line)),
				escapeCell(f.Kind.String()),
				escapeCell(f.Message),
				escapeCell(f.SuggestedFix))
		}
		b.WriteString("\n")
	}

	// Ambiguous links table.
	if len(v.Ambiguous) > 0 {
		b.WriteString("## Ambiguous links\n\n")
		b.WriteString("| File | Line | Detail | Suggested fix |\n")
		b.WriteString("| --- | --- | --- | --- |\n")
		for _, f := range v.Ambiguous {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
				escapeCell(f.Location.Document.String()),
				escapeCell(lineCell(f.Location.Line)),
				escapeCell(f.Message),
				escapeCell(f.SuggestedFix))
		}
		b.WriteString("\n")
	}

	// Orphans.
	b.WriteString("## Isolated orphans\n\n")
	b.WriteString("_No inbound or outbound navigational links. Link them in from a relevant page, or delete them. " +
		"To keep one intentionally unlinked, add front matter `doctopus: orphan-intentional`._\n\n")
	writeDocList(&b, v, v.Orphans)
	b.WriteString("\n")

	// Unreachable.
	b.WriteString("## Unreachable\n\n")
	if v.ReachabilityIndeterminate {
		b.WriteString("_Indeterminate: no root set found (no README.md/index.md, no `type: index`, no `--root`)._\n\n")
	} else {
		b.WriteString("_Not reachable from any root. Add an inbound link from a page that is itself reachable from a root._\n\n")
		writeDocList(&b, v, v.Unreachable)
		b.WriteString("\n")
	}

	// Hubs / authorities table.
	b.WriteString("## Hubs and authorities\n\n")
	if len(v.TopHubs) == 0 && len(v.TopAuthorities) == 0 {
		b.WriteString("None.\n\n")
	} else {
		b.WriteString("| Rank | Hub | Authority |\n| --- | --- | --- |\n")
		n := len(v.TopHubs)
		if len(v.TopAuthorities) > n {
			n = len(v.TopAuthorities)
		}
		for i := 0; i < n; i++ {
			hub, auth := "", ""
			if i < len(v.TopHubs) {
				hub = fmt.Sprintf("%s (%.3f)", v.TitleOf(v.TopHubs[i].ID), v.TopHubs[i].Score)
			}
			if i < len(v.TopAuthorities) {
				auth = fmt.Sprintf("%s (%.3f)", v.TitleOf(v.TopAuthorities[i].ID), v.TopAuthorities[i].Score)
			}
			fmt.Fprintf(&b, "| %d | %s | %s |\n", i+1, escapeCell(hub), escapeCell(auth))
		}
		b.WriteString("\n")
	}

	// Knowledge gaps.
	b.WriteString("## Knowledge gaps\n\n")
	b.WriteString("_Experimental: pairs of disconnected document clusters that may warrant a bridge (ADR 0007)._\n\n")
	if len(v.Gaps) == 0 {
		b.WriteString("None.\n")
	} else {
		b.WriteString("| Cluster A | Cluster B |\n| --- | --- |\n")
		for _, g := range v.Gaps {
			fmt.Fprintf(&b, "| %s | %s |\n",
				escapeCell(g.RepresentativeA.String()),
				escapeCell(g.RepresentativeB.String()))
		}
		if v.GapsTruncated {
			b.WriteString("\n_Note: the gap list was truncated (the corpus has many disconnected clusters)._\n")
		}
	}

	return []byte(b.String())
}

func writeRow(b *strings.Builder, label string, n int) {
	fmt.Fprintf(b, "| %s | %d |\n", escapeCell(label), n)
}

func lineCell(line int) string {
	if line <= 0 {
		return "-"
	}
	return fmt.Sprintf("%d", line)
}

// writeDocList writes a bullet list of documents with their display title, or
// "None." when empty. Both the path (in backticks) and the title are escaped.
func writeDocList(b *strings.Builder, v emit.View, ids []identity.DocumentID) {
	if len(ids) == 0 {
		b.WriteString("None.\n")
		return
	}
	for _, id := range ids {
		fmt.Fprintf(b, "- `%s` — %s\n", escapeInlineCode(id.String()), escapeText(v.TitleOf(id)))
	}
}

// escapeCell escapes a string for safe inclusion in a GFM table cell. A hostile
// title/path must not break the table (a literal `|` ends a cell) or inject
// markdown. We:
//   - escape backslashes first so our own escapes are not double-interpreted;
//   - replace newlines (which would end the table row) with a visible <br> so
//     multi-line messages stay inside one cell;
//   - backslash-escape the GFM-significant inline characters, crucially `|`.
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

// escapeText escapes a string for safe inclusion in flowing markdown (not a
// table): newlines collapse to spaces and markdown-significant characters are
// backslash-escaped so a hostile title cannot inject formatting or HTML.
func escapeText(s string) string {
	if s == "" {
		return ""
	}
	r := strings.NewReplacer(
		`\`, `\\`,
		"\r\n", " ",
		"\n", " ",
		"\r", " ",
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

// escapeInlineCode escapes a string for inclusion inside a single-backtick code
// span: a literal backtick would close the span, and a newline would break it,
// so both are neutralized.
func escapeInlineCode(s string) string {
	r := strings.NewReplacer(
		"`", "ʼ", // replace backtick with a modifier-letter apostrophe (cannot close the span)
		"\r\n", " ",
		"\n", " ",
		"\r", " ",
	)
	return r.Replace(s)
}
