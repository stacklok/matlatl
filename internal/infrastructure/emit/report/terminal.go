package report

import (
	"fmt"
	"io"
	"os"

	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/infrastructure/emit"
)

// bowtieLine renders the one-line bow-tie structure summary shared by the
// terminal and markdown reports (ADR 0012). When the giant SCC has a single
// member there is no cyclic core, which the line says explicitly; otherwise it
// reports the per-bucket counts relative to the core.
func bowtieLine(v emit.View) string {
	if v.Metrics == nil {
		return "Structure: no cyclic core"
	}
	bw := v.Metrics.Bowtie
	c := bw.Counts
	if bw.GiantSCCSize <= 1 {
		return fmt.Sprintf(
			"Structure: no cyclic core (%d in, %d out, %d tendril, %d disconnected)",
			c[graphmodel.BucketIn], c[graphmodel.BucketOut],
			c[graphmodel.BucketTendril], c[graphmodel.BucketDisconnected])
	}
	return fmt.Sprintf(
		"Structure: %d core, %d in, %d out, %d tendril, %d disconnected",
		c[graphmodel.BucketCore], c[graphmodel.BucketIn], c[graphmodel.BucketOut],
		c[graphmodel.BucketTendril], c[graphmodel.BucketDisconnected])
}

// navigabilityLines renders the corpus navigability scalars (ADR 0014) as a list
// of "label: value" lines shared by the terminal and markdown reports. Floats
// are formatted %.3f. When N==0 (or no metrics) it returns a single
// "not computed" line so the section is always present (a stable contract).
func navigabilityLines(v emit.View) []string {
	if v.Metrics == nil || v.Metrics.Navigability.Documents == 0 {
		return []string{"Navigability: not computed (no documents)"}
	}
	n := v.Metrics.Navigability
	return []string{
		fmt.Sprintf("Compactness: %.3f (0 = disconnected, 1 = fully connected)", n.Compactness),
		fmt.Sprintf("Stratum: %.3f (0 = cyclic/symmetric, 1 = pure hierarchy)", n.Stratum),
		fmt.Sprintf("Characteristic path length: %.3f (mean clicks between linked docs)", n.CharacteristicPathLength),
		fmt.Sprintf("Median path length: %.3f", n.MedianPathLength),
		fmt.Sprintf("Diameter: %d (longest shortest path)", n.Diameter),
		fmt.Sprintf("Clustering coefficient: %.3f", n.ClusteringCoefficient),
		fmt.Sprintf("Reachable pairs: %d", n.ReachablePairs),
	}
}

// criticalStructureLines renders the corpus' critical-path structure (ADR 0015):
// the articulation points (cut vertices) and bridges (cut edges) of the
// undirected link closure — single points of failure. It is shared by the
// terminal and markdown reports so they cannot diverge. Each list guards its
// empty case with an explicit "none" line, so the section is always present (a
// stable contract).
func criticalStructureLines(v emit.View) []string {
	out := make([]string, 0, 2+len(v.ArticulationPoints)+len(v.Bridges))
	if len(v.ArticulationPoints) == 0 {
		out = append(out, "Articulation points: none")
	} else {
		out = append(out, fmt.Sprintf("Articulation points (%d): single docs whose removal fragments the corpus",
			len(v.ArticulationPoints)))
		for _, id := range v.ArticulationPoints {
			out = append(out, "  "+id.String())
		}
	}
	if len(v.Bridges) == 0 {
		out = append(out, "Bridges: none")
	} else {
		out = append(out, fmt.Sprintf("Bridges (%d): single links whose removal disconnects two clusters",
			len(v.Bridges)))
		for _, b := range v.Bridges {
			out = append(out, fmt.Sprintf("  %s — %s", b.A, b.B))
		}
	}
	return out
}

// TerminalOptions tunes the terminal report.
type TerminalOptions struct {
	// Color selects color behavior (auto/never/always). Auto is TTY+NO_COLOR aware.
	Color ColorMode
	// Quiet renders only the one-line summary (the legacy `matlatl .` output).
	Quiet bool
}

// Terminal writes a human-facing analysis summary to w. It front-loads the
// actionable problems (U-shaped attention): counts, then broken links + broken
// anchors with file:line, then orphans vs. unreachable (clearly separated, each
// with its distinct remediation hint, ADR 0007), then top hubs/authorities,
// then a knowledge-gap note. Color is emitted only when w is a TTY and NO_COLOR
// is unset (or forced via opts). Output is deterministic (the View is sorted).
func Terminal(w io.Writer, v emit.View, opts TerminalOptions) error {
	p := palette{enabled: useColor(opts.Color, w, os.LookupEnv)}

	if v.Counts.Documents == 0 {
		_, err := fmt.Fprintln(w, "matlatl: no markdown documents found (nothing to analyze)")
		return err
	}
	if opts.Quiet {
		return summaryLine(w, p, v)
	}
	return fullReport(w, p, v)
}

// summaryLine prints the single-line tally (the --quiet path).
func summaryLine(w io.Writer, p palette, v emit.View) error {
	c := v.Counts
	_, err := fmt.Fprintf(w,
		"matlatl: %d documents, %d headings, %d references — %s, %s, %d ambiguous, %d orphan(s), %d unreachable\n",
		c.Documents, c.Headings, c.References,
		p.colorCount(c.BrokenLink, fmt.Sprintf("%d broken link(s)", c.BrokenLink)),
		p.colorCount(c.BrokenAnchor, fmt.Sprintf("%d broken anchor(s)", c.BrokenAnchor)),
		c.Ambiguous, c.Orphan, c.Unreachable)
	return err
}

// colorCount colors a count phrase red when non-zero, green when zero.
func (p palette) colorCount(n int, phrase string) string {
	if n > 0 {
		return p.red(phrase)
	}
	return p.green(phrase)
}

func fullReport(w io.Writer, p palette, v emit.View) error {
	// errWriter collapses the repetitive error handling of many Fprint calls
	// into a single post-hoc check, keeping the layout readable.
	ew := &errWriter{w: w}
	c := v.Counts

	ew.line(p.bold("matlatl analysis report"))
	ew.line("")
	ew.line(fmt.Sprintf("Corpus: %s, %s, %s across %s.",
		plural(c.Documents, "document"), plural(c.Headings, "heading"),
		plural(c.References, "reference"), plural(c.Components, "component")))
	ew.line("")

	// 1. Broken links — the highest-priority problems, front-loaded.
	section(ew, p, "Broken links", len(v.BrokenLinks))
	if len(v.BrokenLinks) == 0 {
		ew.line("  " + p.green("none"))
	}
	for _, f := range v.BrokenLinks {
		ew.line("  " + p.red(location(f)) + " " + f.Message)
		if f.SuggestedFix != "" {
			ew.line("    " + p.dim("fix: "+f.SuggestedFix))
		}
	}
	ew.line("")

	// 2. Broken anchors.
	section(ew, p, "Broken anchors", len(v.BrokenAnchors))
	if len(v.BrokenAnchors) == 0 {
		ew.line("  " + p.green("none"))
	}
	for _, f := range v.BrokenAnchors {
		ew.line("  " + p.red(location(f)) + " " + f.Message)
		if f.SuggestedFix != "" {
			ew.line("    " + p.dim("fix: "+f.SuggestedFix))
		}
	}
	ew.line("")

	// 3. Orphans vs. unreachable — DISTINCT sections, distinct remediation.
	section(ew, p, "Isolated orphans", len(v.Orphans))
	ew.line("  " + p.dim("no inbound or outbound navigational links — link them in from a relevant page, or delete them"))
	if len(v.Orphans) == 0 {
		ew.line("  " + p.green("none"))
	}
	for _, id := range v.Orphans {
		ew.line("  " + p.yellow(id.String()))
	}
	ew.line("")

	if v.ReachabilityIndeterminate {
		section(ew, p, "Unreachable", 0)
		ew.line("  " + p.dim("indeterminate: no root set found (no README.md/index.md, no type:index, no --root)"))
	} else {
		section(ew, p, "Unreachable", len(v.Unreachable))
		ew.line("  " + p.dim("not reachable from any root — add an inbound link from a page reachable from a root"))
		if len(v.Unreachable) == 0 {
			ew.line("  " + p.green("none"))
		}
		for _, id := range v.Unreachable {
			ew.line("  " + p.yellow(id.String()))
		}
	}
	ew.line("")

	// Under-linked + dead-end — the graduated structure tiers (ADR 0012).
	section(ew, p, "Under-linked", len(v.UnderLinked))
	ew.line("  " + p.dim("fewer inbound links than the discoverability threshold — link them in from related pages"))
	if len(v.UnderLinked) == 0 {
		ew.line("  " + p.green("none"))
	}
	for _, id := range v.UnderLinked {
		ew.line("  " + p.yellow(id.String()))
	}
	ew.line("")

	section(ew, p, "Dead-ends", len(v.DeadEnd))
	ew.line("  " + p.dim("inbound links but nothing onward — add outbound links to related documents"))
	if len(v.DeadEnd) == 0 {
		ew.line("  " + p.green("none"))
	}
	for _, id := range v.DeadEnd {
		ew.line("  " + p.yellow(id.String()))
	}
	ew.line("")

	// Bow-tie structure summary (macro-shape relative to the giant core).
	ew.line(p.dim(bowtieLine(v)))
	ew.line("")

	// Navigability scalars (ADR 0014): how navigable the corpus is overall.
	ew.line(p.bold("Navigability"))
	for _, l := range navigabilityLines(v) {
		ew.line("  " + p.dim(l))
	}
	ew.line("")

	// Load-bearing docs (ADR 0015): top documents by betweenness centrality —
	// the connectors most navigation flows through.
	ew.line(p.bold("Load-bearing docs") + p.dim(" (on the most shortest paths between other docs)"))
	if len(v.TopBetweenness) == 0 {
		ew.line("  " + p.dim("none"))
	}
	for _, r := range v.TopBetweenness {
		ew.line(fmt.Sprintf("  %s  %s", p.cyan(fmt.Sprintf("%.3f", r.Score)), v.TitleOf(r.ID)))
	}
	ew.line("")

	// Critical structure (ADR 0015): articulation points + bridges (single points
	// of failure in the link graph).
	ew.line(p.bold("Critical structure") + p.dim(" (single points of failure)"))
	for _, l := range criticalStructureLines(v) {
		ew.line("  " + p.dim(l))
	}
	ew.line("")

	// 4. Top hubs / authorities.
	ew.line(p.bold("Top hubs") + p.dim(" (link out to many docs)"))
	if len(v.TopHubs) == 0 {
		ew.line("  " + p.dim("none"))
	}
	for _, r := range v.TopHubs {
		ew.line(fmt.Sprintf("  %s  %s", p.cyan(fmt.Sprintf("%.3f", r.Score)), v.TitleOf(r.ID)))
	}
	ew.line("")
	ew.line(p.bold("Top authorities") + p.dim(" (linked to by many docs)"))
	if len(v.TopAuthorities) == 0 {
		ew.line("  " + p.dim("none"))
	}
	for _, r := range v.TopAuthorities {
		ew.line(fmt.Sprintf("  %s  %s", p.cyan(fmt.Sprintf("%.3f", r.Score)), v.TitleOf(r.ID)))
	}
	ew.line("")

	// Importance (PageRank): global importance via the random-surfer stationary
	// distribution (Brin & Page 1998), beside hubs/authorities (ADR 0016).
	ew.line(p.bold("Importance (PageRank)") + p.dim(" (global random-surfer importance)"))
	if len(v.TopPageRank) == 0 {
		ew.line("  " + p.dim("none"))
	}
	for _, r := range v.TopPageRank {
		ew.line(fmt.Sprintf("  %s  %s", p.cyan(fmt.Sprintf("%.3f", r.Score)), v.TitleOf(r.ID)))
	}
	ew.line("")

	// 5. Knowledge gaps (closing note).
	switch {
	case v.GapsTruncated:
		ew.line(p.dim(fmt.Sprintf("Knowledge gaps: %d+ disconnected cluster pair(s) (list truncated)", len(v.Gaps))))
	case len(v.Gaps) > 0:
		ew.line(p.dim(fmt.Sprintf("Knowledge gaps: %d disconnected cluster pair(s) that may warrant a bridge (experimental)", len(v.Gaps))))
	default:
		ew.line(p.dim("Knowledge gaps: none"))
	}

	// 6. Suggested links (closing note, ADR 0013).
	switch {
	case v.SuggestedLinksTruncated:
		ew.line(p.dim(fmt.Sprintf("Suggested links: %d+ topology-based suggestion(s) (list truncated)", len(v.SuggestedLinks))))
	case len(v.SuggestedLinks) > 0:
		ew.line(p.dim(fmt.Sprintf("Suggested links: %d topology-based suggestion(s) (experimental)", len(v.SuggestedLinks))))
	default:
		ew.line(p.dim("Suggested links: none"))
	}

	return ew.err
}

// section writes a bold header with a count, coloring a non-zero count red.
func section(ew *errWriter, p palette, title string, n int) {
	head := fmt.Sprintf("%s (%d)", title, n)
	if n > 0 {
		ew.line(p.bold(p.red(head)))
	} else {
		ew.line(p.bold(head))
	}
}

func location(f analysis.Finding) string {
	if f.Location.Line > 0 {
		return fmt.Sprintf("%s:%d", f.Location.Document, f.Location.Line)
	}
	return f.Location.Document.String()
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// errWriter records the first write error so a sequence of Fprint calls can be
// written without per-line error handling, checked once at the end.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) line(s string) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintln(e.w, s)
}
