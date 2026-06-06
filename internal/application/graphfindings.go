package application

import (
	"fmt"
	"strconv"

	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// findingsFromMetrics turns the P3 graph analysis into Findings: isolated
// orphans (Orphan), unreachable documents (Unreachable), under-linked and
// dead-end documents (the graduated structure tiers, ADR 0012), and
// knowledge-gap bridge candidates (KnowledgeGap). Per ADR 0005 orphans/
// unreachable are Warning (fail only under --strict) and gaps are Info (never
// fail). Under-linked/dead-end default to Info but are promoted to Warning when
// structureSev is StructureFindingsWarning (ADR 0012). threshold is the actual
// (normalized) inbound discoverability threshold, carried into the under-linked
// message + details. Output is derived from already-sorted metric slices, so it
// is deterministic.
func findingsFromMetrics(m *graphmodel.GraphMetrics, threshold int, structureSev StructureFindingsSeverity) []analysis.Finding {
	if m == nil {
		return nil
	}
	sev := analysis.Info
	if structureSev == StructureFindingsWarning {
		sev = analysis.Warning
	}
	var out []analysis.Finding

	for _, id := range m.Orphans.Isolated {
		out = append(out, orphanFinding(id))
	}
	for _, id := range m.Orphans.Unreachable {
		out = append(out, unreachableFinding(id))
	}
	for _, id := range m.Orphans.UnderLinked {
		out = append(out, underLinkedFinding(id, m.Degrees.Degree(id).In, threshold, sev))
	}
	for _, id := range m.Orphans.DeadEnd {
		out = append(out, deadEndFinding(id, sev))
	}
	for _, gap := range m.Gaps {
		out = append(out, gapFinding(gap))
	}
	for _, s := range m.SuggestedLinks {
		out = append(out, suggestedLinkFinding(s))
	}
	// Critical-path structure (ADR 0015): articulation points (cut vertices) and
	// bridges (cut edges). Both are Info and NEVER gate the exit code; they are
	// surfaced so a maintainer/agent can shore up single points of failure.
	for _, id := range m.Critical.ArticulationPoints {
		out = append(out, articulationFinding(id, m.Betweenness.Score(id)))
	}
	for _, b := range m.Critical.Bridges {
		out = append(out, bridgeFinding(b))
	}
	return out
}

// articulationFinding turns a cut vertex (ADR 0015) into an Info finding: the
// document is the only connector between two parts of the corpus, so removing or
// unlinking it fragments the graph. Always Info; NEVER gates the exit code. Its
// betweenness score is carried in Details (the load-bearing evidence), formatted
// at fixed precision so the finding text is byte-stable.
func articulationFinding(id identity.DocumentID, betweenness float64) analysis.Finding {
	return analysis.Finding{
		ID:       fmt.Sprintf("%s:%s", analysis.ArticulationPoint, id),
		Kind:     analysis.ArticulationPoint,
		Severity: analysis.Info,
		Location: analysis.Location{Document: id},
		Message: fmt.Sprintf(
			"%q is an articulation point: it is the only connector between two parts of the doc graph (experimental)",
			id),
		SuggestedFix: fmt.Sprintf(
			"%q is the only connector between two parts of the doc graph; if it's removed or unlinked the corpus "+
				"fragments. Add a redundant link path, or treat it as load-bearing.", id),
		Details: map[string]string{
			DetailTargetDocument: id.String(),
			DetailBetweenness:    strconv.FormatFloat(betweenness, 'f', 6, 64),
		},
	}
}

// bridgeFinding turns a cut edge (ADR 0015) into an Info finding: the link
// between two documents is the only connection between two parts of the corpus.
// It is anchored at the canonical-min endpoint (b.A); the other endpoint (b.B)
// is carried in Details. Always Info; NEVER gates the exit code.
func bridgeFinding(b graphmodel.Bridge) analysis.Finding {
	return analysis.Finding{
		ID:       fmt.Sprintf("%s:%s:%s", analysis.Bridge, b.A, b.B),
		Kind:     analysis.Bridge,
		Severity: analysis.Info,
		Location: analysis.Location{Document: b.A},
		Message: fmt.Sprintf(
			"the link between %q and %q is a bridge: the only connection between two parts of the doc graph (experimental)",
			b.A, b.B),
		SuggestedFix: fmt.Sprintf(
			"the link between %q and %q is the only connection between two parts of the doc graph; add another "+
				"path between these clusters so it isn't a single point of failure.", b.A, b.B),
		Details: map[string]string{
			DetailTargetDocument: b.A.String(),
			DetailBridgeEndpoint: b.B.String(),
		},
	}
}

// suggestedLinkFinding turns a topology-based link suggestion (ADR 0013) into an
// Info finding. It is anchored at DocA (Location.Document) and carries the pair,
// the shared-neighbour count, and the coupling/co-citation/Adamic-Adar scores in
// Details so an agent can act without re-deriving them. Always Info: it NEVER
// gates the exit code. The Adamic/Adar float is formatted at fixed precision so
// the finding text is byte-stable.
func suggestedLinkFinding(s graphmodel.LinkSuggestion) analysis.Finding {
	return analysis.Finding{
		ID:       fmt.Sprintf("%s:%s:%s", analysis.SuggestedLink, s.DocA, s.DocB),
		Kind:     analysis.SuggestedLink,
		Severity: analysis.Info,
		Location: analysis.Location{Document: s.DocA},
		Message: fmt.Sprintf(
			"%q and %q share %d connection(s) but do not link to each other "+
				"(topology suggests a relationship; experimental)",
			s.DocA, s.DocB, s.SharedNeighbours),
		SuggestedFix: fmt.Sprintf(
			"If these documents are related, add a navigational link between %q and %q. "+
				"They share %d neighbour(s) (bibliographic coupling %d, co-citation %d).",
			s.DocA, s.DocB, s.SharedNeighbours, s.Coupling, s.CoCitation),
		Details: map[string]string{
			DetailTargetDocument:   s.DocA.String(),
			DetailSuggestedTarget:  s.DocB.String(),
			DetailSharedNeighbours: fmt.Sprintf("%d", s.SharedNeighbours),
			DetailCoupling:         fmt.Sprintf("%d", s.Coupling),
			DetailCoCitation:       fmt.Sprintf("%d", s.CoCitation),
			DetailAdamicAdar:       strconv.FormatFloat(s.AdamicAdar, 'f', 6, 64),
		},
	}
}

func underLinkedFinding(id identity.DocumentID, inDeg, threshold int, sev analysis.Severity) analysis.Finding {
	return analysis.Finding{
		ID:       fmt.Sprintf("%s:%s", analysis.UnderLinked, id),
		Kind:     analysis.UnderLinked,
		Severity: sev,
		Location: analysis.Location{Document: id},
		Message: fmt.Sprintf(
			"%q has only %d inbound link(s) (below the discoverability threshold of %d); it is under-linked",
			id, inDeg, threshold),
		SuggestedFix: fmt.Sprintf(
			"Add inbound links to %q from related pages so readers and agents can discover it; aim for at least %d. "+
				"To keep it intentionally sparse, add front matter `matlatl: orphan-intentional`.", id, threshold),
		Details: map[string]string{
			DetailTargetDocument: id.String(),
			DetailInboundCount:   fmt.Sprintf("%d", inDeg),
		},
	}
}

func deadEndFinding(id identity.DocumentID, sev analysis.Severity) analysis.Finding {
	return analysis.Finding{
		ID:       fmt.Sprintf("%s:%s", analysis.DeadEnd, id),
		Kind:     analysis.DeadEnd,
		Severity: sev,
		Location: analysis.Location{Document: id},
		Message:  fmt.Sprintf("%q is a dead-end: it has inbound links but links to nothing onward", id),
		SuggestedFix: fmt.Sprintf(
			"Add onward internal links from %q to related documents. "+
				"To keep it intentionally terminal, add front matter `matlatl: orphan-intentional`.", id),
		Details: map[string]string{
			DetailTargetDocument: id.String(),
		},
	}
}

func orphanFinding(id identity.DocumentID) analysis.Finding {
	return analysis.Finding{
		ID:       fmt.Sprintf("%s:%s", analysis.Orphan, id),
		Kind:     analysis.Orphan,
		Severity: analysis.Warning,
		Location: analysis.Location{Document: id},
		Message:  fmt.Sprintf("%q is an isolated orphan: no document links to it and it links to nothing", id),
		SuggestedFix: fmt.Sprintf(
			"Link %q in from a relevant page (e.g. an index or a related doc), or delete it if obsolete. "+
				"To keep it intentionally unlinked, add front matter `matlatl: orphan-intentional`.", id),
		Details: map[string]string{
			DetailTargetDocument: id.String(),
		},
	}
}

func unreachableFinding(id identity.DocumentID) analysis.Finding {
	return analysis.Finding{
		ID:       fmt.Sprintf("%s:%s", analysis.Unreachable, id),
		Kind:     analysis.Unreachable,
		Severity: analysis.Warning,
		Location: analysis.Location{Document: id},
		Message:  fmt.Sprintf("%q is unreachable from the root set (nothing reachable links to it)", id),
		SuggestedFix: fmt.Sprintf(
			"Add an inbound link to %q from a page that is itself reachable from a root (README.md/index.md). "+
				"To keep it intentionally unlinked, add front matter `matlatl: orphan-intentional`.", id),
		Details: map[string]string{
			DetailTargetDocument: id.String(),
		},
	}
}

func gapFinding(gap graphmodel.Gap) analysis.Finding {
	return analysis.Finding{
		ID:       fmt.Sprintf("%s:%s:%s", analysis.KnowledgeGap, gap.ComponentA, gap.ComponentB),
		Kind:     analysis.KnowledgeGap,
		Severity: analysis.Info,
		Location: analysis.Location{Document: gap.RepresentativeA},
		Message: fmt.Sprintf(
			"clusters %q and %q have no navigational links between them (experimental knowledge-gap signal)",
			gap.ComponentA, gap.ComponentB),
		SuggestedFix: fmt.Sprintf(
			"If these areas are related, consider linking %q and %q to connect the two clusters.",
			gap.RepresentativeA, gap.RepresentativeB),
		Details: map[string]string{
			DetailComponentA:      gap.ComponentA.String(),
			DetailComponentB:      gap.ComponentB.String(),
			DetailRepresentativeA: gap.RepresentativeA.String(),
			DetailRepresentativeB: gap.RepresentativeB.String(),
		},
	}
}
