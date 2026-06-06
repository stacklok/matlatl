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
