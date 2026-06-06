package graphmodel

import (
	"math"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// betEps is the float tolerance for betweenness comparisons. Scores are small
// rationals (raw integer dependencies over an integer denominator), so 1e-9 is
// comfortably tight.
const betEps = 1e-9

func nearlyBet(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > betEps {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

// TestBetweenness_DirectedPath pins the directed path A->B->C->D->E (N=5) by
// hand. Raw Brandes dependencies: B lies on the 3 paths A->{C,D,E}; C on the 4
// paths {A,B}->{D,E}; D on the 3 paths {A,B,C}->E; A and E on none. Directed
// normalization divides by (N-1)(N-2) = 12 (NO halving).
func TestBetweenness_DirectedPath(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil), doc("C.md", "c", nil),
		doc("D.md", "d", nil), doc("E.md", "e", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "C.md"),
		validRef("C.md", "D.md"), validRef("D.md", "E.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	b := g.ComputeBetweenness()

	const denom = float64((5 - 1) * (5 - 2)) // 12
	nearlyBet(t, "A", b.Score("A.md"), 0)
	nearlyBet(t, "B", b.Score("B.md"), 3/denom)
	nearlyBet(t, "C", b.Score("C.md"), 4/denom)
	nearlyBet(t, "D", b.Score("D.md"), 3/denom)
	nearlyBet(t, "E", b.Score("E.md"), 0)
}

// TestBetweenness_OutStar pins the directed out-star H->{L1..L4} (N=5): no node
// is strictly BETWEEN two others (the hub only emits, never relays), so every
// betweenness score is 0.
func TestBetweenness_OutStar(t *testing.T) {
	c := buildCorpus(t,
		doc("H.md", "h", nil), doc("L1.md", "l1", nil), doc("L2.md", "l2", nil),
		doc("L3.md", "l3", nil), doc("L4.md", "l4", nil),
	)
	refs := []reference.Reference{
		validRef("H.md", "L1.md"), validRef("H.md", "L2.md"),
		validRef("H.md", "L3.md"), validRef("H.md", "L4.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	b := g.ComputeBetweenness()

	for _, id := range g.documents {
		nearlyBet(t, "out-star "+id.String(), b.Score(id), 0)
	}
}

// TestBetweenness_MutualStar pins the mutual-link star L_i<->H (N=5): every leaf
// pair's shortest path goes through H, in both directions, so H has the maximum
// score and every leaf is 0. With 4 leaves there are 4*3 = 12 ordered leaf
// pairs, each routed solely through H, so raw cb[H] = 12, normalized by
// (N-1)(N-2) = 12 -> exactly 1.
func TestBetweenness_MutualStar(t *testing.T) {
	c := buildCorpus(t,
		doc("H.md", "h", nil), doc("L1.md", "l1", nil), doc("L2.md", "l2", nil),
		doc("L3.md", "l3", nil), doc("L4.md", "l4", nil),
	)
	refs := []reference.Reference{
		validRef("H.md", "L1.md"), validRef("L1.md", "H.md"),
		validRef("H.md", "L2.md"), validRef("L2.md", "H.md"),
		validRef("H.md", "L3.md"), validRef("L3.md", "H.md"),
		validRef("H.md", "L4.md"), validRef("L4.md", "H.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	b := g.ComputeBetweenness()

	nearlyBet(t, "H", b.Score("H.md"), 1)
	for _, leaf := range []identity.DocumentID{"L1.md", "L2.md", "L3.md", "L4.md"} {
		nearlyBet(t, "leaf "+leaf.String(), b.Score(leaf), 0)
	}
	// H must be the unique maximum.
	top := b.TopBetweenness(1)
	if len(top) != 1 || top[0].ID != "H.md" {
		t.Errorf("TopBetweenness(1) = %+v, want H.md", top)
	}
}

// TestBetweenness_DirectedCycle pins the directed 3-cycle A->B->C->A: each node
// lies on exactly one shortest path between the other two (e.g. B is between A
// and C), so raw cb = 1 for each, normalized by (N-1)(N-2) = 2 -> 0.5 each.
func TestBetweenness_DirectedCycle(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil), doc("C.md", "c", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "C.md"), validRef("C.md", "A.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	b := g.ComputeBetweenness()

	for _, id := range []identity.DocumentID{"A.md", "B.md", "C.md"} {
		nearlyBet(t, "cycle "+id.String(), b.Score(id), 0.5)
	}
}

// TestBetweenness_TooFewNodes pins the n<3 edge cases: empty, single, and
// two-node corpora all yield all-zero scores (no vertex can lie between two
// others).
func TestBetweenness_TooFewNodes(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		g := BuildReferenceGraph(buildCorpus(t), nil, BuildOptions{})
		if got := g.ComputeBetweenness().Score("x.md"); got != 0 {
			t.Errorf("empty score = %v, want 0", got)
		}
	})
	t.Run("single", func(t *testing.T) {
		g := BuildReferenceGraph(buildCorpus(t, doc("A.md", "a", nil)), nil, BuildOptions{})
		nearlyBet(t, "single A", g.ComputeBetweenness().Score("A.md"), 0)
	})
	t.Run("two-node", func(t *testing.T) {
		c := buildCorpus(t, doc("A.md", "a", nil), doc("B.md", "b", nil))
		g := BuildReferenceGraph(c, []reference.Reference{validRef("A.md", "B.md")}, BuildOptions{})
		b := g.ComputeBetweenness()
		nearlyBet(t, "two-node A", b.Score("A.md"), 0)
		nearlyBet(t, "two-node B", b.Score("B.md"), 0)
	})
}

// TestBetweenness_Deterministic asserts the scores and the TopBetweenness
// ranking are independent of the input reference order (sorted iteration +
// fixed-order float accumulation, ADR 0007).
func TestBetweenness_Deterministic(t *testing.T) {
	docs := []*corpus.Document{
		doc("A.md", "a", nil), doc("B.md", "b", nil), doc("C.md", "c", nil),
		doc("D.md", "d", nil), doc("E.md", "e", nil),
	}
	forward := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "C.md"),
		validRef("C.md", "D.md"), validRef("D.md", "E.md"),
	}
	shuffled := []reference.Reference{
		validRef("D.md", "E.md"), validRef("A.md", "B.md"),
		validRef("C.md", "D.md"), validRef("B.md", "C.md"),
	}
	g1 := BuildReferenceGraph(buildCorpus(t, docs...), forward, BuildOptions{})
	g2 := BuildReferenceGraph(buildCorpus(t, docs...), shuffled, BuildOptions{})
	b1, b2 := g1.ComputeBetweenness(), g2.ComputeBetweenness()
	for _, id := range g1.documents {
		if b1.Score(id) != b2.Score(id) {
			t.Errorf("score %s differs: %v vs %v", id, b1.Score(id), b2.Score(id))
		}
	}
	t1, t2 := b1.TopBetweenness(0), b2.TopBetweenness(0)
	if len(t1) != len(t2) {
		t.Fatalf("TopBetweenness length differs: %d vs %d", len(t1), len(t2))
	}
	for i := range t1 {
		if t1[i] != t2[i] {
			t.Errorf("TopBetweenness[%d] differs: %+v vs %+v", i, t1[i], t2[i])
		}
	}
}
