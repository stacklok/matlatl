package graphmodel

import (
	"cmp"
	"slices"

	"github.com/stacklok/doctopus/internal/domain/identity"
)

// Component is a connected component: an ID (the sorted-minimum member, for
// determinism) and its sorted member documents.
type Component struct {
	ID      identity.DocumentID
	Members []identity.DocumentID
}

// Components is a sorted list of components (ordered by ID).
type Components []Component

// ComponentIDs returns the component IDs in their existing (already-sorted)
// order. Components is constructed sorted by ID (see finalizeComponents), so no
// re-sort is needed.
//
// Planned consumer: the P4/P5 emitters (graph.json component grouping, the
// human report's "clusters" section) will read this to label each document's
// component. It is exported ahead of that consumer as the stable emitter API.
func (comps Components) ComponentIDs() []identity.DocumentID {
	out := make([]identity.DocumentID, 0, len(comps))
	for _, c := range comps {
		out = append(out, c.ID)
	}
	return out
}

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

func (t *tarjan) strongConnect(v identity.DocumentID) {
	t.indices[v] = t.index
	t.low[v] = t.index
	t.index++
	t.stack = append(t.stack, v)
	t.onStack[v] = true

	for _, w := range t.g.projAdj[v] { // sorted neighbors
		switch {
		case t.indexUnset(w):
			t.strongConnect(w)
			t.low[v] = min(t.low[v], t.low[w])
		case t.onStack[w]:
			t.low[v] = min(t.low[v], t.indices[w])
		}
	}

	if t.low[v] == t.indices[v] {
		var scc []identity.DocumentID
		for {
			w := t.stack[len(t.stack)-1]
			t.stack = t.stack[:len(t.stack)-1]
			t.onStack[w] = false
			scc = append(scc, w)
			if w == v {
				break
			}
		}
		t.sccs = append(t.sccs, scc)
	}
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
