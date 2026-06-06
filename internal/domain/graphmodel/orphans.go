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

// DefaultInboundThreshold is the under-linked discoverability floor: a document
// with fewer than this many inbound navigational links (but at least one
// outbound link, so it is not a dead-end) is reported as under-linked. The
// default of 3 follows Wikipedia's "discoverable" heuristic. A configured
// threshold of <=0 is normalized up to this value (Analyze does the floor).
const DefaultInboundThreshold = 3

// OrphanReport classifies documents into a single-bucket structure ladder
// (isolated orphan, dead-end, under-linked) plus the orthogonal unreachable set,
// with intentional orphans and roots suppressed (ADR 0007, ADR 0012).
type OrphanReport struct {
	// Isolated documents have in-degree 0 AND out-degree 0 in the projection
	// (the most-severe orphan tier).
	Isolated []identity.DocumentID
	// DeadEnd documents have inbound links but link to nothing onward (in>0 &&
	// out==0). Mutually exclusive with Isolated and UnderLinked (single bucket).
	DeadEnd []identity.DocumentID
	// UnderLinked documents have outbound links but fewer inbound links than the
	// discoverability threshold (out>0 && 0<=in<threshold, excluding the in==0
	// dead-of-isolated case which is Isolated). Mutually exclusive with the other
	// tiers.
	UnderLinked []identity.DocumentID
	// Unreachable documents are not reached from the root set (excluding those
	// already reported as Isolated, to avoid double-reporting the same doc).
	// Orthogonal to Dead-end/Under-linked: only a fully-isolated Orphan suppresses
	// it (ADR 0012).
	Unreachable []identity.DocumentID
	// Indeterminate mirrors Reachability.Indeterminate: when true, Unreachable is
	// empty (reachability was not computed) but the structure tiers are still
	// populated.
	Indeterminate bool
}

// OrphanOptions tunes the structure-ladder classification.
type OrphanOptions struct {
	// InboundThreshold is the under-linked discoverability floor: a non-exempt
	// document with outbound links but fewer than this many inbound links is
	// under-linked. Callers should pass a normalized (>=1) value; DetectOrphans
	// floors a <=0 value up to DefaultInboundThreshold defensively.
	InboundThreshold int
}

// DetectOrphans computes the structure-ladder + unreachable classification
// (ADR 0007, ADR 0012). Each non-exempt document falls into AT MOST ONE
// structure bucket, in priority order:
//
//  1. in==0 && out==0          → Isolated (fully-isolated orphan, most severe).
//  2. else out==0 (in>0)       → DeadEnd.
//  3. else in<threshold (out>0) → UnderLinked.
//
// Unreachable is computed independently (only when reachability is determinate)
// and is suppressed ONLY by a fully-isolated Orphan — dead-end/under-linked do
// NOT suppress it. Two kinds of node are exempt from ALL structure tiers:
// intentional orphans (front-matter `matlatl: orphan-intentional`) and root-set
// members (configured OR convention) — a declared entry point is its purpose,
// not a defect (ADR 0007). Results are sorted (g.documents is sorted and we
// append in that order).
func (g *ReferenceGraph) DetectOrphans(c *corpus.Corpus, rootSet RootSet, deg DegreeIndex, reach Reachability, opts OrphanOptions) OrphanReport {
	threshold := opts.InboundThreshold
	if threshold <= 0 {
		threshold = DefaultInboundThreshold
	}

	intentional := identity.IDSet(IntentionalOrphans(c))

	// One exemption set for the structure ladder: intentional orphans + roots.
	// A root with out-degree > 0 is already non-isolated, so in practice the root
	// exemption only matters for edgeless roots; but it also (intentionally)
	// suppresses under-linked/dead-end for any declared root.
	exempt := identity.IDSet(rootSet.Roots)
	for id := range intentional {
		exempt[id] = struct{}{}
	}

	var isolated, deadEnd, underLinked []identity.DocumentID
	isolatedSet := make(map[identity.DocumentID]struct{})
	for _, id := range g.documents {
		if _, skip := exempt[id]; skip {
			continue
		}
		in, out := deg[id].In, deg[id].Out
		switch {
		case in == 0 && out == 0:
			isolated = append(isolated, id)
			isolatedSet[id] = struct{}{}
		case out == 0: // in>0
			deadEnd = append(deadEnd, id)
		case in < threshold: // out>0
			underLinked = append(underLinked, id)
		}
	}

	report := OrphanReport{
		Isolated:      isolated,
		DeadEnd:       deadEnd,
		UnderLinked:   underLinked,
		Indeterminate: reach.Indeterminate,
	}
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
