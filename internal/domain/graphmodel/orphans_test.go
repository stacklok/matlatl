package graphmodel

import (
	"slices"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/reference"
)

// chainRefs builds a -> b for each adjacent pair in the slice.
func linkRefs(pairs ...[2]string) []reference.Reference {
	out := make([]reference.Reference, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, validRef(p[0], p[1]))
	}
	return out
}

// TestStructureLadder_Tiers exercises the single-bucket ladder: isolated,
// under-linked (at in==0 and in==2 with threshold 3), healthy (in==3), and
// dead-end (out==0, in>0). Each non-exempt doc lands in AT MOST ONE bucket.
func TestStructureLadder_Tiers(t *testing.T) {
	// Graph (threshold 3):
	//   README.md (root) -> a.md, b.md, c.md, hub.md, dead.md
	//   a.md, b.md, c.md  -> healthy.md          (healthy.md in=3 → none)
	//   hub.md            -> under2.md            (under2.md in=1 → under-linked)
	//   a.md, b.md        -> under2b.md           (under2b.md in=2 → under-linked)
	//   dead.md                                   (in=1, out=0 → dead-end)
	//   lonely.md                                 (in=0, out=0 → isolated)
	//   underout.md       -> README.md            (in=0, out=1 → under-linked)
	c := buildCorpus(t,
		doc("README.md", "r", nil),
		doc("a.md", "a", nil),
		doc("b.md", "b", nil),
		doc("c.md", "c", nil),
		doc("hub.md", "h", nil),
		doc("healthy.md", "he", nil),
		doc("under2.md", "u2", nil),
		doc("under2b.md", "u2b", nil),
		doc("dead.md", "d", nil),
		doc("lonely.md", "l", nil),
		doc("underout.md", "uo", nil),
		doc("sink.md", "s", nil),
	)
	// Every non-terminal doc links onward to sink.md so out>0 (keeps them off the
	// dead-end tier); healthy.md/under2.md/under2b.md likewise link onward.
	refs := linkRefs(
		[2]string{"README.md", "a.md"},
		[2]string{"README.md", "b.md"},
		[2]string{"README.md", "c.md"},
		[2]string{"README.md", "hub.md"},
		[2]string{"README.md", "dead.md"},
		[2]string{"a.md", "healthy.md"},
		[2]string{"b.md", "healthy.md"},
		[2]string{"c.md", "healthy.md"},
		[2]string{"hub.md", "under2.md"},
		[2]string{"a.md", "under2b.md"},
		[2]string{"b.md", "under2b.md"},
		[2]string{"underout.md", "README.md"},
		// onward links so these have out>0 (not dead-ends).
		[2]string{"healthy.md", "sink.md"},
		[2]string{"under2.md", "sink.md"},
		[2]string{"under2b.md", "sink.md"},
	)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{InboundThreshold: 3})
	o := m.Orphans

	wantIsolated := []string{"lonely.md"}
	if got := ids(o.Isolated); !slices.Equal(got, wantIsolated) {
		t.Errorf("isolated = %v, want %v", got, wantIsolated)
	}
	// dead.md (in=1,out=0) and sink.md (in=3,out=0) are dead-ends.
	wantDeadEnd := []string{"dead.md", "sink.md"}
	if got := ids(o.DeadEnd); !slices.Equal(got, wantDeadEnd) {
		t.Errorf("dead-end = %v, want %v", got, wantDeadEnd)
	}
	// under2 (in=1), under2b (in=2), underout (in=0,out=1). README is a root
	// (exempt). a/b/c have in=1 and out=2 → under-linked too. hub in=1.
	wantUnder := []string{"a.md", "b.md", "c.md", "hub.md", "under2.md", "under2b.md", "underout.md"}
	if got := ids(o.UnderLinked); !slices.Equal(got, wantUnder) {
		t.Errorf("under-linked = %v, want %v", got, wantUnder)
	}
	// healthy.md has in==3 → no under-linked finding.
	if slices.Contains(ids(o.UnderLinked), "healthy.md") {
		t.Error("healthy.md (in=3) must not be under-linked")
	}

	// Single-bucket invariant: no doc appears in more than one tier.
	assertSingleBucket(t, o)
}

func assertSingleBucket(t *testing.T, o OrphanReport) {
	t.Helper()
	seen := map[string]string{}
	for _, id := range ids(o.Isolated) {
		seen[id] = "isolated"
	}
	for _, id := range ids(o.DeadEnd) {
		if prev, ok := seen[id]; ok {
			t.Errorf("%s in two buckets: %s and dead-end", id, prev)
		}
		seen[id] = "dead-end"
	}
	for _, id := range ids(o.UnderLinked) {
		if prev, ok := seen[id]; ok {
			t.Errorf("%s in two buckets: %s and under-linked", id, prev)
		}
		seen[id] = "under-linked"
	}
}

// TestStructureLadder_HealthyAtThreshold: a doc with exactly threshold inbound
// links is healthy (not under-linked).
func TestStructureLadder_HealthyAtThreshold(t *testing.T) {
	c := buildCorpus(t,
		doc("README.md", "r", nil),
		doc("x.md", "x", nil),
		doc("y.md", "y", nil),
		doc("target.md", "t", nil),
	)
	// target.md gets exactly 3 inbound; it also links onward so it is not a
	// dead-end.
	refs := linkRefs(
		[2]string{"README.md", "target.md"},
		[2]string{"x.md", "target.md"},
		[2]string{"y.md", "target.md"},
		[2]string{"target.md", "README.md"},
	)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{InboundThreshold: 3})
	if slices.Contains(ids(m.Orphans.UnderLinked), "target.md") {
		t.Errorf("target.md with in==threshold(3) must be healthy, under-linked=%v", ids(m.Orphans.UnderLinked))
	}
}

// TestThresholdNormalization: a <=0 threshold floors to DefaultInboundThreshold.
func TestThresholdNormalization(t *testing.T) {
	c := buildCorpus(t,
		doc("README.md", "r", nil),
		doc("a.md", "a", nil),
		doc("loop.md", "l", nil),
	)
	// loop.md: in=1 (from README), out=1 (to a.md) → under-linked under default 3.
	refs := linkRefs(
		[2]string{"README.md", "loop.md"},
		[2]string{"loop.md", "a.md"},
	)
	g := BuildReferenceGraph(c, refs, BuildOptions{})

	for _, thr := range []int{0, -5} {
		m := Analyze(g, c, AnalyzeOptions{InboundThreshold: thr})
		if !slices.Contains(ids(m.Orphans.UnderLinked), "loop.md") {
			t.Errorf("threshold %d should normalize to %d; loop.md (in=1) should be under-linked, got %v",
				thr, DefaultInboundThreshold, ids(m.Orphans.UnderLinked))
		}
	}
}

// TestExemptions_AcrossTiers: intentional orphans and root-set members are
// exempt from ALL structure tiers (orphan/under-linked/dead-end).
func TestExemptions_AcrossTiers(t *testing.T) {
	c := buildCorpus(t,
		doc("README.md", "r", nil),
		// intentional + would otherwise be under-linked (in=1, out=1).
		doc("intentional.md", "i", map[string]any{"matlatl": "orphan-intentional"}),
		// intentional + would otherwise be a dead-end (in=1, out=0).
		doc("intentional-dead.md", "id", map[string]any{"matlatl": "orphan-intentional"}),
		doc("a.md", "a", nil),
	)
	refs := linkRefs(
		[2]string{"README.md", "intentional.md"},
		[2]string{"intentional.md", "a.md"},
		[2]string{"README.md", "intentional-dead.md"},
	)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{InboundThreshold: 3})

	if slices.Contains(ids(m.Orphans.UnderLinked), "intentional.md") {
		t.Error("intentional.md must be exempt from under-linked")
	}
	if slices.Contains(ids(m.Orphans.DeadEnd), "intentional-dead.md") {
		t.Error("intentional-dead.md must be exempt from dead-end")
	}
}

// TestRootExempt_AcrossTiers: a configured/convention root with few inbound
// links is exempt (an entry point is its purpose, not a defect).
func TestRootExempt_AcrossTiers(t *testing.T) {
	c := buildCorpus(t,
		doc("docs/index.md", "i", nil), // convention root
		doc("a.md", "a", nil),
	)
	// index.md: in=0, out=1 → would be under-linked, but it is a root → exempt.
	refs := linkRefs([2]string{"docs/index.md", "a.md"})
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{InboundThreshold: 3})
	if slices.Contains(ids(m.Orphans.UnderLinked), "docs/index.md") {
		t.Error("root docs/index.md must be exempt from under-linked")
	}
}

// TestUnreachable_NotSuppressedByTiers: dead-end / under-linked do NOT suppress
// unreachable; only a fully-isolated orphan does (ADR 0012).
func TestUnreachable_NotSuppressedByTiers(t *testing.T) {
	c := buildCorpus(t,
		doc("README.md", "r", nil),
		// island: deadcycle.md <-> deadcycle2.md, neither reachable from README.
		doc("island.md", "is", nil),
		doc("island2.md", "is2", nil),
	)
	// island.md -> island2.md (out=1, in=1 via the other), island2.md -> island.md.
	// Both are unreachable from README and both have out>0 (so not isolated).
	refs := linkRefs(
		[2]string{"island.md", "island2.md"},
		[2]string{"island2.md", "island.md"},
	)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{InboundThreshold: 3})

	// They are unreachable (README has no path to them) and NOT isolated.
	if !slices.Contains(ids(m.Orphans.Unreachable), "island.md") {
		t.Errorf("island.md should be unreachable; got %v", ids(m.Orphans.Unreachable))
	}
	if slices.Contains(ids(m.Orphans.Isolated), "island.md") {
		t.Error("island.md has edges; must not be isolated")
	}
	// And under-linked classification is orthogonal (island.md in=1<3).
	if !slices.Contains(ids(m.Orphans.UnderLinked), "island.md") {
		t.Errorf("island.md (in=1) should also be under-linked; got %v", ids(m.Orphans.UnderLinked))
	}
}

// TestIsolatedSuppressesUnreachable: a fully-isolated orphan is NOT also listed
// as unreachable (the more-specific tier wins).
func TestIsolatedSuppressesUnreachable(t *testing.T) {
	c := buildCorpus(t,
		doc("README.md", "r", nil),
		doc("lonely.md", "l", nil),
	)
	g := BuildReferenceGraph(c, nil, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{InboundThreshold: 3})
	if !slices.Contains(ids(m.Orphans.Isolated), "lonely.md") {
		t.Fatalf("lonely.md should be isolated; got %v", ids(m.Orphans.Isolated))
	}
	if slices.Contains(ids(m.Orphans.Unreachable), "lonely.md") {
		t.Error("isolated lonely.md must not also be reported unreachable")
	}
}
