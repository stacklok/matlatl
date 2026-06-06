package graphmodel

import (
	"slices"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// collectBFS runs ForEachSourceBFS over projAdj and copies the per-source order,
// preds and sigma out of the reused maps (the callback must not retain them).
type bfsSnapshot struct {
	order []identity.DocumentID
	preds map[identity.DocumentID][]identity.DocumentID
	sigma map[identity.DocumentID]float64
}

func collectBFS(g *ReferenceGraph) map[identity.DocumentID]bfsSnapshot {
	out := map[identity.DocumentID]bfsSnapshot{}
	g.ForEachSourceBFS(g.projAdj, func(
		s identity.DocumentID,
		order []identity.DocumentID,
		preds map[identity.DocumentID][]identity.DocumentID,
		sigma map[identity.DocumentID]float64,
	) {
		snap := bfsSnapshot{
			order: slices.Clone(order),
			preds: map[identity.DocumentID][]identity.DocumentID{},
			sigma: map[identity.DocumentID]float64{},
		}
		for k, v := range preds {
			snap.preds[k] = slices.Clone(v)
		}
		for k, v := range sigma {
			snap.sigma[k] = v
		}
		out[s] = snap
	})
	return out
}

// TestForEachSourceBFS_Path pins the forward pass on the path A->B->C->D: the
// BFS push order from A is [A,B,C,D], every sigma is 1 (one shortest path), and
// preds[D] = [C].
func TestForEachSourceBFS_Path(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("B.md", "C.md"), validRef("C.md", "D.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	snaps := collectBFS(g)

	a := snaps["A.md"]
	if got, want := a.order, []identity.DocumentID{"A.md", "B.md", "C.md", "D.md"}; !slices.Equal(got, want) {
		t.Errorf("order from A = %v, want %v", got, want)
	}
	for _, id := range []identity.DocumentID{"A.md", "B.md", "C.md", "D.md"} {
		if a.sigma[id] != 1 {
			t.Errorf("sigma[%s] = %v, want 1", id, a.sigma[id])
		}
	}
	if got, want := a.preds["D.md"], []identity.DocumentID{"C.md"}; !slices.Equal(got, want) {
		t.Errorf("preds[D] = %v, want %v", got, want)
	}
}

// TestForEachSourceBFS_Diamond pins the diamond A->{B,C}->D: there are two
// shortest paths A..D so sigma[D] = 2, and preds[D] = [B,C] in SORTED order
// (sorted-neighbour expansion ⇒ deterministic predecessor order).
func TestForEachSourceBFS_Diamond(t *testing.T) {
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "B.md"), validRef("A.md", "C.md"),
		validRef("B.md", "D.md"), validRef("C.md", "D.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	a := collectBFS(g)["A.md"]

	if a.sigma["D.md"] != 2 {
		t.Errorf("sigma[D] = %v, want 2", a.sigma["D.md"])
	}
	if got, want := a.preds["D.md"], []identity.DocumentID{"B.md", "C.md"}; !slices.Equal(got, want) {
		t.Errorf("preds[D] = %v, want %v (sorted)", got, want)
	}
}

// TestForEachSourceBFS_Deterministic asserts the per-source order/preds/sigma are
// independent of input reference order.
func TestForEachSourceBFS_Deterministic(t *testing.T) {
	docs := []*corpus.Document{
		doc("A.md", "a", nil), doc("B.md", "b", nil),
		doc("C.md", "c", nil), doc("D.md", "d", nil),
	}
	ordered := []reference.Reference{
		validRef("A.md", "B.md"), validRef("A.md", "C.md"),
		validRef("B.md", "D.md"), validRef("C.md", "D.md"),
	}
	shuffled := []reference.Reference{
		validRef("C.md", "D.md"), validRef("A.md", "C.md"),
		validRef("B.md", "D.md"), validRef("A.md", "B.md"),
	}
	s1 := collectBFS(BuildReferenceGraph(buildCorpus(t, docs...), ordered, BuildOptions{}))
	s2 := collectBFS(BuildReferenceGraph(buildCorpus(t, docs...), shuffled, BuildOptions{}))
	for _, src := range []identity.DocumentID{"A.md", "B.md", "C.md", "D.md"} {
		if !slices.Equal(s1[src].order, s2[src].order) {
			t.Errorf("order from %s differs: %v vs %v", src, s1[src].order, s2[src].order)
		}
		for _, dst := range []identity.DocumentID{"A.md", "B.md", "C.md", "D.md"} {
			if !slices.Equal(s1[src].preds[dst], s2[src].preds[dst]) {
				t.Errorf("preds[%s] from %s differs", dst, src)
			}
			if s1[src].sigma[dst] != s2[src].sigma[dst] {
				t.Errorf("sigma[%s] from %s differs", dst, src)
			}
		}
	}
}
