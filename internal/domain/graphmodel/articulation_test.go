package graphmodel

import (
	"slices"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// apList extracts the sorted articulation-point IDs as strings.
func apList(cs CriticalStructure) []string {
	out := make([]string, len(cs.ArticulationPoints))
	for i, id := range cs.ArticulationPoints {
		out[i] = id.String()
	}
	return out
}

// bridgeList extracts the bridges as "A|B" strings (A<B canonical, sorted).
func bridgeList(cs CriticalStructure) []string {
	out := make([]string, len(cs.Bridges))
	for i, b := range cs.Bridges {
		out[i] = b.A.String() + "|" + b.B.String()
	}
	return out
}

// TestCriticalStructure_Path pins the path A-B-C-D-E (directed edges, undirected
// closure is the path). Interior vertices {B,C,D} are articulation points; every
// edge is a bridge. The DFS root is A (sorted-first), a LEAF with one child, so
// it is correctly NOT an articulation point.
func TestCriticalStructure_Path(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil), doc("C.md", "c", nil),
		doc("D.md", "d", nil), doc("E.md", "e", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "C.md"),
		validRef("C.md", "D.md"), validRef("D.md", "E.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	cs := g.ComputeCriticalStructure()

	if got, want := apList(cs), []string{"B.md", "C.md", "D.md"}; !slices.Equal(got, want) {
		t.Errorf("articulation points = %v, want %v", got, want)
	}
	want := []string{"A.md|B.md", "B.md|C.md", "C.md|D.md", "D.md|E.md"}
	if got := bridgeList(cs); !slices.Equal(got, want) {
		t.Errorf("bridges = %v, want %v", got, want)
	}
}

// TestCriticalStructure_Cycle pins a cycle A->B->C->A: a 2-connected component
// has no cut vertex and no cut edge.
func TestCriticalStructure_Cycle(t *testing.T) {
	c := buildCorpus(t, doc("A.md", "a", nil), doc("B.md", "b", nil), doc("C.md", "c", nil))
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "C.md"), validRef("C.md", "A.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	cs := g.ComputeCriticalStructure()

	if len(cs.ArticulationPoints) != 0 {
		t.Errorf("articulation points = %v, want none", apList(cs))
	}
	if len(cs.Bridges) != 0 {
		t.Errorf("bridges = %v, want none", bridgeList(cs))
	}
}

// TestCriticalStructure_TrianglesSharingVertex pins two triangles sharing the
// single vertex X: X is the only cut vertex, and there are NO bridges (each
// triangle is 2-edge-connected internally). Triangle 1 = {X,A,B}, triangle 2 =
// {X,C,D}.
func TestCriticalStructure_TrianglesSharingVertex(t *testing.T) {
	c := buildCorpus(t,
		doc("X.md", "x", nil), doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil),
	)
	refs := []reference.Reference{
		// Triangle X-A-B (cycle so each edge is in a cycle).
		validRef("X.md", "A.md"), validRef("A.md", "B.md"), validRef("B.md", "X.md"),
		// Triangle X-C-D.
		validRef("X.md", "C.md"), validRef("C.md", "D.md"), validRef("D.md", "X.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	cs := g.ComputeCriticalStructure()

	if got, want := apList(cs), []string{"X.md"}; !slices.Equal(got, want) {
		t.Errorf("articulation points = %v, want %v", got, want)
	}
	if len(cs.Bridges) != 0 {
		t.Errorf("bridges = %v, want none", bridgeList(cs))
	}
}

// TestCriticalStructure_TrianglesJoinedByEdge pins two triangles joined by the
// single edge B-C: both B and C are cut vertices and the B-C edge is the sole
// bridge. Triangle 1 = {A,B,X}, triangle 2 = {C,D,Y}, joined B->C.
func TestCriticalStructure_TrianglesJoinedByEdge(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil), doc("X.md", "x", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil), doc("Y.md", "y", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "X.md"), validRef("X.md", "A.md"),
		validRef("C.md", "D.md"), validRef("D.md", "Y.md"), validRef("Y.md", "C.md"),
		validRef("B.md", "C.md"), // the join
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	cs := g.ComputeCriticalStructure()

	if got, want := apList(cs), []string{"B.md", "C.md"}; !slices.Equal(got, want) {
		t.Errorf("articulation points = %v, want %v", got, want)
	}
	if got, want := bridgeList(cs), []string{"B.md|C.md"}; !slices.Equal(got, want) {
		t.Errorf("bridges = %v, want %v", got, want)
	}
}

// TestCriticalStructure_Forest pins two disconnected components: the union of
// each component's critical structure. Component 1 is a path A-B-C (B is a cut
// vertex; A-B and B-C are bridges); component 2 is a single edge P-Q (one bridge,
// no articulation). The DFS forest driver handles both.
func TestCriticalStructure_Forest(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil), doc("C.md", "c", nil),
		doc("P.md", "p", nil), doc("Q.md", "q", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "C.md"),
		validRef("P.md", "Q.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	cs := g.ComputeCriticalStructure()

	if got, want := apList(cs), []string{"B.md"}; !slices.Equal(got, want) {
		t.Errorf("articulation points = %v, want %v", got, want)
	}
	want := []string{"A.md|B.md", "B.md|C.md", "P.md|Q.md"}
	if got := bridgeList(cs); !slices.Equal(got, want) {
		t.Errorf("bridges = %v, want %v", got, want)
	}
}

// TestCriticalStructure_RootWithTwoChildren pins the DFS-root special case: a
// star centered on the sorted-FIRST document A linking to B, C, D. As the DFS
// root, A has >=2 children, so it IS an articulation point; every spoke is a
// bridge.
func TestCriticalStructure_RootWithTwoChildren(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("A.md", "C.md"), validRef("A.md", "D.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	cs := g.ComputeCriticalStructure()

	if got, want := apList(cs), []string{"A.md"}; !slices.Equal(got, want) {
		t.Errorf("articulation points = %v, want %v", got, want)
	}
	want := []string{"A.md|B.md", "A.md|C.md", "A.md|D.md"}
	if got := bridgeList(cs); !slices.Equal(got, want) {
		t.Errorf("bridges = %v, want %v", got, want)
	}
}

// TestCriticalStructure_RootWithOneChild pins the complementary root case: a
// path B-A-... where the sorted-first driver is still a leaf is covered by the
// path test; here we confirm a root with a single subtree (a cycle hanging off
// the root) is NOT an articulation point. A-B-C-A is a cycle; A is the root with
// one child and no cut.
func TestCriticalStructure_RootWithOneChild(t *testing.T) {
	c := buildCorpus(t, doc("A.md", "a", nil), doc("B.md", "b", nil), doc("C.md", "c", nil))
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "C.md"), validRef("C.md", "A.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	cs := g.ComputeCriticalStructure()
	if slices.Contains(cs.ArticulationPoints, identity.DocumentID("A.md")) {
		t.Errorf("root A.md must NOT be an articulation point (single subtree)")
	}
}

// TestCriticalStructure_Trivial pins the trivial corpora: empty, single node,
// and a 2-node A-B edge (exactly one bridge, no articulation). A self-loop is
// stripped by the closure, so a single node with a self-reference is still none.
func TestCriticalStructure_Trivial(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		g := BuildReferenceGraph(buildCorpus(t), nil, BuildOptions{})
		cs := g.ComputeCriticalStructure()
		if len(cs.ArticulationPoints) != 0 || len(cs.Bridges) != 0 {
			t.Errorf("empty: got %v / %v, want none", apList(cs), bridgeList(cs))
		}
	})
	t.Run("single", func(t *testing.T) {
		g := BuildReferenceGraph(buildCorpus(t, doc("A.md", "a", nil)), nil, BuildOptions{})
		cs := g.ComputeCriticalStructure()
		if len(cs.ArticulationPoints) != 0 || len(cs.Bridges) != 0 {
			t.Errorf("single: got %v / %v, want none", apList(cs), bridgeList(cs))
		}
	})
	t.Run("two-node", func(t *testing.T) {
		c := buildCorpus(t, doc("A.md", "a", nil), doc("B.md", "b", nil))
		g := BuildReferenceGraph(c, []reference.Reference{validRef("A.md", "B.md")}, BuildOptions{})
		cs := g.ComputeCriticalStructure()
		if len(cs.ArticulationPoints) != 0 {
			t.Errorf("two-node articulation = %v, want none", apList(cs))
		}
		if got, want := bridgeList(cs), []string{"A.md|B.md"}; !slices.Equal(got, want) {
			t.Errorf("two-node bridges = %v, want %v", got, want)
		}
	})
	t.Run("self-loop", func(t *testing.T) {
		// A self-reference is stripped by buildProjection (no self-loops), so a
		// single node with one stays articulation/bridge-free.
		c := buildCorpus(t, doc("A.md", "a", nil))
		g := BuildReferenceGraph(c, []reference.Reference{validRef("A.md", "A.md")}, BuildOptions{})
		cs := g.ComputeCriticalStructure()
		if len(cs.ArticulationPoints) != 0 || len(cs.Bridges) != 0 {
			t.Errorf("self-loop: got %v / %v, want none", apList(cs), bridgeList(cs))
		}
	})
}

// TestCriticalStructure_Deterministic asserts the sorted output is independent
// of the input reference order (sorted forest driver + sorted neighbour visit +
// sorted outputs, ADR 0007).
func TestCriticalStructure_Deterministic(t *testing.T) {
	docs := []*corpus.Document{
		doc("A.md", "a", nil), doc("B.md", "b", nil), doc("X.md", "x", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil), doc("Y.md", "y", nil),
	}
	ordered := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "X.md"), validRef("X.md", "A.md"),
		validRef("C.md", "D.md"), validRef("D.md", "Y.md"), validRef("Y.md", "C.md"),
		validRef("B.md", "C.md"),
	}
	shuffled := []reference.Reference{
		validRef("B.md", "C.md"), validRef("Y.md", "C.md"), validRef("A.md", "B.md"),
		validRef("D.md", "Y.md"), validRef("X.md", "A.md"), validRef("C.md", "D.md"),
		validRef("B.md", "X.md"),
	}
	cs1 := BuildReferenceGraph(buildCorpus(t, docs...), ordered, BuildOptions{}).ComputeCriticalStructure()
	cs2 := BuildReferenceGraph(buildCorpus(t, docs...), shuffled, BuildOptions{}).ComputeCriticalStructure()
	if !slices.Equal(apList(cs1), apList(cs2)) {
		t.Errorf("articulation order differs: %v vs %v", apList(cs1), apList(cs2))
	}
	if !slices.Equal(bridgeList(cs1), bridgeList(cs2)) {
		t.Errorf("bridge order differs: %v vs %v", bridgeList(cs1), bridgeList(cs2))
	}
}
