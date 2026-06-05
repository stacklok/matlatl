package graphmodel

import (
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

// TestDetectGaps_Semantics locks the gap definition (ADR 0007): a gap is a pair
// of two DISTINCT weakly-connected components, each at or above MinComponentSize.
// Distinct WCCs have zero cross-links by construction, so every kept pair is a
// gap; there is no cross-link threshold. Components below MinComponentSize are
// dropped.
func TestDetectGaps_Semantics(t *testing.T) {
	// Two size-2 clusters (A<->A2, B<->B2) and one singleton (lonely). With
	// MinComponentSize:2 only the two size-2 clusters are kept → exactly one gap.
	c := buildCorpus(t,
		doc("A.md", "a", nil), doc("A2.md", "a2", nil),
		doc("B.md", "b", nil), doc("B2.md", "b2", nil),
		doc("lonely.md", "l", nil),
	)
	refs := []reference.Reference{
		validRef("A.md", "A2.md"),
		validRef("B.md", "B2.md"),
	}
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	wcc := g.WeaklyConnectedComponents()

	res := DetectGaps(wcc, GapOptions{MinComponentSize: 2})
	if res.Truncated {
		t.Fatal("unexpected truncation")
	}
	if len(res.Gaps) != 1 {
		t.Fatalf("gap count = %d, want 1 (the two size-2 clusters); singleton excluded: %+v", len(res.Gaps), res.Gaps)
	}
	gap := res.Gaps[0]
	if gap.ComponentA != "A.md" || gap.ComponentB != "B.md" {
		t.Errorf("gap = (%s,%s), want (A.md,B.md) sorted-min IDs", gap.ComponentA, gap.ComponentB)
	}
	if gap.RepresentativeA != "A.md" || gap.RepresentativeB != "B.md" {
		t.Errorf("representatives = (%s,%s), want (A.md,B.md)", gap.RepresentativeA, gap.RepresentativeB)
	}
}

// TestDetectGaps_ZeroValueDefaultsToMinSize2 asserts a zero-value GapOptions (and
// any MinComponentSize<2) is normalized to 2, so all-singleton input yields ZERO
// gaps — the safe default that keeps singletons as orphans only.
func TestDetectGaps_ZeroValueDefaultsToMinSize2(t *testing.T) {
	c := buildCorpus(t,
		doc("a.md", "a", nil), doc("b.md", "b", nil), doc("c.md", "c", nil),
	)
	g := BuildReferenceGraph(c, nil, BuildOptions{})
	wcc := g.WeaklyConnectedComponents()

	for _, opts := range []GapOptions{{}, {MinComponentSize: 0}, {MinComponentSize: 1}} {
		res := DetectGaps(wcc, opts)
		if len(res.Gaps) != 0 {
			t.Errorf("DetectGaps(%+v) over all-singleton input = %d gaps, want 0", opts, len(res.Gaps))
		}
		if res.Truncated {
			t.Errorf("DetectGaps(%+v) over all-singleton input truncated unexpectedly", opts)
		}
	}
}

// TestDetectGaps_DoSBounded is the must-fix regression. Two facets:
//
//  1. 1,500+ ISOLATED SINGLETON components with the pipeline default
//     (MinComponentSize:2) complete quickly and produce ZERO gaps — singletons
//     are orphans, never gaps, so the C(1500,2)≈1.1M pair blow-up never happens.
//  2. To prove the hard MaxGaps cap actually fires (and sets Truncated, no silent
//     cap), feed 1,500 size-2 clusters: C(1500,2) candidate pairs are bounded at
//     exactly MaxGaps with Truncated set. Both run effectively instantly.
func TestDetectGaps_DoSBounded(t *testing.T) {
	const n = 1500

	// Facet 1: all singletons, pipeline default → 0 gaps, fast, no OOM.
	singletons := corpus.NewCorpus()
	for i := 0; i < n; i++ {
		id := identity.DocumentID(itoaFixed(i) + ".md")
		if err := singletons.Add(&corpus.Document{ID: id, Root: &corpus.Section{Level: 0, StartLine: 1, EndLine: 1}}); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}
	sg := BuildReferenceGraph(singletons, nil, BuildOptions{})
	swcc := sg.WeaklyConnectedComponents()
	if len(swcc) != n {
		t.Fatalf("expected %d singleton WCCs, got %d", n, len(swcc))
	}
	if res := DetectGaps(swcc, GapOptions{MinComponentSize: 2}); len(res.Gaps) != 0 || res.Truncated {
		t.Errorf("MinComponentSize:2 over %d singletons = %d gaps (truncated=%v), want 0/false",
			n, len(res.Gaps), res.Truncated)
	}

	// Facet 2: n size-2 clusters → C(n,2) candidate pairs hard-capped at MaxGaps.
	pairs := corpus.NewCorpus()
	var refs []reference.Reference
	for i := 0; i < n; i++ {
		a := identity.DocumentID(itoaFixed(i) + "a.md")
		b := identity.DocumentID(itoaFixed(i) + "b.md")
		for _, id := range []identity.DocumentID{a, b} {
			if err := pairs.Add(&corpus.Document{ID: id, Root: &corpus.Section{Level: 0, StartLine: 1, EndLine: 1}}); err != nil {
				t.Fatalf("add %s: %v", id, err)
			}
		}
		refs = append(refs, validRef(a.String(), b.String()))
	}
	pg := BuildReferenceGraph(pairs, refs, BuildOptions{})
	pwcc := pg.WeaklyConnectedComponents()
	if len(pwcc) != n {
		t.Fatalf("expected %d size-2 WCCs, got %d", n, len(pwcc))
	}
	res := DetectGaps(pwcc, GapOptions{MinComponentSize: 2})
	if len(res.Gaps) != MaxGaps {
		t.Errorf("over %d size-2 clusters = %d gaps, want exactly MaxGaps=%d", n, len(res.Gaps), MaxGaps)
	}
	if !res.Truncated {
		t.Error("hitting MaxGaps must set Truncated (no silent cap)")
	}
}

// itoaFixed renders i as a zero-padded 6-digit string so DocumentIDs sort
// lexically the same as numerically (keeps the test's component ordering
// intuitive without importing strconv into a hot assertion).
func itoaFixed(i int) string {
	const width = 6
	buf := []byte("000000")
	for p := width - 1; p >= 0; p-- {
		buf[p] = byte('0' + i%10)
		i /= 10
	}
	return string(buf)
}
