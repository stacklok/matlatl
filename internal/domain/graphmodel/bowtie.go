package graphmodel

import (
	"slices"

	"github.com/stacklok/matlatl/internal/domain/identity"
)

// BowtieBucket classifies each document relative to the corpus's giant strongly
// connected component (the "core") in the bow-tie structure model (ADR 0012). It
// is pure classification DATA — it produces no per-document finding; it is
// surfaced in graph.json, the report summary, and over MCP so an agent can read
// the macro-shape of the corpus.
type BowtieBucket int

const (
	// BucketDisconnected is the zero value: the document is in a weak component
	// that does NOT contain the giant SCC.
	BucketDisconnected BowtieBucket = iota
	// BucketCore documents are members of the giant SCC.
	BucketCore
	// BucketIn documents can reach the core but are not reachable from it.
	BucketIn
	// BucketOut documents are reachable from the core but cannot reach it.
	BucketOut
	// BucketTendril documents are in the same weak component as the core but
	// neither reach it nor are reached from it.
	BucketTendril
)

// String returns the canonical bucket name used in artifacts.
func (b BowtieBucket) String() string {
	switch b {
	case BucketCore:
		return "core"
	case BucketIn:
		return "in"
	case BucketOut:
		return "out"
	case BucketTendril:
		return "tendril"
	case BucketDisconnected:
		return "disconnected"
	default:
		return "disconnected"
	}
}

// BowtieReport is the bow-tie classification of every document relative to the
// giant SCC. It is deterministic: GiantSCC is chosen by most-members, tie-broken
// by the smallest sorted-min ID, and every traversal iterates sorted slices.
type BowtieReport struct {
	// Bucket maps each document to its bow-tie bucket.
	Bucket map[identity.DocumentID]BowtieBucket
	// GiantSCC is the ID (sorted-min member) of the chosen giant SCC, or "" when
	// the corpus is empty.
	GiantSCC identity.DocumentID
	// GiantSCCSize is the member count of the giant SCC. A size of 1 means the
	// corpus has no cyclic core (every SCC is a singleton); the human report
	// labels this "no cyclic core" but the buckets are still populated
	// deterministically.
	GiantSCCSize int
	// Counts tallies the documents per bucket.
	Counts map[BowtieBucket]int
}

// BucketOf returns the bow-tie bucket of id (BucketDisconnected when unknown or
// when no report was computed).
func (r BowtieReport) BucketOf(id identity.DocumentID) BowtieBucket {
	if r.Bucket == nil {
		return BucketDisconnected
	}
	return r.Bucket[id]
}

// ClassifyBowtie computes the bow-tie report relative to the giant SCC. It reuses
// the already-computed SCC and WCC component lists (Analyze passes them in) so it
// does no redundant traversal of the component decomposition; it only runs two
// forward/reverse BFS passes over the document projection from the core.
//
// Determinism: the giant SCC is picked by descending member count, tie-broken by
// the smallest component ID; BFS uses sorted projAdj / projRev neighbor lists and
// a sorted seed order, so the report is identical regardless of map order.
func (g *ReferenceGraph) ClassifyBowtie(scc, wcc Components) BowtieReport {
	report := BowtieReport{
		Bucket: make(map[identity.DocumentID]BowtieBucket, len(g.documents)),
		Counts: make(map[BowtieBucket]int),
	}
	if len(g.documents) == 0 || len(scc) == 0 {
		return report
	}

	giant := pickGiantSCC(scc)
	coreSet := identity.IDSet(giant.Members)
	report.GiantSCC = giant.ID
	report.GiantSCCSize = len(giant.Members)

	// Forward reachability from the core (over out-edges): the OUT side.
	fromCore := g.bfsFrom(giant.Members, g.projAdj)
	// Reverse reachability into the core (over in-edges): the IN side.
	toCore := g.bfsFrom(giant.Members, g.projRev)

	// WCC membership of the core, to distinguish TENDRIL from DISCONNECTED.
	coreWCC := identity.IDSet(wccMembersContaining(wcc, giant.ID))

	for _, id := range g.documents { // sorted
		var bucket BowtieBucket
		switch {
		case isMember(coreSet, id):
			bucket = BucketCore
		case isMember(toCore, id):
			// Reaches the core → IN. Testing IN before OUT is sound only because
			// `scc` is the SCC decomposition of the SAME projection this BFS walks:
			// a node in BOTH toCore and fromCore would lie on a cycle through the
			// core and would therefore already have been collapsed INTO the core SCC
			// (the BucketCore case above). So outside the core the toCore/fromCore
			// sets are disjoint and this precedence is unambiguous.
			bucket = BucketIn
		case isMember(fromCore, id):
			bucket = BucketOut
		case isMember(coreWCC, id):
			bucket = BucketTendril
		default:
			bucket = BucketDisconnected
		}
		report.Bucket[id] = bucket
		report.Counts[bucket]++
	}
	return report
}

// pickGiantSCC selects the giant SCC: the component with the most members,
// tie-broken by the smallest component ID (sorted-min member). scc is sorted by
// ID, so iterating it gives a deterministic tie-break (first/smallest ID wins on
// equal size).
func pickGiantSCC(scc Components) Component {
	best := scc[0]
	for _, c := range scc[1:] {
		if len(c.Members) > len(best.Members) {
			best = c
		}
		// Equal size: keep best (smaller ID, since scc is ID-sorted).
	}
	return best
}

// bfsFrom runs a BFS from every seed over the given adjacency (out-edges for
// forward reachability, in-edges for reverse), returning the set of REACHED
// documents including the seeds. Neighbor lists are pre-sorted so expansion is
// deterministic.
func (g *ReferenceGraph) bfsFrom(seeds []identity.DocumentID, adj map[identity.DocumentID][]identity.DocumentID) map[identity.DocumentID]struct{} {
	reached := make(map[identity.DocumentID]struct{}, len(seeds))
	queue := make([]identity.DocumentID, 0, len(seeds))
	sorted := slices.Clone(seeds)
	slices.Sort(sorted)
	for _, s := range sorted {
		if _, ok := reached[s]; !ok {
			reached[s] = struct{}{}
			queue = append(queue, s)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, nb := range adj[cur] { // sorted
			if _, ok := reached[nb]; !ok {
				reached[nb] = struct{}{}
				queue = append(queue, nb)
			}
		}
	}
	return reached
}

// wccMembersContaining returns the members of the weak component that contains
// id, or nil if none does.
func wccMembersContaining(wcc Components, id identity.DocumentID) []identity.DocumentID {
	for _, c := range wcc {
		if slices.Contains(c.Members, id) {
			return c.Members
		}
	}
	return nil
}

func isMember(set map[identity.DocumentID]struct{}, id identity.DocumentID) bool {
	_, ok := set[id]
	return ok
}
