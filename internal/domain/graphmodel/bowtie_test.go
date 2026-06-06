package graphmodel

import (
	"slices"
	"testing"

	"github.com/stacklok/matlatl/internal/domain/corpus"
	"github.com/stacklok/matlatl/internal/domain/identity"
	"github.com/stacklok/matlatl/internal/domain/reference"
)

func docID(s string) identity.DocumentID { return identity.DocumentID(s) }

// bowtieCorpus builds a classic bow-tie:
//
//	CORE (3-cycle):   core1 -> core2 -> core3 -> core1
//	IN  funnel:       in1 -> core1   (reaches core, not reached by it)
//	OUT terminal:     core3 -> out1  (reached by core, cannot reach it)
//	TENDRIL:          in1 -> tendril (same WCC as core; neither reaches nor
//	                  reached by core, since tendril has no path INTO core and
//	                  core has no path to it)
//	DISCONNECTED:     dis1 -> dis2   (separate WCC entirely)
func bowtieDocs() []*corpus.Document {
	return []*corpus.Document{
		doc("core1.md", "c1", nil),
		doc("core2.md", "c2", nil),
		doc("core3.md", "c3", nil),
		doc("in1.md", "i1", nil),
		doc("out1.md", "o1", nil),
		doc("tendril.md", "t1", nil),
		doc("dis1.md", "d1", nil),
		doc("dis2.md", "d2", nil),
	}
}

func bowtieRefs() []reference.Reference {
	return linkRefs(
		[2]string{"core1.md", "core2.md"},
		[2]string{"core2.md", "core3.md"},
		[2]string{"core3.md", "core1.md"},
		[2]string{"in1.md", "core1.md"},
		[2]string{"core3.md", "out1.md"},
		[2]string{"in1.md", "tendril.md"},
		[2]string{"dis1.md", "dis2.md"},
	)
}

func TestClassifyBowtie_Buckets(t *testing.T) {
	c := corpus.NewCorpus()
	for _, d := range bowtieDocs() {
		_ = c.Add(d)
	}
	g := BuildReferenceGraph(c, bowtieRefs(), BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{})
	r := m.Bowtie

	want := map[string]BowtieBucket{
		"core1.md":   BucketCore,
		"core2.md":   BucketCore,
		"core3.md":   BucketCore,
		"in1.md":     BucketIn,
		"out1.md":    BucketOut,
		"tendril.md": BucketTendril,
		"dis1.md":    BucketDisconnected,
		"dis2.md":    BucketDisconnected,
	}
	for id, wantBucket := range want {
		if got := r.BucketOf(docID(id)); got != wantBucket {
			t.Errorf("bucket(%s) = %s, want %s", id, got, wantBucket)
		}
	}
	if r.GiantSCCSize != 3 {
		t.Errorf("giant SCC size = %d, want 3", r.GiantSCCSize)
	}
	if r.GiantSCC.String() != "core1.md" {
		t.Errorf("giant SCC ID = %q, want core1.md", r.GiantSCC)
	}
	if r.Counts[BucketCore] != 3 || r.Counts[BucketIn] != 1 || r.Counts[BucketOut] != 1 ||
		r.Counts[BucketTendril] != 1 || r.Counts[BucketDisconnected] != 2 {
		t.Errorf("counts = %+v, want core3 in1 out1 tendril1 disconnected2", r.Counts)
	}
}

// TestClassifyBowtie_GiantTieBreak: two equal-size SCCs → the smallest sorted-min
// ID wins as the giant core.
func TestClassifyBowtie_GiantTieBreak(t *testing.T) {
	c := buildCorpus(t,
		doc("aaa.md", "a", nil),
		doc("aab.md", "b", nil),
		doc("zzy.md", "y", nil),
		doc("zzz.md", "z", nil),
	)
	// Two disjoint 2-cycles of equal size; aaa.md is the smallest ID.
	refs := linkRefs(
		[2]string{"aaa.md", "aab.md"},
		[2]string{"aab.md", "aaa.md"},
		[2]string{"zzy.md", "zzz.md"},
		[2]string{"zzz.md", "zzy.md"},
	)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{})
	if m.Bowtie.GiantSCC.String() != "aaa.md" {
		t.Errorf("tie-break giant SCC = %q, want aaa.md (smallest ID)", m.Bowtie.GiantSCC)
	}
	if m.Bowtie.GiantSCCSize != 2 {
		t.Errorf("giant SCC size = %d, want 2", m.Bowtie.GiantSCCSize)
	}
}

// TestClassifyBowtie_AllSingletons: an acyclic graph (every SCC a singleton) is
// the degenerate "no cyclic core" case. Buckets are still populated
// deterministically (giant size 1).
func TestClassifyBowtie_AllSingletons(t *testing.T) {
	c := buildCorpus(t,
		doc("a.md", "a", nil),
		doc("b.md", "b", nil),
		doc("c.md", "c", nil),
	)
	refs := linkRefs(
		[2]string{"a.md", "b.md"},
		[2]string{"b.md", "c.md"},
	)
	g := BuildReferenceGraph(c, refs, BuildOptions{})
	m := Analyze(g, c, AnalyzeOptions{})
	r := m.Bowtie
	if r.GiantSCCSize != 1 {
		t.Errorf("all-singleton giant SCC size = %d, want 1 (no cyclic core)", r.GiantSCCSize)
	}
	// Every doc gets a deterministic bucket (no panics, full coverage).
	for _, id := range g.Documents() {
		if _, ok := r.Bucket[id]; !ok {
			t.Errorf("doc %s missing a bucket assignment", id)
		}
	}
	// The giant "core" is the singleton a.md (smallest ID among max-size SCCs).
	if r.GiantSCC.String() != "a.md" {
		t.Errorf("giant SCC = %q, want a.md", r.GiantSCC)
	}
	if r.BucketOf(docID("a.md")) != BucketCore {
		t.Errorf("a.md should be the (singleton) core")
	}
}

// TestClassifyBowtie_Empty: an empty corpus yields an empty, valid report.
func TestClassifyBowtie_Empty(t *testing.T) {
	c := corpus.NewCorpus()
	g := BuildReferenceGraph(c, nil, BuildOptions{})
	r := g.ClassifyBowtie(nil, nil)
	if len(r.Bucket) != 0 || r.GiantSCC != "" || r.GiantSCCSize != 0 {
		t.Errorf("empty corpus should yield empty bowtie report, got %+v", r)
	}
	// BucketOf on a nil report is safe.
	if (BowtieReport{}).BucketOf("x.md") != BucketDisconnected {
		t.Error("BucketOf on zero report should be disconnected")
	}
}

// TestClassifyBowtie_Determinism: shuffling document + edge insertion order must
// not change the bow-tie report (mirrors the components determinism test).
func TestClassifyBowtie_Determinism(t *testing.T) {
	build := func(order []int) BowtieReport {
		all := bowtieDocs()
		c := corpus.NewCorpus()
		for _, i := range order {
			_ = c.Add(all[i])
		}
		refs := bowtieRefs()
		slices.Reverse(refs)
		g := BuildReferenceGraph(c, refs, BuildOptions{})
		return Analyze(g, c, AnalyzeOptions{}).Bowtie
	}

	r1 := build([]int{0, 1, 2, 3, 4, 5, 6, 7})
	r2 := build([]int{7, 6, 5, 4, 3, 2, 1, 0})
	r3 := build([]int{3, 0, 7, 1, 5, 2, 6, 4})

	for _, r := range []BowtieReport{r2, r3} {
		if r.GiantSCC != r1.GiantSCC || r.GiantSCCSize != r1.GiantSCCSize {
			t.Errorf("giant SCC differs across order: %q/%d vs %q/%d",
				r1.GiantSCC, r1.GiantSCCSize, r.GiantSCC, r.GiantSCCSize)
		}
		for id, b := range r1.Bucket {
			if r.Bucket[id] != b {
				t.Errorf("bucket(%s) differs across order: %s vs %s", id, b, r.Bucket[id])
			}
		}
	}
}
