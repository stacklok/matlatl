package graphmodel

import (
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// Betweenness holds per-document betweenness-centrality scores (ADR 0015). It is
// pure DATA, exactly like HitsScores: betweenness measures how often a document
// lies on shortest paths between OTHER documents — a high score marks a
// load-bearing connector whose removal lengthens or severs navigation. It
// produces no finding and never gates the check exit code; it is surfaced
// per-node and as a top-N block in graph.json, the human reports, and over MCP.
//
// Concurrency / freeze boundary: the score map is built once by
// ComputeBetweenness and frozen thereafter. It is unexported and reached only
// through the read accessors (Score, TopBetweenness), so a downstream consumer
// cannot mutate the shared map; after construction the value is safe for
// concurrent reads (the P6 fan-out boundary), mirroring HitsScores.
type Betweenness struct {
	score map[identity.DocumentID]float64
}

// Score returns the betweenness score for id (0 if unknown). Read-only.
func (b Betweenness) Score(id identity.DocumentID) float64 { return b.score[id] }

// TopBetweenness returns documents ranked by betweenness score descending, ties
// broken by DocumentID ascending (deterministic). n<=0 returns all. It reuses
// the hits.go rankDesc total order (direct float compare, no epsilon).
func (b Betweenness) TopBetweenness(n int) []RankedDocument {
	return rankDesc(b.score, n)
}

// ComputeBetweenness computes directed betweenness centrality over the document
// projection (projAdj) with Brandes' algorithm (ADR 0015). Scores are normalized
// by (n-1)(n-2) — the number of ordered (s,t) pairs excluding a given vertex v —
// so they land in [0,1]; there is NO halving (the graph is directed). A corpus
// with n<3 documents has no vertex that can lie strictly between two others, so
// every score is 0.
//
// Determinism: the per-source forward pass runs in sorted source order with
// sorted neighbour expansion (ForEachSourceBFS), the predecessor lists it yields
// are sorted, and the dependency back-accumulation walks the BFS order in
// reverse — so every float division and sum runs in a fixed order and the result
// is byte-stable regardless of map iteration order (ADR 0007). Cost: O(V·(V+E))
// time, O(V+E) transient memory (no V² matrix), matching the streaming SSSP
// shape of ForEachSourceDistances.
func (g *ReferenceGraph) ComputeBetweenness() Betweenness {
	n := len(g.documents)
	cb := make(map[identity.DocumentID]float64, n)
	for _, id := range g.documents {
		cb[id] = 0
	}
	if n < 3 {
		return Betweenness{score: cb}
	}

	// delta is the per-source dependency accumulator δ, reused (cleared) per
	// source so we hold no V² state.
	delta := make(map[identity.DocumentID]float64, n)
	g.ForEachSourceBFS(g.projAdj, func(
		s identity.DocumentID,
		order []identity.DocumentID,
		preds map[identity.DocumentID][]identity.DocumentID,
		sigma map[identity.DocumentID]float64,
	) {
		clear(delta)
		// Walk the BFS order in REVERSE (Brandes' stack S), accumulating each
		// node's dependency into its sorted predecessors.
		for i := len(order) - 1; i >= 0; i-- {
			w := order[i]
			for _, v := range preds[w] { // sorted ⇒ deterministic float-add order
				delta[v] += (sigma[v] / sigma[w]) * (1 + delta[w])
			}
			if w != s {
				cb[w] += delta[w]
			}
		}
	})

	// Normalize by the ordered-pair count (n-1)(n-2); n>=3 guarantees >0. No
	// halving: betweenness is over the DIRECTED projection.
	denom := float64((n - 1) * (n - 2))
	for _, id := range g.documents { // sorted (irrelevant to the result, but tidy)
		cb[id] /= denom
	}
	return Betweenness{score: cb}
}
