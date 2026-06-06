package graphmodel

import (
	"math"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// prEps is the float tolerance for PageRank comparisons. The power iteration
// converges to ~1e-6 (the L1 threshold), so 1e-4 is comfortably loose for
// hand-computed checks and tight enough to catch a wrong formula.
const prEps = 1e-4

func nearlyPR(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > prEps {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

// TestPageRank_Empty: an empty graph converges trivially with no scores.
func TestPageRank_Empty(t *testing.T) {
	g := BuildReferenceGraph(buildCorpus(t), nil, BuildOptions{})
	pr := g.ComputePageRank(PageRankOptions{})
	if !pr.Converged {
		t.Errorf("empty graph should report Converged")
	}
	if len(pr.Top(0)) != 0 {
		t.Errorf("empty graph should have no ranked docs")
	}
}

// TestPageRank_SingleDoc: a one-document corpus scores 1.0.
func TestPageRank_SingleDoc(t *testing.T) {
	g := BuildReferenceGraph(buildCorpus(t, doc("A.md", "a", nil)), nil, BuildOptions{})
	pr := g.ComputePageRank(PageRankOptions{})
	nearlyPR(t, "A", pr.Score("A.md"), 1.0)
}

// TestPageRank_SumsToOne asserts Σ PR ≈ 1 even with a DANGLING node (a doc with
// no out-links). The dangling mass must be redistributed (not lost), so the
// total stays 1.
func TestPageRank_SumsToOne(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil), doc("C.md", "c", nil),
	)
	// A->B, B->C; C is a dangling sink (no out-links).
	refs := []reference.Reference{validRef("A.md", "B.md"), validRef("B.md", "C.md")}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	pr := g.ComputePageRank(PageRankOptions{})

	var sum float64
	for _, id := range g.Documents() {
		sum += pr.Score(id)
	}
	nearlyPR(t, "ΣPR", sum, 1.0)
}

// TestPageRank_OutDegreeDivisionApplied is the load-bearing distinction from
// HITS: the per-edge term divides by the SOURCE out-degree. Build a graph where
// a raw (HITS-style, no division) sum and a normalized (PageRank) sum give a
// DIFFERENT ranking, and assert PageRank produces the normalized answer.
//
// Graph: H is a hub linking out to X and Y (out-degree 2). X also links to Y
// (out-degree 1). So Y has two in-links: from H (which splits its mass over 2)
// and from X (which gives all its mass to Y). T is a lone doc linking to Y too
// via a long chain... Simpler concrete check: with division, H's contribution to
// each of X,Y is PR[H]/2; a node fed only by a SINGLE-out-degree source gets that
// source's full mass. We assert Y (fed by the undivided X) outranks X (fed only
// by the divided H), which would NOT hold under a raw HITS-style sum where H's
// undivided score would dominate.
func TestPageRank_OutDegreeDivisionApplied(t *testing.T) {
	c := buildCorpus(t,
		doc("H.md", "h", nil), doc("X.md", "x", nil), doc("Y.md", "y", nil),
	)
	refs := []reference.Reference{
		validRef("H.md", "X.md"), // H out-degree 2 → each gets PR[H]/2
		validRef("H.md", "Y.md"),
		validRef("X.md", "Y.md"), // X out-degree 1 → Y gets all of PR[X]
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	pr := g.ComputePageRank(PageRankOptions{})

	// Y receives H/2 + X (X feeds it undivided); X receives only H/2. So Y > X.
	if pr.Score("Y.md") <= pr.Score("X.md") {
		t.Errorf("expected Y (fed undivided by X) to outrank X (fed only by divided H): Y=%v X=%v",
			pr.Score("Y.md"), pr.Score("X.md"))
	}
	// Top is Y, then either H or X.
	top := pr.Top(1)
	if len(top) != 1 || top[0].ID != "Y.md" {
		t.Errorf("Top(1) = %+v, want Y.md", top)
	}
}

// TestPageRank_KnownTwoCycle pins a hand-computable case: a mutual link A<->B
// (each out-degree 1, no dangling). By symmetry both scores are 0.5.
func TestPageRank_KnownTwoCycle(t *testing.T) {
	c := buildCorpus(t, doc("A.md", "a", nil), doc("B.md", "b", nil))
	refs := []reference.Reference{validRef("A.md", "B.md"), validRef("B.md", "A.md")}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	pr := g.ComputePageRank(PageRankOptions{})
	nearlyPR(t, "A", pr.Score("A.md"), 0.5)
	nearlyPR(t, "B", pr.Score("B.md"), 0.5)
}

// TestPageRank_Deterministic asserts the scores are identical regardless of the
// input reference order (the determinism / sorted-float-sum contract).
func TestPageRank_Deterministic(t *testing.T) {
	docs := []*corpus.Document{
		doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil),
	}
	forward := []reference.Reference{
		validRef("A.md", "B.md"), validRef("A.md", "C.md"),
		validRef("B.md", "D.md"), validRef("C.md", "D.md"),
		validRef("D.md", "A.md"),
	}
	shuffled := []reference.Reference{
		forward[4], forward[0], forward[3], forward[1], forward[2],
	}
	g1 := BuildReferenceGraph(buildCorpus(t, docs...), forward, BuildOptions{})
	g2 := BuildReferenceGraph(buildCorpus(t, docs...), shuffled, BuildOptions{})
	pr1 := g1.ComputePageRank(PageRankOptions{})
	pr2 := g2.ComputePageRank(PageRankOptions{})
	for _, id := range g1.Documents() {
		if math.Abs(pr1.Score(id)-pr2.Score(id)) > 1e-12 {
			t.Errorf("%s: PageRank differs across input order: %v vs %v", id, pr1.Score(id), pr2.Score(id))
		}
	}
}
