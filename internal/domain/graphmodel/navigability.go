package graphmodel

import (
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// Navigability holds the corpus-level navigability / structural-health scalars
// (P9). It is pure DATA, like BowtieReport: it produces no finding and never
// gates the check exit code; it is surfaced in graph.json (summary.navigability),
// the human reports, and over MCP so an agent can read how navigable the corpus
// is. Floats are plain float64 here in the domain; the fixed-precision wire Float
// lives only in the graphjson layer (exactly like HITS scores).
type Navigability struct {
	// Compactness (Cp) is the directed reachability-weighted compactness in [0,1]
	// over the document projection: 1 means every ordered pair reaches the other
	// in one hop (fully connected), 0 means nothing reaches anything. Unreachable
	// ordered pairs are charged the maximum sub-distance K=N (the doc count), so a
	// disconnected corpus scores low.
	Compactness float64
	// Stratum measures how linear/hierarchical the directed reachability is, in
	// [0,1]: 1 is a pure chain (a strict status order), 0 is a pure cycle / fully
	// symmetric structure (no net flow direction). Computed from per-node status
	// = inStatus - outStatus over FINITE sub-distances.
	Stratum float64
	// CharacteristicPathLength is the MEAN shortest-path distance over all finite
	// (reachable) ordered pairs in the UNDIRECTED closure. 0 when there are no
	// finite pairs.
	CharacteristicPathLength float64
	// MedianPathLength is the MEDIAN of the same finite-pair distance distribution
	// (computed from the histogram, no float sort). 0 when there are no finite
	// pairs.
	MedianPathLength float64
	// ClusteringCoefficient is the Watts-Strogatz global clustering coefficient in
	// [0,1] over the undirected closure: the mean local clustering over nodes with
	// undirected degree >= 2 (degree-<2 nodes are EXCLUDED, not counted as 0).
	ClusteringCoefficient float64
	// Diameter is the longest finite shortest-path distance in the undirected
	// closure (the eccentricity bound). 0 when there are no finite pairs.
	Diameter int
	// ReachablePairs is the number of ordered (i!=j) pairs with a FINITE
	// undirected-closure distance — the count behind CPL/median/diameter.
	ReachablePairs int
	// Documents is N, the document count the metrics were computed over.
	Documents int
}

// ComputeNavigability computes the corpus navigability scalars over the document
// projection (ADR 0014). Compactness and stratum use the DIRECTED projection;
// characteristic/median path length, diameter and clustering use the UNDIRECTED
// closure N(x)=out(x)∪in(x). It is deterministic: all iteration is over the
// sorted g.documents and sorted neighbour lists, float sums are accumulated in
// that fixed order, and the median is read from a histogram (no float sort).
//
// Edge cases: N<=1 yields a zero-valued struct (Documents=N) — there are no
// ordered pairs, so every metric is 0 with no division by zero.
func (g *ReferenceGraph) ComputeNavigability() Navigability {
	n := len(g.documents)
	nav := Navigability{Documents: n}
	if n <= 1 {
		return nav
	}

	nav.Compactness, nav.Stratum = g.computeCompactnessStratum(n)
	cpl, median, diameter, reachable := g.computePathLengths(n)
	nav.CharacteristicPathLength = cpl
	nav.MedianPathLength = median
	nav.Diameter = diameter
	nav.ReachablePairs = reachable
	nav.ClusteringCoefficient = g.computeClustering()
	return nav
}

// computeCompactnessStratum runs a single directed APSP pass over the projection
// and derives both compactness (reachability-weighted, K=N substituted for
// unreachable ordered pairs) and stratum (from the per-node finite status
// imbalance). N>=2 is guaranteed by the caller.
func (g *ReferenceGraph) computeCompactnessStratum(n int) (compactness, stratum float64) {
	// statusOut[i] = Σ_{reachable j≠i} d(i,j); statusIn[j] += d(i,j). Both over
	// FINITE sub-distances only (unreachable pairs contribute nothing to status).
	statusOut := make(map[identity.DocumentID]float64, n)
	statusIn := make(map[identity.DocumentID]float64, n)

	// sumCD = Σ_{i≠j} (reachable ? d(i,j) : N). Accumulate per source in sorted
	// order so the float sum is byte-stable.
	var sumCD float64
	g.ForEachSourceDistances(g.projAdj, func(src identity.DocumentID, dist map[identity.DocumentID]int) {
		var srcOut float64
		for _, dst := range g.documents { // sorted ⇒ deterministic float order
			if dst == src {
				continue
			}
			if d, ok := dist[dst]; ok {
				sumCD += float64(d)
				srcOut += float64(d)
				statusIn[dst] += float64(d)
			} else {
				sumCD += float64(n) // K = N for unreachable ordered pairs
			}
		}
		statusOut[src] = srcOut
	})

	// Compactness: Cp = ((N²−N)*N − sumCD) / ((N²−N)*(N−1)).
	nn := float64(n*n - n) // N²−N (number of ordered pairs); >0 for N>=2
	compactness = (nn*float64(n) - sumCD) / (nn * float64(n-1))

	// Stratum: S(i)=statusIn[i]−statusOut[i]; AP=Σ|S(i)|; LAP=N²/2 (N even) or
	// (N²−1)/2 (N odd); Stratum=min(AP/LAP,1).
	var ap float64
	for _, id := range g.documents { // sorted
		s := statusIn[id] - statusOut[id]
		if s < 0 {
			s = -s
		}
		ap += s
	}
	var lap float64
	if n%2 == 0 {
		lap = float64(n*n) / 2
	} else {
		lap = float64(n*n-1) / 2
	}
	stratum = ap / lap
	if stratum > 1 {
		stratum = 1
	}
	return compactness, stratum
}

// computePathLengths runs a second APSP pass over the UNDIRECTED closure and
// returns the mean (characteristic) and median shortest-path length, the
// diameter, and the finite ordered-pair count. The distance distribution is
// tallied into a histogram (max finite distance <= N), so the median is read
// from cumulative counts without a float sort. N>=2 is guaranteed by the caller.
func (g *ReferenceGraph) computePathLengths(n int) (cpl, median float64, diameter, reachable int) {
	undirected := g.undirectedClosure()
	// A shortest-path distance over N nodes is at most N-1; size N+1 for safety.
	counts := make([]int, n+1)
	g.ForEachSourceDistances(undirected, func(src identity.DocumentID, dist map[identity.DocumentID]int) {
		// Iterating dist in (random) map order is safe here — the only sink is a
		// commutative integer histogram increment (counts[d]++), which is
		// order-independent — so this is NOT an exception to the package's
		// "sorted iteration everywhere" determinism rule.
		for dst, d := range dist {
			if dst == src {
				continue // skip self-distance 0
			}
			counts[d]++
		}
	})

	// weighted is the Σ(dist*count) numerator of the mean. It is widened to int64
	// to document intent: the worst case (~N² pairs * diameter N) is well within
	// int on matlatl's 64-bit targets, but int64 makes the bound explicit.
	var total int
	var weighted int64
	for d := 1; d <= n; d++ {
		c := counts[d]
		if c == 0 {
			continue
		}
		total += c
		weighted += int64(d) * int64(c)
		diameter = d // counts walked ascending ⇒ last non-zero bucket is the max
	}
	if total == 0 {
		return 0, 0, 0, 0
	}
	cpl = float64(weighted) / float64(total)
	median = medianFromHistogram(counts, total)
	return cpl, median, diameter, total
}

// medianFromHistogram returns the median distance from an ascending histogram of
// finite distances (counts[d] = #pairs at distance d) with total>0 entries. For
// an even total it averages the two central order statistics; both are found by
// walking cumulative counts (no float sort), so it stays deterministic.
func medianFromHistogram(counts []int, total int) float64 {
	// 1-based ranks of the central element(s).
	loRank := (total + 1) / 2
	hiRank := total/2 + 1
	var lo, hi float64
	var cumulative int
	gotLo, gotHi := false, false
	for d := 0; d < len(counts); d++ {
		cumulative += counts[d]
		if !gotLo && cumulative >= loRank {
			lo = float64(d)
			gotLo = true
		}
		if !gotHi && cumulative >= hiRank {
			hi = float64(d)
			gotHi = true
		}
		if gotLo && gotHi {
			break
		}
	}
	return (lo + hi) / 2
}

// computeClustering returns the Watts-Strogatz global clustering coefficient: the
// mean local clustering over nodes whose undirected degree is >= 2. A node with
// degree < 2 has no definable local clustering and is EXCLUDED from the mean (it
// is not counted as 0), per the Watts-Strogatz convention (ADR 0014). The
// undirected closure is precomputed once (the PredictLinks neighbours pattern)
// and intersections use the sorted-list intersectSize from linkprediction.go.
func (g *ReferenceGraph) computeClustering() float64 {
	neighbours := g.undirectedClosure()
	var sum float64
	var counted int
	for _, v := range g.documents { // sorted
		nv := neighbours[v]
		k := len(nv)
		if k < 2 {
			continue
		}
		// links = Σ_{u∈N(v)} |N(u)∩N(v)| counts each neighbour-neighbour edge
		// twice, i.e. 2*edges. Dividing by k*(k−1)=2*maxPairs gives
		// edges/maxPairs, the correct local clustering.
		var links int
		for _, u := range nv { // sorted
			links += intersectSize(neighbours[u], nv)
		}
		local := float64(links) / float64(k*(k-1))
		sum += local
		counted++
	}
	if counted == 0 {
		return 0
	}
	return sum / float64(counted)
}

// undirectedClosure builds the undirected neighbour-closure adjacency
// N(x)=out(x)∪in(x) for every document once, reusing undirectedNeighbours (the
// sorted merge from linkprediction.go). Each neighbour list is sorted, deduped
// and self-loop-free, so it feeds BFS/intersectSize directly.
func (g *ReferenceGraph) undirectedClosure() map[identity.DocumentID][]identity.DocumentID {
	closure := make(map[identity.DocumentID][]identity.DocumentID, len(g.documents))
	for _, id := range g.documents { // sorted
		closure[id] = g.undirectedNeighbours(id)
	}
	return closure
}
