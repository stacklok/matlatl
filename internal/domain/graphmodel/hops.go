package graphmodel

import (
	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// DefaultFarFromRootThreshold is the hop-distance floor for the far-from-root
// finding (ADR 0021): a document reachable from the root set but at or beyond
// this many hops from the NEAREST root is "reachable but effectively
// undiscoverable" by link traversal, so it is flagged (a non-gating Info
// finding). The default of 6 follows the research note's §4-D "reachable but
// far" idea: an agent or reader following links from an entry point is unlikely
// to reach a document that deep. A configured threshold of <=0 is normalized up
// to this value in ComputeHopsFromRoot, the single normalization point (Analyze
// passes the option through raw).
const DefaultFarFromRootThreshold = 6

// HopsResult is the per-document hops-from-root distance map plus the derived
// far-from-root outliers (ADR 0021). It is pure data computed by a single
// multi-source BFS; the application layer turns FarFromRoot into non-gating Info
// findings and the emit layer surfaces distances (via Distance) as per-node
// graph.json data.
//
// A document ABSENT from the distance map is UNREACHABLE from the root set (BFS
// never visited it); the emit layer renders that (and the whole-corpus
// indeterminate case) as hopsFromRoot: -1. When Indeterminate is true (empty
// root set) the distance map is empty and nothing is computed — mirroring
// Reachability (ADR 0007): callers must not treat every document as far.
//
// The distance map is UNEXPORTED and read only through Distance, following the
// frozen-carrier house pattern (HitsScores/PageRankScores/Betweenness): the map
// is not handed out, so no emitter can range it in unsorted order (ADR 0004).
type HopsResult struct {
	// dist maps each REACHED document to its shortest hop distance from the
	// nearest root (a root has distance 0). Absence = unreachable. Empty when
	// Indeterminate. Read it through Distance; never ranged for output.
	dist map[identity.DocumentID]int
	// Indeterminate mirrors RootSet.Indeterminate: when true the root set was
	// empty, so hops were not computed (dist and FarFromRoot are empty).
	Indeterminate bool
	// FarFromRoot is the sorted set of documents with a FINITE distance >=
	// Threshold that are not exempt (root-set members and intentional orphans are
	// excluded, per structureExemptSet). Unreachable documents are never here
	// (they are absent from the distance map, reported as unreachable instead).
	FarFromRoot []identity.DocumentID
	// Threshold is the actual (normalized) hop-distance floor used, carried so the
	// finding message/details and any downstream decision agree.
	Threshold int
}

// Distance returns the hop distance of id from the nearest root and whether it
// was reached. A false second result means id is unreachable (or the root set
// was indeterminate). Read-only accessor over the frozen result — the only way
// to read a distance, since the map is unexported.
func (h HopsResult) Distance(id identity.DocumentID) (int, bool) {
	d, ok := h.dist[id]
	return d, ok
}

// rootDistances runs the SINGLE root-seeded multi-source breadth-first search
// shared by ComputeReachability and ComputeHopsFromRoot, returning each reached
// document's shortest distance from the nearest root (a root has distance 0;
// absence = unreachable). Returns nil when the root set is indeterminate.
//
// All root-set members present in the graph are seeded at distance 0 into one
// queue (in sorted root order), then BFS expands over the sorted document
// projection, so the first time a document is dequeued its distance is the
// minimum over all roots (edges are unweighted, so BFS yields exact shortest
// paths). The explicit-head-index queue idiom (never queue = queue[1:], which
// reallocates the backing array; see apsp.go) keeps the pass O(V+E) time and
// O(V) memory.
//
// This is the ONE traversal both reachability and hops-from-root derive from:
// Reachability's reached set is exactly the key set of this map, so the two
// artifacts cannot disagree (a doc is never simultaneously unreachable and at a
// finite hop distance).
func (g *ReferenceGraph) rootDistances(rs RootSet) map[identity.DocumentID]int {
	if rs.Indeterminate {
		return nil
	}
	dist := make(map[identity.DocumentID]int, len(g.documents))
	// queue is a reused FIFO with an explicit head index; resetting head (rather
	// than reslicing) keeps one backing array for the whole BFS (see apsp.go).
	queue := make([]identity.DocumentID, 0, len(g.documents))
	for _, r := range rs.Roots { // sorted root order ⇒ deterministic seeding
		if !g.HasDocument(r) {
			continue
		}
		if _, seen := dist[r]; !seen {
			dist[r] = 0
			queue = append(queue, r)
		}
	}
	head := 0
	for head < len(queue) {
		cur := queue[head]
		head++
		d := dist[cur]
		for _, nb := range g.projAdj[cur] { // sorted neighbours ⇒ deterministic
			if _, seen := dist[nb]; !seen {
				dist[nb] = d + 1
				queue = append(queue, nb)
			}
		}
	}
	return dist
}

// ComputeHopsFromRoot computes the shortest directed distance from the root set
// to every reachable document (ADR 0021) via the shared rootDistances BFS, then
// derives the far-from-root outliers.
//
// When the root set is indeterminate it returns Indeterminate=true and computes
// nothing (callers must not mark everything far, ADR 0007/0021). This is the
// single normalization point for the threshold: a threshold <=0 is floored to
// DefaultFarFromRootThreshold here (Analyze passes the raw option through), and
// HopsResult.Threshold is what the findings read. The far-from-root outliers are
// the reached, non-exempt documents at or beyond the threshold; the exemption
// set (root-set members ∪ intentional orphans) is the SAME structureExemptSet
// DetectOrphans uses, so a declared entry point or an opt-out is never flagged.
func (g *ReferenceGraph) ComputeHopsFromRoot(c *corpus.Corpus, rs RootSet, threshold int) HopsResult {
	if threshold <= 0 {
		threshold = DefaultFarFromRootThreshold
	}
	if rs.Indeterminate {
		return HopsResult{Indeterminate: true, Threshold: threshold}
	}

	dist := g.rootDistances(rs)

	// Far-from-root outliers: reached (finite distance), at/beyond the threshold,
	// and not exempt. g.documents is sorted and we append in that order, so
	// FarFromRoot is sorted without a re-sort.
	exempt := structureExemptSet(c, rs)
	var far []identity.DocumentID
	for _, id := range g.documents { // sorted
		d, ok := dist[id]
		if !ok || d < threshold {
			continue
		}
		if _, skip := exempt[id]; skip {
			continue
		}
		far = append(far, id)
	}

	return HopsResult{dist: dist, FarFromRoot: far, Threshold: threshold}
}
