package graphmodel

import (
	"github.com/stacklok/matlatl/internal/domain/identity"
)

// ForEachSourceDistances streams an all-pairs-shortest-path (APSP) computation
// over the given adjacency without ever materializing a V² distance matrix: it
// runs one breadth-first search per source document (in sorted g.documents
// order) and invokes visit with the source and its single-source shortest-path
// distance map. Edges are unweighted, so BFS yields the exact shortest-path
// distances (hop counts).
//
// The dist map passed to visit is REUSED across sources (cleared between
// sources via the builtin clear) — it is owned by this helper and valid ONLY
// for the duration of the visit call; a caller that needs to retain distances
// must copy them. dist[src] == 0 (the self-distance) is included; a destination
// absent from dist is UNREACHABLE from src. Neighbour lists are iterated in the
// sorted order buildProjection guarantees, so the traversal — and therefore any
// reduction a caller computes over it — is fully deterministic regardless of
// map iteration order (ADR 0004).
//
// Cost: O(V·(V+E)) time and O(V) transient memory (one reused dist map plus one
// BFS queue), since no per-source result is stored.
//
// Reuse contract (P10 betweenness): this is deliberately the minimal streaming
// SSSP primitive. A sibling helper for betweenness will need, in addition to the
// distances, the BFS discovery ORDER (for the dependency-accumulation back-pass)
// and the shortest-path PREDECESSOR counts (sigma). Those are intentionally NOT
// computed here so unweighted distance consumers (navigability) pay nothing for
// them; the sibling should follow this same per-source streaming shape (sorted
// source order, sorted neighbour expansion, no stored V² state) rather than
// generalizing this function with extra out-parameters.
func (g *ReferenceGraph) ForEachSourceDistances(
	adj map[identity.DocumentID][]identity.DocumentID,
	visit func(src identity.DocumentID, dist map[identity.DocumentID]int),
) {
	dist := make(map[identity.DocumentID]int, len(g.documents))
	// queue is a reused FIFO with an explicit head index. We must NOT pop via
	// queue = queue[1:]: reslicing advances the backing-array pointer, so a later
	// append past the (shrinking) tail capacity reallocates a fresh array EVERY
	// source — an O(V) per-source allocation that is O(V²) over the whole pass.
	// Resetting len to 0 (head=0) instead keeps one backing array for the whole
	// run, holding transient memory to O(V) as documented.
	queue := make([]identity.DocumentID, 0, len(g.documents))
	for _, src := range g.documents { // sorted
		clear(dist)
		queue = queue[:0]
		head := 0
		dist[src] = 0
		queue = append(queue, src)
		for head < len(queue) {
			cur := queue[head]
			head++
			d := dist[cur]
			for _, nb := range adj[cur] { // sorted
				if _, seen := dist[nb]; seen {
					continue
				}
				dist[nb] = d + 1
				queue = append(queue, nb)
			}
		}
		visit(src, dist)
	}
}

// ForEachSourceBFS streams the per-source Brandes forward pass for betweenness
// centrality over the given adjacency — the sibling primitive ForEachSourceDistances
// promises in its reuse contract (ADR 0014/0015). For every source s (in sorted
// g.documents order) it runs one BFS and invokes visit with, in addition to the
// distances ForEachSourceDistances would give, the two extra quantities Brandes'
// back-pass needs:
//
//   - order: the BFS discovery (push) order — Brandes' stack S. The dependency
//     back-accumulation walks this in REVERSE.
//   - preds: shortest-path predecessors. preds[w] lists every v with an edge
//     v→w on a shortest path s→…→w (i.e. dist[w]==dist[v]+1). Because neighbours
//     are expanded in sorted order, each preds[w] is appended in sorted order, so
//     the float divisions/sums the caller performs over it run in a fixed order
//     and are byte-stable (ADR 0007).
//   - sigma: the number of shortest paths from s to each node (float64, as
//     Brandes specifies, to match the dependency arithmetic).
//
// Reuse contract: the dist/sigma/preds maps and the order/queue slices are OWNED
// by this helper and REUSED across sources (dist/sigma cleared, preds' slices
// re-sliced to empty, order/queue re-sliced per source). They are valid ONLY for
// the duration of the visit call; the callback MUST NOT retain them (copy what
// it needs). preds may carry leftover keys with EMPTY slices from earlier
// sources — read preds[w] only for w that appear in order (every such w had its
// predecessor list rebuilt this source). The explicit queue head index avoids
// the reslice-reallocation pitfall ForEachSourceDistances documents, and reusing
// the predecessor backing arrays keeps the V·(V+E) pass at O(V+E) transient
// memory with no per-source slice churn and no V² state.
func (g *ReferenceGraph) ForEachSourceBFS(
	adj map[identity.DocumentID][]identity.DocumentID,
	visit func(
		src identity.DocumentID,
		order []identity.DocumentID,
		preds map[identity.DocumentID][]identity.DocumentID,
		sigma map[identity.DocumentID]float64,
	),
) {
	dist := make(map[identity.DocumentID]int, len(g.documents))
	sigma := make(map[identity.DocumentID]float64, len(g.documents))
	preds := make(map[identity.DocumentID][]identity.DocumentID, len(g.documents))
	order := make([]identity.DocumentID, 0, len(g.documents))
	queue := make([]identity.DocumentID, 0, len(g.documents))

	for _, s := range g.documents { // sorted source order ⇒ deterministic accumulation
		clear(dist)
		clear(sigma)
		// Re-slice every predecessor list to zero length rather than clear(preds):
		// this REUSES the slice backing arrays across sources, so the V·(V+E)
		// Brandes pass does not reallocate a fresh predecessor slice per (source,
		// node) — the dominant allocation if the map were cleared each source.
		// Leftover keys hold empty slices, which is harmless: the back-pass reads
		// preds[w] only for w in order, and every such w had its list freshly
		// appended this source (preds[w] is rebuilt from empty before w is reached).
		for k := range preds {
			preds[k] = preds[k][:0]
		}
		order = order[:0]
		queue = queue[:0]
		head := 0

		dist[s] = 0
		sigma[s] = 1
		queue = append(queue, s)
		for head < len(queue) {
			v := queue[head]
			head++
			order = append(order, v)
			for _, w := range adj[v] { // sorted neighbours ⇒ sorted preds[w]
				if _, seen := dist[w]; !seen {
					dist[w] = dist[v] + 1
					queue = append(queue, w)
				}
				if dist[w] == dist[v]+1 {
					sigma[w] += sigma[v]
					preds[w] = append(preds[w], v)
				}
			}
		}
		visit(s, order, preds, sigma)
	}
}
