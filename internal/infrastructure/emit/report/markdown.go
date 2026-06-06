package report

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
)

// ReportMarkdownName is the conventional committable report filename.
const ReportMarkdownName = "report.md"

// suggestedLinkTopN bounds how many suggested links the human reports surface
// (mirrors the emit-package topN used for hubs/authorities).
const suggestedLinkTopN = 5

// Markdown renders a committable GitHub-flavored Markdown report from the View:
// a corpus-overview table, a broken-link/anchor table (file, line, target,
// suggested fix), orphan and unreachable lists with remediation, a hub/authority
// table, and a knowledge-gap section. Every cell is escaped (emit.EscapeTableCell)
// so a hostile document title/path cannot break a GFM table or inject markdown
// (ADR 0003). Output is deterministic (the View is sorted).
func Markdown(v emit.View) []byte {
	var b strings.Builder
	c := v.Counts

	b.WriteString("# matlatl report\n\n")

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
	writeRow(&b, "Under-linked", c.UnderLinked)
	writeRow(&b, "Dead-ends", c.DeadEnd)
	writeRow(&b, "Knowledge gaps", c.KnowledgeGap)
	b.WriteString("\n")

	// One-line bow-tie structure summary (ADR 0012): the macro-shape of the
	// corpus relative to its giant strongly-connected core.
	writeBowtie(&b, v)
	b.WriteString("\n")

	// Navigability scalars (ADR 0014): how navigable the corpus is overall.
	b.WriteString("## Navigability\n\n")
	for _, l := range navigabilityLines(v) {
		fmt.Fprintf(&b, "- %s\n", emit.EscapeMarkdownText(l))
	}
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
				emit.EscapeTableCell(f.Location.Document.String()),
				emit.EscapeTableCell(lineCell(f.Location.Line)),
				emit.EscapeTableCell(f.Kind.String()),
				emit.EscapeTableCell(f.Message),
				emit.EscapeTableCell(f.SuggestedFix))
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
				emit.EscapeTableCell(f.Location.Document.String()),
				emit.EscapeTableCell(lineCell(f.Location.Line)),
				emit.EscapeTableCell(f.Message),
				emit.EscapeTableCell(f.SuggestedFix))
		}
		b.WriteString("\n")
	}

	// Orphans.
	b.WriteString("## Isolated orphans\n\n")
	b.WriteString("_No inbound or outbound navigational links. Link them in from a relevant page, or delete them. " +
		"To keep one intentionally unlinked, add front matter `matlatl: orphan-intentional`._\n\n")
	writeDocList(&b, v, v.Orphans)
	b.WriteString("\n")

	// Under-linked.
	b.WriteString("## Under-linked\n\n")
	b.WriteString("_Fewer inbound links than the discoverability threshold. Add inbound links from related, " +
		"more-connected pages so readers and agents can find them. To keep one intentionally sparse, add " +
		"front matter `matlatl: orphan-intentional`._\n\n")
	writeDocList(&b, v, v.UnderLinked)
	b.WriteString("\n")

	// Dead-ends.
	b.WriteString("## Dead-ends\n\n")
	b.WriteString("_Have inbound links but link to nothing onward. Add onward internal links to related " +
		"documents. To keep one intentionally terminal, add front matter `matlatl: orphan-intentional`._\n\n")
	writeDocList(&b, v, v.DeadEnd)
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
			fmt.Fprintf(&b, "| %d | %s | %s |\n", i+1, emit.EscapeTableCell(hub), emit.EscapeTableCell(auth))
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
				emit.EscapeTableCell(g.RepresentativeA.String()),
				emit.EscapeTableCell(g.RepresentativeB.String()))
		}
		if v.GapsTruncated {
			b.WriteString("\n_Note: the gap list was truncated (the corpus has many disconnected clusters)._\n")
		}
	}
	b.WriteString("\n")

	// Suggested links (ADR 0013): topology-based, additive to knowledge gaps.
	b.WriteString("## Suggested links\n\n")
	b.WriteString("_Experimental: unlinked document pairs that share neighbours, ranked by " +
		"Adamic/Adar; topology suggests they may warrant a link (ADR 0013)._\n\n")
	if len(v.SuggestedLinks) == 0 {
		b.WriteString("None.\n")
	} else {
		b.WriteString("| From | To | Shared | A/A score |\n| --- | --- | --- | --- |\n")
		shown := v.SuggestedLinks
		if len(shown) > suggestedLinkTopN {
			shown = shown[:suggestedLinkTopN]
		}
		for _, s := range shown {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
				emit.EscapeTableCell(s.DocA.String()),
				emit.EscapeTableCell(s.DocB.String()),
				emit.EscapeTableCell(strconv.Itoa(s.SharedNeighbours)),
				emit.EscapeTableCell(strconv.FormatFloat(s.AdamicAdar, 'f', 6, 64)))
		}
		if len(v.SuggestedLinks) > suggestedLinkTopN {
			fmt.Fprintf(&b, "\n_Showing the top %d of %d suggestion(s)._\n", suggestedLinkTopN, len(v.SuggestedLinks))
		}
		if v.SuggestedLinksTruncated {
			b.WriteString("\n_Note: the suggestion list was truncated (capped, or a hub neighbour was skipped)._\n")
		}
	}

	return []byte(b.String())
}

// writeBowtie writes the one-line bow-tie structure summary into the markdown
// report (e.g. "Structure: 3 core, 1 in, 2 out, 0 tendril, 1 disconnected").
func writeBowtie(b *strings.Builder, v emit.View) {
	b.WriteString("**")
	b.WriteString(bowtieLine(v))
	b.WriteString("**\n")
}

func writeRow(b *strings.Builder, label string, n int) {
	fmt.Fprintf(b, "| %s | %d |\n", emit.EscapeTableCell(label), n)
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
		fmt.Fprintf(b, "- `%s` — %s\n", emit.EscapeInlineCode(id.String()), emit.EscapeMarkdownText(v.TitleOf(id)))
	}
}
