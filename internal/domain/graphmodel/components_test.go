package graphmodel

import (
	"slices"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// TestCondensation_RepMapAndSortedAdjacency: a DAG A->B->C has three singleton
// SCCs; the rep map is the identity and the condensation adjacency mirrors the
// projection (A->B, B->C), sorted.
func TestCondensation_RepMapAndSortedAdjacency(t *testing.T) {
	c := buildCorpus(t, doc("A.md", "a", nil), doc("B.md", "b", nil), doc("C.md", "c", nil))
	refs := []reference.Reference{validRef("A.md", "B.md"), validRef("B.md", "C.md")}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	scc := g.StronglyConnectedComponents()
	repOf, adj := g.Condensation(scc)

	for _, id := range []identity.DocumentID{"A.md", "B.md", "C.md"} {
		if repOf[id] != id {
			t.Errorf("repOf[%s] = %s, want self (singleton SCC)", id, repOf[id])
		}
	}
	if !slices.Equal(ids(adj["A.md"]), []string{"B.md"}) {
		t.Errorf("adj[A.md] = %v, want [B.md]", ids(adj["A.md"]))
	}
	if !slices.Equal(ids(adj["B.md"]), []string{"C.md"}) {
		t.Errorf("adj[B.md] = %v, want [C.md]", ids(adj["B.md"]))
	}
	if len(adj["C.md"]) != 0 {
		t.Errorf("adj[C.md] = %v, want empty (sink)", ids(adj["C.md"]))
	}
}

// TestCondensation_SelfEdgeGuard: a cycle A<->B collapses to ONE SCC (rep = the
// sorted-min member A.md). The intra-SCC edges (A->B, B->A) must NOT produce a
// self-edge in the condensation (sv == sw is skipped), so the condensation is
// acyclic / has no self-loops.
func TestCondensation_SelfEdgeGuard(t *testing.T) {
	c := buildCorpus(t, doc("A.md", "a", nil), doc("B.md", "b", nil))
	refs := []reference.Reference{validRef("A.md", "B.md"), validRef("B.md", "A.md")}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	scc := g.StronglyConnectedComponents()
	repOf, adj := g.Condensation(scc)

	if repOf["A.md"] != "A.md" || repOf["B.md"] != "A.md" {
		t.Errorf("both members must map to rep A.md, got repOf=%v", repOf)
	}
	// The single SCC rep must have NO out-edges (the only edges were intra-SCC).
	if got := adj["A.md"]; len(got) != 0 {
		t.Errorf("condensation must skip intra-SCC self-edge, got adj[A.md] = %v", ids(got))
	}
}

// TestCondensation_MultiEdgeDedup: two distinct edges between the same pair of
// SCCs collapse to one condensation edge, and neighbour lists are sorted.
func TestCondensation_MultiEdgeDedup(t *testing.T) {
	// SCC1 = {A,B} (cycle), SCC2 = {C}. Both A->C and B->C cross to SCC2; the
	// condensation must have exactly one A.md->C.md edge.
	c := buildCorpus(t, doc("A.md", "a", nil), doc("B.md", "b", nil), doc("C.md", "c", nil))
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "A.md"),
		validRef("A.md", "C.md"), validRef("B.md", "C.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	scc := g.StronglyConnectedComponents()
	_, adj := g.Condensation(scc)
	if !slices.Equal(ids(adj["A.md"]), []string{"C.md"}) {
		t.Errorf("multi-edge between SCCs must dedup to one: adj[A.md] = %v, want [C.md]", ids(adj["A.md"]))
	}
}
