// Package llmstxt renders the llms.txt family — the LLM-facing navigation and
// context artifacts:
//
//   - llms.txt       a spec-compliant, curated index of the most IMPORTANT
//     reachable docs (H1 title, blockquote summary, H2 link
//     sections, an "Optional" section last, plus a known-gaps note).
//   - llms-full.txt  the concatenated CLEAN markdown of every reachable doc,
//     importance-ordered, each preceded by a context header.
//   - llms-small.txt the same shape as llms-full but filtered to hubs + root /
//     getting-started docs, for tight context windows.
//
// It is infrastructure: it reads the frozen emit.View (+ a root-confined
// BodyReader for the raw bodies) and never mutates the domain (ADR 0004). Output
// is deterministic: documents are importance-ordered with a total tie-break, so
// the same corpus always yields byte-identical artifacts.
//
// Importance ranking (the "lost in the middle" mitigation — most-connected docs
// FIRST): documents are ordered by HITS authority score DESC, then in-degree
// DESC, then DocumentID ASC (a total order). Authority captures "many important
// docs point here" (hub/spoke centrality); in-degree is the concrete tie-break;
// the ID makes it deterministic. llms.txt lists the top-ranked reachable docs in
// the main sections and the lower-signal remainder under "## Optional".
package llmstxt

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stacklok/matlatl/internal/infrastructure/emit"
)

// Artifact filenames (stable; downstream tooling keys on these).
const (
	LLMSTxtName      = "llms.txt"
	LLMSFullTxtName  = "llms-full.txt"
	LLMSSmallTxtName = "llms-small.txt"
)

// optionalThreshold is the number of top-ranked reachable documents listed in
// the curated main section of llms.txt before the remainder spills into the
// "## Optional" section. It keeps the curated list tight (the research finding:
// a short curated index beats dumping everything).
const optionalThreshold = 20

// rankedDoc is a document paired with the importance signals used to order it.
type rankedDoc struct {
	view      emit.DocView
	authority float64
	inDegree  int
}

// Options configure the llms.txt family. Title overrides the corpus title (else
// it is derived from the highest-ranked root doc, falling back to a default).
type Options struct {
	// Title is the corpus/project title for the H1 (from a flag/config). Empty
	// means derive from the root doc / a default.
	Title string
}

// LLMSTxt renders llms.txt: the spec-compliant curated index. Shape: a single H1
// title, a one/two-sentence blockquote summary (corpus counts + what it is), H2
// sections of `[title](path): description` links to the most important reachable
// docs FIRST, an "## Optional" H2 last for the lower-signal remainder, and a
// "## Known gaps" note flagging orphan/broken counts so an agent knows the
// corpus is incomplete.
func LLMSTxt(v emit.View, opts Options) []byte {
	var b strings.Builder

	title := resolveTitle(v, opts)
	fmt.Fprintf(&b, "# %s\n\n", oneLine(title))
	fmt.Fprintf(&b, "> %s\n\n", summaryLine(v))

	ranked := rankedReachable(v)
	if len(ranked) == 0 {
		b.WriteString("_No reachable documents._\n")
		writeKnownGaps(&b, v)
		return []byte(b.String())
	}

	primary := ranked
	var optional []rankedDoc
	if len(ranked) > optionalThreshold {
		primary = ranked[:optionalThreshold]
		optional = ranked[optionalThreshold:]
	}

	b.WriteString("## Documentation\n\n")
	for _, rd := range primary {
		writeLink(&b, rd.view)
	}
	b.WriteString("\n")

	if len(optional) > 0 {
		b.WriteString("## Optional\n\n")
		for _, rd := range optional {
			writeLink(&b, rd.view)
		}
		b.WriteString("\n")
	}

	writeKnownGaps(&b, v)
	return []byte(b.String())
}

// writeLink writes one curated `- [title](path): description` line. The link
// text/description are single-lined (no body dumping) and parenthesis/newline
// safe so they cannot break the markdown link.
func writeLink(b *strings.Builder, d emit.DocView) {
	desc := oneLine(d.Description)
	if desc != "" {
		fmt.Fprintf(b, "- [%s](%s): %s\n", linkText(d.Title), linkPath(d.ID), desc)
		return
	}
	fmt.Fprintf(b, "- [%s](%s)\n", linkText(d.Title), linkPath(d.ID))
}

// writeKnownGaps writes the incompleteness note so an agent knows what is
// missing (orphans, unreachable, broken links/anchors, ambiguous). Always
// emitted (a clean corpus reports "none") so the section is a stable contract.
func writeKnownGaps(b *strings.Builder, v emit.View) {
	b.WriteString("## Known gaps\n\n")
	c := v.Counts
	total := c.Orphan + c.Unreachable + c.UnderLinked + c.DeadEnd +
		c.BrokenLink + c.BrokenAnchor + c.Ambiguous + c.SuggestedLink
	if total == 0 {
		b.WriteString("- None: every document is reachable and all links resolve.\n")
		return
	}
	b.WriteString("This corpus is INCOMPLETE for navigation; an agent should not assume full coverage:\n\n")
	fmt.Fprintf(b, "- %d orphan document(s) (nothing links to them)\n", c.Orphan)
	fmt.Fprintf(b, "- %d unreachable document(s) (not reachable from a root)\n", c.Unreachable)
	fmt.Fprintf(b, "- %d under-linked document(s) (few inbound links; hard to discover)\n", c.UnderLinked)
	fmt.Fprintf(b, "- %d dead-end document(s) (no onward links)\n", c.DeadEnd)
	fmt.Fprintf(b, "- %d broken link(s)\n", c.BrokenLink)
	fmt.Fprintf(b, "- %d broken anchor(s)\n", c.BrokenAnchor)
	fmt.Fprintf(b, "- %d ambiguous link(s)\n", c.Ambiguous)
	fmt.Fprintf(b, "- %d suggested link(s) (unlinked but topologically related; experimental)\n", c.SuggestedLink)
}

// summaryLine is the one/two-sentence blockquote summary: what the corpus is
// plus its headline counts.
func summaryLine(v emit.View) string {
	c := v.Counts
	return oneLine(fmt.Sprintf(
		"A markdown documentation corpus of %d document(s) across %d component(s), "+
			"with %d heading(s) and %d resolved reference(s). "+
			"Entries are ordered by importance (most-connected first).",
		c.Documents, c.Components, c.Headings, c.References))
}

// rankedReachable returns the reachable documents in importance order (authority
// DESC, in-degree DESC, ID ASC). When reachability is indeterminate (no root
// set), every document is treated as reachable so the corpus is still surfaced
// (ADR 0007: indeterminate is not unreachable).
func rankedReachable(v emit.View) []rankedDoc {
	reachable, indeterminate := emit.ReachableSet(v.Metrics)
	out := make([]rankedDoc, 0, len(v.Docs))
	for _, d := range v.Docs {
		if !indeterminate {
			if _, ok := reachable[d.ID]; !ok {
				continue
			}
		}
		var auth float64
		if v.Metrics != nil {
			auth = v.Metrics.HITS.AuthorityScore(d.ID)
		}
		out = append(out, rankedDoc{view: d, authority: auth, inDegree: d.InDegree})
	}
	sortByImportance(out)
	return out
}

// sortByImportance applies the documented total order in place.
func sortByImportance(rds []rankedDoc) {
	slices.SortFunc(rds, func(a, b rankedDoc) int {
		switch {
		case a.authority > b.authority:
			return -1
		case a.authority < b.authority:
			return 1
		case a.inDegree > b.inDegree:
			return -1
		case a.inDegree < b.inDegree:
			return 1
		}
		return strings.Compare(a.view.ID.String(), b.view.ID.String())
	})
}

// resolveTitle picks the corpus title: the configured Title, else the
// highest-ranked root document's title, else a default.
func resolveTitle(v emit.View, opts Options) string {
	if strings.TrimSpace(opts.Title) != "" {
		return opts.Title
	}
	if v.Metrics != nil && !v.Metrics.RootSet.Indeterminate {
		// Roots are sorted; rank them by importance and take the top.
		var roots []rankedDoc
		for _, id := range v.Metrics.RootSet.Roots {
			if d, ok := v.Doc(id); ok {
				roots = append(roots, rankedDoc{
					view:      d,
					authority: v.Metrics.HITS.AuthorityScore(id),
					inDegree:  d.InDegree,
				})
			}
		}
		if len(roots) > 0 {
			sortByImportance(roots)
			if t := strings.TrimSpace(roots[0].view.Title); t != "" {
				return t
			}
		}
	}
	return "Documentation corpus"
}
