package graphmodel

import (
	"cmp"
	"slices"

	"github.com/stacklok/matlatl/internal/domain/identity"
)

// Component is a connected component: an ID (the sorted-minimum member, for
// determinism) and its sorted member documents.
type Component struct {
	ID      identity.DocumentID
	Members []identity.DocumentID
}

// Components is a sorted list of components (ordered by ID).
type Components []Component

// WeaklyConnectedComponents computes WCCs over the UNDIRECTED projection using
// union-find with path compression and union-by-size. Component IDs are the
// lexicographically smallest member, and the component list is sorted by ID, so
// the result is fully deterministic regardless of map order (ADR 0007).
func (g *ReferenceGraph) WeaklyConnectedComponents() Components {
	uf := newUnionFind(g.documents)
	for _, id := range g.documents {
		// Union along out-edges (the undirected projection unions both directions;
		// projAdj already de-duplicated, and we only need to union one way since
		// union is symmetric).
		for _, nb := range g.projAdj[id] {
			uf.union(id, nb)
		}
	}
	return uf.components()
}

// StronglyConnectedComponents computes SCCs over the DIRECTED projection using
// Tarjan's algorithm with sorted neighbor iteration and a sorted document
// driver order, then assigns each SCC the sorted-min-member ID and sorts the
// component list by ID — fully deterministic.
func (g *ReferenceGraph) StronglyConnectedComponents() Components {
	t := &tarjan{
		g:       g,
		indices: make(map[identity.DocumentID]int),
		low:     make(map[identity.DocumentID]int),
		onStack: make(map[identity.DocumentID]bool),
	}
	for _, id := range g.documents { // sorted driver order
		if _, seen := t.indices[id]; !seen {
			t.strongConnect(id)
		}
	}
	return finalizeComponents(t.sccs)
}

// Condensation collapses each strongly-connected component to a single
// representative node (its sorted-min member = the existing Component.ID) and
// returns the condensation DAG over those representatives (ADR 0016). repOf maps
// every document to its SCC representative; adj is the condensation out-edge
// adjacency between DISTINCT representatives, built from the sorted document
// projection (g.projAdj) so neighbour lists are sorted and de-duplicated and the
// result is fully deterministic. A self-edge within an SCC (sv == sw) is skipped
// so the condensation stays acyclic. It takes the SCC list as a parameter
// (rather than recomputing) so the caller's single Tarjan pass is reused.
func (g *ReferenceGraph) Condensation(scc Components) (repOf map[identity.DocumentID]identity.DocumentID, adj map[identity.DocumentID][]identity.DocumentID) {
	repOf = make(map[identity.DocumentID]identity.DocumentID, len(g.documents))
	for _, comp := range scc { // sorted by ID
		for _, m := range comp.Members { // sorted
			repOf[m] = comp.ID
		}
	}

	// Accumulate distinct out-edges between representatives using a set so a
	// multi-edge (many member→member links between two SCCs) collapses to one.
	outSet := make(map[identity.DocumentID]map[identity.DocumentID]struct{}, len(scc))
	for _, v := range g.documents { // sorted driver order
		sv := repOf[v]
		for _, w := range g.projAdj[v] { // sorted neighbours
			sw := repOf[w]
			if sv == sw {
				continue // intra-SCC edge: skip so the condensation is acyclic
			}
			s := outSet[sv]
			if s == nil {
				s = make(map[identity.DocumentID]struct{})
				outSet[sv] = s
			}
			s[sw] = struct{}{}
		}
	}

	adj = make(map[identity.DocumentID][]identity.DocumentID, len(outSet))
	for rep, s := range outSet {
		adj[rep] = sortedKeys(s) // sorted, de-duplicated
	}
	return repOf, adj
}

// --- union-find ---

type unionFind struct {
	parent map[identity.DocumentID]identity.DocumentID
	size   map[identity.DocumentID]int
	all    []identity.DocumentID
}

func newUnionFind(ids []identity.DocumentID) *unionFind {
	uf := &unionFind{
		parent: make(map[identity.DocumentID]identity.DocumentID, len(ids)),
		size:   make(map[identity.DocumentID]int, len(ids)),
		all:    ids,
	}
	for _, id := range ids {
		uf.parent[id] = id
		uf.size[id] = 1
	}
	return uf
}

func (uf *unionFind) find(x identity.DocumentID) identity.DocumentID {
	root := x
	for uf.parent[root] != root {
		root = uf.parent[root]
	}
	// Path compression.
	for uf.parent[x] != root {
		uf.parent[x], x = root, uf.parent[x]
	}
	return root
}

func (uf *unionFind) union(a, b identity.DocumentID) {
	ra, rb := uf.find(a), uf.find(b)
	if ra == rb {
		return
	}
	// Union by size; ties broken by smaller id staying as root for determinism.
	if uf.size[ra] < uf.size[rb] || (uf.size[ra] == uf.size[rb] && rb < ra) {
		ra, rb = rb, ra
	}
	uf.parent[rb] = ra
	uf.size[ra] += uf.size[rb]
}

func (uf *unionFind) components() Components {
	groups := make(map[identity.DocumentID][]identity.DocumentID)
	for _, id := range uf.all {
		root := uf.find(id)
		groups[root] = append(groups[root], id)
	}
	return finalizeComponentGroups(groups)
}

// --- Tarjan ---

type tarjan struct {
	g       *ReferenceGraph
	index   int
	stack   []identity.DocumentID
	indices map[identity.DocumentID]int
	low     map[identity.DocumentID]int
	onStack map[identity.DocumentID]bool
	sccs    [][]identity.DocumentID
}

// frame is one node's state on the explicit DFS work stack: the node and the
// index of the next neighbor (in sorted projAdj order) still to visit.
type frame struct {
	v    identity.DocumentID
	next int
}

// strongConnect runs Tarjan's SCC visit from a root using an EXPLICIT stack
// instead of native recursion, so an arbitrarily long link chain (e.g. 20k
// documents in one path) cannot overflow the goroutine stack (P6
// concurrency-readiness). It is the faithful iterative transcription of the
// recursive algorithm: a frame is pushed when a node is first discovered, its
// neighbors are advanced one at a time, the low-link is propagated back to the
// parent on each return, and an SCC is popped from the value stack when a node's
// low-link equals its own index. Neighbor iteration order (sorted projAdj) and
// the resulting low-link values are identical to the recursive version, so the
// SCCs are byte-for-byte the same after finalizeComponents sorts them.
func (t *tarjan) strongConnect(root identity.DocumentID) {
	work := []frame{{v: root}}
	t.visit(root)

	for len(work) > 0 {
		top := &work[len(work)-1]
		v := top.v
		adj := t.g.projAdj[v]

		if top.next < len(adj) {
			w := adj[top.next]
			top.next++
			switch {
			case t.indexUnset(w):
				// Descend into w: push a new frame (recursion's call).
				t.visit(w)
				work = append(work, frame{v: w})
			case t.onStack[w]:
				t.low[v] = min(t.low[v], t.indices[w])
			}
			continue
		}

		// All neighbors of v processed (recursion's return point).
		if t.low[v] == t.indices[v] {
			t.popSCC(v)
		}
		work = work[:len(work)-1]
		// Propagate v's low-link up to its parent (recursion's
		// low[parent] = min(low[parent], low[v])).
		if len(work) > 0 {
			parent := work[len(work)-1].v
			t.low[parent] = min(t.low[parent], t.low[v])
		}
	}
}

// visit assigns v its discovery index/low-link and pushes it onto the value
// stack (the shared part of the recursive call's preamble).
func (t *tarjan) visit(v identity.DocumentID) {
	t.indices[v] = t.index
	t.low[v] = t.index
	t.index++
	t.stack = append(t.stack, v)
	t.onStack[v] = true
}

// popSCC pops the value stack down to and including root, forming one SCC.
func (t *tarjan) popSCC(root identity.DocumentID) {
	var scc []identity.DocumentID
	for {
		w := t.stack[len(t.stack)-1]
		t.stack = t.stack[:len(t.stack)-1]
		t.onStack[w] = false
		scc = append(scc, w)
		if w == root {
			break
		}
	}
	t.sccs = append(t.sccs, scc)
}

func (t *tarjan) indexUnset(v identity.DocumentID) bool {
	_, ok := t.indices[v]
	return !ok
}

// finalizeComponents assigns each raw component its sorted-min-member ID, sorts
// members, and sorts the component list by ID.
func finalizeComponents(raw [][]identity.DocumentID) Components {
	out := make(Components, 0, len(raw))
	for _, members := range raw {
		m := slices.Clone(members)
		slices.Sort(m)
		out = append(out, Component{ID: m[0], Members: m})
	}
	slices.SortFunc(out, func(a, b Component) int {
		return cmp.Compare(a.ID, b.ID)
	})
	return out
}

func finalizeComponentGroups(groups map[identity.DocumentID][]identity.DocumentID) Components {
	raw := make([][]identity.DocumentID, 0, len(groups))
	for _, members := range groups {
		raw = append(raw, members)
	}
	return finalizeComponents(raw)
}
