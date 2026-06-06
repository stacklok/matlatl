package graphmodel

import (
	"cmp"
	"slices"

	"github.com/stacklok/matlatl/internal/domain/identity"
)

// Bridge is an undirected edge whose removal disconnects the document graph (a
// single point of failure between two clusters). A and B are stored canonically
// with A < B by DocumentID so the pair is order-independent and deterministic.
type Bridge struct {
	A identity.DocumentID
	B identity.DocumentID
}

// CriticalStructure is the corpus' critical-path structure (ADR 0015): the
// articulation points (cut VERTICES) and bridges (cut EDGES) of the UNDIRECTED
// link closure. An articulation point is a document whose removal fragments the
// corpus into more pieces; a bridge is the only link connecting two parts. Both
// are surfaced as non-gating Info findings AND as graph.json data. Slices are
// sorted for determinism.
type CriticalStructure struct {
	// ArticulationPoints are the cut vertices, sorted by DocumentID.
	ArticulationPoints []identity.DocumentID
	// Bridges are the cut edges (A<B canonical), sorted by (A, B).
	Bridges []Bridge
}

// IsArticulation reports whether id is a cut vertex. Linear scan over the
// (typically small) sorted articulation-point set; used by the emitters to set
// the per-node isArticulation flag.
func (cs CriticalStructure) IsArticulation(id identity.DocumentID) bool {
	_, found := slices.BinarySearch(cs.ArticulationPoints, id)
	return found
}

// abFrame is one node's state on the explicit articulation/bridge DFS work
// stack (matching components.go's iterative-Tarjan style): the node, its DFS
// parent, the index of the next undirected-closure neighbour to visit, and the
// count of DFS-tree children discovered so far (for the root special case).
type abFrame struct {
	v         identity.DocumentID
	parent    identity.DocumentID
	hasParent bool
	next      int
	children  int
}

// ComputeCriticalStructure finds the articulation points and bridges of the
// UNDIRECTED link closure (N(x)=out(x)∪in(x)) with Tarjan's low-link algorithm,
// driven iteratively over an explicit stack — NO recursion — so an arbitrarily
// long link chain cannot overflow the goroutine stack (the components.go
// stack-safety contract, ADR 0015). Betweenness, by contrast, runs over the
// DIRECTED projection (centrality.go); the directed/undirected split mirrors
// ADR 0014's navigability metrics.
//
// Determinism: the closure neighbour lists are sorted/deduped/self-loop-free
// (undirectedClosure), the DFS is driven from every undiscovered document in
// sorted g.documents order (a forest over disconnected components), neighbours
// are visited in sorted order, and both output slices are sorted — so the result
// is byte-stable regardless of map iteration order (ADR 0007).
//
// Edge cases: empty/single-node → none; a 2-node A-B → one bridge, no
// articulation; a cycle → none/none; a path A-B-C-D → {B,C} articulation and
// every edge a bridge. The DFS root is an articulation point IFF it has >=2
// DFS-tree children.
func (g *ReferenceGraph) ComputeCriticalStructure() CriticalStructure {
	neighbours := g.undirectedClosure() // sorted, deduped, self-loop-free

	disc := make(map[identity.DocumentID]int, len(g.documents))
	low := make(map[identity.DocumentID]int, len(g.documents))
	counter := 0

	// A set so we can dedupe articulation points (a vertex can satisfy the cut
	// condition via several children); bridges are emitted at most once each.
	apSet := make(map[identity.DocumentID]struct{})
	var bridges []Bridge

	for _, root := range g.documents { // sorted forest driver
		if _, seen := disc[root]; seen {
			continue
		}
		// Drive one DFS tree from root with an explicit work stack.
		work := []abFrame{{v: root}}
		disc[root] = counter
		low[root] = counter
		counter++

		for len(work) > 0 {
			top := &work[len(work)-1]
			v := top.v
			adj := neighbours[v]

			if top.next < len(adj) {
				w := adj[top.next]
				top.next++
				// Skip the single edge back to the DFS parent (undirected edges
				// appear in both directions; the tree edge to the parent is not a
				// back edge).
				if top.hasParent && w == top.parent {
					top.hasParent = false // only skip the parent ONCE (multigraph guard)
					continue
				}
				if _, seen := disc[w]; !seen {
					top.children++
					disc[w] = counter
					low[w] = counter
					counter++
					work = append(work, abFrame{v: w, parent: v, hasParent: true})
				} else if disc[w] < low[v] {
					// Back edge: tighten v's low-link toward w's discovery time.
					low[v] = disc[w]
				}
				continue
			}

			// All neighbours of v processed (recursion's return point). Pop v and
			// propagate its low-link / cut-conditions to its parent.
			finished := work[len(work)-1]
			work = work[:len(work)-1]
			if len(work) == 0 {
				// v is the DFS-tree root: articulation iff it has >=2 children.
				if finished.children >= 2 {
					apSet[finished.v] = struct{}{}
				}
				continue
			}
			parentFrame := &work[len(work)-1]
			p := parentFrame.v
			if low[finished.v] < low[p] {
				low[p] = low[finished.v]
			}
			// Non-root articulation: parent is not the DFS root AND no descendant
			// of v reaches above p (low[v] >= disc[p]). The root case is handled
			// when the root itself is popped (above), so guard parent!=root here.
			if len(work) > 1 && low[finished.v] >= disc[p] {
				apSet[p] = struct{}{}
			}
			// Bridge: no descendant of v reaches p or above (strict >), so the
			// p-v edge is the only connection to v's subtree.
			if low[finished.v] > disc[p] {
				bridges = append(bridges, canonicalBridge(p, finished.v))
			}
		}
	}

	aps := make([]identity.DocumentID, 0, len(apSet))
	for id := range apSet {
		aps = append(aps, id)
	}
	slices.Sort(aps)
	slices.SortFunc(bridges, func(x, y Bridge) int {
		if c := cmp.Compare(x.A, y.A); c != 0 {
			return c
		}
		return cmp.Compare(x.B, y.B)
	})
	if aps == nil {
		aps = []identity.DocumentID{}
	}
	if bridges == nil {
		bridges = []Bridge{}
	}
	return CriticalStructure{ArticulationPoints: aps, Bridges: bridges}
}

// canonicalBridge returns a Bridge with A<B by DocumentID, so an edge is stored
// the same way regardless of which endpoint the DFS finished first.
func canonicalBridge(a, b identity.DocumentID) Bridge {
	if a < b {
		return Bridge{A: a, B: b}
	}
	return Bridge{A: b, B: a}
}
