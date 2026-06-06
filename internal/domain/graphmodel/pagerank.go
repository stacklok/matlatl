package graphmodel

import (
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// PageRankScores holds per-document PageRank scores plus iteration metadata, for
// determinism auditing. PageRank is the stationary distribution of a random
// surfer over the directed document projection (Brin & Page 1998): the
// probability mass that accumulates at a document is its global importance. It is
// pure DATA, exactly like HitsScores and Betweenness — it produces no finding and
// never gates the check exit code; it is surfaced per-node and as a top-N block
// in graph.json and the human reports, and it drives the reading-order trails
// (ADR 0016).
//
// Unlike HITS (which sums RAW neighbour scores and L2-normalizes each round),
// PageRank divides each contributing neighbour's score by that neighbour's
// out-degree and conserves total mass (Σ PR = 1), so there is NO normalization
// step: dangling nodes (no out-links) have their mass redistributed uniformly
// (Langville & Meyer 2006; the NetworkX dangling convention).
//
// Concurrency / freeze boundary: the score map is built once by ComputePageRank
// and frozen thereafter. It is unexported and reached only through the read
// accessors (Score, Top), so a downstream consumer cannot mutate the shared map;
// after construction the value is safe for concurrent reads (the P6 fan-out
// boundary), mirroring HitsScores and Betweenness.
type PageRankScores struct {
	score      map[identity.DocumentID]float64
	Iterations int
	Converged  bool
}

// Score returns the PageRank score for id (0 if unknown). Read-only.
func (p PageRankScores) Score(id identity.DocumentID) float64 { return p.score[id] }

// Top returns documents ranked by PageRank score descending, ties broken by
// DocumentID ascending (deterministic). n<=0 returns all. It reuses the hits.go
// rankDesc total order (direct float compare, no epsilon).
func (p PageRankScores) Top(n int) []RankedDocument {
	return rankDesc(p.score, n)
}

// DefaultPageRankDamping is the standard damping factor d (Brin & Page 1998):
// the probability the random surfer follows a link rather than teleporting.
const DefaultPageRankDamping = 0.85

// DefaultPageRankEpsilon is the per-document L1 convergence threshold: iteration
// stops when the total absolute change Σ|newPR-pr| drops below N*epsilon.
const DefaultPageRankEpsilon = 1e-6

// DefaultPageRankMaxIterations bounds the power iteration so a pathological graph
// cannot spin forever (matching HitsOptions' cap).
const DefaultPageRankMaxIterations = 100

// PageRankOptions tunes the PageRank power iteration. Zero values are normalized
// to the documented defaults (Damping 0.85, Epsilon 1e-6, MaxIterations 100).
type PageRankOptions struct {
	// Damping is the teleport-vs-follow factor d. <=0 (or >=1) is normalized to
	// DefaultPageRankDamping.
	Damping float64
	// Epsilon is the L1 convergence threshold per document. <=0 normalized to
	// DefaultPageRankEpsilon.
	Epsilon float64
	// MaxIterations bounds the power iteration. <=0 normalized to
	// DefaultPageRankMaxIterations.
	MaxIterations int
}

// ComputePageRank runs the PageRank power iteration over the directed document
// projection (ADR 0016). Per node v:
//
//	newPR[v] = (1-d)/N + d*( Σ_{u → v} pr[u]/outdeg(u) + danglingSum/N )
//
// where d is the damping factor, N the document count, and danglingSum the total
// score of dangling nodes (no out-links), redistributed uniformly so total mass
// is conserved (Σ PR = 1). There is NO L2 normalization (unlike HITS).
//
// Determinism (CLAUDE.md): every float SUM runs in a fixed order — the
// per-node neighbour contribution iterates g.projRev[v] (already sorted), and
// danglingSum accumulates over g.documents (sorted) — so the float addition order
// is byte-stable regardless of map iteration order. Convergence is the L1 delta
// Σ_{v}|newPR[v]-pr[v]| < N*epsilon; on convergence the completed iteration is
// counted (matching hits.go). An empty graph returns early (Converged); a
// single-document corpus scores 1.0.
func (g *ReferenceGraph) ComputePageRank(opts PageRankOptions) PageRankScores {
	d := opts.Damping
	if d <= 0 || d >= 1 {
		d = DefaultPageRankDamping
	}
	eps := opts.Epsilon
	if eps <= 0 {
		eps = DefaultPageRankEpsilon
	}
	maxIter := opts.MaxIterations
	if maxIter <= 0 {
		maxIter = DefaultPageRankMaxIterations
	}

	docs := g.documents // sorted
	n := len(docs)
	pr := make(map[identity.DocumentID]float64, n)
	if n == 0 {
		return PageRankScores{score: pr, Converged: true}
	}
	nf := float64(n)
	init := 1 / nf
	for _, id := range docs {
		pr[id] = init
	}
	if n == 1 {
		pr[docs[0]] = 1.0
		return PageRankScores{score: pr, Converged: true}
	}

	var iter int
	converged := false
	for iter = 0; iter < maxIter; iter++ {
		// Dangling mass: total score of nodes with no out-links, accumulated over
		// the SORTED document set so the float sum is order-stable (CLAUDE.md).
		var danglingSum float64
		for _, id := range docs {
			if len(g.projAdj[id]) == 0 {
				danglingSum += pr[id]
			}
		}
		danglingShare := d * danglingSum / nf
		base := (1 - d) / nf

		newPR := make(map[identity.DocumentID]float64, n)
		for _, v := range docs {
			// Inbound contribution: each in-neighbour u of v donates pr[u]/outdeg(u).
			// projRev[v] is already sorted, so this float sum is order-stable.
			var inbound float64
			for _, u := range g.projRev[v] {
				// len(projAdj[u]) > 0 is guaranteed: u ∈ projRev[v] means u has the
				// out-edge u→v, so its out-degree is at least 1 — the divisor is never
				// zero (a dangling node has no out-edges, so it is in no projRev list).
				inbound += pr[u] / float64(len(g.projAdj[u]))
			}
			newPR[v] = base + d*inbound + danglingShare
		}

		// L1 delta over the sorted document set.
		var delta float64
		for _, v := range docs {
			diff := newPR[v] - pr[v]
			if diff < 0 {
				diff = -diff
			}
			delta += diff
		}
		pr = newPR
		if delta < nf*eps {
			converged = true
			iter++ // count this completed iteration
			break
		}
	}

	return PageRankScores{score: pr, Iterations: iter, Converged: converged}
}
