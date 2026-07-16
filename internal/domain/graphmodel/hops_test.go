package graphmodel

import (
	"slices"
	"testing"
)

// TestHops_MultiRootNearestWins seeds two convention roots (README.md, index.md)
// and asserts every document gets its distance from the NEAREST root, not the
// first-seeded one: c.md is 3 hops down the README chain but 1 hop from index.md,
// so its distance is 1.
func TestHops_MultiRootNearestWins(t *testing.T) {
	c := buildCorpus(t,
		doc("README.md", "r", nil),
		doc("index.md", "i", nil),
		doc("a.md", "a", nil),
		doc("b.md", "b", nil),
		doc("c.md", "cc", nil),
		doc("far.md", "f", nil),
		doc("unreach.md", "u", nil),
	)
	refs := linkRefs(
		[2]string{"README.md", "a.md"}, // a = 1
		[2]string{"a.md", "b.md"},      // b = 2
		[2]string{"b.md", "c.md"},      // c via README = 3
		[2]string{"index.md", "c.md"},  // c via index = 1  ← nearest wins
		[2]string{"c.md", "far.md"},    // far = 2
	)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	h := g.ComputeHopsFromRoot(c, ResolveRootSet(c, nil), 6)

	if h.Indeterminate {
		t.Fatal("Indeterminate = true, want false (README.md/index.md are roots)")
	}
	want := map[string]int{"README.md": 0, "index.md": 0, "a.md": 1, "b.md": 2, "c.md": 1, "far.md": 2}
	for id, wd := range want {
		got, ok := h.Distance(docID(id))
		if !ok {
			t.Errorf("%s: not reached, want distance %d", id, wd)
			continue
		}
		if got != wd {
			t.Errorf("%s: distance = %d, want %d", id, got, wd)
		}
	}
	// unreach.md has no inbound edge → absent from the distance map (unreachable).
	if d, ok := h.Distance(docID("unreach.md")); ok {
		t.Errorf("unreach.md: reached at distance %d, want absent (unreachable)", d)
	}
}

// TestHops_UnreachableAbsent confirms a document with no path from any root is
// simply absent from Dist (the sentinel the emit layer renders as -1), never a
// zero-distance false positive.
func TestHops_UnreachableAbsent(t *testing.T) {
	c := buildCorpus(t,
		doc("README.md", "r", nil),
		doc("reached.md", "re", nil),
		doc("island.md", "is", nil),
	)
	refs := linkRefs([2]string{"README.md", "reached.md"})
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	h := g.ComputeHopsFromRoot(c, ResolveRootSet(c, nil), 6)

	if _, ok := h.Distance(docID("reached.md")); !ok {
		t.Error("reached.md: absent, want reached")
	}
	if _, ok := h.Distance(docID("island.md")); ok {
		t.Error("island.md: reached, want absent (unreachable)")
	}
}

// TestHops_CycleTerminates proves a cycle in the projection does not loop
// forever and yields finite BFS distances (each node visited once).
func TestHops_CycleTerminates(t *testing.T) {
	c := buildCorpus(t,
		doc("README.md", "r", nil),
		doc("a.md", "a", nil),
		doc("b.md", "b", nil),
	)
	refs := linkRefs(
		[2]string{"README.md", "a.md"},
		[2]string{"a.md", "b.md"},
		[2]string{"b.md", "a.md"}, // cycle a <-> b
	)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	h := g.ComputeHopsFromRoot(c, ResolveRootSet(c, nil), 6)

	if d, _ := h.Distance(docID("a.md")); d != 1 {
		t.Errorf("a.md distance = %d, want 1", d)
	}
	if d, _ := h.Distance(docID("b.md")); d != 2 {
		t.Errorf("b.md distance = %d, want 2", d)
	}
}

// TestHops_EmptyCorpus: an empty corpus has no roots, so the result is
// indeterminate with empty distance map and far list (no panic).
func TestHops_EmptyCorpus(t *testing.T) {
	c := buildCorpus(t)
	g := BuildReferenceGraph(c, nil, BuildOptions{})
	h := g.ComputeHopsFromRoot(c, ResolveRootSet(c, nil), 6)

	if !h.Indeterminate {
		t.Error("Indeterminate = false, want true (empty corpus has no roots)")
	}
	if len(h.dist) != 0 || len(h.FarFromRoot) != 0 {
		t.Errorf("Dist=%d FarFromRoot=%d, want both empty", len(h.dist), len(h.FarFromRoot))
	}
}

// TestHops_SingleRootNode: a lone root has distance 0 to itself and nothing is
// far.
func TestHops_SingleRootNode(t *testing.T) {
	c := buildCorpus(t, doc("README.md", "r", nil))
	g := BuildReferenceGraph(c, nil, BuildOptions{})
	h := g.ComputeHopsFromRoot(c, ResolveRootSet(c, nil), 6)

	if h.Indeterminate {
		t.Fatal("Indeterminate = true, want false (README.md is a root)")
	}
	if d, ok := h.Distance(docID("README.md")); !ok || d != 0 {
		t.Errorf("README.md distance = (%d,%v), want (0,true)", d, ok)
	}
	if len(h.FarFromRoot) != 0 {
		t.Errorf("FarFromRoot = %v, want empty", ids(h.FarFromRoot))
	}
}

// TestHops_IndeterminateRootSet: no root convention/glob matches → indeterminate,
// nothing computed (mirrors reachability, ADR 0007/0021).
func TestHops_IndeterminateRootSet(t *testing.T) {
	c := buildCorpus(t,
		doc("a.md", "a", nil),
		doc("b.md", "b", nil),
	)
	refs := linkRefs([2]string{"a.md", "b.md"})
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	rs := ResolveRootSet(c, nil)
	if !rs.Indeterminate {
		t.Fatal("precondition: root set should be indeterminate (no README/index/type:index)")
	}
	h := g.ComputeHopsFromRoot(c, rs, 6)
	if !h.Indeterminate {
		t.Error("Indeterminate = false, want true")
	}
	if len(h.dist) != 0 || len(h.FarFromRoot) != 0 {
		t.Errorf("Dist=%d FarFromRoot=%d, want both empty", len(h.dist), len(h.FarFromRoot))
	}
}

// TestHops_ThresholdBoundary: with threshold 3, the doc at exactly 3 hops is
// flagged and the doc at 2 hops is not (>= threshold, not > threshold).
func TestHops_ThresholdBoundary(t *testing.T) {
	c := buildCorpus(t,
		doc("README.md", "r", nil),
		doc("d1.md", "d1", nil),
		doc("d2.md", "d2", nil),
		doc("d3.md", "d3", nil),
		doc("d4.md", "d4", nil),
	)
	refs := linkRefs(
		[2]string{"README.md", "d1.md"}, // 1
		[2]string{"d1.md", "d2.md"},     // 2
		[2]string{"d2.md", "d3.md"},     // 3 ← flagged
		[2]string{"d3.md", "d4.md"},     // 4 ← flagged
	)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	h := g.ComputeHopsFromRoot(c, ResolveRootSet(c, nil), 3)

	if h.Threshold != 3 {
		t.Errorf("Threshold = %d, want 3", h.Threshold)
	}
	want := []string{"d3.md", "d4.md"}
	if got := ids(h.FarFromRoot); !slices.Equal(got, want) {
		t.Errorf("FarFromRoot = %v, want %v", got, want)
	}
}

// TestHops_Exemptions: a far doc that is a root member, an intentional orphan, or
// unreachable is NEVER flagged; only a plain far doc is. Also checks a <=0
// threshold is normalized to the default.
func TestHops_Exemptions(t *testing.T) {
	c := buildCorpus(t,
		doc("README.md", "r", nil),
		doc("near.md", "n", nil),
		doc("deep.md", "d", nil),
		doc("intentional.md", "i", map[string]any{"matlatl": "orphan-intentional"}),
		doc("unreach.md", "u", nil),
	)
	refs := linkRefs(
		[2]string{"README.md", "near.md"},      // 1
		[2]string{"near.md", "deep.md"},        // 2 ← far, plain → flagged
		[2]string{"near.md", "intentional.md"}, // 2 ← far but orphan-intentional → exempt
	)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	h := g.ComputeHopsFromRoot(c, ResolveRootSet(c, nil), 2)

	want := []string{"deep.md"}
	if got := ids(h.FarFromRoot); !slices.Equal(got, want) {
		t.Errorf("FarFromRoot = %v, want %v (intentional/unreachable excluded)", got, want)
	}
	// README.md is absent from the far list because it is a root at distance 0
	// (0 < threshold), NOT because the exemption fired: a root can never reach the
	// >= threshold branch, so structureExemptSet's root membership is a DEFENSIVE
	// guard here, not a code path this test can exercise. unreach.md is absent
	// because it is unreachable (never entered the distance map).
	if slices.Contains(ids(h.FarFromRoot), "README.md") {
		t.Error("README.md unexpectedly flagged far (a root is distance 0)")
	}

	// A <=0 threshold normalizes up to the default.
	hd := g.ComputeHopsFromRoot(c, ResolveRootSet(c, nil), 0)
	if hd.Threshold != DefaultFarFromRootThreshold {
		t.Errorf("normalized threshold = %d, want %d", hd.Threshold, DefaultFarFromRootThreshold)
	}
}
