package application

import (
	"fmt"

	"github.com/stacklok/matlatl/internal/domain/analysis"
	"github.com/stacklok/matlatl/internal/domain/graphmodel"
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// findingsFromMetrics turns the P3 graph analysis into Findings: isolated
// orphans (Orphan), unreachable documents (Unreachable), and knowledge-gap
// bridge candidates (KnowledgeGap). Per ADR 0005 orphans/unreachable are
// Warning (fail only under --strict) and gaps are Info (never fail). Output is
// derived from already-sorted metric slices, so it is deterministic.
func findingsFromMetrics(m *graphmodel.GraphMetrics) []analysis.Finding {
	if m == nil {
		return nil
	}
	var out []analysis.Finding

	for _, id := range m.Orphans.Isolated {
		out = append(out, orphanFinding(id))
	}
	for _, id := range m.Orphans.Unreachable {
		out = append(out, unreachableFinding(id))
	}
	for _, gap := range m.Gaps {
		out = append(out, gapFinding(gap))
	}
	return out
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
