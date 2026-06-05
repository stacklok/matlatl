package graphmodel

import (
	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// Degree holds the in/out navigational degree of a document in the projection.
type Degree struct {
	In  int
	Out int
}

// DegreeIndex maps each document to its projection in/out degree.
//
// Freeze boundary: a DegreeIndex is built once by BuildDegreeIndex and is
// treated as immutable thereafter (it is embedded by value in the shared
// *GraphMetrics). Read it through the Degree accessor rather than mutating the
// map; after construction it is safe for concurrent reads (the P6 fan-out
// boundary).
type DegreeIndex map[identity.DocumentID]Degree

// Degree returns the in/out projection degree of id (the zero Degree if id is
// unknown). Read-only accessor over the frozen index.
func (d DegreeIndex) Degree(id identity.DocumentID) Degree { return d[id] }

// BuildDegreeIndex computes in/out degree for every document from the projection.
func (g *ReferenceGraph) BuildDegreeIndex() DegreeIndex {
	idx := make(DegreeIndex, len(g.documents))
	for _, id := range g.documents {
		idx[id] = Degree{
			Out: len(g.projAdj[id]),
			In:  len(g.projRev[id]),
		}
	}
	return idx
}

// Reachability is the BFS reachability result over the document projection.
type Reachability struct {
	// Indeterminate is true when the root set was empty (ADR 0007): reachability
	// was not computed and Reached/Unreachable are empty.
	Indeterminate bool
	// Reached is the sorted set of documents reachable from the root set
	// (includes the roots themselves).
	Reached []identity.DocumentID
	// Unreachable is the sorted set of in-corpus documents not reached.
	Unreachable []identity.DocumentID
}

// ComputeReachability runs BFS from the root set over projection out-edges. When
// the root set is indeterminate, it returns Indeterminate=true and computes
// nothing (callers must not mark everything unreachable, ADR 0005/0007).
// Iteration is sorted for determinism.
func (g *ReferenceGraph) ComputeReachability(rs RootSet) Reachability {
	if rs.Indeterminate {
		return Reachability{Indeterminate: true}
	}
	reached := make(map[identity.DocumentID]struct{})
	queue := make([]identity.DocumentID, 0, len(rs.Roots))
	for _, r := range rs.Roots {
		if !g.HasDocument(r) {
			continue
		}
		if _, seen := reached[r]; !seen {
			reached[r] = struct{}{}
			queue = append(queue, r)
		}
	}
	// BFS; neighbors are already sorted in projAdj, so expansion is deterministic.
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, nb := range g.projAdj[cur] {
			if _, seen := reached[nb]; !seen {
				reached[nb] = struct{}{}
				queue = append(queue, nb)
			}
		}
	}

	reachedList := make([]identity.DocumentID, 0, len(reached))
	var unreachable []identity.DocumentID
	for _, id := range g.documents {
		if _, ok := reached[id]; ok {
			reachedList = append(reachedList, id)
		} else {
			unreachable = append(unreachable, id)
		}
	}
	return Reachability{Reached: reachedList, Unreachable: unreachable}
}

// OrphanReport classifies documents into isolated orphans and unreachable docs,
// with intentional orphans suppressed (ADR 0007).
type OrphanReport struct {
	// Isolated documents have in-degree 0 AND out-degree 0 in the projection.
	Isolated []identity.DocumentID
	// Unreachable documents are not reached from the root set (excluding those
	// already reported as Isolated, to avoid double-reporting the same doc).
	Unreachable []identity.DocumentID
	// Indeterminate mirrors Reachability.Indeterminate: when true, Unreachable is
	// empty (reachability was not computed) but Isolated is still populated.
	Indeterminate bool
}

// DetectOrphans computes the orphan/unreachable classification. Isolated orphans
// are independent of the root set (degree-based) and always computed; unreachable
// is only computed when reachability is determinate. Intentional orphans are
// excluded from both lists. Results are sorted.
func (g *ReferenceGraph) DetectOrphans(c *corpus.Corpus, deg DegreeIndex, reach Reachability) OrphanReport {
	intentional := identity.IDSet(IntentionalOrphans(c))

	var isolated []identity.DocumentID
	isolatedSet := make(map[identity.DocumentID]struct{})
	for _, id := range g.documents {
		if _, skip := intentional[id]; skip {
			continue
		}
		if deg[id].In == 0 && deg[id].Out == 0 {
			isolated = append(isolated, id)
			isolatedSet[id] = struct{}{}
		}
	}

	report := OrphanReport{Isolated: isolated, Indeterminate: reach.Indeterminate}
	if reach.Indeterminate {
		return report
	}
	for _, id := range reach.Unreachable {
		if _, skip := intentional[id]; skip {
			continue
		}
		if _, iso := isolatedSet[id]; iso {
			continue // already reported as the more specific Isolated orphan
		}
		report.Unreachable = append(report.Unreachable, id)
	}
	// reach.Unreachable is built by iterating g.documents (sorted) and appending
	// in that order, so report.Unreachable is already in sorted document order;
	// no re-sort needed.
	return report
}
