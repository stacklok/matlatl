package graphmodel

import (
	"slices"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// trailsOf is a small helper running the full pipeline ComputeTrails wiring.
func trailsOf(g *ReferenceGraph) []Trail {
	pr := g.ComputePageRank(PageRankOptions{})
	wcc := g.WeaklyConnectedComponents()
	scc := g.StronglyConnectedComponents()
	return ComputeTrails(pr, wcc, scc, func() (map[identity.DocumentID]identity.DocumentID, map[identity.DocumentID][]identity.DocumentID) {
		return g.Condensation(scc)
	})
}

func trailRoots(trails []Trail) []string {
	out := make([]string, len(trails))
	for i, t := range trails {
		out[i] = t.Root.String()
	}
	return out
}

// TestTrails_Singleton: a lone document is a one-element trail rooted at itself.
func TestTrails_Singleton(t *testing.T) {
	g := BuildReferenceGraph(buildCorpus(t, doc("A.md", "a", nil)), nil, BuildOptions{})
	trails := trailsOf(g)
	if len(trails) != 1 {
		t.Fatalf("want 1 trail, got %d", len(trails))
	}
	if trails[0].Root != "A.md" || !slices.Equal(ids(trails[0].Order), []string{"A.md"}) {
		t.Errorf("singleton trail = %+v, want root A.md order [A.md]", trails[0])
	}
}

// TestTrails_TwoComponents: two disconnected clusters yield two trails, sorted by
// root, each containing only its own members.
func TestTrails_TwoComponents(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("X.md", "x", nil), doc("Y.md", "y", nil),
	)
	refs := []reference.Reference{validRef("A.md", "B.md"), validRef("X.md", "Y.md")}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	trails := trailsOf(g)
	if len(trails) != 2 {
		t.Fatalf("want 2 trails, got %d: %+v", len(trails), trails)
	}
	// Roots are sorted; both components have a clear root (the higher-PageRank
	// sink B / Y, since A->B and X->Y push mass downstream).
	roots := trailRoots(trails)
	if !slices.IsSorted(roots) {
		t.Errorf("trails not sorted by root: %v", roots)
	}
	// Each trail's members are exactly its own component.
	for _, tr := range trails {
		got := ids(tr.Order)
		slices.Sort(got)
		switch tr.Root {
		case "B.md":
			if !slices.Equal(got, []string{"A.md", "B.md"}) {
				t.Errorf("B-trail members = %v, want [A.md B.md]", got)
			}
		case "Y.md":
			if !slices.Equal(got, []string{"X.md", "Y.md"}) {
				t.Errorf("Y-trail members = %v, want [X.md Y.md]", got)
			}
		default:
			t.Errorf("unexpected trail root %q", tr.Root)
		}
	}
}

// TestTrails_DAGPrefersPageRank: on a small DAG with a branching frontier, the
// order is topologically valid (a prerequisite never appears after its dependent)
// AND, when two SCCs are simultaneously available, the higher-PageRank one is
// emitted first.
func TestTrails_DAGPrefersPageRank(t *testing.T) {
	// Root R links to B and C; B and C both link to D. After R, the frontier is
	// {B, C}. D depends on both, so it is last. Among {B,C} the higher-PageRank
	// one comes first. C also receives an extra in-link from an external feeder F
	// (F->C), so C accrues more PageRank than B and must be emitted before B.
	c := buildCorpus(t,
		doc("R.md", "r", nil), doc("B.md", "b", nil), doc("C.md", "c", nil),
		doc("D.md", "d", nil), doc("F.md", "f", nil),
	)
	refs := []reference.Reference{
		validRef("R.md", "B.md"), validRef("R.md", "C.md"),
		validRef("B.md", "D.md"), validRef("C.md", "D.md"),
		validRef("F.md", "C.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	trails := trailsOf(g)
	if len(trails) != 1 {
		t.Fatalf("want 1 trail (single weak component), got %d", len(trails))
	}
	order := ids(trails[0].Order)
	pos := map[string]int{}
	for i, id := range order {
		pos[id] = i
	}
	// Topological validity: every prerequisite precedes its dependents.
	for _, edge := range [][2]string{{"R.md", "B.md"}, {"R.md", "C.md"}, {"B.md", "D.md"}, {"C.md", "D.md"}, {"F.md", "C.md"}} {
		if pos[edge[0]] >= pos[edge[1]] {
			t.Errorf("topological order violated: %s must precede %s in %v", edge[0], edge[1], order)
		}
	}
	// PageRank preference: C (extra inbound from F) before B once both are available.
	if pos["C.md"] >= pos["B.md"] {
		t.Errorf("expected higher-PageRank C before B: order=%v", order)
	}
}

// TestTrails_CycleMemberOrdering: a multi-node SCC (a cycle) is emitted as one
// frontier unit, its members ordered by PageRank DESC then ID ASC. A 2-cycle
// A<->B is symmetric, so members tie on PageRank and fall back to ID order.
func TestTrails_CycleMemberOrdering(t *testing.T) {
	c := buildCorpus(t, doc("A.md", "a", nil), doc("B.md", "b", nil))
	refs := []reference.Reference{validRef("A.md", "B.md"), validRef("B.md", "A.md")}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	trails := trailsOf(g)
	if len(trails) != 1 {
		t.Fatalf("want 1 trail, got %d", len(trails))
	}
	// Symmetric cycle: PageRank ties, so member order is ID-ascending.
	if !slices.Equal(ids(trails[0].Order), []string{"A.md", "B.md"}) {
		t.Errorf("cycle order = %v, want [A.md B.md] (PageRank tie → ID asc)", ids(trails[0].Order))
	}
}

// TestTrails_MultiNodeSCCMemberOrderByPageRank: a 3-node cycle A→B→C→A is one
// SCC; an external feeder F→B gives B distinctly higher PageRank than A/C. The
// SCC's members are emitted by PageRank DESC, so B must LEAD the SCC's slice in
// Order — exercising the PageRank-DESC primary key in sortByPageRankDesc (which
// the symmetric 2-cycle above never reaches, since its members tie).
func TestTrails_MultiNodeSCCMemberOrderByPageRank(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil), doc("C.md", "c", nil), doc("F.md", "f", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "C.md"), validRef("C.md", "A.md"), // the 3-cycle
		validRef("F.md", "B.md"), // feeder → B accrues extra PageRank
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	pr := g.ComputePageRank(PageRankOptions{})
	// Sanity: B is the highest-PageRank cycle member (the feeder's mass lands on it).
	if pr.Score("B.md") <= pr.Score("A.md") || pr.Score("B.md") <= pr.Score("C.md") {
		t.Fatalf("expected B to have the highest PageRank among the cycle; got A=%v B=%v C=%v",
			pr.Score("A.md"), pr.Score("B.md"), pr.Score("C.md"))
	}

	trails := trailsOf(g)
	// One weak component containing all four docs.
	if len(trails) != 1 {
		t.Fatalf("want 1 trail, got %d: %+v", len(trails), trails)
	}
	order := ids(trails[0].Order)
	// F is the only zero-in-degree SCC, so it is read first; the cycle SCC follows
	// as one unit. Within that unit, B (highest PageRank) must come before A and C.
	posB, posA, posC := indexOf(order, "B.md"), indexOf(order, "A.md"), indexOf(order, "C.md")
	if posB < 0 || posA < 0 || posC < 0 {
		t.Fatalf("missing cycle members in order %v", order)
	}
	if posB > posA || posB > posC {
		t.Errorf("highest-PageRank cycle member B must lead the SCC slice; order=%v", order)
	}
}

func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return -1
}

// TestTrails_Deterministic asserts trails are identical regardless of input
// reference order.
func TestTrails_Deterministic(t *testing.T) {
	docs := []*corpus.Document{
		doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil),
	}
	forward := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "C.md"),
		validRef("A.md", "D.md"), validRef("D.md", "C.md"),
	}
	shuffled := []reference.Reference{forward[3], forward[1], forward[0], forward[2]}
	t1 := trailsOf(BuildReferenceGraph(buildCorpus(t, docs...), forward, BuildOptions{}))
	t2 := trailsOf(BuildReferenceGraph(buildCorpus(t, docs...), shuffled, BuildOptions{}))
	if len(t1) != len(t2) {
		t.Fatalf("trail count differs: %d vs %d", len(t1), len(t2))
	}
	for i := range t1 {
		if t1[i].Root != t2[i].Root || !slices.Equal(ids(t1[i].Order), ids(t2[i].Order)) {
			t.Errorf("trail %d differs across input order:\n %+v\n %+v", i, t1[i], t2[i])
		}
	}
}
