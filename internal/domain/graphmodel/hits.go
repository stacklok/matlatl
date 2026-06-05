package graphmodel

import (
	"math"
	"slices"

	"github.com/stacklok/doctopus/internal/domain/identity"
)

// HitsScores holds per-document hub and authority scores plus the iteration
// metadata, for determinism auditing.
type HitsScores struct {
	Hub        map[identity.DocumentID]float64
	Authority  map[identity.DocumentID]float64
	Iterations int
	Converged  bool
}

// HitsOptions tunes the HITS power iteration.
type HitsOptions struct {
	MaxIterations int     // default 100
	Epsilon       float64 // L2-delta convergence threshold; default 1e-8
}

// ComputeHITS runs the HITS hub/authority algorithm over the directed document
// projection. Iteration order is sorted (documents and neighbors) and scores are
// L2-normalized each round, so output is deterministic across runs and input
// orderings (ADR 0007). Authority(p) = sum of Hub(q) over q→p; Hub(p) = sum of
// Authority(q) over p→q.
func (g *ReferenceGraph) ComputeHITS(opts HitsOptions) HitsScores {
	maxIter := opts.MaxIterations
	if maxIter <= 0 {
		maxIter = 100
	}
	eps := opts.Epsilon
	if eps <= 0 {
		eps = 1e-8
	}

	docs := g.documents // sorted
	hub := make(map[identity.DocumentID]float64, len(docs))
	auth := make(map[identity.DocumentID]float64, len(docs))
	for _, id := range docs {
		hub[id] = 1
		auth[id] = 1
	}
	if len(docs) == 0 {
		return HitsScores{Hub: hub, Authority: auth, Converged: true}
	}

	var iter int
	converged := false
	for iter = 0; iter < maxIter; iter++ {
		newAuth := make(map[identity.DocumentID]float64, len(docs))
		newHub := make(map[identity.DocumentID]float64, len(docs))

		// Authority(p) = sum over q with q->p of hub[q]. Use in-edges.
		for _, p := range docs {
			var sum float64
			for _, q := range g.projRev[p] {
				sum += hub[q]
			}
			newAuth[p] = sum
		}
		// Hub(p) = sum over q with p->q of authority[q] (use the just-updated auth).
		for _, p := range docs {
			var sum float64
			for _, q := range g.projAdj[p] {
				sum += newAuth[q]
			}
			newHub[p] = sum
		}

		normalizeL2(newAuth, docs)
		normalizeL2(newHub, docs)

		delta := l2Delta(auth, newAuth, docs) + l2Delta(hub, newHub, docs)
		auth, hub = newAuth, newHub
		if delta < eps {
			converged = true
			iter++ // count this completed iteration
			break
		}
	}

	return HitsScores{Hub: hub, Authority: auth, Iterations: iter, Converged: converged}
}

// normalizeL2 scales the scores so their L2 norm is 1, iterating in sorted order.
// A zero vector is left as-is.
func normalizeL2(m map[identity.DocumentID]float64, order []identity.DocumentID) {
	var sumsq float64
	for _, id := range order {
		v := m[id]
		sumsq += v * v
	}
	if sumsq == 0 {
		return
	}
	norm := math.Sqrt(sumsq)
	for _, id := range order {
		m[id] /= norm
	}
}

// l2Delta returns the L2 distance between two score maps over a fixed order.
func l2Delta(a, b map[identity.DocumentID]float64, order []identity.DocumentID) float64 {
	var sumsq float64
	for _, id := range order {
		d := a[id] - b[id]
		sumsq += d * d
	}
	return math.Sqrt(sumsq)
}

// RankedDocument pairs a document with a score, for deterministic top-N output.
type RankedDocument struct {
	ID    identity.DocumentID
	Score float64
}

// TopAuthorities returns documents ranked by authority score descending, ties
// broken by DocumentID ascending (deterministic). n<=0 returns all.
func (h HitsScores) TopAuthorities(n int) []RankedDocument {
	return rankDesc(h.Authority, n)
}

// TopHubs returns documents ranked by hub score descending, ties broken by
// DocumentID ascending. n<=0 returns all.
func (h HitsScores) TopHubs(n int) []RankedDocument {
	return rankDesc(h.Hub, n)
}

func rankDesc(scores map[identity.DocumentID]float64, n int) []RankedDocument {
	out := make([]RankedDocument, 0, len(scores))
	for id, s := range scores {
		out = append(out, RankedDocument{ID: id, Score: s})
	}
	slices.SortFunc(out, func(a, b RankedDocument) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})
	if n > 0 && n < len(out) {
		out = out[:n]
	}
	return out
}
